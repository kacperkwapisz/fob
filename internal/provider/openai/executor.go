package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/translate"
)

const modelsCacheTTL = 5 * time.Minute

type Executor struct {
	mu    sync.Mutex
	cache map[string]modelsCache
}

type modelsCache struct {
	models []domain.ModelInfo
	until  time.Time
}

func NewExecutor() *Executor {
	return &Executor{cache: map[string]modelsCache{}}
}

func (e *Executor) ID() domain.ProviderID         { return domain.ProviderOpenAI }
func (e *Executor) Format() domain.ExecutorFormat { return domain.FormatOpenAI }

func (e *Executor) Models() []domain.ModelInfo { return nil }

func (e *Executor) ModelsFor(ctx context.Context, credential domain.Credential) []domain.ModelInfo {
	slug := SourceSlug(credential)
	base := SourceBaseURL(credential)
	if slug == "" || base == "" {
		return nil
	}
	key := credential.ID
	if key == "" {
		key = slug + "|" + base
	}
	e.mu.Lock()
	if hit, ok := e.cache[key]; ok && time.Now().Before(hit.until) {
		models := hit.models
		e.mu.Unlock()
		return models
	}
	e.mu.Unlock()

	models, err := fetchModels(ctx, credential)
	if err != nil || len(models) == 0 {
		return nil
	}
	e.mu.Lock()
	e.cache[key] = modelsCache{models: models, until: time.Now().Add(modelsCacheTTL)}
	e.mu.Unlock()
	return models
}

func (e *Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	base := SourceBaseURL(credential)
	if base == "" {
		return provider.ExecuteResult{OK: false, Status: 503, Retryable: false, Body: map[string]any{"error": map[string]any{"message": "openai source is missing a base URL", "type": "server_error"}}, Message: "missing base URL"}, nil
	}
	rec := translate.AsMap(body)
	slug := SourceSlug(credential)
	if model := translate.AsStr(rec["model"]); model != "" {
		rec["model"] = StripSlug(slug, model)
	}
	path := "/v1/chat/completions"
	if opts.CountTokens {
		return provider.ExecuteResult{OK: false, Status: 400, Body: map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "count_tokens is not supported for OpenAI-compatible sources"}}, Message: "count_tokens unsupported"}, nil
	}
	headers := map[string]string{
		"Accept": "application/json",
	}
	if credential.Tokens.AccessToken != "" {
		headers["Authorization"] = "Bearer " + credential.Tokens.AccessToken
	}
	if opts.Stream {
		rec["stream"] = true
		headers["Accept"] = "text/event-stream"
		if rec["stream_options"] == nil {
			rec["stream_options"] = map[string]any{"include_usage": true}
		}
	} else {
		rec["stream"] = false
	}
	res, err := provider.PostJSON(ctx, JoinURL(base, path), rec, headers)
	if err != nil {
		return provider.ExecuteResult{}, err
	}
	return wrap(res, opts.Stream)
}

func (e *Executor) Refresh(_ context.Context, credential domain.Credential) (domain.Credential, error) {
	return credential, nil
}

func fetchModels(ctx context.Context, credential domain.Credential) ([]domain.ModelInfo, error) {
	base := SourceBaseURL(credential)
	slug := SourceSlug(credential)
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	headers := map[string]string{"Accept": "application/json"}
	if credential.Tokens.AccessToken != "" {
		headers["Authorization"] = "Bearer " + credential.Tokens.AccessToken
	}
	res, err := provider.GetJSON(ctx, JoinURL(base, "/v1/models"), headers)
	if err != nil {
		return nil, err
	}
	body, _, err := provider.ReadJSON(res)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("models %d", res.StatusCode)
	}
	return parseModelList(slug, body), nil
}

func parseModelList(slug string, body any) []domain.ModelInfo {
	root := translate.AsMap(body)
	rows := translate.AsArr(root["data"])
	if len(rows) == 0 {
		if arr, ok := body.([]any); ok {
			rows = arr
		}
	}
	out := []domain.ModelInfo{}
	seen := map[string]bool{}
	for _, row := range rows {
		m := translate.AsMap(row)
		id := translate.AsStr(m["id"])
		if id == "" {
			continue
		}
		public := PublicModelID(slug, id)
		if seen[public] {
			continue
		}
		seen[public] = true
		name := translate.AsStr(m["name"], translate.AsStr(m["display_name"], id))
		owned := translate.AsStr(m["owned_by"], slug)
		info := domain.ModelInfo{
			ID:              public,
			Object:          "model",
			OwnedBy:         owned,
			Name:            name,
			DisplayName:     name,
			Input:           []string{"text"},
			InputModalities: []string{"text"},
		}
		if n, ok := asInt(m["context_length"]); ok {
			info.ContextLength = n
		} else if n, ok := asInt(m["context_window"]); ok {
			info.ContextLength = n
		}
		if n, ok := asInt(m["max_output_tokens"]); ok {
			info.MaxOutputTokens = n
		} else if n, ok := asInt(m["max_tokens"]); ok {
			info.MaxOutputTokens = n
		}
		out = append(out, info)
	}
	return out
}

func asInt(v any) (int, bool) {
	n, ok := translate.AsNum(v)
	if !ok || n <= 0 {
		return 0, false
	}
	return int(n), true
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
	if json.Unmarshal(raw, &body) != nil {
		body = map[string]any{"raw": string(raw)}
	}
	return provider.ExecuteResult{OK: true, Status: res.StatusCode, Body: body}, nil
}

func Discover(ctx context.Context, baseURL, secret string) (label string, models []domain.ModelInfo, err error) {
	base, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return "", nil, err
	}
	secret = strings.TrimSpace(secret)
	cred := domain.Credential{
		Provider: domain.ProviderOpenAI,
		Label:    "openai",
		Tokens: domain.CredentialTokens{
			AccessToken: secret,
			Extra:       map[string]any{extraBaseURL: base, extraSlug: "openai"},
		},
	}
	models, err = fetchModels(ctx, cred)
	if err != nil {
		return "", nil, fmt.Errorf("could not list models: %w", err)
	}
	return SourceHost(domain.Credential{Tokens: domain.CredentialTokens{Extra: map[string]any{extraBaseURL: base}}}), models, nil
}
