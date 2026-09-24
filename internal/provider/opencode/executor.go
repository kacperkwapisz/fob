package opencode

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

const (
	modelsCacheTTL = 5 * time.Minute
	extraKind      = "api_key"
	sessionHeader  = "x-opencode-session"
)

var inferenceRoot = "https://opencode.ai/inference"

// SetInferenceRootForTest overrides the Console inference root. Returns the previous value.
func SetInferenceRootForTest(root string) string {
	prev := inferenceRoot
	inferenceRoot = root
	return prev
}

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

func (e *Executor) ID() domain.ProviderID { return domain.ProviderOpenCode }

func (e *Executor) Format() domain.ExecutorFormat { return domain.FormatOpenAI }

func (e *Executor) FormatFor(model string) domain.ExecutorFormat {
	return FormatForFamily(FamilyOf(model))
}

func (e *Executor) Models() []domain.ModelInfo { return CatalogModels() }

func (e *Executor) ModelsFor(ctx context.Context, credential domain.Credential) []domain.ModelInfo {
	key := credential.ID + "|" + credential.Tokens.AccessToken
	e.mu.Lock()
	if hit, ok := e.cache[key]; ok && time.Now().Before(hit.until) {
		models := hit.models
		e.mu.Unlock()
		return models
	}
	e.mu.Unlock()

	live, err := fetchLiveIDs(ctx, credential.Tokens.AccessToken)
	if err != nil {
		return CatalogModels()
	}
	models := FilterLive(live)
	e.mu.Lock()
	e.cache[key] = modelsCache{models: models, until: time.Now().Add(modelsCacheTTL)}
	e.mu.Unlock()
	return models
}

func (e *Executor) Execute(ctx context.Context, credential domain.Credential, body any, opts provider.ExecuteOptions) (provider.ExecuteResult, error) {
	rec := translate.AsMap(body)
	model := translate.AsStr(rec["model"])
	wire := StripPrefix(model)
	if wire == "" {
		return provider.ExecuteResult{OK: false, Status: 400, Body: map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "model is required"}}, Message: "model required"}, nil
	}
	family := FamilyOf(wire)
	if family == FamilySkip {
		return provider.ExecuteResult{OK: false, Status: 400, Retryable: false, Body: map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "opencode model is not supported: " + wire}}, Message: "unsupported opencode model"}, nil
	}
	if opts.CountTokens {
		return provider.ExecuteResult{OK: false, Status: 400, Body: map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "count_tokens is not supported for OpenCode"}}, Message: "count_tokens unsupported"}, nil
	}
	rec["model"] = wire
	if family == FamilyMessages {
		if _, ok := translate.AsNum(rec["max_tokens"]); !ok {
			rec["max_tokens"] = 1024
		}
	}
	path := pathFor(family)
	headers := map[string]string{
		"Accept":        "application/json",
		"Authorization": "Bearer " + credential.Tokens.AccessToken,
	}
	if opts.InboundHeaders != nil {
		if s := strings.TrimSpace(opts.InboundHeaders[sessionHeader]); s != "" {
			headers[sessionHeader] = s
		}
	}
	if opts.Stream {
		switch family {
		case FamilyChat, FamilyResponses:
			rec["stream"] = true
			headers["Accept"] = "text/event-stream"
		case FamilyMessages:
			rec["stream"] = true
			headers["Accept"] = "text/event-stream"
		}
	} else if family == FamilyChat || family == FamilyResponses {
		rec["stream"] = false
	}
	res, err := provider.PostJSON(ctx, inferenceRoot+path, rec, headers)
	if err != nil {
		return provider.ExecuteResult{}, err
	}
	return wrap(res, opts.Stream)
}

func (e *Executor) Refresh(_ context.Context, credential domain.Credential) (domain.Credential, error) {
	return credential, nil
}

func pathFor(family Family) string {
	switch family {
	case FamilyResponses:
		return "/openai/v1/responses"
	case FamilyMessages:
		return "/anthropic/v1/messages"
	default:
		return "/openai/v1/chat/completions"
	}
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
		go func() {
			defer res.Body.Close()
			provider.ParseSSE(res.Body, ch)
		}()
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

func fetchLiveIDs(ctx context.Context, secret string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	headers := map[string]string{"Accept": "application/json"}
	if secret != "" {
		headers["Authorization"] = "Bearer " + secret
	}
	res, err := provider.GetJSON(ctx, inferenceRoot+"/v1/models", headers)
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
	root := translate.AsMap(body)
	rows := translate.AsArr(root["data"])
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		id := translate.AsStr(translate.AsMap(row)["id"])
		if id != "" {
			out = append(out, id)
		}
	}
	return out, nil
}

func ValidateKey(ctx context.Context, secret string) (label string, err error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "", fmt.Errorf("API key is required")
	}
	ids, err := fetchLiveIDs(ctx, secret)
	if err != nil {
		return "", fmt.Errorf("could not list OpenCode models: %w", err)
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("OpenCode listed no models for this key")
	}
	return "OpenCode", nil
}

func CredentialExtra() map[string]any {
	return map[string]any{"kind": extraKind}
}
