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
}

type orderedModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func CatalogModels(provider domain.ProviderID) []domain.ModelInfo {
	var top map[string]json.RawMessage
	if json.Unmarshal(prices.APIJSON, &top) != nil {
		return nil
	}
	packRaw, ok := top[providerCatalog[provider]]
	if !ok {
		return nil
	}
	var pack struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(packRaw, &pack) != nil {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(pack.Models)))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	out := []domain.ModelInfo{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			break
		}
		var m orderedModel
		if err := dec.Decode(&m); err != nil {
			break
		}
		id := m.ID
		if id == "" {
			id, _ = key.(string)
		}
		if !keep(provider, id) {
			continue
		}
		out = append(out, domain.ModelInfo{ID: id, Object: "model", OwnedBy: string(provider), DisplayName: m.Name})
	}
	return out
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
