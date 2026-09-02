package codex

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

func TestFetchCodexSubJSON(t *testing.T) {
	g := readGolden(t, "sub-codex.json")
	restore := provider.SetJSONClientForTests(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if r.URL.Path != "/backend-api/wham/usage" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != UserAgent {
			t.Fatalf("ua %s", r.Header.Get("User-Agent"))
		}
		return jsonResp(200, g)
	})})
	defer restore()
	snap, err := FetchSub(context.Background(), domain.Credential{
		Provider: domain.ProviderCodex,
		Tokens:   domain.CredentialTokens{AccessToken: "tok", AccountID: "acct"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Plan != "Plus" {
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
