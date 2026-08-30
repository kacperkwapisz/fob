package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
)

func TestCursorOAuthStartIsDevicePoll(t *testing.T) {
	login := CursorLogin(&env.Env{JWTSecret: strings.Repeat("a", 64)})
	start, err := login.Start(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if start.Kind != "device" {
		t.Fatalf("kind %s", start.Kind)
	}
	if !strings.Contains(start.URL, "/loginDeepControl") {
		t.Fatalf("url %s", start.URL)
	}
	if start.State == "" {
		t.Fatal("empty state")
	}
}

func TestCursorSecretStartAndMeComplete(t *testing.T) {
	restore := httpx.SetClient(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if strings.Contains(r.URL.Path, "/v1/me") {
			return jsonResp(200, map[string]any{"userEmail": "ada@example.com", "apiKeyName": "test", "userId": 1})
		}
		return jsonResp(404, map[string]any{"error": "no"})
	})})
	defer restore()
	login := CursorLogin(&env.Env{JWTSecret: strings.Repeat("a", 64)})
	start, err := login.Start(context.Background(), "", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if start.Kind != "secret" {
		t.Fatalf("kind %s", start.Kind)
	}
	result, err := login.Complete(context.Background(), "", "", "", "crsr_test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "cursor" {
		t.Fatalf("provider %s", result.Provider)
	}
	if result.Label != "ada@example.com" {
		t.Fatalf("label %s", result.Label)
	}
	if result.Tokens.AccessToken != "crsr_test" {
		t.Fatalf("token %s", result.Tokens.AccessToken)
	}
	if kind, _ := result.Tokens.Extra["kind"].(string); kind != "api_key" {
		t.Fatalf("kind %v", result.Tokens.Extra["kind"])
	}
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
