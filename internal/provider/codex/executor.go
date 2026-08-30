package codex

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
	api      = "https://chatgpt.com/backend-api/codex"
	tokenURL = "https://auth.openai.com/oauth/token"
)

type Executor struct{ ClientID string }

func (e Executor) ID() domain.ProviderID         { return domain.ProviderCodex }
func (e Executor) Format() domain.ExecutorFormat { return domain.FormatCodex }
func (e Executor) Models() []domain.ModelInfo    { return provider.CatalogModels(domain.ProviderCodex) }

func (e Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	sanitized := SanitizeBody(body, opts.InboundHeaders)
	if opts.Compact {
		sanitized = SanitizeCompactBody(body, opts.InboundHeaders)
	}
	cacheKey, _ := sanitized["prompt_cache_key"].(string)
	path := "/responses"
	if opts.Compact {
		path = "/responses/compact"
	}
	upstreamStream := !opts.Compact
	headers := UpstreamHeaders(credential.Tokens.AccessToken, accountID(credential), opts.InboundHeaders, upstreamStream, cacheKey)
	res, err := provider.PostJSON(ctx, api+path, sanitized, headers)
	if err != nil {
		return provider.ExecuteResult{}, err
	}
	return wrap(res, opts.Stream && upstreamStream)
}

func (e Executor) Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error) {
	if credential.Tokens.RefreshToken == "" {
		return credential, nil
	}
	form := url.Values{
		"client_id": {e.ClientID}, "grant_type": {"refresh_token"},
		"refresh_token": {credential.Tokens.RefreshToken}, "scope": {"openid email profile offline_access"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(form.Encode()))
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
		return credential, fmt.Errorf("codex refresh failed: %d", res.StatusCode)
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

func accountID(credential domain.Credential) string {
	if credential.Tokens.AccountID != "" {
		return credential.Tokens.AccountID
	}
	return chatgptAccountID(credential.Tokens.AccessToken, extraString(credential.Tokens.Extra, "id_token"))
}

func extraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
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
	ch := make(chan any, 16)
	go provider.ParseSSE(res.Body, ch)
	body := collectCodexResponse(ch)
	return provider.ExecuteResult{OK: true, Status: res.StatusCode, Body: body}, nil
}

func collectCodexResponse(ch <-chan any) any {
	var last any = map[string]any{"object": "response", "status": "completed", "output": []any{}}
	for chunk := range ch {
		ev := translate.AsMap(chunk)
		typ := translate.AsStr(ev["type"])
		switch typ {
		case "response.completed", "response.done", "response.incomplete", "response.failed":
			if resp := ev["response"]; resp != nil {
				return resp
			}
		case "error":
			return ev
		}
		last = chunk
	}
	return last
}
