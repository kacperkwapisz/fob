package openai

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestSlugify(t *testing.T) {
	if got := Slugify("OpenRouter"); got != "openrouter" {
		t.Fatalf("got %s", got)
	}
	if got := Slugify("Local LM Studio"); got != "local-lm-studio" {
		t.Fatalf("got %s", got)
	}
	if got := Slugify("123 start"); got != "s-123-start" {
		t.Fatalf("got %s", got)
	}
	if got := Slugify(""); got != "src" {
		t.Fatalf("got %s", got)
	}
	if got := Slugify("Żółć"); got != "src" {
		t.Fatalf("unicode %s", got)
	}
	if !ValidSlug(Slugify(strings.Repeat("a", 80))) {
		t.Fatal("long label should clip to a valid slug")
	}
	if !ValidSlug(Slugify("OpenRouter")) {
		t.Fatal("openrouter should be valid")
	}
	if ValidSlug("OpenRouter") || ValidSlug("") || ValidSlug("1abc") {
		t.Fatal("expected invalid")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	got, err := NormalizeBaseURL("https://openrouter.ai/api/v1/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://openrouter.ai/api" {
		t.Fatalf("got %s", got)
	}
	got, err = NormalizeBaseURL("http://127.0.0.1:1234/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:1234" {
		t.Fatalf("got %s", got)
	}
	got, err = NormalizeBaseURL("https://api.openai.com")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.openai.com" {
		t.Fatalf("got %s", got)
	}
	if _, err := NormalizeBaseURL("not-a-url"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NormalizeBaseURL("ftp://x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestUniqueSlugSkipsReservedAndCollisions(t *testing.T) {
	existing := []domain.Credential{
		{Provider: domain.ProviderOpenAI, Tokens: domain.CredentialTokens{Extra: map[string]any{"slug": "openrouter"}}},
	}
	if got := UniqueSlug(existing, "OpenRouter"); got != "openrouter-2" {
		t.Fatalf("got %s", got)
	}
	if got := UniqueSlug(nil, "Claude"); got != "src-claude" {
		t.Fatalf("got %s", got)
	}
	if got := UniqueSlug(nil, "gpt"); got != "src-gpt" {
		t.Fatalf("got %s", got)
	}
	if got := UniqueSlug(nil, "local"); got != "local" {
		t.Fatalf("got %s", got)
	}
	long := strings.Repeat("a", 48)
	existing = []domain.Credential{
		{Provider: domain.ProviderOpenAI, Tokens: domain.CredentialTokens{Extra: map[string]any{"slug": long}}},
	}
	got := UniqueSlug(existing, long)
	if got == long || !ValidSlug(got) {
		t.Fatalf("collision %s", got)
	}
}

func TestPublicModelID(t *testing.T) {
	if got := PublicModelID("openrouter", "gpt-4o"); got != "openrouter/gpt-4o" {
		t.Fatalf("got %s", got)
	}
	if got := PublicModelID("openrouter", "openrouter/auto"); got != "openrouter/openrouter/auto" {
		t.Fatalf("got %s", got)
	}
	if got := StripSlug("openrouter", "openrouter/openrouter/auto"); got != "openrouter/auto" {
		t.Fatalf("got %s", got)
	}
	if got := StripSlug("openrouter", "openrouter/gpt-4o"); got != "gpt-4o" {
		t.Fatalf("got %s", got)
	}
	if got := StripSlug("openrouter", "gpt-4o"); got != "gpt-4o" {
		t.Fatalf("got %s", got)
	}
}
