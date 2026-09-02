package sub

import (
	"context"
	"errors"
	"testing"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/store"
)

func TestCollectSkipsUnknownAndIsolatesErrors(t *testing.T) {
	prev := fetchers
	t.Cleanup(func() { fetchers = prev })
	fetchers = map[domain.ProviderID]Fetcher{
		domain.ProviderClaude: func(_ context.Context, c domain.Credential) (Snapshot, error) {
			return Snapshot{Windows: []Window{{ID: "five-hour", Label: "5 hours", UsedPercent: Ptr(10)}}}, nil
		},
		domain.ProviderCodex: func(_ context.Context, c domain.Credential) (Snapshot, error) {
			return Snapshot{}, errors.New("wham down")
		},
	}
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	vault := store.NewVault(d, make([]byte, 32))
	if _, err := vault.Save(store.SaveCredential{Provider: domain.ProviderClaude, Label: "work", Tokens: domain.CredentialTokens{AccessToken: "a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Save(store.SaveCredential{Provider: domain.ProviderCodex, Label: "codex", Tokens: domain.CredentialTokens{AccessToken: "b"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Save(store.SaveCredential{Provider: domain.ProviderCursor, Label: "cursor", Tokens: domain.CredentialTokens{AccessToken: "c"}}); err != nil {
		t.Fatal(err)
	}
	got := Collect(context.Background(), vault, map[domain.ProviderID]provider.Executor{})
	if len(got) != 2 {
		t.Fatalf("len %d %+v", len(got), got)
	}
	var ok, fail Snapshot
	for _, s := range got {
		if s.Provider == domain.ProviderClaude {
			ok = s
		}
		if s.Provider == domain.ProviderCodex {
			fail = s
		}
	}
	if !ok.OK || len(ok.Windows) != 1 {
		t.Fatalf("ok %+v", ok)
	}
	if fail.OK || fail.Error != "wham down" {
		t.Fatalf("fail %+v", fail)
	}
}
