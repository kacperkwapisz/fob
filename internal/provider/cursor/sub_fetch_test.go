package cursor

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

func TestFetchCursorSubJSON(t *testing.T) {
	g := readGolden(t, "sub-cursor.json")
	restore := provider.SetJSONClientForTests(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if r.Method != http.MethodPost {
			t.Fatalf("method %s", r.Method)
		}
		if r.Header.Get("x-cursor-client-type") != "cli" {
			t.Fatalf("client %s", r.Header.Get("x-cursor-client-type"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "GetCurrentPeriodUsage"):
			return jsonResp(200, g["usage"])
		case strings.HasSuffix(r.URL.Path, "GetPlanInfo"):
			return jsonResp(200, g["plan"])
		default:
			return jsonResp(404, map[string]any{"path": r.URL.Path})
		}
	})})
	defer restore()
	snap, err := FetchSub(context.Background(), domain.Credential{
		Provider: domain.ProviderCursor,
		Tokens:   domain.CredentialTokens{AccessToken: "jwt", Extra: map[string]any{"kind": "oauth"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Plan != "Ultra · $200/mo" {
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
