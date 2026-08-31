package grok

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
)

func TestFetchGrokSubJSON(t *testing.T) {
	g := readGolden(t, "sub-grok.json")
	restore := provider.SetJSONClientForTests(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" {
			t.Fatalf("auth %s", r.Header.Get("X-XAI-Token-Auth"))
		}
		if strings.Contains(r.URL.RawQuery, "format=credits") {
			return jsonResp(200, g["weekly"])
		}
		if strings.HasSuffix(r.URL.Path, "/billing") {
			return jsonResp(200, g["monthly"])
		}
		return jsonResp(404, map[string]any{})
	})})
	defer restore()
	snap, err := FetchSub(context.Background(), domain.Credential{
		Provider: domain.ProviderGrok,
		Tokens:   domain.CredentialTokens{AccessToken: "tok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Plan != "SuperGrok" {
		t.Fatalf("%+v", snap)
	}
}
