package translate

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/kacperkwapisz/fob/internal/domain"
)

type RequestResult struct {
	Model  string
	Stream bool
	Body   any
}

type StreamState struct {
	ID               string
	Started          bool
	Finished         bool
	ToolIndex        int
	PromptTokens     int64
	CompletionTokens int64
	CacheRead        int64
	CacheWrite       int64
}

func EmptyStreamState() StreamState {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return StreamState{ID: "chatcmpl_" + hex.EncodeToString(buf)}
}

func RewriteModel(body any, model string) any {
	m := AsMap(body)
	if AsStr(m["model"]) == model {
		return body
	}
	out := cloneMap(m)
	out["model"] = model
	return out
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func AsMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func AsArr(v any) []any {
	a, _ := v.([]any)
	if a == nil {
		return []any{}
	}
	return a
}

func AsStr(v any, fallback ...string) string {
	if s, ok := v.(string); ok {
		return s
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return ""
}

func AsNum(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func CloneJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = CloneJSON(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = CloneJSON(child)
		}
		return out
	default:
		return t
	}
}

var _ = domain.InboundOpenAIChat
