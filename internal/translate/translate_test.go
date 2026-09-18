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
	if state.PromptTokens != 3 || state.CompletionTokens != 2 {
		t.Fatalf("usage state %+v", state)
	}
}

func TestGrokStreamToClaudeCapturesUsage(t *testing.T) {
	state := EmptyStreamState()
	TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.created", "response": map[string]any{"id": "resp_1"},
	}, &state)
	TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.output_text.delta", "delta": "Hey",
	}, &state)
	TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp_1", "output": []any{},
			"usage": map[string]any{"input_tokens": 283.0, "output_tokens": 846.0},
		},
	}, &state)
	if !state.Finished {
		t.Fatal("expected finished")
	}
	if state.PromptTokens != 283 || state.CompletionTokens != 846 {
		t.Fatalf("usage state prompt=%d completion=%d", state.PromptTokens, state.CompletionTokens)
	}
}

func TestGrokStreamToResponsesCapturesUsage(t *testing.T) {
	state := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIResponses, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.created", "response": map[string]any{"id": "resp_1"},
	}, &state)
	TranslateStream(domain.InboundOpenAIResponses, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.output_text.delta", "delta": "Hey",
	}, &state)
	TranslateStream(domain.InboundOpenAIResponses, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp_1", "output": []any{},
			"usage": map[string]any{"input_tokens": 283.0, "output_tokens": 846.0},
		},
	}, &state)
	if !state.Finished {
		t.Fatal("expected finished")
	}
	if state.PromptTokens != 283 || state.CompletionTokens != 846 {
		t.Fatalf("usage state prompt=%d completion=%d", state.PromptTokens, state.CompletionTokens)
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

	out = TranslateRequest(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", false, map[string]any{
		"model": "composer-2.5",
		"input": "hi",
	})
	msgs := AsArr(AsMap(out.Body)["messages"])
	if len(msgs) != 1 || AsStr(AsMap(msgs[0])["role"]) != "user" || AsStr(AsMap(msgs[0])["content"]) != "hi" {
		t.Fatalf("string input %+v", out.Body)
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

func TestOpenAITranslateMatrix(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatOpenAI, "openrouter/gpt-4o", false, map[string]any{
		"model":    "openrouter/gpt-4o",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if AsStr(AsMap(out.Body)["model"]) != "openrouter/gpt-4o" {
		t.Fatal(out.Body)
	}
	out = TranslateRequest(domain.InboundClaudeMessages, domain.FormatOpenAI, "openrouter/gpt-4o", false, map[string]any{
		"model": "openrouter/gpt-4o", "system": "be brief",
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
	resp := AsMap(TranslateResponse(domain.InboundOpenAIChat, domain.FormatOpenAI, "openrouter/gpt-4o", map[string]any{}, upstream))
	if AsStr(resp["object"]) != "chat.completion" {
		t.Fatal(resp["object"])
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

func TestCursorThinkingToResponsesAndClaude(t *testing.T) {
	chunk := map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "composer-2.5",
		"choices": []any{map[string]any{"index": 0, "delta": ChatReasoningDelta("plan it"), "finish_reason": nil}},
	}
	text := map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "composer-2.5",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "ok"}, "finish_reason": nil}},
	}

	respState := EmptyStreamState()
	think := TranslateStream(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", map[string]any{}, chunk, &respState)
	if !containsSub(think, `"type":"response.reasoning_summary_text.delta"`) || !containsSub(think, "plan it") {
		t.Fatalf("responses summary %v", think)
	}
	if !containsSub(think, `"type":"response.reasoning_text.delta"`) {
		t.Fatalf("responses text %v", think)
	}
	out := TranslateStream(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", map[string]any{}, text, &respState)
	if !containsSub(out, `"type":"response.output_text.delta"`) || !containsSub(out, "ok") {
		t.Fatalf("responses content %v", out)
	}

	claudeState := EmptyStreamState()
	think = TranslateStream(domain.InboundClaudeMessages, domain.FormatCursor, "composer-2.5", map[string]any{}, chunk, &claudeState)
	if !containsSub(think, `"type":"thinking"`) || !containsSub(think, `"type":"thinking_delta"`) || !containsSub(think, "plan it") {
		t.Fatalf("claude thinking %v", think)
	}
	out = TranslateStream(domain.InboundClaudeMessages, domain.FormatCursor, "composer-2.5", map[string]any{}, text, &claudeState)
	if !containsSub(out, `"type":"text_delta"`) || !containsSub(out, "ok") {
		t.Fatalf("claude text %v", out)
	}

	completed := TranslateResponse(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", map[string]any{}, map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion",
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok", "reasoning_content": "plan it"}, "finish_reason": "stop"}},
	})
	items := AsArr(AsMap(completed)["output"])
	if len(items) < 2 || AsStr(AsMap(items[0])["type"]) != "reasoning" {
		t.Fatalf("responses output %+v", completed)
	}
	if flattenText(firstNonNil(AsMap(items[0])["summary"], AsMap(items[0])["content"])) != "plan it" {
		t.Fatalf("responses reasoning %+v", items[0])
	}

	claude := AsMap(TranslateResponse(domain.InboundClaudeMessages, domain.FormatCursor, "composer-2.5", map[string]any{}, map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion",
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "ok", "reasoning": "plan it"}, "finish_reason": "stop"}},
	}))
	blocks := AsArr(claude["content"])
	if len(blocks) < 2 || AsStr(AsMap(blocks[0])["type"]) != "thinking" || AsStr(AsMap(blocks[0])["thinking"]) != "plan it" {
		t.Fatalf("claude content %+v", claude)
	}
}

func TestClaudeThinkingStreamToResponsesAndChat(t *testing.T) {
	state := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg_1"},
	}, &state)
	think := TranslateStream(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "content_block_delta", "delta": map[string]any{"type": "thinking_delta", "thinking": "hmm"},
	}, &state)
	if !containsSub(think, `"type":"response.reasoning_summary_text.delta"`) || !containsSub(think, "hmm") {
		t.Fatalf("%v", think)
	}

	chatState := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg_1"},
	}, &chatState)
	chat := TranslateStream(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "content_block_delta", "delta": map[string]any{"type": "thinking_delta", "thinking": "hmm"},
	}, &chatState)
	if !containsSub(chat, `"reasoning_content":"hmm"`) || !containsSub(chat, `"reasoning":"hmm"`) {
		t.Fatalf("%v", chat)
	}
}

func TestCodexReasoningTextToChatAndClaude(t *testing.T) {
	chatState := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.created", "response": map[string]any{"id": "resp_1"},
	}, &chatState)
	chat := TranslateStream(domain.InboundOpenAIChat, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.reasoning_text.delta", "delta": "hmm",
	}, &chatState)
	if !containsSub(chat, `"reasoning_content":"hmm"`) || !containsSub(chat, `"reasoning":"hmm"`) {
		t.Fatalf("%v", chat)
	}

	claudeState := EmptyStreamState()
	TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.created", "response": map[string]any{"id": "resp_1"},
	}, &claudeState)
	think := TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.reasoning_summary_text.delta", "delta": "hmm",
	}, &claudeState)
	if !containsSub(think, `"type":"thinking_delta"`) || !containsSub(think, "hmm") {
		t.Fatalf("%v", think)
	}
}

func TestOpenAIChatHTTPImageToClaude(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", false, map[string]any{
		"model": "claude-opus-4-7",
		"messages": []any{map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "what is this"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.jpg"}},
		}}},
	})
	msg := AsMap(AsArr(AsMap(out.Body)["messages"])[0])
	blocks := AsArr(msg["content"])
	if len(blocks) != 2 {
		t.Fatalf("%+v", out.Body)
	}
	src := AsMap(AsMap(blocks[1])["source"])
	if AsStr(src["type"]) != "url" || AsStr(src["url"]) != "https://example.com/cat.jpg" {
		t.Fatalf("source %+v", src)
	}

	back := claudeContentToOpenai([]any{map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/cat.jpg"}}})
	part := AsMap(AsArr(back["content"])[0])
	if AsStr(AsMap(part["image_url"])["url"]) != "https://example.com/cat.jpg" {
		t.Fatalf("roundtrip %+v", back)
	}
}

func TestOpenAIChatReasoningContentToClaude(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", false, map[string]any{
		"model": "claude-opus-4-7",
		"messages": []any{map[string]any{
			"role": "assistant", "content": "ok", "reasoning_content": "plan it",
		}},
	})
	msg := AsMap(AsArr(AsMap(out.Body)["messages"])[0])
	blocks := AsArr(msg["content"])
	if len(blocks) < 2 || AsStr(AsMap(blocks[0])["type"]) != "thinking" || AsStr(AsMap(blocks[0])["thinking"]) != "plan it" {
		t.Fatalf("%+v", out.Body)
	}
}

func TestCursorToolsToResponsesAndClaude(t *testing.T) {
	chunk := map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "composer-2.5",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "c1", "type": "function",
			"function": map[string]any{"name": "lookup", "arguments": `{"q":"x"}`},
		}}}, "finish_reason": nil}},
	}
	done := map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "composer-2.5",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
		"usage":   map[string]any{"prompt_tokens": 4.0, "completion_tokens": 2.0, "prompt_tokens_details": map[string]any{"cached_tokens": 1.0}, "completion_tokens_details": map[string]any{"reasoning_tokens": 3.0}},
	}

	respState := EmptyStreamState()
	added := TranslateStream(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", map[string]any{}, chunk, &respState)
	if !containsSub(added, `"type":"response.output_item.added"`) || !containsSub(added, "lookup") {
		t.Fatalf("added %v", added)
	}
	if !containsSub(added, `"type":"response.function_call_arguments.delta"`) || !containsSub(added, `\"q\":\"x\"`) {
		t.Fatalf("args %v", added)
	}
	fin := TranslateStream(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", map[string]any{}, done, &respState)
	if !containsSub(fin, `"type":"response.completed"`) || !containsSub(fin, `"call_id":"c1"`) {
		t.Fatalf("completed %v", fin)
	}
	if !containsSub(fin, `"input_tokens":4`) || !containsSub(fin, `"reasoning_tokens":3`) {
		t.Fatalf("usage %v", fin)
	}

	claudeState := EmptyStreamState()
	start := TranslateStream(domain.InboundClaudeMessages, domain.FormatCursor, "composer-2.5", map[string]any{}, chunk, &claudeState)
	if !containsSub(start, `"type":"tool_use"`) || !containsSub(start, `"type":"input_json_delta"`) {
		t.Fatalf("claude tool %v", start)
	}
	stop := TranslateStream(domain.InboundClaudeMessages, domain.FormatCursor, "composer-2.5", map[string]any{}, done, &claudeState)
	if !containsSub(stop, `"stop_reason":"tool_use"`) || !containsSub(stop, `"output_tokens":2`) {
		t.Fatalf("claude stop %v", stop)
	}
}

func TestClaudeAndCodexToolsStream(t *testing.T) {
	respState := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "message_start", "message": map[string]any{"id": "msg_1", "usage": map[string]any{"input_tokens": 5.0}},
	}, &respState)
	added := TranslateStream(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "content_block_start", "content_block": map[string]any{"type": "tool_use", "id": "t1", "name": "lookup"},
	}, &respState)
	if !containsSub(added, `"type":"response.output_item.added"`) || !containsSub(added, "lookup") {
		t.Fatalf("%v", added)
	}
	args := TranslateStream(domain.InboundOpenAIResponses, domain.FormatClaude, "claude-opus-4-7", map[string]any{}, map[string]any{
		"type": "content_block_delta", "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"q":`},
	}, &respState)
	if !containsSub(args, `"type":"response.function_call_arguments.delta"`) {
		t.Fatalf("%v", args)
	}

	claudeState := EmptyStreamState()
	TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.created", "response": map[string]any{"id": "resp_1"},
	}, &claudeState)
	tool := TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.output_item.added", "item": map[string]any{"type": "function_call", "call_id": "c1", "name": "lookup"},
	}, &claudeState)
	if !containsSub(tool, `"type":"tool_use"`) || !containsSub(tool, "lookup") {
		t.Fatalf("%v", tool)
	}
	delta := TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.function_call_arguments.delta", "delta": `{"q":"x"}`,
	}, &claudeState)
	if !containsSub(delta, `"type":"input_json_delta"`) {
		t.Fatalf("%v", delta)
	}
	done := TranslateStream(domain.InboundClaudeMessages, domain.FormatGrok, "grok-4.6", map[string]any{}, map[string]any{
		"type": "response.completed", "response": map[string]any{"id": "resp_1", "output": []any{}, "usage": map[string]any{"input_tokens": 2.0, "output_tokens": 1.0}},
	}, &claudeState)
	if !containsSub(done, `"stop_reason":"tool_use"`) {
		t.Fatalf("%v", done)
	}
}

func TestResponsesRequestKeepsReasoningAndToolChoice(t *testing.T) {
	out := TranslateRequest(domain.InboundOpenAIResponses, domain.FormatCursor, "composer-2.5", false, map[string]any{
		"model": "composer-2.5",
		"input": []any{
			map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "plan"}}},
			map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}},
		},
		"tools":             []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}},
		"tool_choice":       map[string]any{"type": "function", "name": "lookup"},
		"reasoning":         map[string]any{"effort": "high"},
		"max_output_tokens": 1024.0,
	})
	body := AsMap(out.Body)
	if AsStr(body["reasoning_effort"]) != "high" || numOf(body["max_tokens"]) != 1024 {
		t.Fatalf("%+v", body)
	}
	choice := AsMap(body["tool_choice"])
	if AsStr(AsMap(choice["function"])["name"]) != "lookup" {
		t.Fatalf("tool_choice %+v", body["tool_choice"])
	}
	var sawReasoning bool
	for _, raw := range AsArr(body["messages"]) {
		m := AsMap(raw)
		if MessageReasoning(m) == "plan" {
			sawReasoning = true
		}
	}
	if !sawReasoning {
		t.Fatalf("messages %+v", body["messages"])
	}

	claude := TranslateRequest(domain.InboundOpenAIChat, domain.FormatClaude, "claude-opus-4-7", false, map[string]any{
		"model":       "claude-opus-4-7",
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":       []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "parameters": map[string]any{"type": "object"}}}},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
	})
	tc := AsMap(AsMap(claude.Body)["tool_choice"])
	if AsStr(tc["type"]) != "tool" || AsStr(tc["name"]) != "lookup" {
		t.Fatalf("%+v", claude.Body)
	}
}

func TestCursorStreamMarksFinishedOnStop(t *testing.T) {
	state := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIChat, domain.FormatCursor, "gpt-5.6-terra-medium", map[string]any{}, map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}, "finish_reason": nil}},
	}, &state)
	if state.Finished {
		t.Fatal("delta must not finish")
	}
	done := TranslateStream(domain.InboundOpenAIChat, domain.FormatCursor, "gpt-5.6-terra-medium", map[string]any{}, map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 12.0, "completion_tokens": 3.0, "routed_model": "gpt-5.6-terra-medium"},
	}, &state)
	if !state.Finished {
		t.Fatalf("expected finished, got lines %v", done)
	}
	if !contains(done, "data: [DONE]") {
		t.Fatalf("missing DONE: %v", done)
	}
	if state.PromptTokens != 12 || state.CompletionTokens != 3 {
		t.Fatalf("usage %+v", state)
	}
	if state.RoutedModel != "gpt-5.6-terra-medium" {
		t.Fatalf("routed %q", state.RoutedModel)
	}
}

func TestCursorStreamCapturesErrorReason(t *testing.T) {
	state := EmptyStreamState()
	TranslateStream(domain.InboundOpenAIChat, domain.FormatCursor, "gpt-5.6-terra-medium", map[string]any{}, map[string]any{
		"id": "chatcmpl_1", "object": "chat.completion.chunk", "model": "gpt-5.6-terra-medium",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "Cursor went silent waiting for the next token"}, "finish_reason": "error"}},
	}, &state)
	if state.Finished {
		t.Fatal("error must not finish")
	}
	if state.Error != "Cursor went silent waiting for the next token" {
		t.Fatalf("error %q", state.Error)
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

func containsSub(ss []string, want string) bool {
	for _, s := range ss {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
