package opencode

import "testing"

func TestFamilyOfKnownModels(t *testing.T) {
	cases := []struct {
		id   string
		want Family
	}{
		{"kimi-k2.6", FamilyChat},
		{"opencode/kimi-k2.6", FamilyChat},
		{"glm-5.1", FamilyChat},
		{"minimax-m2.7", FamilyChat},
		{"big-pickle", FamilyChat},
		{"gpt-5.5", FamilyResponses},
		{"opencode/gpt-5.5", FamilyResponses},
		{"grok-4.6", FamilyResponses},
		{"claude-sonnet-4-6", FamilyMessages},
		{"opencode/claude-sonnet-4-6", FamilyMessages},
		{"qwen3.6-plus", FamilyMessages},
		{"gemini-3.1-pro", FamilySkip},
		{"opencode/gemini-3.1-pro", FamilySkip},
		{"jev-1.13", FamilySkip},
		{"test", FamilySkip},
	}
	for _, tc := range cases {
		if got := FamilyOf(tc.id); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.id, got, tc.want)
		}
	}
}

func TestFormatForFamily(t *testing.T) {
	if FormatForFamily(FamilyChat) != "openai" {
		t.Fatal("chat")
	}
	if FormatForFamily(FamilyResponses) != "codex" {
		t.Fatal("responses")
	}
	if FormatForFamily(FamilyMessages) != "claude" {
		t.Fatal("messages")
	}
}

func TestPublicAndStrip(t *testing.T) {
	if PublicID("kimi-k2.6") != "opencode/kimi-k2.6" {
		t.Fatal(PublicID("kimi-k2.6"))
	}
	if StripPrefix("opencode/kimi-k2.6") != "kimi-k2.6" {
		t.Fatal(StripPrefix("opencode/kimi-k2.6"))
	}
}

func TestCatalogOmitsGeminiAndJev(t *testing.T) {
	for _, m := range CatalogModels() {
		id := StripPrefix(m.ID)
		if FamilyOf(id) == FamilySkip {
			t.Fatalf("listed skip model %s", m.ID)
		}
		if id == "gemini-3.1-pro" || id == "jev-1.13" {
			t.Fatalf("unexpected %s", m.ID)
		}
		if m.ID != PublicID(id) {
			t.Fatalf("public id %s", m.ID)
		}
	}
	if len(CatalogModels()) < 50 {
		t.Fatalf("expected a full catalog, got %d", len(CatalogModels()))
	}
}
