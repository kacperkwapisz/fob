package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	api      = "https://api.anthropic.com"
	tokenURL = "https://platform.claude.com/v1/oauth/token"
	scope    = "user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"
)

type Executor struct{ ClientID string }

func (e Executor) ID() domain.ProviderID         { return domain.ProviderClaude }
func (e Executor) Format() domain.ExecutorFormat { return domain.FormatClaude }
func (e Executor) Models() []domain.ModelInfo    { return provider.CatalogModels(domain.ProviderClaude) }

func (e Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	prepared := PrepareUpstream(body, credential.Tokens.AccessToken, credential.ID, credential.Tokens.AccountID, opts.InboundHeaders, opts.Stream, opts.CountTokens, opts.CallerKey)
	path := "/v1/messages?beta=true"
	if opts.CountTokens {
		path = "/v1/messages/count_tokens"
	}
	res, err := provider.PostJSON(ctx, api+path, prepared.Body, prepared.Headers)
	if err != nil {
		return provider.ExecuteResult{}, err
	}
	return wrap(res, opts.Stream && !opts.CountTokens, prepared.Reverse)
}

func (e Executor) Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error) {
	if credential.Tokens.RefreshToken == "" {
		return credential, nil
	}
	payload, _ := json.Marshal(map[string]string{
		"client_id": e.ClientID, "grant_type": "refresh_token", "refresh_token": credential.Tokens.RefreshToken, "scope": scope,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return credential, err
	}
	req.Header.Set("content-type", "application/json")
	res, err := httpx.Client().Do(req)
	if err != nil {
		return credential, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return credential, fmt.Errorf("claude refresh failed: %d", res.StatusCode)
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

func wrap(res *http.Response, stream bool, reverse map[string]string) (provider.ExecuteResult, error) {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		defer res.Body.Close()
		body := safeJSON(res)
		return provider.ExecuteResult{OK: false, Status: res.StatusCode, Retryable: provider.IsRetryableStatus(res.StatusCode), Body: body, Message: errMsg(body, res.StatusCode)}, nil
	}
	if stream {
		ch := make(chan any, 16)
		go func() {
			defer res.Body.Close()
			provider.ParseSSE(res.Body, ch)
		}()
		out := make(chan any, 16)
		go func() {
			defer close(out)
			for chunk := range ch {
				out <- RestoreStreamChunk(chunk, reverse)
			}
		}()
		return provider.ExecuteResult{OK: true, Status: res.StatusCode, Stream: out}, nil
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var body any
	_ = json.Unmarshal(raw, &body)
	if reverse != nil {
		body = RestoreClaudeToolNames(body, reverse)
	}
	return provider.ExecuteResult{OK: true, Status: res.StatusCode, Body: body}, nil
}

func safeJSON(res *http.Response) any {
	raw, _ := io.ReadAll(res.Body)
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return map[string]any{"error": string(raw)}
	}
	return v
}

func errMsg(body any, status int) string {
	err := translate.AsMap(translate.AsMap(body)["error"])
	if s := translate.AsStr(err["message"]); s != "" {
		return s
	}
	if s := translate.AsStr(translate.AsMap(body)["message"]); s != "" {
		return s
	}
	return fmt.Sprintf("upstream %d", status)
}
