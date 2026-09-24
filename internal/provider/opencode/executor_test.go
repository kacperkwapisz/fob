package opencode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
)

func TestPathFor(t *testing.T) {
	if pathFor(FamilyChat) != "/openai/v1/chat/completions" {
		t.Fatal(pathFor(FamilyChat))
	}
	if pathFor(FamilyResponses) != "/openai/v1/responses" {
		t.Fatal(pathFor(FamilyResponses))
	}
	if pathFor(FamilyMessages) != "/anthropic/v1/messages" {
		t.Fatal(pathFor(FamilyMessages))
	}
}

func TestFormatFor(t *testing.T) {
	ex := NewExecutor()
	if ex.FormatFor("opencode/gpt-5.5") != domain.FormatCodex {
		t.Fatal(ex.FormatFor("opencode/gpt-5.5"))
	}
	if ex.FormatFor("opencode/claude-sonnet-4-6") != domain.FormatClaude {
		t.Fatal(ex.FormatFor("opencode/claude-sonnet-4-6"))
	}
	if ex.FormatFor("opencode/kimi-k2.6") != domain.FormatOpenAI {
		t.Fatal(ex.FormatFor("opencode/kimi-k2.6"))
	}
}

func TestExecuteRoutesByFamily(t *testing.T) {
	var saw struct {
		path, auth, model, session string
		maxTokens                  any
		stream                     any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw.path = r.URL.Path
		saw.auth = r.Header.Get("Authorization")
		saw.session = r.Header.Get("x-opencode-session")
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		saw.model, _ = body["model"].(string)
		saw.maxTokens = body["max_tokens"]
		saw.stream = body["stream"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion"}`))
	}))
	defer srv.Close()

	prev := inferenceRoot
	inferenceRoot = srv.URL
	defer func() { inferenceRoot = prev }()

	ex := NewExecutor()
	cred := domain.Credential{
		ID: "oc1", Provider: domain.ProviderOpenCode, Label: "OpenCode",
		Tokens: domain.CredentialTokens{AccessToken: "oc_sk_test", Extra: CredentialExtra()},
	}

	res, err := ex.Execute(context.Background(), cred, map[string]any{
		"model": "opencode/kimi-k2.6", "messages": []any{},
	}, provider.ExecuteOptions{InboundHeaders: map[string]string{"x-opencode-session": "sess-1"}})
	if err != nil || !res.OK {
		t.Fatalf("chat: %+v err=%v", res, err)
	}
	if saw.path != "/openai/v1/chat/completions" || saw.model != "kimi-k2.6" || saw.auth != "Bearer oc_sk_test" || saw.session != "sess-1" || saw.stream != false {
		t.Fatalf("chat saw=%+v", saw)
	}

	res, err = ex.Execute(context.Background(), cred, map[string]any{
		"model": "opencode/gpt-5.5", "input": "hi",
	}, provider.ExecuteOptions{})
	if err != nil || !res.OK {
		t.Fatalf("responses: %+v err=%v", res, err)
	}
	if saw.path != "/openai/v1/responses" || saw.model != "gpt-5.5" {
		t.Fatalf("responses saw=%+v", saw)
	}

	res, err = ex.Execute(context.Background(), cred, map[string]any{
		"model": "opencode/claude-sonnet-4-6", "messages": []any{},
	}, provider.ExecuteOptions{})
	if err != nil || !res.OK {
		t.Fatalf("messages: %+v err=%v", res, err)
	}
	if saw.path != "/anthropic/v1/messages" || saw.model != "claude-sonnet-4-6" || saw.maxTokens != float64(1024) {
		t.Fatalf("messages saw=%+v", saw)
	}

	res, err = ex.Execute(context.Background(), cred, map[string]any{"model": "opencode/gemini-3.1-pro"}, provider.ExecuteOptions{})
	if err != nil || res.OK || res.Status != 400 || res.Retryable {
		t.Fatalf("gemini: %+v err=%v", res, err)
	}
}

func TestFilterLive(t *testing.T) {
	got := FilterLive([]string{"kimi-k2.6", "gemini-3.1-pro", "jev-1.13", "gpt-5.5"})
	ids := map[string]bool{}
	for _, m := range got {
		ids[m.ID] = true
	}
	if !ids["opencode/kimi-k2.6"] || !ids["opencode/gpt-5.5"] {
		t.Fatalf("%v", ids)
	}
	if ids["opencode/gemini-3.1-pro"] || ids["opencode/jev-1.13"] {
		t.Fatalf("skip models leaked: %v", ids)
	}
}

func TestValidateKeyRequiresSecret(t *testing.T) {
	if _, err := ValidateKey(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("%v", err)
	}
}
