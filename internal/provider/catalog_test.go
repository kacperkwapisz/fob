package provider

import (
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestCatalogModelsPreserveJSONOrder(t *testing.T) {
	models := CatalogModels(domain.ProviderClaude)
	if len(models) < 2 {
		t.Fatalf("len %d", len(models))
	}
	if models[0].ID != "claude-opus-4-7" {
		t.Fatalf("first %s", models[0].ID)
	}
	if models[1].ID != "claude-opus-4-8" {
		t.Fatalf("second %s", models[1].ID)
	}
	for _, m := range models {
		if m.Object != "model" || m.OwnedBy != "claude" {
			t.Fatalf("%+v", m)
		}
	}
}

func TestCatalogDropsImageAndImagine(t *testing.T) {
	for _, m := range CatalogModels(domain.ProviderCodex) {
		if contains(m.ID, "image") {
			t.Fatalf("image model %s", m.ID)
		}
	}
	for _, m := range CatalogModels(domain.ProviderGrok) {
		if contains(m.ID, "imagine") {
			t.Fatalf("imagine model %s", m.ID)
		}
	}
}

func TestCatalogExposesDiscoveryFields(t *testing.T) {
	models := CatalogModels(domain.ProviderGrok)
	byID := map[string]domain.ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	grok := byID["grok-4.6"]
	if grok.ContextLength != 500000 || grok.MaxOutputTokens != 500000 {
		t.Fatalf("grok-4.6 limits %+v", grok)
	}
	if grok.Name != "Grok 4.6" || grok.DisplayName != "Grok 4.6" {
		t.Fatalf("name %+v", grok)
	}
	if !equalStrings(grok.InputModalities, []string{"text", "image"}) || !equalStrings(grok.Input, []string{"text", "image"}) {
		t.Fatalf("input %+v", grok.InputModalities)
	}
	if !grok.Reasoning || !equalStrings(grok.Efforts, []string{"low", "medium", "high", "xhigh"}) {
		t.Fatalf("efforts %+v reasoning %v", grok.Efforts, grok.Reasoning)
	}
	if grok.Cost == nil || grok.Cost.Input == nil || *grok.Cost.Input != 2 {
		t.Fatalf("cost %+v", grok.Cost)
	}

	non := byID["grok-4.20-0309-non-reasoning"]
	if non.Reasoning || len(non.Efforts) != 0 {
		t.Fatalf("non-reasoning %+v", non)
	}
	reasoning := byID["grok-4.20-0309-reasoning"]
	if !reasoning.Reasoning || len(reasoning.Efforts) != 0 {
		t.Fatalf("reasoning twin %+v", reasoning)
	}

	claude := CatalogModels(domain.ProviderClaude)[0]
	if claude.ID != "claude-opus-4-7" || claude.ContextLength != 1000000 || claude.MaxOutputTokens != 128000 {
		t.Fatalf("claude %+v", claude)
	}
	if !equalStrings(claude.Efforts, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("claude efforts %v", claude.Efforts)
	}
}

func TestLimitsReadsOpenAISnapshotOutsideCodexAllowlist(t *testing.T) {
	ctx, maxOut, cost, input, ok := Limits("gpt-5.2")
	if !ok {
		t.Fatal("missing gpt-5.2")
	}
	if ctx != 400000 || maxOut != 128000 {
		t.Fatalf("limits %d %d", ctx, maxOut)
	}
	if cost == nil || cost.Input == nil || *cost.Input != 1.75 {
		t.Fatalf("cost %+v", cost)
	}
	if !equalStrings(input, []string{"text", "image"}) {
		t.Fatalf("input %v", input)
	}
	for _, m := range CatalogModels(domain.ProviderCodex) {
		if m.ID == "gpt-5.2" {
			t.Fatal("codex list leaked gpt-5.2")
		}
	}
}

func TestCatalogCodexChatGPTAllowlist(t *testing.T) {
	got := map[string]bool{}
	for _, m := range CatalogModels(domain.ProviderCodex) {
		got[m.ID] = true
	}
	for _, id := range []string{"gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol"} {
		if !got[id] {
			t.Fatalf("missing %s", id)
		}
	}
	for _, id := range []string{"gpt-4o", "gpt-5", "gpt-5.1", "gpt-5.6", "gpt-5.3-codex", "o3", "text-embedding-3-small"} {
		if got[id] {
			t.Fatalf("unexpected %s", id)
		}
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
