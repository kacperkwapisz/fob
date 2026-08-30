package proxy

import (
	"context"
	"testing"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/store"
)

type fakeExec struct {
	id     domain.ProviderID
	format domain.ExecutorFormat
	models []domain.ModelInfo
	fn     func(domain.Credential) provider.ExecuteResult
}

func (f fakeExec) ID() domain.ProviderID         { return f.id }
func (f fakeExec) Format() domain.ExecutorFormat { return f.format }
func (f fakeExec) Models() []domain.ModelInfo    { return f.models }
func (f fakeExec) Execute(_ context.Context, c domain.Credential, _ any, _ provider.ExecuteOptions) (provider.ExecuteResult, error) {
	return f.fn(c), nil
}
func (f fakeExec) Refresh(_ context.Context, c domain.Credential) (domain.Credential, error) {
	return c, nil
}

func testFob(t *testing.T, execs map[domain.ProviderID]provider.Executor) (*Fob, *db.DB) {
	t.Helper()
	e, err := env.Load(map[string]string{"JWT_SECRET": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	prices, err := store.NewPriceStore(d)
	if err != nil {
		t.Fatal(err)
	}
	return &Fob{
		Env: e, Vault: store.NewVault(d, e.JWTKey), Keys: store.NewKeyStore(d),
		Usage: store.NewUsageStore(d), Prices: prices, Executors: execs, Settings: store.NewSettingsStore(d),
	}, d
}

func TestRetriesNextCredentialBeforeFirstByte(t *testing.T) {
	var seen []string
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-4-7", Object: "model", OwnedBy: "claude"}},
			fn: func(c domain.Credential) provider.ExecuteResult {
				seen = append(seen, c.ID)
				if c.ID == "a" {
					return provider.ExecuteResult{OK: false, Status: 429, Retryable: true, Body: map[string]any{"error": "busy"}, Message: "busy"}
				}
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
					"id": "msg", "content": []any{map[string]any{"type": "text", "text": "ok"}},
					"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 1.0, "output_tokens": 1.0},
				}}
			},
		},
		domain.ProviderCodex:  fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex, fn: func(domain.Credential) provider.ExecuteResult { return provider.ExecuteResult{OK: false, Status: 500} }},
		domain.ProviderGrok:   fakeExec{id: domain.ProviderGrok, format: domain.FormatGrok, fn: func(domain.Credential) provider.ExecuteResult { return provider.ExecuteResult{OK: false, Status: 500} }},
		domain.ProviderCursor: fakeExec{id: domain.ProviderCursor, format: domain.FormatCursor, fn: func(domain.Credential) provider.ExecuteResult { return provider.ExecuteResult{OK: false, Status: 500} }},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "a", Provider: domain.ProviderClaude, Label: "A", Tokens: domain.CredentialTokens{AccessToken: "a", Extra: map[string]any{}}})
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "b", Provider: domain.ProviderClaude, Label: "B", Tokens: domain.CredentialTokens{AccessToken: "b", Extra: map[string]any{}}})
	created, err := fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "claude-opus-4-7", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
	})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(seen) != 2 || seen[0] != "a" || seen[1] != "b" {
		t.Fatalf("seen %v", seen)
	}
	choices := AsArr(AsMap(result.Body)["choices"])
	if AsStr(AsMap(AsMap(choices[0])["message"])["content"]) != "ok" {
		t.Fatalf("%+v", result.Body)
	}
}

func TestClaude429FailoversToCursor(t *testing.T) {
	var seen []string
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-4-7", Object: "model", OwnedBy: "claude"}},
			fn: func(c domain.Credential) provider.ExecuteResult {
				seen = append(seen, c.ID)
				return provider.ExecuteResult{OK: false, Status: 429, Retryable: true, Body: map[string]any{"error": "quota"}, Message: "quota"}
			},
		},
		domain.ProviderCursor: fakeExec{
			id: domain.ProviderCursor, format: domain.FormatCursor,
			models: []domain.ModelInfo{{ID: "claude-opus-5-medium", Object: "model", OwnedBy: "cursor"}},
			fn: func(c domain.Credential) provider.ExecuteResult {
				seen = append(seen, c.ID)
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
					"id": "chatcmpl_1", "object": "chat.completion",
					"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "from cursor"}, "finish_reason": "stop"}},
					"usage":   map[string]any{"prompt_tokens": 1.0, "completion_tokens": 1.0},
				}}
			},
		},
		domain.ProviderCodex: fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex, fn: func(domain.Credential) provider.ExecuteResult { return provider.ExecuteResult{OK: false, Status: 500} }},
		domain.ProviderGrok:  fakeExec{id: domain.ProviderGrok, format: domain.FormatGrok, fn: func(domain.Credential) provider.ExecuteResult { return provider.ExecuteResult{OK: false, Status: 500} }},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "claude-1", Provider: domain.ProviderClaude, Label: "Claude", Tokens: domain.CredentialTokens{AccessToken: "c", Extra: map[string]any{}}})
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "cursor-1", Provider: domain.ProviderCursor, Label: "Cursor", Tokens: domain.CredentialTokens{AccessToken: "k", Extra: map[string]any{"kind": "oauth"}}})
	created, _ := fob.Keys.Create("t", nil, nil, nil)
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "claude-opus-5", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
	})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(seen) != 2 || seen[0] != "claude-1" || seen[1] != "cursor-1" {
		t.Fatalf("seen %v", seen)
	}
	choices := AsArr(AsMap(result.Body)["choices"])
	if AsStr(AsMap(AsMap(choices[0])["message"])["content"]) != "from cursor" {
		t.Fatalf("%+v", result.Body)
	}
}

func AsMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func AsArr(v any) []any {
	a, _ := v.([]any)
	if a == nil {
		return []any{}
	}
	return a
}
func AsStr(v any) string { s, _ := v.(string); return s }
