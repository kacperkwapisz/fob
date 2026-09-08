package provider

import (
	"encoding/json"
	"strings"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/prices"
)

var providerCatalog = map[domain.ProviderID]string{
	domain.ProviderClaude: "anthropic",
	domain.ProviderCodex:  "openai",
	domain.ProviderGrok:   "xai",
	domain.ProviderCursor: "cursor",
	domain.ProviderOpenAI: "openai",
}

type catalogLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type catalogModel struct {
	ID    string                `json:"id"`
	Name  string                `json:"name"`
	Limit *catalogLimit         `json:"limit"`
	Cost  *domain.ModelCostJSON `json:"cost"`
}

var (
	claudeEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	codexEfforts  = []string{"none", "low", "medium", "high", "xhigh"}
	grokEfforts   = []string{"low", "medium", "high", "xhigh"}
)

func CatalogModels(provider domain.ProviderID) []domain.ModelInfo {
	rows, ok := catalogPack(providerCatalog[provider])
	if !ok {
		return nil
	}
	out := []domain.ModelInfo{}
	for _, m := range rows {
		if !keep(provider, m.ID) {
			continue
		}
		out = append(out, modelInfo(provider, m))
	}
	return out
}

func catalogPack(packID string) ([]catalogModel, bool) {
	raw, ok := packJSON(packID)
	if !ok {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var out []catalogModel
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			break
		}
		var m catalogModel
		if err := dec.Decode(&m); err != nil {
			break
		}
		if m.ID == "" {
			m.ID, _ = key.(string)
		}
		out = append(out, m)
	}
	return out, true
}

func packJSON(packID string) (json.RawMessage, bool) {
	src := prices.APIJSON
	if packID == "cursor" {
		src = prices.CursorJSON
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(src, &top) != nil {
		return nil, false
	}
	packRaw, ok := top[packID]
	if !ok {
		return nil, false
	}
	var pack struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(packRaw, &pack) != nil || len(pack.Models) == 0 {
		return nil, false
	}
	return pack.Models, true
}

func modelInfo(provider domain.ProviderID, m catalogModel) domain.ModelInfo {
	input := []string{"text", "image"}
	info := domain.ModelInfo{
		ID:              m.ID,
		Object:          "model",
		OwnedBy:         string(provider),
		Name:            m.Name,
		DisplayName:     m.Name,
		Input:           input,
		InputModalities: input,
		Efforts:         modelEfforts(provider, m.ID),
	}
	if m.Limit != nil {
		info.ContextLength = m.Limit.Context
		info.MaxOutputTokens = m.Limit.Output
	}
	if cost := nonemptyCost(m.Cost); cost != nil {
		info.Cost = cost
	}
	info.Reasoning = modelReasoning(provider, m.ID, info.Efforts)
	return info
}

func nonemptyCost(cost *domain.ModelCostJSON) *domain.ModelCostJSON {
	if cost == nil || (cost.Input == nil && cost.Output == nil && cost.CacheRead == nil && cost.CacheWrite == nil) {
		return nil
	}
	return cost
}

func modelEfforts(provider domain.ProviderID, id string) []string {
	switch provider {
	case domain.ProviderClaude:
		return append([]string{}, claudeEfforts...)
	case domain.ProviderCodex:
		return append([]string{}, codexEfforts...)
	case domain.ProviderGrok:
		return grokModelEfforts(id)
	default:
		return nil
	}
}

func grokModelEfforts(id string) []string {
	low := strings.ToLower(id)
	if i := strings.LastIndex(low, "/"); i >= 0 {
		low = low[i+1:]
	}
	if strings.Contains(low, "non-reasoning") {
		return nil
	}
	if strings.HasSuffix(low, "-reasoning") && !strings.Contains(low, "multi-agent") {
		return nil
	}
	if strings.HasPrefix(low, "grok-3-mini") || strings.HasPrefix(low, "grok-4.20-multi-agent") || strings.HasPrefix(low, "grok-4.3") || strings.HasPrefix(low, "grok-4.5") || strings.HasPrefix(low, "grok-4.6") {
		return append([]string{}, grokEfforts...)
	}
	return nil
}

func modelReasoning(provider domain.ProviderID, id string, efforts []string) bool {
	low := strings.ToLower(id)
	if strings.Contains(low, "non-reasoning") {
		return false
	}
	if len(efforts) > 0 {
		return true
	}
	if strings.Contains(low, "thinking") || strings.Contains(low, "reasoning") {
		return true
	}
	return provider == domain.ProviderClaude || provider == domain.ProviderCodex
}

// Limits returns snapshot context, output, cost, and modalities for an id.
// Native catalogs win; Cursor overlay fills cost for Cursor-only ids.
func Limits(id string) (contextLength, maxOutput int, cost *domain.ModelCostJSON, input []string, ok bool) {
	id = strings.TrimPrefix(id, "cursor/")
	if strings.HasPrefix(id, "cursor-") {
		if contextLength, maxOutput, cost, input, ok = limitsIn(domain.ProviderGrok, strings.TrimPrefix(id, "cursor-")); ok {
			return contextLength, maxOutput, cost, input, true
		}
	}
	for _, providerID := range []domain.ProviderID{domain.ProviderClaude, domain.ProviderCodex, domain.ProviderGrok} {
		if contextLength, maxOutput, cost, input, ok = limitsIn(providerID, id); ok {
			return contextLength, maxOutput, cost, input, true
		}
	}
	if contextLength, maxOutput, cost, input, ok = limitsIn(domain.ProviderCursor, id); ok {
		return contextLength, maxOutput, cost, input, true
	}
	return 0, 0, nil, nil, false
}

func limitsIn(provider domain.ProviderID, id string) (int, int, *domain.ModelCostJSON, []string, bool) {
	rows, ok := catalogPack(providerCatalog[provider])
	if !ok {
		return 0, 0, nil, nil, false
	}
	for _, m := range rows {
		if m.ID != id {
			continue
		}
		var ctx, maxOut int
		if m.Limit != nil {
			ctx, maxOut = m.Limit.Context, m.Limit.Output
		}
		return ctx, maxOut, nonemptyCost(m.Cost), []string{"text", "image"}, true
	}
	return 0, 0, nil, nil, false
}

func PriceProvider(provider domain.ProviderID) string { return providerCatalog[provider] }

// ChatGPT-account Codex catalog. Matches pi-ai openai-codex.json.
var chatgptCodexModels = map[string]bool{
	"gpt-5.6-luna":        true,
	"gpt-5.6-terra":       true,
	"gpt-5.6-sol":         true,
	"gpt-5.5":             true,
	"gpt-5.4":             true,
	"gpt-5.4-mini":        true,
	"gpt-5.3-codex-spark": true,
}

func keepCodex(id string) bool {
	return chatgptCodexModels[id]
}

func keep(provider domain.ProviderID, id string) bool {
	switch provider {
	case domain.ProviderClaude:
		return strings.HasPrefix(id, "claude-")
	case domain.ProviderCodex:
		return keepCodex(id)
	case domain.ProviderGrok:
		return strings.HasPrefix(id, "grok-") && !strings.Contains(id, "imagine")
	case domain.ProviderCursor:
		return true
	default:
		return false
	}
}
