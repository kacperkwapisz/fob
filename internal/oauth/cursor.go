package oauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
)

const (
	cursorWebsiteURL    = "https://cursor.com"
	cursorAPIBase       = "https://api2.cursor.sh"
	cursorPublicAPI     = "https://api.cursor.com"
	cursorLoginPath     = "/loginDeepControl"
	cursorPollPath      = "/auth/poll"
	cursorTokenPath     = "/auth/token"
	cursorExchangePath  = "/auth/exchange_user_api_key"
	cursorMePath        = "/v1/me"
	cursorRefreshSkewMS = 5 * 60 * 1000
	cursorPollMax       = 150
	cursorPollBase      = time.Second
	cursorPollMaxDelay  = 10 * time.Second
	cursorPollBackoff   = 1.2
)

type cursorDevice struct {
	uuid      string
	verifier  string
	createdAt int64
}

var (
	cursorMu      sync.Mutex
	cursorDevices = map[string]cursorDevice{}
)

type cursorLogin struct{}

func CursorLogin(_ *env.Env) ProviderLogin { return &cursorLogin{} }

func (l *cursorLogin) ID() domain.ProviderID { return domain.ProviderCursor }

func (l *cursorLogin) Start(_ context.Context, _, mode string) (LoginStart, error) {
	if mode == "secret" {
		return LoginStart{Kind: "secret", URL: "https://cursor.com/dashboard?tab=integrations"}, nil
	}
	verifier, challenge := PKCE()
	uuid := RandomState()
	cursorMu.Lock()
	cursorDevices[uuid] = cursorDevice{uuid: uuid, verifier: verifier, createdAt: time.Now().UnixMilli()}
	cursorMu.Unlock()
	return LoginStart{
		Kind:      "device",
		URL:       BuildCursorAuthorizeURL(challenge, uuid),
		State:     uuid,
		ExpiresAt: time.Now().UnixMilli() + 15*60*1000,
	}, nil
}

func (l *cursorLogin) Complete(ctx context.Context, _, state, deviceCode, secret string) (LoginResult, error) {
	if secret != "" {
		return completeCursorAPIKey(ctx, secret)
	}
	uuid := deviceCode
	if uuid == "" {
		uuid = state
	}
	if uuid == "" {
		return LoginResult{}, fmt.Errorf("missing cursor login state")
	}
	cursorMu.Lock()
	pending, ok := cursorDevices[uuid]
	cursorMu.Unlock()
	if !ok {
		return LoginResult{}, fmt.Errorf("unknown or expired cursor login — start again")
	}
	tokens, err := PollCursorAuth(ctx, pending.uuid, pending.verifier)
	if err != nil {
		return LoginResult{}, err
	}
	cursorMu.Lock()
	delete(cursorDevices, uuid)
	cursorMu.Unlock()
	return tokens, nil
}

func BuildCursorAuthorizeURL(challenge, uuid string) string {
	q := url.Values{"challenge": {challenge}, "uuid": {uuid}, "mode": {"login"}, "redirectTarget": {"cli"}}
	return cursorWebsiteURL + cursorLoginPath + "?" + q.Encode()
}

func JWTExpiryMS(token string) int64 {
	fallback := time.Now().UnixMilli() + 60*60*1000 - cursorRefreshSkewMS
	parts := splitDot(token)
	if len(parts) < 2 {
		return fallback
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return fallback
	}
	if n, ok := payload["exp"].(float64); ok {
		return int64(n)*1000 - cursorRefreshSkewMS
	}
	return fallback
}

func PollCursorAuth(ctx context.Context, uuid, verifier string) (LoginResult, error) {
	delay := cursorPollBase
	consecutive := 0
	for attempt := 0; attempt < cursorPollMax; attempt++ {
		select {
		case <-ctx.Done():
			return LoginResult{}, fmt.Errorf("cursor_oauth_cancelled")
		case <-time.After(delay):
		}
		u := fmt.Sprintf("%s%s?uuid=%s&verifier=%s", cursorAPIBase, cursorPollPath, url.QueryEscape(uuid), url.QueryEscape(verifier))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return LoginResult{}, err
		}
		res, err := httpx.Client().Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return LoginResult{}, fmt.Errorf("cursor_oauth_cancelled")
			}
			consecutive++
			if consecutive >= 3 {
				return LoginResult{}, fmt.Errorf("cursor_auth_poll_network_failed")
			}
			continue
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 404 {
			consecutive = 0
			next := time.Duration(float64(delay) * cursorPollBackoff)
			if next > cursorPollMaxDelay {
				next = cursorPollMaxDelay
			}
			delay = next
			continue
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			consecutive++
			if consecutive >= 3 {
				return LoginResult{}, fmt.Errorf("cursor_auth_poll_failed:%d", res.StatusCode)
			}
			continue
		}
		var jsonMap map[string]any
		_ = json.Unmarshal(raw, &jsonMap)
		access := asString(jsonMap["accessToken"])
		refresh := asString(jsonMap["refreshToken"])
		if access != "" && refresh != "" {
			return oauthResult(access, refresh), nil
		}
		consecutive++
		if consecutive >= 3 {
			return LoginResult{}, fmt.Errorf("cursor_auth_poll_missing_tokens")
		}
	}
	return LoginResult{}, fmt.Errorf("cursor_auth_poll_timeout")
}

func completeCursorAPIKey(ctx context.Context, raw string) (LoginResult, error) {
	apiKey := trim(raw)
	if apiKey == "" {
		return LoginResult{}, fmt.Errorf("paste a Cursor API key")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cursorPublicAPI+cursorMePath, nil)
	if err != nil {
		return LoginResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return LoginResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return LoginResult{}, fmt.Errorf("cursor key check failed: %d", res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	var jsonMap map[string]any
	_ = json.Unmarshal(body, &jsonMap)
	email := asString(jsonMap["userEmail"])
	if email == "" {
		email = asString(jsonMap["email"])
	}
	name := asString(jsonMap["apiKeyName"])
	label := email
	if label == "" {
		label = name
	}
	if label == "" {
		label = "Cursor key"
	}
	return LoginResult{
		Provider: domain.ProviderCursor,
		Label:    label,
		Tokens: domain.CredentialTokens{
			AccessToken: apiKey,
			AccountID:   asString(jsonMap["userId"]),
			Email:       email,
			Extra:       map[string]any{"kind": "api_key"},
		},
	}, nil
}

func ExchangeCursorAPIKey(ctx context.Context, apiKey string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cursorAPIBase+cursorExchangePath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	res, err := httpx.Client().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("cursor_token_exchange_failed:%d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	var jsonMap map[string]any
	_ = json.Unmarshal(raw, &jsonMap)
	access := asString(jsonMap["accessToken"])
	if access == "" {
		return "", fmt.Errorf("cursor_token_exchange_missing_tokens")
	}
	return access, nil
}

func RefreshCursorOAuth(ctx context.Context, refreshToken string) (access, refresh string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"refreshToken": refreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cursorAPIBase+cursorTokenPath, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return "", "", err
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return readTokenPair(raw, "cursor_token_refresh")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, cursorAPIBase+cursorExchangePath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+refreshToken)
	res, err = httpx.Client().Do(req)
	if err != nil {
		return "", "", err
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		detail := string(raw)
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return "", "", fmt.Errorf("cursor_token_refresh_failed:%d:%s", res.StatusCode, detail)
	}
	return readTokenPair(raw, "cursor_token_exchange")
}

func readTokenPair(raw []byte, label string) (string, string, error) {
	var jsonMap map[string]any
	_ = json.Unmarshal(raw, &jsonMap)
	access := asString(jsonMap["accessToken"])
	refresh := asString(jsonMap["refreshToken"])
	if access == "" || refresh == "" {
		return "", "", fmt.Errorf("%s_missing_tokens", label)
	}
	return access, refresh, nil
}

func oauthResult(access, refresh string) LoginResult {
	exp := JWTExpiryMS(access)
	return LoginResult{
		Provider: domain.ProviderCursor,
		Label:    "Cursor",
		Tokens: domain.CredentialTokens{
			AccessToken:  access,
			RefreshToken: refresh,
			Extra:        map[string]any{"kind": "oauth"},
		},
		ExpiresAt: &exp,
	}
}

func splitDot(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trim(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
