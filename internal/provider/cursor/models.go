package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/translate"
)

type Model struct {
	ID         string
	Name       string
	VariantIDs map[string]variantPair
}

func PublicID(wireID string) string {
	if wireID == "auto" || wireID == "default" {
		return "cursor-auto"
	}
	return wireID
}

func WireID(public string) string {
	switch public {
	case "cursor-auto", "auto", "default":
		return "default"
	default:
		return public
	}
}

func StripPublicPrefix(id string) string {
	return strings.TrimPrefix(id, "cursor/")
}

func RestoreWirePrefix(id string, known []string) string {
	if strings.HasPrefix(id, "cursor-") || strings.HasPrefix(id, "composer-") {
		return id
	}
	prefixed := "cursor-" + id
	for _, k := range known {
		if k == prefixed {
			return prefixed
		}
	}
	return id
}

func ExpandAvailableModels(body any) []Model {
	var entries []any
	if arr, ok := body.([]any); ok {
		entries = arr
	} else {
		entries = translate.AsArr(translate.AsMap(body)["models"])
	}
	byID := map[string]Model{}
	for _, entry := range entries {
		rec := translate.AsMap(entry)
		baseID := strings.TrimSpace(translate.AsStr(rec["name"]))
		if baseID == "" {
			baseID = strings.TrimSpace(translate.AsStr(rec["id"]))
		}
		if baseID == "" {
			continue
		}
		baseDisplay := translate.AsStr(rec["clientDisplayName"])
		if baseDisplay == "" {
			baseDisplay = translate.AsStr(rec["displayName"])
		}
		if baseDisplay == "" {
			baseDisplay = translate.AsStr(rec["name"])
		}
		if baseDisplay == "" {
			baseDisplay = baseID
		}
		rawVariants := translate.AsArr(rec["variants"])
		variantIDs := readVariantIDs(rec["variantIds"], rec["variant_ids"])
		if len(rawVariants) == 0 {
			byID[baseID] = Model{ID: baseID, Name: baseDisplay, VariantIDs: variantIDs}
			continue
		}
		for _, variant := range rawVariants {
			v := translate.AsMap(variant)
			params := readParams(firstNonNil(v["parameterValues"], v["parameter_values"]))
			id := WireIDFromVariant(baseID, params)
			byID[id] = Model{ID: id, Name: displayNameForVariant(baseDisplay, params), VariantIDs: variantIDs}
		}
	}
	out := make([]Model, 0, len(byID))
	for _, m := range byID {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

type param struct{ ID, Value string }

func WireIDFromVariant(baseName string, params []param) string {
	var effort string
	fast, thinking := false, false
	for _, p := range params {
		if p.ID == "effort" || p.ID == "reasoning" {
			effort = p.Value
		}
		if p.ID == "fast" && p.Value == "true" {
			fast = true
		}
		if p.ID == "thinking" && p.Value == "true" {
			thinking = true
		}
	}
	id := baseName
	if effort != "" {
		id = baseName + "-" + effort
	}
	if thinking {
		id += "-thinking"
	}
	if fast {
		id += "-fast"
	}
	return id
}

func displayNameForVariant(baseDisplay string, params []param) string {
	fast, thinking := false, false
	for _, p := range params {
		if p.ID == "fast" && p.Value == "true" {
			fast = true
		}
		if p.ID == "thinking" && p.Value == "true" {
			thinking = true
		}
	}
	name := baseDisplay
	if thinking && !strings.Contains(strings.ToLower(name), "thinking") {
		name += " Thinking"
	}
	if fast && !containsWord(strings.ToLower(name), "fast") {
		name += " (fast)"
	}
	return name
}

func readParams(value any) []param {
	var out []param
	for _, entry := range translate.AsArr(value) {
		rec := translate.AsMap(entry)
		id, _ := rec["id"].(string)
		val, _ := rec["value"].(string)
		if id == "" || val == "" {
			continue
		}
		out = append(out, param{ID: id, Value: val})
	}
	return out
}

type variantPair struct{ standard, fast string }

var (
	snapshot     []Model
	liveIDs      []string
	variantIndex = map[string]map[string]variantPair{}
)

func init() {
	var raw []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(modelsRawJSON, &raw)
	for _, m := range raw {
		snapshot = append(snapshot, Model{ID: m.ID, Name: m.Name})
	}
	indexVariants(snapshot)
}

func readVariantIDs(raw ...any) map[string]variantPair {
	var src any
	for _, v := range raw {
		if v != nil {
			src = v
			break
		}
	}
	m := translate.AsMap(src)
	if len(m) == 0 {
		return nil
	}
	out := map[string]variantPair{}
	for effort, rec := range m {
		pairMap := translate.AsMap(rec)
		out[effort] = variantPair{
			standard: translate.AsStr(pairMap["standard"]),
			fast:     translate.AsStr(pairMap["fast"]),
		}
	}
	return out
}

func indexVariants(models []Model) {
	for _, m := range models {
		if len(m.VariantIDs) > 0 {
			key := m.ID
			if strings.HasSuffix(key, "-fast") {
				key = strings.TrimSuffix(key, "-fast")
			}
			if variantIndex[key] == nil {
				variantIndex[key] = map[string]variantPair{}
			}
			for effort, pair := range m.VariantIDs {
				existing := variantIndex[key][effort]
				if pair.standard != "" {
					existing.standard = pair.standard
				}
				if pair.fast != "" {
					existing.fast = pair.fast
				}
				variantIndex[key][effort] = existing
			}
			continue
		}
		base := m.ID
		fast := strings.HasSuffix(base, "-fast")
		if fast {
			base = strings.TrimSuffix(base, "-fast")
		}
		effort := ""
		for _, s := range []string{"-high", "-medium", "-low", "-xhigh", "-max", "-none"} {
			if strings.HasSuffix(strings.TrimSuffix(base, "-thinking"), s) || strings.HasSuffix(base, s) {
				effort = strings.TrimPrefix(s, "-")
				break
			}
		}
		key := base
		if effort != "" {
			key = strings.TrimSuffix(strings.TrimSuffix(base, "-thinking"), "-"+effort)
			key = strings.TrimSuffix(key, "-thinking")
		}
		if variantIndex[key] == nil {
			variantIndex[key] = map[string]variantPair{}
		}
		pair := variantIndex[key][effort]
		if fast {
			pair.fast = m.ID
		} else {
			pair.standard = m.ID
		}
		variantIndex[key][effort] = pair
	}
}

func lookupVariants(base string) map[string]variantPair {
	if v, ok := variantIndex[base]; ok {
		return v
	}
	trimmed := strings.TrimSuffix(base, "-fast")
	trimmed = strings.TrimSuffix(trimmed, "-thinking")
	return variantIndex[trimmed]
}

func RegisterModelVariants(models []Model) {
	indexVariants(models)
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	liveIDs = ids
}

func Snapshot() []Model { return snapshot }

func ToModelInfo(models []Model, prefix bool) []domain.ModelInfo {
	out := make([]domain.ModelInfo, len(models))
	for i, m := range models {
		id := PublicID(m.ID)
		if prefix {
			id = "cursor/" + id
		}
		out[i] = domain.ModelInfo{ID: id, Object: "model", OwnedBy: "cursor", DisplayName: m.Name}
	}
	return out
}

func FetchAvailable(ctx context.Context, accessToken string) ([]Model, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]any{
		"includeLongContextModels": true, "useModelParameters": true, "useCloudAgentEffortModes": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBaseURL+AvailableModels, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range ClientHeaders(ClientCLI) {
		req.Header.Set(k, v)
	}
	res, err := httpx.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("cursor_available_models_failed:%d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	var body any
	_ = json.Unmarshal(raw, &body)
	expanded := ExpandAvailableModels(body)
	if len(expanded) == 0 {
		return nil, fmt.Errorf("cursor_available_models_empty")
	}
	return expanded, nil
}

func MapNativeToWire(id string, known []string) string {
	for _, k := range known {
		if k == id {
			return id
		}
	}
	restored := RestoreWirePrefix(id, known)
	for _, k := range known {
		if k == restored {
			return restored
		}
	}
	if strings.HasPrefix(id, "grok-4.6") || strings.HasPrefix(id, "grok-4.5") {
		candidate := "cursor-grok-4.5-medium"
		for _, k := range known {
			if k == candidate {
				return candidate
			}
		}
		for _, k := range known {
			if strings.HasPrefix(k, "cursor-grok-4.5") {
				return k
			}
		}
	}
	family := stripEffort(id)
	medium := family + "-medium"
	for _, k := range known {
		if k == medium {
			return medium
		}
	}
	for _, k := range known {
		if k == family || strings.HasPrefix(k, family+"-") {
			return k
		}
	}
	return ""
}

func MapWireToNative(id string) (domain.ProviderID, string, bool) {
	stripped := strings.TrimPrefix(id, "cursor-")
	if strings.HasPrefix(stripped, "grok-") || strings.HasPrefix(id, "cursor-grok-") {
		model := "grok-4.5"
		if strings.HasPrefix(stripped, "grok-") {
			model = stripEffort(stripped)
		}
		return domain.ProviderGrok, model, true
	}
	if strings.HasPrefix(stripped, "claude-") {
		return domain.ProviderClaude, stripEffort(stripped), true
	}
	if strings.HasPrefix(stripped, "gpt-") || strings.HasPrefix(stripped, "o1") || strings.HasPrefix(stripped, "o3") || strings.HasPrefix(stripped, "o4") {
		return domain.ProviderCodex, stripEffort(stripped), true
	}
	return "", "", false
}

func stripEffort(id string) string {
	id = strings.ReplaceAll(id, "-thinking", "")
	id = strings.TrimSuffix(id, "-fast")
	for _, s := range []string{"-high", "-medium", "-low", "-xhigh", "-max", "-none"} {
		id = strings.TrimSuffix(id, s)
	}
	return id
}

func containsWord(s, w string) bool {
	return strings.Contains(" "+s+" ", " "+w+" ")
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func KnownIDs() []string {
	if len(liveIDs) > 0 {
		return append([]string{}, liveIDs...)
	}
	out := make([]string, len(snapshot))
	for i, m := range snapshot {
		out[i] = m.ID
	}
	return out
}

func LookupWireID(nameOrID string) string {
	key := strings.TrimSpace(nameOrID)
	if key == "" {
		return ""
	}
	for _, m := range snapshot {
		if m.ID == key || strings.EqualFold(m.ID, key) || strings.EqualFold(m.Name, key) {
			return m.ID
		}
	}
	return ""
}

func pricedRoutedID(nameOrID string) string {
	id := LookupWireID(nameOrID)
	if id == "" {
		id = strings.TrimSpace(nameOrID)
	}
	if id == "" || WireID(id) == "default" {
		return ""
	}
	return id
}
