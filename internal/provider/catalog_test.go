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
