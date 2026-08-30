package grok

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
)

func TestGrokPostsCLIIdentityHeaders(t *testing.T) {
	var gotURL string
	var got http.Header
	restore := httpx.SetClient(stubJSON(t, func(r *http.Request) (int, any) {
		gotURL = r.URL.String()
		got = r.Header.Clone()
		return 200, map[string]any{"id": "c", "output": []any{}}
	}))
	defer restore()

	cred := grokCred()
	result, err := Executor{ClientID: "cid"}.Execute(context.Background(), cred, map[string]any{
		"model": "grok-4.5",
		"input": []any{map[string]any{"type": "message", "role": "user", "content": "hi"}},
	}, provider.ExecuteOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("%+v", result)
	}
	if !strings.HasSuffix(gotURL, "/v1/responses") && gotURL != "https://cli-chat-proxy.grok.com/v1/responses" {
		t.Fatalf("url %s", gotURL)
	}
	if got.Get("Authorization") != "Bearer access-tok" {
		t.Fatalf("auth %s", got.Get("Authorization"))
	}
	if got.Get("Accept") != "application/json" {
		t.Fatalf("accept %s", got.Get("Accept"))
	}
	if got.Get("x-grok-conv-id") != "" {
		t.Fatalf("unexpected conv id %s", got.Get("x-grok-conv-id"))
	}
	want := map[string]string{
		"x-grok-client-version":    "0.2.120",
		"User-Agent":               "xai-grok-workspace/0.2.120",
		"x-grok-client-identifier": "grok-shell",
		"X-XAI-Token-Auth":         "xai-grok-cli",
		"x-authenticateresponse":   "authenticate-response",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Fatalf("%s = %q want %q", k, got.Get(k), v)
		}
	}
}

func TestGrokForwardsPromptCacheKey(t *testing.T) {
	var got http.Header
	restore := httpx.SetClient(stubJSON(t, func(r *http.Request) (int, any) {
		got = r.Header.Clone()
		return 200, map[string]any{"id": "r", "output": []any{}}
	}))
	defer restore()
	_, err := Executor{ClientID: "cid"}.Execute(context.Background(), grokCred(), map[string]any{
		"model": "grok-4.5", "input": "hi", "prompt_cache_key": "sess-1",
	}, provider.ExecuteOptions{Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("Accept") != "text/event-stream" {
		t.Fatalf("accept %s", got.Get("Accept"))
	}
	if got.Get("x-grok-conv-id") != "sess-1" {
		t.Fatalf("conv %s", got.Get("x-grok-conv-id"))
	}
}

func TestGrokRefreshOmitsCLIHeaders(t *testing.T) {
	var gotURL, method string
	var got http.Header
	restore := httpx.SetClient(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if strings.Contains(r.URL.Path, "openid-configuration") {
			return jsonResp(200, map[string]any{"token_endpoint": "https://auth.x.ai/oauth/token"})
		}
		gotURL = r.URL.String()
		method = r.Method
		got = r.Header.Clone()
		return jsonResp(200, map[string]any{"access_token": "new", "refresh_token": "new-r", "expires_in": 3600})
	})})
	defer restore()
	next, err := Executor{ClientID: "cid"}.Refresh(context.Background(), grokCred())
	if err != nil {
		t.Fatal(err)
	}
	if next.Tokens.AccessToken != "new" {
		t.Fatalf("%s", next.Tokens.AccessToken)
	}
	if gotURL != "https://auth.x.ai/oauth/token" {
		t.Fatalf("url %s", gotURL)
	}
	if method != http.MethodPost {
		t.Fatalf("method %s", method)
	}
	if !strings.Contains(got.Get("Content-Type"), "application/x-www-form-urlencoded") {
		t.Fatalf("ct %s", got.Get("Content-Type"))
	}
	for _, k := range []string{"x-grok-client-version", "User-Agent", "x-grok-client-identifier", "X-XAI-Token-Auth", "x-authenticateresponse"} {
		if got.Get(k) != "" {
			t.Fatalf("cli header %s leaked", k)
		}
	}
}

func grokCred() domain.Credential {
	return domain.Credential{
		ID: "g1", Provider: domain.ProviderGrok, Label: "Grok",
		Tokens:    domain.CredentialTokens{AccessToken: "access-tok", RefreshToken: "refresh-tok"},
		CreatedAt: 1, UpdatedAt: 1,
	}
}

func stubJSON(t *testing.T, fn func(*http.Request) (int, any)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		status, body := fn(r)
		return jsonResp(status, body)
	})}
}

type roundTrip func(*http.Request) *http.Response

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func jsonResp(status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	rec.Header().Set("content-type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.Write(raw)
	res := rec.Result()
	res.Body = io.NopCloser(strings.NewReader(string(raw)))
	return res
}
