package oauth

import (
	"encoding/base64"
	"testing"

	"github.com/kacperkwapisz/fob/internal/env"
)

func TestCodexStartIncludesCLIQuery(t *testing.T) {
	login := CodexLogin(&env.Env{CodexClientID: env.EmbeddedCodexClientID})
	start, err := login.Start(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"id_token_add_organizations=true",
		"codex_cli_simplified_flow=true",
		"originator=codex-tui",
	} {
		if !contains(start.URL, want) {
			t.Fatalf("missing %s in %s", want, start.URL)
		}
	}
}

func TestChatgptAccountIDPrefersAccessJWT(t *testing.T) {
	access := jwtWith(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-access"}}`)
	idTok := jwtWith(`{"email":"a@b.c"}`)
	if got := chatgptAccountID(access, idTok); got != "acct-access" {
		t.Fatalf("%s", got)
	}
}

func jwtWith(payload string) string {
	return "aaa." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
}
