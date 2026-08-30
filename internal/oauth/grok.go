package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
)

const (
	grokDiscovery = "https://auth.x.ai/.well-known/openid-configuration"
	grokScope     = "openid profile email offline_access grok-cli:access api:access"
	grokGrant     = "urn:ietf:params:oauth:grant-type:device_code"
)

type grokDevice struct {
	deviceCode    string
	tokenEndpoint string
	createdAt     int64
}

var (
	grokMu      sync.Mutex
	grokDevices = map[string]grokDevice{}
)

type grokLogin struct{ clientID string }

func GrokLogin(e *env.Env) ProviderLogin { return &grokLogin{clientID: e.GrokClientID} }

func (l *grokLogin) ID() domain.ProviderID { return domain.ProviderGrok }

func (l *grokLogin) Start(ctx context.Context, _, _ string) (LoginStart, error) {
	disc, err := grokDiscover(ctx)
	if err != nil {
		return LoginStart{}, err
	}
	form := url.Values{"client_id": {l.clientID}, "scope": {grokScope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.device, strings.NewReader(form.Encode()))
	if err != nil {
		return LoginStart{}, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return LoginStart{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return LoginStart{}, fmt.Errorf("grok device code failed: %d %s", res.StatusCode, raw)
	}
	var jsonMap map[string]any
	if err := json.Unmarshal(raw, &jsonMap); err != nil {
		return LoginStart{}, err
	}
	deviceCode := asString(jsonMap["device_code"])
	userCode := asString(jsonMap["user_code"])
	verification := asString(jsonMap["verification_uri_complete"])
	if verification == "" {
		verification = asString(jsonMap["verification_uri"])
	}
	if deviceCode == "" || verification == "" {
		return LoginStart{}, fmt.Errorf("grok device response incomplete")
	}
	grokMu.Lock()
	grokDevices[deviceCode] = grokDevice{deviceCode: deviceCode, tokenEndpoint: disc.token, createdAt: time.Now().UnixMilli()}
	grokMu.Unlock()
	exp := time.Now().UnixMilli() + 900_000
	if n, ok := jsonMap["expires_in"].(float64); ok {
		exp = time.Now().UnixMilli() + int64(n)*1000
	}
	return LoginStart{Kind: "device", URL: verification, UserCode: userCode, State: deviceCode, ExpiresAt: exp}, nil
}

func (l *grokLogin) Complete(ctx context.Context, _, _, deviceCode, _ string) (LoginResult, error) {
	if deviceCode == "" {
		return LoginResult{}, fmt.Errorf("missing device code")
	}
	grokMu.Lock()
	pending, ok := grokDevices[deviceCode]
	grokMu.Unlock()
	if !ok {
		return LoginResult{}, fmt.Errorf("unknown or expired device code")
	}
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		form := url.Values{
			"grant_type":  {grokGrant},
			"device_code": {deviceCode},
			"client_id":   {l.clientID},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, pending.tokenEndpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return LoginResult{}, err
		}
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		res, err := httpx.Client().Do(req)
		if err != nil {
			return LoginResult{}, err
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		var jsonMap map[string]any
		_ = json.Unmarshal(raw, &jsonMap)
		if res.StatusCode >= 200 && res.StatusCode < 300 && asString(jsonMap["access_token"]) != "" {
			grokMu.Lock()
			delete(grokDevices, deviceCode)
			grokMu.Unlock()
			email := asString(jsonMap["email"])
			label := email
			if label == "" {
				label = "Grok"
			}
			var exp *int64
			if n, ok := jsonMap["expires_in"].(float64); ok {
				v := time.Now().UnixMilli() + int64(n)*1000
				exp = &v
			}
			return LoginResult{
				Provider: domain.ProviderGrok,
				Label:    label,
				Tokens: domain.CredentialTokens{
					AccessToken:  asString(jsonMap["access_token"]),
					RefreshToken: asString(jsonMap["refresh_token"]),
					Email:        email,
					Extra:        map[string]any{},
				},
				ExpiresAt: exp,
			}, nil
		}
		errStr := asString(jsonMap["error"])
		if errStr != "" && errStr != "authorization_pending" && errStr != "slow_down" {
			grokMu.Lock()
			delete(grokDevices, deviceCode)
			grokMu.Unlock()
			return LoginResult{}, fmt.Errorf("grok device login failed: %s", errStr)
		}
		wait := 5 * time.Second
		if errStr == "slow_down" {
			wait = 8 * time.Second
		}
		select {
		case <-ctx.Done():
			return LoginResult{}, ctx.Err()
		case <-time.After(wait):
		}
	}
	grokMu.Lock()
	delete(grokDevices, deviceCode)
	grokMu.Unlock()
	return LoginResult{}, fmt.Errorf("grok device login timed out")
}

type grokDisc struct{ device, token string }

func grokDiscover(ctx context.Context) (grokDisc, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokDiscovery, nil)
	if err != nil {
		return grokDisc{}, err
	}
	res, err := httpx.Client().Do(req)
	if err != nil {
		return grokDisc{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return grokDisc{}, fmt.Errorf("grok oidc discovery failed")
	}
	raw, _ := io.ReadAll(res.Body)
	var jsonMap map[string]any
	if err := json.Unmarshal(raw, &jsonMap); err != nil {
		return grokDisc{}, err
	}
	device := asString(jsonMap["device_authorization_endpoint"])
	token := asString(jsonMap["token_endpoint"])
	if device == "" || token == "" {
		return grokDisc{}, fmt.Errorf("grok oidc discovery missing endpoints")
	}
	return grokDisc{device: device, token: token}, nil
}
