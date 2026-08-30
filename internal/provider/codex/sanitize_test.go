package codex

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/translate"
)

func TestSanitizeCodexBody(t *testing.T) {
	out := SanitizeBody(map[string]any{"instructions": nil, "input": []any{}}, nil)
	if out["instructions"] != "" {
		t.Fatalf("%v", out["instructions"])
	}
	if out["store"] != false {
		t.Fatalf("store %v", out["store"])
	}
	if out["stream"] != true {
		t.Fatalf("stream %v", out["stream"])
	}
	out = SanitizeBody(map[string]any{"max_output_tokens": 16.0, "temperature": 0.2}, nil)
	if _, ok := out["max_output_tokens"]; ok {
		t.Fatal("max_output_tokens")
	}
	if _, ok := out["temperature"]; ok {
		t.Fatal("temperature")
	}
	inc, _ := out["include"].([]any)
	if len(inc) != 1 || inc[0] != "reasoning.encrypted_content" {
		t.Fatalf("include %v", out["include"])
	}
	out = SanitizeBody(map[string]any{"parallel_tool_calls": true}, nil)
	if _, ok := out["parallel_tool_calls"]; ok {
		t.Fatal("expected drop")
	}
	long := "msg_" + strings.Repeat("a", 80)
	out = SanitizeBody(map[string]any{"input": []any{map[string]any{"type": "message", "id": long, "role": "user", "content": "hi"}}}, nil)
	id := out["input"].([]any)[0].(map[string]any)["id"].(string)
	if len([]rune(id)) > 64 || !strings.HasPrefix(id, "msg_") {
		t.Fatalf("%s", id)
	}
}

func TestCodexUpstreamHeaders(t *testing.T) {
	h := UpstreamHeaders("tok", "acct", map[string]string{"user-agent": "Cursor/1.0", "originator": "", "session-id": "sess"}, false, "cache-1")
	if h["Originator"] != Originator || h["User-Agent"] != UserAgent || h["ChatGPT-Account-Id"] != "acct" || h["Session-Id"] != "sess" {
		t.Fatalf("%+v", h)
	}
	if h["X-Client-Request-Id"] != "sess" {
		t.Fatalf("request id %+v", h)
	}
	stream := UpstreamHeaders("tok", "acct", nil, true, "")
	if stream["OpenAI-Beta"] != "responses=experimental" || stream["Accept"] != "text/event-stream" || stream["Session-Id"] == "" {
		t.Fatalf("%+v", stream)
	}
}

func TestSanitizeCompactBody(t *testing.T) {
	out := SanitizeCompactBody(map[string]any{"instructions": nil}, nil)
	if out["stream"] != false || out["store"] != false {
		t.Fatalf("%+v", out)
	}
}

func TestCollectCodexResponse(t *testing.T) {
	ch := make(chan any, 2)
	ch <- map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_1"}}
	ch <- map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_1", "status": "completed", "output": []any{}}}
	close(ch)
	out := translate.AsMap(collectCodexResponse(ch))
	if translate.AsStr(out["id"]) != "resp_1" || translate.AsStr(out["status"]) != "completed" {
		t.Fatalf("%+v", out)
	}
}

func TestChatgptAccountIDFromAccessJWT(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct-from-access"}}`))
	token := "aaa." + payload + ".sig"
	if got := chatgptAccountID(token, ""); got != "acct-from-access" {
		t.Fatalf("%s", got)
	}
}
