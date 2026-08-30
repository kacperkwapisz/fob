package cursor

import (
	"context"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/oauth"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/translate"
)

type Executor struct {
	live []Model
}

func (e *Executor) ID() domain.ProviderID         { return domain.ProviderCursor }
func (e *Executor) Format() domain.ExecutorFormat { return domain.FormatCursor }

func (e *Executor) Models() []domain.ModelInfo {
	models := e.live
	if len(models) == 0 {
		models = Snapshot()
	}
	out := ToModelInfo(models, false)
	for i := range out {
		out[i].ID = stringsTrimCursorSlash(out[i].ID)
	}
	return out
}

func stringsTrimCursorSlash(id string) string {
	return StripPublicPrefix(id)
}

func (e *Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	rec := translate.AsMap(body)
	if n, ok := rec["n"].(float64); ok && n > 1 {
		return provider.ExecuteResult{OK: false, Status: 400, Body: map[string]any{"error": map[string]any{"message": "n > 1 is not supported for Cursor", "type": "invalid_request_error"}}, Message: "n > 1 is not supported for Cursor"}, nil
	}
	if rec["logprobs"] == true {
		return provider.ExecuteResult{OK: false, Status: 400, Body: map[string]any{"error": map[string]any{"message": "logprobs is not supported for Cursor", "type": "invalid_request_error"}}, Message: "logprobs is not supported for Cursor"}, nil
	}
	clients := []ClientKind{ClientCLI}
	if kind, _ := credential.Tokens.Extra["kind"].(string); kind == "api_key" {
		clients = []ClientKind{ClientSDK, ClientCLI}
	}
	var last provider.ExecuteResult
	for _, client := range clients {
		access, err := accessToken(ctx, credential, client)
		if err != nil {
			last = provider.ExecuteResult{OK: false, Status: 502, Retryable: true, Body: map[string]any{"error": map[string]any{"message": err.Error(), "type": "server_error"}}, Message: err.Error()}
			continue
		}
		result, err := RunChat(ctx, access, rec, opts.Stream, client)
		if err != nil {
			msg := err.Error()
			status := 502
			if contains401(msg) {
				status = 401
			}
			last = provider.ExecuteResult{OK: false, Status: status, Retryable: true, Body: map[string]any{"error": map[string]any{"message": msg, "type": "server_error"}}, Message: msg}
			continue
		}
		if result.Status == 401 {
			last = provider.ExecuteResult{OK: false, Status: 401, Retryable: true, Body: result.Body, Message: "cursor unauthorized"}
			continue
		}
		if result.Status >= 400 {
			return provider.ExecuteResult{OK: false, Status: result.Status, Retryable: provider.IsRetryableStatus(result.Status), Body: result.Body, Message: result.Message}, nil
		}
		go e.refreshModels(access)
		if opts.Stream && result.Stream != nil {
			return provider.ExecuteResult{OK: true, Status: 200, Stream: result.Stream}, nil
		}
		return provider.ExecuteResult{OK: true, Status: 200, Body: result.Body}, nil
	}
	if last.Status == 0 {
		last = provider.ExecuteResult{OK: false, Status: 502, Retryable: true, Body: map[string]any{"error": map[string]any{"message": "cursor execute failed", "type": "server_error"}}, Message: "cursor execute failed"}
	}
	return last, nil
}

func (e *Executor) Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error) {
	if kind, _ := credential.Tokens.Extra["kind"].(string); kind == "api_key" {
		access, err := oauth.ExchangeCursorAPIKey(ctx, credential.Tokens.AccessToken)
		if err != nil {
			return credential, err
		}
		if credential.Tokens.Extra == nil {
			credential.Tokens.Extra = map[string]any{}
		}
		credential.Tokens.Extra["kind"] = "api_key"
		credential.Tokens.Extra["exchanged"] = access
		exp := oauth.JWTExpiryMS(access)
		credential.ExpiresAt = &exp
		credential.UpdatedAt = time.Now().UnixMilli()
		return credential, nil
	}
	if credential.Tokens.RefreshToken == "" {
		return credential, nil
	}
	access, refresh, err := oauth.RefreshCursorOAuth(ctx, credential.Tokens.RefreshToken)
	if err != nil {
		return credential, err
	}
	credential.Tokens.AccessToken = access
	credential.Tokens.RefreshToken = refresh
	if credential.Tokens.Extra == nil {
		credential.Tokens.Extra = map[string]any{}
	}
	credential.Tokens.Extra["kind"] = "oauth"
	exp := oauth.JWTExpiryMS(access)
	credential.ExpiresAt = &exp
	credential.UpdatedAt = time.Now().UnixMilli()
	return credential, nil
}

func (e *Executor) refreshModels(access string) {
	models, err := FetchAvailable(context.Background(), access)
	if err == nil {
		e.live = models
		RegisterModelVariants(models)
	}
}

func accessToken(ctx context.Context, credential domain.Credential, _ ClientKind) (string, error) {
	if kind, _ := credential.Tokens.Extra["kind"].(string); kind != "api_key" {
		return credential.Tokens.AccessToken, nil
	}
	if cached, _ := credential.Tokens.Extra["exchanged"].(string); cached != "" {
		return cached, nil
	}
	return oauth.ExchangeCursorAPIKey(ctx, credential.Tokens.AccessToken)
}

func contains401(msg string) bool {
	return strings.Contains(msg, "401")
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
