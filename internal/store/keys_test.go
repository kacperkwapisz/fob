package store

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestKeyStoreCreateVerifyRevoke(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	keys := NewKeyStore(d)
	created, err := keys.Create("opencode", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^sk-fob-[0-9a-f]{48}$`).MatchString(created.Secret) {
		t.Fatalf("secret %s", created.Secret)
	}
	if strings.Contains(created.Secret, "--") {
		t.Fatal("double dash")
	}
	if created.Key.Prefix != created.Secret[:12] {
		t.Fatalf("prefix %s", created.Key.Prefix)
	}
	found, err := keys.Verify(created.Secret)
	if err != nil || found == nil || found.ID != created.Key.ID || found.Name != "opencode" {
		t.Fatalf("verify %+v %v", found, err)
	}
	miss, err := keys.Verify("sk-fob-nope")
	if err != nil || miss != nil {
		t.Fatalf("miss %+v %v", miss, err)
	}
	ok, err := keys.Revoke(created.Key.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	revoked, err := keys.Verify(created.Secret)
	if err != nil || revoked != nil {
		t.Fatalf("revoked %+v", revoked)
	}
}

func TestKeyStoreScopesProvider(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	keys := NewKeyStore(d)
	created, err := keys.Create("claude-only", []domain.ProviderID{domain.ProviderClaude}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := keys.Verify(created.Secret)
	if err != nil || key == nil {
		t.Fatal(err)
	}
	if err := keys.Allows(*key, domain.ProviderClaude, "claude-opus-4-7"); err != nil {
		t.Fatal(err)
	}
	if err := keys.Allows(*key, domain.ProviderGrok, "grok-4.5"); err == nil {
		t.Fatal("expected scope miss")
	}
}
