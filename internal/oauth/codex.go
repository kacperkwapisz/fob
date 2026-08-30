package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
)

const CodexRedirectURI = "http://localhost:1455/auth/callback"

const (
	codexAuthURL  = "https://auth.openai.com/oauth/authorize"
	codexTokenURL = "https://auth.openai.com/oauth/token"
	codexScope    = "openid email profile offline_access"
)

type codexLogin struct{ clientID string }

func CodexLogin(e *env.Env) ProviderLogin { return &codexLogin{clientID: e.CodexClientID} }

func (l *codexLogin) ID() domain.ProviderID { return domain.ProviderCodex }

func (l *codexLogin) Start(_ context.Context, _, _ string) (LoginStart, error) {
	verifier, challenge := PKCE()
	state := RandomState()
	PutPending(PendingLogin{
		Provider:    domain.ProviderCodex,
		State:       state,
		Verifier:    verifier,
		RedirectURI: CodexRedirectURI,
		CreatedAt:   time.Now().UnixMilli(),
	})
	u, _ := url.Parse(codexAuthURL)
	q := u.Query()
	q.Set("client_id", l.clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", CodexRedirectURI)
	q.Set("scope", codexScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("id_token_add_organizations", "true")
	q.Set("codex_cli_simplified_flow", "true")
	q.Set("originator", "codex-tui")
	u.RawQuery = q.Encode()
	return LoginStart{Kind: "redirect", URL: u.String(), State: state}, nil
}

func (l *codexLogin) Complete(ctx context.Context, code, state, _, _ string) (LoginResult, error) {
	if code == "" {
		return LoginResult{}, fmt.Errorf("missing authorization code")
	}
	var pending *PendingLogin
	if state != "" {
		pending = TakePending(state)
	}
	if pending == nil {
		pending = TakePendingForProvider(domain.ProviderCodex)
	}
	if pending == nil || pending.Provider != domain.ProviderCodex {
		return LoginResult{}, fmt.Errorf("unknown or expired oauth state — start login again")
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {l.clientID},
		"code":          {code},
		"redirect_uri":  {pending.RedirectURI},
		"code_verifier": {pending.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return LoginResult{}, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return LoginResult{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return LoginResult{}, fmt.Errorf("codex token exchange failed: %d %s", res.StatusCode, raw)
	}
	var jsonMap map[string]any
	if err := json.Unmarshal(raw, &jsonMap); err != nil {
		return LoginResult{}, err
	}
	access := asString(jsonMap["access_token"])
	if access == "" {
		return LoginResult{}, fmt.Errorf("codex token response missing access_token")
	}
	id := decodeJWT(asString(jsonMap["id_token"]))
	email := asString(id["email"])
	accountID := chatgptAccountID(access, asString(jsonMap["id_token"]))
	label := email
	if label == "" {
		label = "Codex"
	}
	var exp *int64
	if n, ok := jsonMap["expires_in"].(float64); ok {
		v := time.Now().UnixMilli() + int64(n)*1000
		exp = &v
	}
	extra := map[string]any{}
	if tok := asString(jsonMap["id_token"]); tok != "" {
		extra["id_token"] = tok
	}
	return LoginResult{
		Provider: domain.ProviderCodex,
		Label:    label,
		Tokens: domain.CredentialTokens{
			AccessToken:  access,
			RefreshToken: asString(jsonMap["refresh_token"]),
			AccountID:    accountID,
			Email:        email,
			Extra:        extra,
		},
		ExpiresAt: exp,
	}, nil
}

func decodeJWT(token string) map[string]any {
	if token == "" {
		return map[string]any{}
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{}
	}
	return out
}
