package claude

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

func TestFetchClaudeSubJSON(t *testing.T) {
	g := readGolden(t, "sub-claude.json")
	restore := provider.SetJSONClientForTests(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if r.Method != http.MethodGet {
			t.Fatalf("method %s", r.Method)
		}
		if r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Fatalf("beta %s", r.Header.Get("anthropic-beta"))
		}
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/api/oauth/usage"):
			return jsonResp(200, g["usage"])
		case strings.HasSuffix(path, "/api/oauth/profile"):
			return jsonResp(200, g["profile"])
		default:
			return jsonResp(404, map[string]any{"error": path})
		}
	})})
	defer restore()
	snap, err := FetchSub(context.Background(), domain.Credential{
		Provider: domain.ProviderClaude,
		Tokens:   domain.CredentialTokens{AccessToken: "oat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Plan != "Max" || len(snap.Windows) < 2 {
		t.Fatalf("%+v", snap)
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
