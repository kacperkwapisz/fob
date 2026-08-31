package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/provider"
)

const ClaudeRedirectURI = "http://localhost:54545/callback"

const (
	claudeAuthURL  = "https://claude.ai/oauth/authorize"
	claudeTokenURL = "https://platform.claude.com/v1/oauth/token"
	claudeScope    = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

type claudeLogin struct{ clientID string }

func ClaudeLogin(e *env.Env) ProviderLogin { return &claudeLogin{clientID: e.ClaudeClientID} }

func (l *claudeLogin) ID() domain.ProviderID { return domain.ProviderClaude }

func (l *claudeLogin) Start(_ context.Context, _, _ string) (LoginStart, error) {
	verifier, challenge := PKCE()
	state := RandomState()
	PutPending(PendingLogin{
		Provider:    domain.ProviderClaude,
		State:       state,
		Verifier:    verifier,
		RedirectURI: ClaudeRedirectURI,
		CreatedAt:   time.Now().UnixMilli(),
	})
	u, _ := url.Parse(claudeAuthURL)
	q := u.Query()
	q.Set("client_id", l.clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", ClaudeRedirectURI)
	q.Set("scope", claudeScope)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return LoginStart{Kind: "redirect", URL: u.String(), State: state}, nil
}

func (l *claudeLogin) Complete(ctx context.Context, code, state, _, _ string) (LoginResult, error) {
	if code == "" {
		return LoginResult{}, fmt.Errorf("missing authorization code")
	}
	var pending *PendingLogin
	if state != "" {
		pending = TakePending(state)
	}
	if pending == nil {
		pending = TakePendingForProvider(domain.ProviderClaude)
	}
	if pending == nil || pending.Provider != domain.ProviderClaude {
		return LoginResult{}, fmt.Errorf("unknown or expired oauth state — start login again")
	}
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     l.clientID,
		"code":          code,
		"redirect_uri":  pending.RedirectURI,
		"code_verifier": pending.Verifier,
		"state":         pending.State,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeTokenURL, bytes.NewReader(body))
	if err != nil {
		return LoginResult{}, err
	}
	res, err := provider.ClaudeOAuthDo(req)
	if err != nil {
		return LoginResult{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return LoginResult{}, fmt.Errorf("claude token exchange failed: %d %s", res.StatusCode, raw)
	}
	var jsonMap map[string]any
	if err := json.Unmarshal(raw, &jsonMap); err != nil {
		return LoginResult{}, err
	}
	access := asString(jsonMap["access_token"])
	if access == "" {
		return LoginResult{}, fmt.Errorf("claude token response missing access_token")
	}
	email := asString(jsonMap["email"])
	if email == "" {
		email = asString(asMap(jsonMap["account"])["email"])
	}
	label := email
	if label == "" {
		label = "Claude"
	}
	var exp *int64
	if n, ok := jsonMap["expires_in"].(float64); ok {
		v := time.Now().UnixMilli() + int64(n)*1000
		exp = &v
	}
	return LoginResult{
		Provider: domain.ProviderClaude,
		Label:    label,
		Tokens: domain.CredentialTokens{
			AccessToken:  access,
			RefreshToken: asString(jsonMap["refresh_token"]),
			AccountID:    asString(jsonMap["organization_uuid"]),
			Email:        email,
			Extra:        map[string]any{},
		},
		ExpiresAt: exp,
	}, nil
}
