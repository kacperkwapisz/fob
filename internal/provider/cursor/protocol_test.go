package cursor

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/kacperkwapisz/fob/internal/provider/cursor/agentpb"
	"github.com/kacperkwapisz/fob/internal/translate"
)

func TestParseMessages(t *testing.T) {
	parsed := ParseMessages([]OpenAIMessage{
		{Role: "system", Content: "terse"},
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: nil, ToolCalls: []OpenAIToolCall{{ID: "c1", Type: "function", Function: struct{ Name, Arguments string }{"ls", `{"path":"."}`}}}},
		{Role: "tool", ToolCallID: "c1", Content: "a.ts"},
	})
	if parsed.SystemPrompt != "terse" || parsed.UserText != "list files" {
		t.Fatalf("%+v", parsed)
	}
	if len(parsed.ToolResults) != 1 || parsed.ToolResults[0].ToolCallID != "c1" || parsed.ToolResults[0].Content != "a.ts" {
		t.Fatalf("%+v", parsed.ToolResults)
	}
}

func TestRunCursorChatScripted(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	var clientMsgs []*agentpb.AgentClientMessage
	SetBridgeFactoryForTests(func(_, _, _ string, _ bool, _ ClientKind) *Bridge {
		var onData func([]byte)
		var onClose func(int)
		var alive = true
		return &Bridge{
			Write: func(frame []byte) {
				if len(frame) < 5 {
					return
				}
				var msg agentpb.AgentClientMessage
				if err := proto.Unmarshal(frame[5:], &msg); err == nil {
					clientMsgs = append(clientMsgs, &msg)
					if msg.GetRunRequest() != nil && onData != nil {
						go func() {
							onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
								Message: &agentpb.AgentServerMessage_InteractionUpdate{
									InteractionUpdate: &agentpb.InteractionUpdate{
										Message: &agentpb.InteractionUpdate_TextDelta{TextDelta: &agentpb.TextDeltaUpdate{Text: "OK"}},
									},
								},
							}), 0))
							onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
								Message: &agentpb.AgentServerMessage_InteractionUpdate{
									InteractionUpdate: &agentpb.InteractionUpdate{
										Message: &agentpb.InteractionUpdate_TurnEnded{TurnEnded: &agentpb.TurnEndedUpdate{}},
									},
								},
							}), 0))
							onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
								Message: &agentpb.AgentServerMessage_ConversationCheckpointUpdate{
									ConversationCheckpointUpdate: &agentpb.ConversationStateStructure{},
								},
							}), 0))
							if onClose != nil {
								onClose(0)
							}
						}()
					}
				}
			},
			End:     func() { alive = false },
			OnData:  func(cb func([]byte)) { onData = cb },
			OnClose: func(cb func(int)) { onClose = cb },
			Alive:   func() bool { return alive },
		}
	})
	defer SetBridgeFactoryForTests(nil)
	defer CleanupAllSessionState()

	result, err := RunChat(context.Background(), "tok", map[string]any{
		"model": "composer-2.5", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": true,
	}, true, ClientCLI)
	if err != nil || result.Status != 200 || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	var events []any
	for ev := range result.Stream {
		events = append(events, ev)
	}
	joined := marshalJSON(events)
	if !containsStr(joined, "OK") || !containsStr(joined, "stop") {
		t.Fatalf("%s", joined)
	}
	found := false
	for _, m := range clientMsgs {
		if m.GetRunRequest() != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("no runRequest")
	}
	_ = translate.AsMap
}

func TestRunCursorChatConnectEndThenClose(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	SetBridgeFactoryForTests(func(_, _, _ string, _ bool, _ ClientKind) *Bridge {
		var onData func([]byte)
		var onClose func(int)
		alive := true
		return &Bridge{
			Write: func(frame []byte) {
				if len(frame) < 5 {
					return
				}
				var msg agentpb.AgentClientMessage
				if err := proto.Unmarshal(frame[5:], &msg); err == nil && msg.GetRunRequest() != nil && onData != nil {
					go func() {
						onData(frameConnect([]byte(`{"error":{"code":"not_found","message":"Error"}}`), connectEndStreamFlag))
						if onClose != nil {
							onClose(0)
						}
					}()
				}
			},
			End:     func() { alive = false },
			OnData:  func(cb func([]byte)) { onData = cb },
			OnClose: func(cb func(int)) { onClose = cb },
			Alive:   func() bool { return alive },
		}
	})
	defer SetBridgeFactoryForTests(nil)
	defer CleanupAllSessionState()

	stream, err := RunChat(context.Background(), "tok", map[string]any{
		"model": "cursor-auto", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": true,
	}, true, ClientCLI)
	if err != nil || stream.Stream == nil {
		t.Fatalf("%+v %v", stream, err)
	}
	for range stream.Stream {
	}

	result, err := RunChat(context.Background(), "tok", map[string]any{
		"model": "cursor-auto", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": false,
	}, false, ClientCLI)
	if err != nil || result.Status != 200 {
		t.Fatalf("%+v %v", result, err)
	}
	body := translate.AsMap(result.Body)
	choice := translate.AsMap(translate.AsArr(body["choices"])[0])
	msg := translate.AsMap(choice["message"])
	if !containsStr(translate.AsStr(msg["content"]), "not_found") {
		t.Fatalf("%+v", result.Body)
	}
}

func TestResolveCursorAutoWireID(t *testing.T) {
	if got := resolveModelID("cursor-auto", ""); got != "default" {
		t.Fatalf("got %s", got)
	}
}

func TestBuildCursorRequestAutoUsesRequestedModel(t *testing.T) {
	payload := buildCursorRequest("default", "", "hi", nil, "c1", nil, nil, nil, nil, false, nil)
	var msg agentpb.AgentClientMessage
	if err := proto.Unmarshal(payload.requestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	run := msg.GetRunRequest()
	if run == nil || !run.GetClientSupportsRoutedModelUpdate() {
		t.Fatalf("%+v", run)
	}
	if run.GetRequestedModel() == nil || run.GetRequestedModel().GetModelId() != "default" {
		t.Fatalf("requested=%+v details=%+v", run.GetRequestedModel(), run.GetModelDetails())
	}
	if run.GetModelDetails() != nil {
		t.Fatalf("auto must not send modelDetails")
	}
}

func TestComputeUsageIncludesRoutedModel(t *testing.T) {
	usage := computeUsage(&streamProtoState{
		turnUsage: &turnUsage{input: 10, output: 2, cacheRead: 3},
		routedID:  "composer-2.5",
	})
	if translate.AsStr(usage["routed_model"]) != "composer-2.5" {
		t.Fatalf("%+v", usage)
	}
	if n, _ := usage["prompt_tokens"].(int); n != 13 {
		t.Fatalf("%+v", usage)
	}
}

func TestProcessServerCapturesRoutedModel(t *testing.T) {
	state := &streamProtoState{}
	processServer(&agentpb.AgentServerMessage{
		Message: &agentpb.AgentServerMessage_InteractionUpdate{
			InteractionUpdate: &agentpb.InteractionUpdate{
				Message: &agentpb.InteractionUpdate_RoutedModel{
					RoutedModel: &agentpb.RoutedModelUpdate{DisplayName: "Composer 2.5"},
				},
			},
		},
	}, nil, nil, nil, state, nil, nil, nil, nil)
	if state.routedID != "composer-2.5" {
		t.Fatalf("routedID=%q", state.routedID)
	}
	processServer(&agentpb.AgentServerMessage{
		Message: &agentpb.AgentServerMessage_InteractionUpdate{
			InteractionUpdate: &agentpb.InteractionUpdate{
				Message: &agentpb.InteractionUpdate_RoutedModel{
					RoutedModel: &agentpb.RoutedModelUpdate{DisplayName: "Auto (default)"},
				},
			},
		},
	}, nil, nil, nil, state, nil, nil, nil, nil)
	if state.routedID != "composer-2.5" {
		t.Fatalf("auto must not replace routedID=%q", state.routedID)
	}
}

func TestCollectNonStreamSkipsEmptyChoices(t *testing.T) {
	result := collectNonStream("chatcmpl-x", 1, "composer-2.5", []map[string]any{
		{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "created": int64(1), "model": "composer-2.5",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "OK"}, "finish_reason": nil}},
		},
		{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "created": int64(1), "model": "composer-2.5",
			"choices": []any{},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		},
	}, &streamProtoState{})
	if result.Status != 200 {
		t.Fatalf("%+v", result)
	}
	body := translate.AsMap(result.Body)
	choice := translate.AsMap(translate.AsArr(body["choices"])[0])
	msg := translate.AsMap(choice["message"])
	if translate.AsStr(msg["content"]) != "OK" {
		t.Fatalf("%+v", result.Body)
	}
	if translate.AsStr(choice["finish_reason"]) != "stop" {
		t.Fatalf("%+v", result.Body)
	}
}

func TestRunCursorChatNonStream(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	SetBridgeFactoryForTests(func(_, _, _ string, _ bool, _ ClientKind) *Bridge {
		var onData func([]byte)
		var onClose func(int)
		alive := true
		return &Bridge{
			Write: func(frame []byte) {
				if len(frame) < 5 {
					return
				}
				var msg agentpb.AgentClientMessage
				if err := proto.Unmarshal(frame[5:], &msg); err == nil && msg.GetRunRequest() != nil && onData != nil {
					go func() {
						onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
							Message: &agentpb.AgentServerMessage_InteractionUpdate{
								InteractionUpdate: &agentpb.InteractionUpdate{
									Message: &agentpb.InteractionUpdate_TextDelta{TextDelta: &agentpb.TextDeltaUpdate{Text: "OK"}},
								},
							},
						}), 0))
						onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
							Message: &agentpb.AgentServerMessage_InteractionUpdate{
								InteractionUpdate: &agentpb.InteractionUpdate{
									Message: &agentpb.InteractionUpdate_TurnEnded{TurnEnded: &agentpb.TurnEndedUpdate{}},
								},
							},
						}), 0))
						onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
							Message: &agentpb.AgentServerMessage_ConversationCheckpointUpdate{
								ConversationCheckpointUpdate: &agentpb.ConversationStateStructure{},
							},
						}), 0))
						if onClose != nil {
							onClose(0)
						}
					}()
				}
			},
			End:     func() { alive = false },
			OnData:  func(cb func([]byte)) { onData = cb },
			OnClose: func(cb func(int)) { onClose = cb },
			Alive:   func() bool { return alive },
		}
	})
	defer SetBridgeFactoryForTests(nil)
	defer CleanupAllSessionState()

	result, err := RunChat(context.Background(), "tok", map[string]any{
		"model": "composer-2.5", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": false,
	}, false, ClientCLI)
	if err != nil || result.Status != 200 {
		t.Fatalf("%+v %v", result, err)
	}
	body := translate.AsMap(result.Body)
	choice := translate.AsMap(translate.AsArr(body["choices"])[0])
	msg := translate.AsMap(choice["message"])
	if translate.AsStr(msg["content"]) != "OK" {
		t.Fatalf("%+v", result.Body)
	}
}

func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

func TestThinkingFilter(t *testing.T) {
	f := newThinkingFilter()
	c, r := f.Process("hello <think>secret</think> world")
	if c != "hello  world" || r != "secret" {
		t.Fatalf("content=%q reasoning=%q", c, r)
	}
}

func TestKvGetBlob(t *testing.T) {
	store := map[string][]byte{"abcd": []byte("data")}
	var wrote [][]byte
	bridge := &Bridge{Write: func(b []byte) { wrote = append(wrote, append([]byte(nil), b...)) }, End: func() {}, Alive: func() bool { return true }}
	ok := handleKv(&agentpb.KvServerMessage{
		Id:      7,
		Message: &agentpb.KvServerMessage_GetBlobArgs{GetBlobArgs: &agentpb.GetBlobArgs{BlobId: []byte{0xab, 0xcd}}},
	}, store, bridge)
	if !ok || len(wrote) != 1 {
		t.Fatalf("ok=%v wrote=%d", ok, len(wrote))
	}
	var msg agentpb.AgentClientMessage
	if err := proto.Unmarshal(wrote[0][5:], &msg); err != nil {
		t.Fatal(err)
	}
	if msg.GetKvClientMessage() == nil || msg.GetKvClientMessage().GetId() != 7 {
		t.Fatalf("%+v", msg.GetKvClientMessage())
	}
}

func TestMcpPauseAndResume(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	var clientMsgs []*agentpb.AgentClientMessage
	var onData func([]byte)
	SetBridgeFactoryForTests(func(_, _, _ string, _ bool, _ ClientKind) *Bridge {
		alive := true
		return &Bridge{
			Write: func(frame []byte) {
				if len(frame) < 5 {
					return
				}
				var msg agentpb.AgentClientMessage
				if proto.Unmarshal(frame[5:], &msg) != nil {
					return
				}
				clientMsgs = append(clientMsgs, &msg)
				if msg.GetRunRequest() != nil && onData != nil {
					go func() {
						onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
							Message: &agentpb.AgentServerMessage_ExecServerMessage{
								ExecServerMessage: &agentpb.ExecServerMessage{
									Id: 3, ExecId: "e1",
									Message: &agentpb.ExecServerMessage_McpArgs{McpArgs: &agentpb.McpArgs{
										Name: "ls", ToolName: "ls", ToolCallId: "c1",
									}},
								},
							},
						}), 0))
					}()
				}
			},
			End:     func() { alive = false },
			OnData:  func(cb func([]byte)) { onData = cb },
			OnClose: func(func(int)) {},
			Alive:   func() bool { return alive },
		}
	})
	defer SetBridgeFactoryForTests(nil)
	defer CleanupAllSessionState()

	result, err := RunChat(context.Background(), "tok", map[string]any{
		"model":    "composer-2.5",
		"messages": []any{map[string]any{"role": "user", "content": "list files"}},
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "ls", "parameters": map[string]any{"type": "object"}}}},
		"stream":   true,
	}, true, ClientCLI)
	if err != nil || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	joined := ""
	for ev := range result.Stream {
		joined += marshalJSON(ev)
	}
	if !containsStr(joined, "ls") || !containsStr(joined, "tool_calls") {
		t.Fatalf("%s", joined)
	}

	result, err = RunChat(context.Background(), "tok", map[string]any{
		"model": "composer-2.5",
		"messages": []any{
			map[string]any{"role": "user", "content": "list files"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "ls", "arguments": "{}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "a.ts"},
		},
		"stream": true,
	}, true, ClientCLI)
	if err != nil {
		t.Fatal(err)
	}
	foundResult := false
	for _, m := range clientMsgs {
		if m.GetExecClientMessage() != nil && m.GetExecClientMessage().GetMcpResult() != nil {
			foundResult = true
		}
	}
	if !foundResult {
		t.Fatalf("no mcp result in %+v", clientMsgs)
	}
}

func TestResumeToolsLateProtocolErrorDoesNotPanic(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	var onData func([]byte)
	var onClose func(int)
	mcpResult := make(chan struct{})
	SetBridgeFactoryForTests(func(_, _, _ string, _ bool, _ ClientKind) *Bridge {
		alive := true
		return &Bridge{
			Write: func(frame []byte) {
				if len(frame) < 5 {
					return
				}
				var msg agentpb.AgentClientMessage
				if proto.Unmarshal(frame[5:], &msg) != nil {
					return
				}
				if msg.GetRunRequest() != nil && onData != nil {
					go func() {
						onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
							Message: &agentpb.AgentServerMessage_ExecServerMessage{
								ExecServerMessage: &agentpb.ExecServerMessage{
									Id: 3, ExecId: "e1",
									Message: &agentpb.ExecServerMessage_McpArgs{McpArgs: &agentpb.McpArgs{
										Name: "ls", ToolName: "ls", ToolCallId: "c1",
									}},
								},
							},
						}), 0))
					}()
				}
				if msg.GetExecClientMessage() != nil && msg.GetExecClientMessage().GetMcpResult() != nil {
					select {
					case <-mcpResult:
					default:
						close(mcpResult)
					}
				}
			},
			End:     func() { alive = false },
			OnData:  func(cb func([]byte)) { onData = cb },
			OnClose: func(cb func(int)) { onClose = cb },
			Alive:   func() bool { return alive },
		}
	})
	defer SetBridgeFactoryForTests(nil)
	defer CleanupAllSessionState()

	result, err := RunChat(context.Background(), "tok", map[string]any{
		"model":    "composer-2.5",
		"messages": []any{map[string]any{"role": "user", "content": "list files"}},
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "ls", "parameters": map[string]any{"type": "object"}}}},
		"stream":   true,
	}, true, ClientCLI)
	if err != nil || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	for range result.Stream {
	}

	result, err = RunChat(context.Background(), "tok", map[string]any{
		"model": "composer-2.5",
		"messages": []any{
			map[string]any{"role": "user", "content": "list files"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "ls", "arguments": "{}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "a.ts"},
		},
		"stream": true,
	}, true, ClientCLI)
	if err != nil || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}

	select {
	case <-mcpResult:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mcp result")
	}
	if onClose == nil || onData == nil {
		t.Fatal("missing resume callbacks")
	}
	onClose(0)
	for range result.Stream {
	}
	onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
		Message: &agentpb.AgentServerMessage_InteractionQuery{
			InteractionQuery: &agentpb.InteractionQuery{Id: 1},
		},
	}), 0))
}
