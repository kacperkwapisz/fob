package translate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestOpenAIChatToClaudeCache(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", false, map[string]any{
		"model": "claude-opus-4-7",
		"messages": []any{
			map[string]any{"role": "system", "content": "you are terse"},
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools":            []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}},
		"prompt_cache_key": "sess-1",
	})
	body := AsMap(out.Body)
	if AsStr(body["model"]) != "claude-opus-4-7" {
		t.Fatalf("%v", body["model"])
	}
	system := AsArr(body["system"])
	last := AsMap(system[len(system)-1])
	if AsStr(AsMap(last["cache_control"])["type"]) != "ephemeral" {
		t.Fatalf("system cache %+v", last)
	}
	tools := AsArr(body["tools"])
	if AsStr(AsMap(AsMap(tools[len(tools)-1])["cache_control"])["type"]) != "ephemeral" {
		t.Fatalf("tools cache %+v", tools)
	}
	if AsStr(AsMap(tools[0])["name"]) == "" {
		t.Fatal("tool name")
	}
}

func TestClaudeMessagesToGrok(t *testing.T) {
	out := TranslateRequest(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.5", false, map[string]any{
		"model": "grok-4.5", "system": "be brief",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	body := AsMap(out.Body)
	if AsStr(body["instructions"]) != "be brief" {
		t.Fatalf("instructions %v", body["instructions"])
	}
	input := AsArr(body["input"])
	if AsStr(input[0].(map[string]any)["type"]) != "message" || AsStr(input[0].(map[string]any)["role"]) != "user" {
		t.Fatalf("%+v", input[0])
	}
}

func TestClaudeResponseToOpenAIChat(t *testing.T) {
	nowUnixFn = func() int64 { return 1788089810 }
	defer func() { nowUnixFn = unixNow }()
	out := TranslateResponse(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"id": "msg_1", "content": []any{map[string]any{"type": "text", "text": "hello"}},
		"stop_reason": "end_turn", "usage": map[string]any{"input_tokens": 10.0, "output_tokens": 2.0},
	})
	rec := AsMap(out)
	if AsStr(rec["object"]) != "chat.completion" {
		t.Fatalf("%v", rec["object"])
	}
	choice := AsMap(AsArr(rec["choices"])[0])
	if AsStr(AsMap(choice["message"])["content"]) != "hello" {
		t.Fatalf("%+v", choice)
	}
	if numOf(AsMap(rec["usage"])["prompt_tokens"]) != 10 {
		t.Fatalf("%+v", rec["usage"])
	}
}

func TestGrokResponsesToOpenAIChat(t *testing.T) {
	nowUnixFn = func() int64 { return 1788089810 }
	defer func() { nowUnixFn = unixNow }()
	out := TranslateResponse(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"id": "e13c0c81-7de7-925d-afd4-4c51e8fbf45e", "object": "response",
		"output": []any{
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}}},
			map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "Hey, I'm Mark."}}},
		},
		"usage": map[string]any{"input_tokens": 283.0, "output_tokens": 846.0},
	})
	rec := AsMap(out)
	if AsStr(rec["object"]) != "chat.completion" {
		t.Fatalf("%v", rec["object"])
	}
	choice := AsMap(AsArr(rec["choices"])[0])
	msg := AsMap(choice["message"])
	if AsStr(msg["content"]) != "Hey, I'm Mark." {
		t.Fatalf("content %+v", msg)
	}
	if AsStr(choice["finish_reason"]) != "stop" {
		t.Fatalf("finish_reason %v", choice["finish_reason"])
	}
	if AsStr(msg["reasoning_content"]) != "thinking" {
		t.Fatalf("reasoning %v", msg["reasoning_content"])
	}
	if numOf(AsMap(rec["usage"])["completion_tokens"]) != 846 {
		t.Fatalf("%+v", rec["usage"])
	}
}

func TestGrokStreamToOpenAIChat(t *testing.T) {
	nowUnixFn = func() int64 { return 1788089810 }
	defer func() { nowUnixFn = unixNow }()
	state := EmptyStreamState()
	start := TranslateStream(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.created", "response": map[string]any{"id": "resp_1"},
	}, &state)
	if len(start) == 0 || !strings.Contains(start[0], "assistant") {
		t.Fatalf("%v", start)
	}
	delta := TranslateStream(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.output_text.delta", "delta": "Hey",
	}, &state)
	if len(delta) == 0 || !strings.Contains(delta[0], "Hey") {
		t.Fatalf("%v", delta)
	}
	done := TranslateStream(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp_1", "output": []any{},
			"usage": map[string]any{"input_tokens": 3.0, "output_tokens": 2.0},
		},
	}, &state)
	if !contains(done, "data: [DONE]") {
		t.Fatalf("%v", done)
	}
}

func TestClaudeStreamToOpenAI(t *testing.T) {
	nowUnixFn = func() int64 { return 1788089810 }
	defer func() { nowUnixFn = unixNow }()
	state := EmptyStreamState()
	start := TranslateStream(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg_1", "usage": map[string]any{"input_tokens": 3.0}},
	}, &state)
	if len(start) == 0 || !strings.HasPrefix(start[0], "data: ") {
		t.Fatalf("%v", start)
	}
	delta := TranslateStream(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": "hi"},
	}, &state)
	if !strings.Contains(delta[0], "hi") {
		t.Fatalf("%v", delta)
	}
	stop := TranslateStream(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{"type": "message_stop"}, &state)
	if !contains(stop, "data: [DONE]") {
		t.Fatalf("%v", stop)
	}
}

func TestMissingPairRewritesModel(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", false, map[string]any{
		"model": "alias", "messages": []any{},
	})
	if AsStr(AsMap(out.Body)["model"]) != "claude-opus-4-7" {
		t.Fatalf("%v", out.Body)
	}
}

func TestCursorTranslateMatrix(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatCursor, "composer-2.5", false, map[string]any{
		"model":    "composer-2.5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}},
	})
	body := AsMap(out.Body)
	if AsStr(body["model"]) != "composer-2.5" {
		t.Fatal(body["model"])
	}
	if AsStr(AsMap(AsArr(body["messages"])[0])["content"]) != "hi" {
		t.Fatal(body["messages"])
	}
	if len(AsArr(body["tools"])) != 1 {
		t.Fatal(body["tools"])
	}

	out = TranslateRequest(domain.InboundClaudeMessages, domain.FormatCursor, "composer-2.5", false, map[string]any{
		"model": "composer-2.5", "system": "be brief",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if AsStr(AsMap(AsArr(AsMap(out.Body)["messages"])[0])["role"]) != "system" {
		t.Fatalf("%+v", out.Body)
	}

	upstream := map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion",
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 1.0, "completion_tokens": 1.0},
	}
	resp := AsMap(TranslateResponse(domain.InboundOpenAIChat, domain.FormatCursor, "composer-2.5", map[string]any{}, upstream))
	if AsStr(resp["object"]) != "chat.completion" {
		t.Fatal(resp["object"])
	}
	if AsStr(AsMap(AsMap(AsArr(resp["choices"])[0])["message"])["content"]) != "hello" {
		t.Fatal(resp)
	}
}

func TestTranslateGoldenChatToClaude(t *testing.T) {
	raw, err := os.ReadFile(golden(t, "translate.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]any
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", false, map[string]any{
		"model": "claude-opus-4-7",
		"messages": []any{
			map[string]any{"role": "system", "content": "you are terse"},
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools":            []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}},
		"prompt_cache_key": "sess-1",
	})
	got, _ := json.Marshal(out.Body)
	want, _ := json.Marshal(g["chatToClaude"])
	if string(got) != string(want) {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestFlattenCodexMultiAgentLeavesPlainTools(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message", "role": "user",
				"content": []any{map[string]any{"type": "input_text", "text": "hi"}},
			},
		},
		"tools": []any{
			map[string]any{
				"type": "function", "name": "lookup",
				"parameters": map[string]any{"type": "object"},
			},
		},
	}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("panic: %v", rec)
		}
	}()
	out := FlattenCodexMultiAgent(body)
	if fmt.Sprintf("%p", out) != fmt.Sprintf("%p", body) {
		t.Fatalf("expected original body, got %+v", out)
	}
}

func TestFlattenCodexMultiAgentRewritesAgentMessage(t *testing.T) {
	out := AsMap(FlattenCodexMultiAgent(map[string]any{
		"input": []any{
			map[string]any{
				"type":    "agent_message",
				"content": []any{map[string]any{"type": "encrypted_content", "encrypted_content": "hello"}},
			},
		},
	}))
	item := AsMap(AsArr(out["input"])[0])
	if AsStr(item["type"]) != "message" || AsStr(item["role"]) != "user" {
		t.Fatalf("%+v", item)
	}
	part := AsMap(AsArr(item["content"])[0])
	if AsStr(part["type"]) != "input_text" || AsStr(part["text"]) != "hello" {
		t.Fatalf("%+v", part)
	}
}

func TestFlattenCodexMultiAgentStripsCollabEncryption(t *testing.T) {
	out := AsMap(FlattenCodexMultiAgent(map[string]any{
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{
				"name": "spawn_agent",
				"parameters": map[string]any{
					"properties": map[string]any{
						"message": map[string]any{"type": "string", "encrypted": true},
					},
				},
			},
		},
	}))
	msg := AsMap(AsMap(AsMap(AsMap(AsArr(out["tools"])[0])["parameters"])["properties"])["message"])
	if _, ok := msg["encrypted"]; ok {
		t.Fatalf("encrypted still present: %+v", msg)
	}
	if AsStr(msg["type"]) != "string" {
		t.Fatalf("%+v", msg)
	}
}

func golden(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden", name)
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
