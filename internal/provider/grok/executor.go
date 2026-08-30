package grok

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
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	api        = "https://cli-chat-proxy.grok.com/v1"
	tokenURL   = "https://auth.x.ai/oauth/token"
	discovery  = "https://auth.x.ai/.well-known/openid-configuration"
	cliVersion = "0.2.120"
)

type Executor struct{ ClientID string }

func (e Executor) ID() domain.ProviderID         { return domain.ProviderGrok }
func (e Executor) Format() domain.ExecutorFormat { return domain.FormatGrok }
func (e Executor) Models() []domain.ModelInfo    { return provider.CatalogModels(domain.ProviderGrok) }

func (e Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	rec := translate.AsMap(body)
	model := translate.AsStr(rec["model"])
	sanitized := SanitizeBody(body, model)
	cacheKey, _ := sanitized["prompt_cache_key"].(string)
	accept := "application/json"
	if opts.Stream {
		accept = "text/event-stream"
	}
	headers := map[string]string{
		"Authorization":            "Bearer " + credential.Tokens.AccessToken,
		"Accept":                   accept,
		"x-grok-client-version":    cliVersion,
		"User-Agent":               "xai-grok-workspace/" + cliVersion,
		"x-grok-client-identifier": "grok-shell",
		"X-XAI-Token-Auth":         "xai-grok-cli",
		"x-authenticateresponse":   "authenticate-response",
	}
	if cacheKey != "" {
		headers["x-grok-conv-id"] = cacheKey
	}
	res, err := provider.PostJSON(ctx, api+"/responses", sanitized, headers)
	if err != nil {
		return provider.ExecuteResult{}, err
	}
	return wrap(res, opts.Stream)
}

func (e Executor) Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error) {
	if credential.Tokens.RefreshToken == "" {
		return credential, nil
	}
	token := tokenURL
	if u, err := tokenEndpoint(ctx); err == nil && u != "" {
		token = u
	}
	form := url.Values{"client_id": {e.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {credential.Tokens.RefreshToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, token, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return credential, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return credential, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return credential, fmt.Errorf("grok refresh failed: %d", res.StatusCode)
	}
	var jsonMap map[string]any
	_ = json.Unmarshal(raw, &jsonMap)
	credential.Tokens.AccessToken = translate.AsStr(jsonMap["access_token"], credential.Tokens.AccessToken)
	if r := translate.AsStr(jsonMap["refresh_token"]); r != "" {
		credential.Tokens.RefreshToken = r
	}
	if n, ok := jsonMap["expires_in"].(float64); ok {
		v := time.Now().UnixMilli() + int64(n)*1000
		credential.ExpiresAt = &v
	}
	credential.UpdatedAt = time.Now().UnixMilli()
	return credential, nil
}

func tokenEndpoint(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery, nil)
	if err != nil {
		return tokenURL, err
	}
	res, err := httpx.Client().Do(req)
	if err != nil {
		return tokenURL, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return tokenURL, nil
	}
	raw, _ := io.ReadAll(res.Body)
	var jsonMap map[string]any
	_ = json.Unmarshal(raw, &jsonMap)
	if s := translate.AsStr(jsonMap["token_endpoint"]); s != "" {
		return s, nil
	}
	return tokenURL, nil
}

func wrap(res *http.Response, stream bool) (provider.ExecuteResult, error) {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var body any
		if json.Unmarshal(raw, &body) != nil {
			body = map[string]any{"error": string(raw)}
		}
		return provider.ExecuteResult{OK: false, Status: res.StatusCode, Retryable: provider.IsRetryableStatus(res.StatusCode), Body: body, Message: fmt.Sprintf("upstream %d", res.StatusCode)}, nil
	}
	if stream {
		ch := make(chan any, 16)
		go provider.ParseSSE(res.Body, ch)
		return provider.ExecuteResult{OK: true, Status: res.StatusCode, Stream: ch}, nil
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var body any
	_ = json.Unmarshal(raw, &body)
	return provider.ExecuteResult{OK: true, Status: res.StatusCode, Body: body}, nil
}
