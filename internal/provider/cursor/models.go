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
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/translate"
)

type Model struct {
	ID            string
	Name          string
	ContextWindow int
	MaxTokens     int
	VariantIDs    map[string]variantPair
}

func PublicID(wireID string) string {
	if wireID == "auto" || wireID == "default" {
		return "cursor-auto"
	}
	if isGrokID(wireID) && !strings.HasPrefix(wireID, "cursor-") {
		return "cursor-" + wireID
	}
	return wireID
}

func WireID(public string) string {
	switch public {
	case "cursor-auto", "auto", "default":
		return "default"
	}
	if strings.HasPrefix(public, "cursor-grok-") {
		return strings.TrimPrefix(public, "cursor-")
	}
	return public
}

func grokBare(id string) string {
	return strings.TrimPrefix(id, "cursor-")
}

func isGrokID(id string) bool {
	return strings.HasPrefix(grokBare(id), "grok-")
}

func grokIDForms(id string) []string {
	if !isGrokID(id) {
		return []string{id}
	}
	bare := grokBare(id)
	return []string{id, bare, "cursor-" + bare}
}

func StripPublicPrefix(id string) string {
	return strings.TrimPrefix(id, "cursor/")
}

// Longer tokens first so extra-high wins over high.
var effortSuffixes = []string{"-extra-high", "-xhigh", "-medium", "-minimal", "-high", "-low", "-max", "-none"}

func publicFamilyID(wireID string) string {
	id := strings.TrimSuffix(wireID, "-fast")
	thinking := strings.Contains(id, "-thinking")
	core := strings.ReplaceAll(id, "-thinking", "")
	for _, s := range effortSuffixes {
		if strings.HasSuffix(core, s) {
			core = strings.TrimSuffix(core, s)
			break
		}
	}
	if thinking {
		return core + "-thinking"
	}
	return core
}

func CollapseForList(models []Model) []Model {
	type group struct {
		rep  Model
		rank int
	}
	groups := map[string]*group{}
	order := make([]string, 0, len(models))
	for _, m := range models {
		key := publicFamilyID(m.ID)
		if key == "" {
			continue
		}
		rank := variantListRank(m)
		if g, ok := groups[key]; ok {
			if rank < g.rank {
				g.rep = Model{ID: key, Name: m.Name, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens, VariantIDs: m.VariantIDs}
				g.rank = rank
			}
			continue
		}
		order = append(order, key)
		cp := m
		cp.ID = key
		groups[key] = &group{rep: cp, rank: rank}
	}
	out := make([]Model, 0, len(order))
	for _, key := range order {
		out = append(out, groups[key].rep)
	}
	return out
}

func variantListRank(m Model) int {
	score := 0
	id := m.ID
	if strings.HasSuffix(id, "-fast") {
		score += 1000
		id = strings.TrimSuffix(id, "-fast")
	}
	name := strings.ToLower(m.Name)
	if containsWord(name, "fast") {
		score += 1000
	}
	if strings.Contains(name, "extra high") || containsWord(name, "minimal") || containsWord(name, "medium") || containsWord(name, "high") || containsWord(name, "low") || containsWord(name, "max") || containsWord(name, "none") {
		score += 10
	}
	core := strings.ReplaceAll(id, "-thinking", "")
	switch {
	case strings.HasSuffix(core, "-medium"):
		score += 1
	case strings.HasSuffix(core, "-extra-high"), strings.HasSuffix(core, "-xhigh"):
		score += 3
	case strings.HasSuffix(core, "-high"):
		score += 2
	case strings.HasSuffix(core, "-max"):
		score += 4
	case strings.HasSuffix(core, "-low"):
		score += 5
	case strings.HasSuffix(core, "-none"), strings.HasSuffix(core, "-minimal"):
		score += 6
	}
	return score
}

func RestoreWirePrefix(id string, known []string) string {
	if isGrokID(id) {
		if strings.HasPrefix(id, "cursor-") {
			return id
		}
		prefixed := "cursor-" + grokBare(id)
		for _, k := range known {
			if k == prefixed {
				return prefixed
			}
		}
		return grokBare(id)
	}
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

func grokFamily(id string) string {
	return stripEffort(grokBare(id))
}

func grokAliasKey(id string) string {
	if !isGrokID(id) {
		return ""
	}
	bare := grokBare(id)
	if strings.HasPrefix(id, "cursor-") {
		return bare
	}
	return "cursor-" + bare
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
		ctx := jsonInt(firstNonNil(rec["contextWindow"], rec["context_window"]))
		maxTok := jsonInt(firstNonNil(rec["maxTokens"], rec["max_tokens"]))
		if len(rawVariants) == 0 {
			byID[baseID] = Model{ID: baseID, Name: baseDisplay, ContextWindow: ctx, MaxTokens: maxTok, VariantIDs: variantIDs}
			continue
		}
		for _, variant := range rawVariants {
			v := translate.AsMap(variant)
			params := readParams(firstNonNil(v["parameterValues"], v["parameter_values"]))
			id := WireIDFromVariant(baseID, params)
			byID[id] = Model{ID: id, Name: displayNameForVariant(baseDisplay, params), ContextWindow: ctx, MaxTokens: maxTok, VariantIDs: variantIDs}
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
		ID            string `json:"id"`
		Name          string `json:"name"`
		ContextWindow int    `json:"contextWindow"`
		MaxTokens     int    `json:"maxTokens"`
	}
	_ = json.Unmarshal(modelsRawJSON, &raw)
	for _, m := range raw {
		snapshot = append(snapshot, Model{ID: m.ID, Name: m.Name, ContextWindow: m.ContextWindow, MaxTokens: m.MaxTokens})
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

func variantFamilyKey(id string) string {
	if family := publicFamilyID(id); family != "" {
		return family
	}
	return strings.TrimSuffix(id, "-fast")
}

func keepIfThinking(id string, want bool) string {
	if id == "" || strings.Contains(id, "-thinking") != want {
		return ""
	}
	return id
}

func filterPairsForFamily(family string, pairs map[string]variantPair) map[string]variantPair {
	want := strings.Contains(family, "-thinking")
	out := map[string]variantPair{}
	for effort, pair := range pairs {
		filtered := variantPair{standard: keepIfThinking(pair.standard, want), fast: keepIfThinking(pair.fast, want)}
		if filtered.standard != "" || filtered.fast != "" {
			out[effort] = filtered
		}
	}
	return out
}

func indexVariants(models []Model) {
	for _, m := range models {
		family := variantFamilyKey(m.ID)
		if len(m.VariantIDs) > 0 {
			mergeVariantPairs(family, filterPairsForFamily(family, m.VariantIDs))
			continue
		}
		effort := effortFromID(m.ID)
		pair := variantPair{}
		if strings.HasSuffix(m.ID, "-fast") {
			pair.fast = m.ID
		} else {
			pair.standard = m.ID
		}
		mergeVariantPairs(family, map[string]variantPair{effort: pair})
	}
}

func mergeVariantPairs(key string, pairs map[string]variantPair) {
	for _, dest := range grokIDForms(key) {
		if variantIndex[dest] == nil {
			variantIndex[dest] = map[string]variantPair{}
		}
		for effort, pair := range pairs {
			existing := variantIndex[dest][effort]
			if pair.standard != "" {
				existing.standard = pair.standard
			}
			if pair.fast != "" {
				existing.fast = pair.fast
			}
			variantIndex[dest][effort] = existing
		}
	}
}

func lookupVariants(base string) map[string]variantPair {
	seen := map[string]bool{}
	keys := []string{base, publicFamilyID(base)}
	if alias := grokAliasKey(base); alias != "" {
		keys = append(keys, alias, publicFamilyID(alias))
	}
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if v, ok := variantIndex[key]; ok {
			return v
		}
	}
	return nil
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

func expandFastTwins(families, source []Model) []Model {
	out := make([]Model, 0, len(families)*2)
	for _, m := range families {
		out = append(out, m)
		if !familyHasFast(source, m.ID) {
			continue
		}
		fast := m
		fast.ID = m.ID + "-fast"
		if !containsWord(strings.ToLower(m.Name), "fast") {
			fast.Name = strings.TrimSpace(m.Name) + " (fast)"
		}
		out = append(out, fast)
	}
	return out
}

func familyHasFast(models []Model, family string) bool {
	for _, m := range models {
		if publicFamilyID(m.ID) != family && m.ID != family {
			continue
		}
		if strings.HasSuffix(m.ID, "-fast") {
			return true
		}
		for _, pair := range m.VariantIDs {
			if pair.fast != "" {
				return true
			}
		}
	}
	return false
}

func ToModelInfo(models []Model, prefix bool) []domain.ModelInfo {
	source := models
	models = expandFastTwins(CollapseForList(models), source)
	out := make([]domain.ModelInfo, len(models))
	for i, m := range models {
		id := PublicID(m.ID)
		if prefix {
			id = "cursor/" + id
		}
		info := domain.ModelInfo{
			ID: id, Object: "model", OwnedBy: "cursor",
			Name: m.Name, DisplayName: m.Name,
			ContextLength: m.ContextWindow, MaxOutputTokens: m.MaxTokens,
		}
		limitID := strings.TrimSuffix(id, "-fast")
		if ctx, maxOut, cost, input, ok := provider.Limits(limitID); ok {
			if ctx > 0 {
				info.ContextLength = ctx
			}
			if maxOut > 0 {
				info.MaxOutputTokens = maxOut
			}
			info.Cost = cost
			info.Input = input
			info.InputModalities = input
		} else {
			info.Input = []string{"text", "image"}
			info.InputModalities = info.Input
		}
		if info.ContextLength == 0 {
			info.ContextLength = 200000
		}
		if info.MaxOutputTokens == 0 {
			info.MaxOutputTokens = 64000
		}
		info.Efforts = familyEffortsFrom(source, publicFamilyID(m.ID))
		thinking := strings.Contains(m.ID, "-thinking") || strings.Contains(strings.ToLower(m.ID), "reasoning")
		info.Reasoning = thinking || len(info.Efforts) > 0
		out[i] = info
	}
	return out
}

func familyEffortsFrom(models []Model, family string) []string {
	has := map[string]bool{}
	for _, m := range models {
		if publicFamilyID(m.ID) != family {
			continue
		}
		e := canonicalEffort(effortFromID(m.ID))
		if e != "" {
			has[e] = true
		}
		for key := range m.VariantIDs {
			e := canonicalEffort(key)
			if e == "" {
				continue
			}
			has[e] = true
		}
	}
	var out []string
	for _, e := range []string{"none", "low", "medium", "high", "xhigh", "max"} {
		if has[e] {
			out = append(out, e)
		}
	}
	return out
}

func effortFromID(id string) string {
	id = strings.TrimSuffix(id, "-fast")
	core := strings.ReplaceAll(id, "-thinking", "")
	for _, s := range effortSuffixes {
		if strings.HasSuffix(core, s) {
			return strings.TrimPrefix(s, "-")
		}
	}
	return ""
}

func canonicalEffort(effort string) string {
	switch effort {
	case "extra-high":
		return "xhigh"
	case "minimal":
		return "none"
	default:
		return effort
	}
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
	if isGrokID(id) {
		family := grokFamily(id)
		thinking := strings.Contains(id, "-thinking")
		for _, form := range grokIDForms(family) {
			if hit := preferredWire(form, thinking, known); hit != "" {
				return hit
			}
		}
	}
	thinking := strings.Contains(id, "-thinking")
	family := stripEffort(id)
	if hit := preferredWire(family, thinking, known); hit != "" {
		return hit
	}
	return ""
}

func preferredWire(family string, thinking bool, known []string) string {
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	efforts := []string{"medium", "high", "xhigh", "max", "low", "none", "extra-high", "minimal"}
	var candidates []string
	if thinking {
		for _, e := range efforts {
			candidates = append(candidates, family+"-thinking-"+e, family+"-"+e+"-thinking")
		}
		candidates = append(candidates, family+"-thinking")
	} else if set[family] {
		return family
	} else {
		for _, e := range efforts {
			candidates = append(candidates, family+"-"+e)
		}
	}
	for _, c := range candidates {
		if set[c] {
			return c
		}
	}
	for _, k := range known {
		if !sameFamily(k, family) {
			continue
		}
		hasThinking := strings.Contains(k, "-thinking")
		if hasThinking == thinking {
			return k
		}
	}
	return ""
}

func sameFamily(id, family string) bool {
	if id == family {
		return true
	}
	return strings.HasPrefix(id, family+"-")
}

func MapWireToNative(id string) (domain.ProviderID, string, bool) {
	stripped := strings.TrimPrefix(id, "cursor-")
	if strings.HasPrefix(stripped, "grok-") || strings.HasPrefix(id, "cursor-grok-") {
		return domain.ProviderGrok, stripEffort(stripped), true
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
	for _, s := range effortSuffixes {
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

func jsonInt(v any) int {
	n, ok := translate.AsNum(v)
	if !ok || n <= 0 {
		return 0
	}
	return int(n)
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
