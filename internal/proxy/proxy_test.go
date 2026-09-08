package proxy

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/provider/cursor"
	"github.com/kacperkwapisz/fob/internal/store"
)

type fakeExec struct {
	id      domain.ProviderID
	format  domain.ExecutorFormat
	models  []domain.ModelInfo
	fn      func(domain.Credential) provider.ExecuteResult
	refresh func(domain.Credential) (domain.Credential, error)
}

func (f fakeExec) ID() domain.ProviderID         { return f.id }
func (f fakeExec) Format() domain.ExecutorFormat { return f.format }
func (f fakeExec) Models() []domain.ModelInfo    { return f.models }
func (f fakeExec) Execute(_ context.Context, c domain.Credential, _ any, _ provider.ExecuteOptions) (provider.ExecuteResult, error) {
	return f.fn(c), nil
}
func (f fakeExec) Refresh(_ context.Context, c domain.Credential) (domain.Credential, error) {
	if f.refresh != nil {
		return f.refresh(c)
	}
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

func TestGrokStreamMetersAPIEquivalentUSD(t *testing.T) {
	ch := make(chan any, 4)
	go func() {
		defer close(ch)
		ch <- map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}}
		ch <- map[string]any{"type": "response.output_text.delta", "delta": "hi"}
		ch <- map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id": "resp_1", "output": []any{},
				"usage": map[string]any{"input_tokens": 1_000_000.0, "output_tokens": 0.0},
			},
		}
	}()
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderGrok: fakeExec{
			id: domain.ProviderGrok, format: domain.FormatGrok,
			models: []domain.ModelInfo{{ID: "grok-4.6", Object: "model", OwnedBy: "grok"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Stream: ch}
			},
		},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "g1", Provider: domain.ProviderGrok, Label: "Grok", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}}})
	created, err := fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundClaudeMessages,
		Body:    map[string]any{"model": "grok-4.6", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
		Stream:  true,
	})
	if err != nil || !result.OK || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	for range result.Stream {
	}
	today, err := fob.Usage.Since(24 * 60 * 60 * 1000)
	if err != nil {
		t.Fatal(err)
	}
	if today.Requests != 1 || today.PromptTokens != 1_000_000 || today.USD != 2 {
		t.Fatalf("meter %+v", today)
	}
}

func TestListModelsCollapsesCursorVariants(t *testing.T) {
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderGrok: fakeExec{
			id: domain.ProviderGrok, format: domain.FormatGrok,
			models: []domain.ModelInfo{{ID: "grok-4.5", Object: "model", OwnedBy: "grok", DisplayName: "Grok 4.5"}},
		},
		domain.ProviderCursor: &cursor.Executor{},
		domain.ProviderClaude: fakeExec{id: domain.ProviderClaude, format: domain.FormatClaude},
		domain.ProviderCodex:  fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "g", Provider: domain.ProviderGrok, Label: "Grok", Tokens: domain.CredentialTokens{AccessToken: "g", Extra: map[string]any{}}})
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "c", Provider: domain.ProviderCursor, Label: "Cursor", Tokens: domain.CredentialTokens{AccessToken: "c", Extra: map[string]any{"kind": "oauth"}}})
	models := ListModels(fob)
	ids := map[string]int{}
	for _, m := range models {
		ids[m.ID]++
		if ids[m.ID] > 1 {
			t.Fatalf("duplicate id %s", m.ID)
		}
	}
	for _, id := range []string{"grok-4.5", "claude-opus-5", "claude-opus-5-thinking", "composer-2.5", "cursor-grok-4.5", "cursor-grok-4.6"} {
		if ids[id] != 1 {
			t.Fatalf("missing %s", id)
		}
	}
	for _, id := range []string{"claude-opus-5-medium", "claude-opus-5-high-fast", "composer-2.5-fast", "cursor-grok-4.5-medium", "grok-4.6"} {
		if ids[id] != 0 {
			t.Fatalf("variant listed %s", id)
		}
	}
}

func TestListModelsSkipsCursorWhenNativeOwnsID(t *testing.T) {
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-5", Object: "model", OwnedBy: "claude", DisplayName: "Claude Opus 5"}},
		},
		domain.ProviderCursor: &cursor.Executor{},
		domain.ProviderCodex:  fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex},
		domain.ProviderGrok:   fakeExec{id: domain.ProviderGrok, format: domain.FormatGrok},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "claude", Provider: domain.ProviderClaude, Label: "Claude", Tokens: domain.CredentialTokens{AccessToken: "c", Extra: map[string]any{}}})
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "cursor", Provider: domain.ProviderCursor, Label: "Cursor", Tokens: domain.CredentialTokens{AccessToken: "k", Extra: map[string]any{"kind": "oauth"}}})
	var owner string
	count := 0
	for _, m := range ListModels(fob) {
		if m.ID == "claude-opus-5" {
			count++
			owner = m.OwnedBy
		}
	}
	if count != 1 || owner != "claude" {
		t.Fatalf("count %d owner %s", count, owner)
	}
}

func TestCursorStreamMetersOkOnStop(t *testing.T) {
	ch := make(chan any, 4)
	go func() {
		defer close(ch)
		ch <- map[string]any{
			"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}, "finish_reason": nil}},
		}
		ch <- map[string]any{
			"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 10.0, "completion_tokens": 2.0},
		}
	}()
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderCursor: fakeExec{
			id: domain.ProviderCursor, format: domain.FormatCursor,
			models: []domain.ModelInfo{{ID: "gpt-5.6-terra-medium", Object: "model", OwnedBy: "cursor"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Stream: ch}
			},
		},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "c1", Provider: domain.ProviderCursor, Label: "Cursor", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{"kind": "oauth"}}})
	created, err := fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "gpt-5.6-terra-medium", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
		Stream:  true,
	})
	if err != nil || !result.OK || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	for range result.Stream {
	}
	today, err := fob.Usage.Since(24 * 60 * 60 * 1000)
	if err != nil {
		t.Fatal(err)
	}
	if today.Requests != 1 || today.Errors != 0 || today.PromptTokens != 10 || today.CompletionTokens != 2 {
		t.Fatalf("meter %+v", today)
	}
}

func TestCursorStreamLogsProviderError(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	ch := make(chan any, 2)
	go func() {
		defer close(ch)
		ch <- map[string]any{
			"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}, "finish_reason": nil}},
		}
		ch <- map[string]any{
			"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "Cursor went silent waiting for the next token"}, "finish_reason": "error"}},
		}
	}()
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderCursor: fakeExec{
			id: domain.ProviderCursor, format: domain.FormatCursor,
			models: []domain.ModelInfo{{ID: "gpt-5.6-terra-medium", Object: "model", OwnedBy: "cursor"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Stream: ch}
			},
		},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "c1", Provider: domain.ProviderCursor, Label: "Cursor", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{"kind": "oauth"}}})
	created, err := fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "gpt-5.6-terra-medium", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
		Stream:  true,
	})
	if err != nil || !result.OK || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	for range result.Stream {
	}
	log := buf.String()
	if !strings.Contains(log, "fob error  502  cursor/gpt-5.6-terra-medium") {
		t.Fatalf("log %q", log)
	}
	if !strings.Contains(log, "Cursor went silent waiting for the next token") {
		t.Fatalf("missing close reason %q", log)
	}
	if strings.Contains(log, "stream ended before the provider finished") {
		t.Fatalf("generic unfinished leaked %q", log)
	}
}

func TestKeepaliveRefreshesExpiredCredentialWithoutTraffic(t *testing.T) {
	refreshed := 0
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderGrok: fakeExec{
			id: domain.ProviderGrok, format: domain.FormatGrok,
			models: []domain.ModelInfo{{ID: "grok-4.6", Object: "model", OwnedBy: "grok"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: false, Status: 500}
			},
			refresh: func(c domain.Credential) (domain.Credential, error) {
				refreshed++
				exp := time.Now().UnixMilli() + 6*60*60*1000
				c.Tokens.AccessToken = "new"
				c.ExpiresAt = &exp
				return c, nil
			},
		},
	})
	defer d.Close()
	expired := time.Now().UnixMilli() - 60*1000
	_, _ = fob.Vault.Save(store.SaveCredential{
		ID: "g1", Provider: domain.ProviderGrok, Label: "Grok",
		Tokens:    domain.CredentialTokens{AccessToken: "old", RefreshToken: "r", Extra: map[string]any{}},
		ExpiresAt: &expired,
	})
	n, err := KeepaliveCredentials(context.Background(), fob)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || refreshed != 1 {
		t.Fatalf("n=%d refreshed=%d", n, refreshed)
	}
	got, err := fob.Vault.Get("g1")
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Tokens.AccessToken != "new" {
		t.Fatalf("token %q", got.Tokens.AccessToken)
	}
	if got.ExpiresAt == nil || *got.ExpiresAt <= time.Now().UnixMilli() {
		t.Fatalf("expires %+v", got.ExpiresAt)
	}
}

func TestUnknownModelLogsHumanError(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	fob, d := testFob(t, nil)
	defer d.Close()
	created, err := fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "nope-9"},
		Key:     *key,
	})
	if err != nil || result.OK || result.Status != 404 {
		t.Fatalf("%+v %v", result, err)
	}
	msg := AsStr(AsMap(AsMap(result.Body)["error"])["message"])
	if !strings.Contains(msg, "unknown model: nope-9") || !strings.Contains(msg, "/v1/models") {
		t.Fatalf("client %q", msg)
	}
	if !strings.Contains(buf.String(), "fob error") || !strings.Contains(buf.String(), "nope-9") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestNoCredentialClaudeShapeAndLog(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderGrok: fakeExec{id: domain.ProviderGrok, format: domain.FormatGrok, models: []domain.ModelInfo{{ID: "grok-4.6", Object: "model", OwnedBy: "grok"}}},
	})
	defer d.Close()
	created, _ := fob.Keys.Create("t", nil, nil, nil)
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundClaudeMessages,
		Body:    map[string]any{"model": "grok-4.6", "messages": []any{}},
		Key:     *key,
	})
	if err != nil || result.OK || result.Status != 503 {
		t.Fatalf("%+v %v", result, err)
	}
	errMap := AsMap(AsMap(result.Body)["error"])
	if errMap["type"] != "api_error" {
		t.Fatalf("claude type %+v", errMap)
	}
	if _, ok := errMap["code"]; ok {
		t.Fatalf("openai code leaked %+v", errMap)
	}
	msg := AsStr(errMap["message"])
	if !strings.Contains(msg, "no grok credential") || !strings.Contains(msg, "panel") {
		t.Fatalf("client %q", msg)
	}
	if !strings.Contains(buf.String(), "grok/grok-4.6") || !strings.Contains(buf.String(), "claude.messages") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestUpstreamHTMLNotForwarded(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-4-7", Object: "model", OwnedBy: "claude"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{
					OK: false, Status: 502, Retryable: true,
					Body:    map[string]any{"error": "<html>cloudflare ray</html>"},
					Message: "upstream 502",
				}
			},
		},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "c1", Provider: domain.ProviderClaude, Label: "Claude", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}}})
	created, _ := fob.Keys.Create("t", nil, nil, nil)
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "claude-opus-4-7", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
	})
	if err != nil || result.OK || result.Status != 502 {
		t.Fatalf("%+v %v", result, err)
	}
	msg := AsStr(AsMap(AsMap(result.Body)["error"])["message"])
	if strings.Contains(msg, "<html") || strings.Contains(msg, "cloudflare") || strings.Contains(msg, "upstream 502") {
		t.Fatalf("opaque client %q", msg)
	}
	if !strings.Contains(msg, "Claude") {
		t.Fatalf("client %q", msg)
	}
	if strings.Contains(buf.String(), "<html") {
		t.Fatalf("dump in log %q", buf.String())
	}
}

func TestSuccessStaysQuiet(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderGrok: fakeExec{
			id: domain.ProviderGrok, format: domain.FormatGrok,
			models: []domain.ModelInfo{{ID: "grok-4.6", Object: "model", OwnedBy: "grok"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
					"id": "r", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "ok"}}}},
					"usage": map[string]any{"input_tokens": 1.0, "output_tokens": 1.0},
				}}
			},
		},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "g1", Provider: domain.ProviderGrok, Label: "Grok", Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}}})
	created, _ := fob.Keys.Create("t", nil, nil, nil)
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "grok-4.6", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
	})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if strings.Contains(buf.String(), "fob") {
		t.Fatalf("happy path logged %q", buf.String())
	}
}

func TestFailoverDoesNotLogError(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderClaude: fakeExec{
			id: domain.ProviderClaude, format: domain.FormatClaude,
			models: []domain.ModelInfo{{ID: "claude-opus-4-7", Object: "model", OwnedBy: "claude"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: false, Status: 429, Retryable: true, Message: "busy"}
			},
		},
		domain.ProviderCursor: fakeExec{
			id: domain.ProviderCursor, format: domain.FormatCursor,
			models: []domain.ModelInfo{{ID: "claude-opus-5-medium", Object: "model", OwnedBy: "cursor"}},
			fn: func(domain.Credential) provider.ExecuteResult {
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
					"id": "chatcmpl_1", "object": "chat.completion",
					"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
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
	if strings.Contains(buf.String(), "fob error") {
		t.Fatalf("failover logged error %q", buf.String())
	}
}

type liveFake struct {
	fakeExec
	byID map[string][]domain.ModelInfo
}

func (f liveFake) ModelsFor(_ context.Context, c domain.Credential) []domain.ModelInfo {
	return f.byID[c.ID]
}

func TestOpenAISourceRoutesPrefixedModel(t *testing.T) {
	var seen []string
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderOpenAI: liveFake{
			fakeExec: fakeExec{
				id: domain.ProviderOpenAI, format: domain.FormatOpenAI,
				fn: func(c domain.Credential) provider.ExecuteResult {
					seen = append(seen, c.ID)
					return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
						"id": "chatcmpl_1", "object": "chat.completion",
						"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": c.ID}, "finish_reason": "stop"}},
						"usage":   map[string]any{"prompt_tokens": 1.0, "completion_tokens": 1.0},
					}}
				},
			},
			byID: map[string][]domain.ModelInfo{
				"or1": {{ID: "openrouter/gpt-4o", Object: "model", OwnedBy: "openrouter"}},
				"lm1": {{ID: "local/llama-3", Object: "model", OwnedBy: "local"}},
			},
		},
		domain.ProviderCodex: fakeExec{id: domain.ProviderCodex, format: domain.FormatCodex, fn: func(domain.Credential) provider.ExecuteResult {
			return provider.ExecuteResult{OK: false, Status: 500}
		}},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "or1", Provider: domain.ProviderOpenAI, Label: "OpenRouter", Tokens: domain.CredentialTokens{AccessToken: "k1", Extra: map[string]any{"base_url": "https://openrouter.ai/api", "slug": "openrouter"}}})
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "lm1", Provider: domain.ProviderOpenAI, Label: "Local", Tokens: domain.CredentialTokens{AccessToken: "k2", Extra: map[string]any{"base_url": "http://127.0.0.1:1234", "slug": "local"}}})
	created, err := fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "openrouter/gpt-4o", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
	})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(seen) != 1 || seen[0] != "or1" {
		t.Fatalf("seen %v", seen)
	}
	ids := map[string]bool{}
	for _, m := range ListModels(fob) {
		ids[m.ID] = true
	}
	if !ids["openrouter/gpt-4o"] || !ids["local/llama-3"] {
		t.Fatalf("listed %+v", ids)
	}
}

func TestUnprefixedGPTStillHitsCodexWithOpenAISource(t *testing.T) {
	var seen []string
	fob, d := testFob(t, map[domain.ProviderID]provider.Executor{
		domain.ProviderCodex: fakeExec{
			id: domain.ProviderCodex, format: domain.FormatCodex,
			models: []domain.ModelInfo{{ID: "gpt-5.4", Object: "model", OwnedBy: "codex"}},
			fn: func(c domain.Credential) provider.ExecuteResult {
				seen = append(seen, c.ID)
				return provider.ExecuteResult{OK: true, Status: 200, Body: map[string]any{
					"id": "resp", "object": "response", "output": []any{},
					"usage": map[string]any{"input_tokens": 1.0, "output_tokens": 1.0},
				}}
			},
		},
		domain.ProviderOpenAI: liveFake{
			fakeExec: fakeExec{
				id: domain.ProviderOpenAI, format: domain.FormatOpenAI,
				fn: func(c domain.Credential) provider.ExecuteResult {
					seen = append(seen, c.ID)
					return provider.ExecuteResult{OK: false, Status: 500}
				},
			},
		},
	})
	defer d.Close()
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "codex-1", Provider: domain.ProviderCodex, Label: "Codex", Tokens: domain.CredentialTokens{AccessToken: "c", Extra: map[string]any{}}})
	_, _ = fob.Vault.Save(store.SaveCredential{ID: "or1", Provider: domain.ProviderOpenAI, Label: "OpenRouter", Tokens: domain.CredentialTokens{AccessToken: "k", Extra: map[string]any{"base_url": "https://openrouter.ai/api", "slug": "openrouter"}}})
	created, _ := fob.Keys.Create("t", nil, nil, nil)
	key, _ := fob.Keys.Verify(created.Secret)
	result, err := Proxy(context.Background(), fob, Request{
		Inbound: domain.InboundOpenAIChat,
		Body:    map[string]any{"model": "gpt-5.4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}},
		Key:     *key,
	})
	if err != nil || !result.OK {
		t.Fatalf("%+v %v", result, err)
	}
	if len(seen) != 1 || seen[0] != "codex-1" {
		t.Fatalf("seen %v", seen)
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
