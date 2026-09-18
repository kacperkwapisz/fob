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

func TestOverlappingSamePromptGetsNewConversationID(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	var ids []string
	started := make(chan struct{}, 2)
	release := make(chan struct{})
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
				if proto.Unmarshal(frame[5:], &msg) != nil {
					return
				}
				if run := msg.GetRunRequest(); run != nil {
					ids = append(ids, run.GetConversationId())
					select {
					case started <- struct{}{}:
					default:
					}
					go func() {
						<-release
						if onData != nil {
							onData(frameConnect(mustMarshal(&agentpb.AgentServerMessage{
								Message: &agentpb.AgentServerMessage_InteractionUpdate{
									InteractionUpdate: &agentpb.InteractionUpdate{
										Message: &agentpb.InteractionUpdate_TurnEnded{TurnEnded: &agentpb.TurnEndedUpdate{}},
									},
								},
							}), 0))
						}
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

	body := map[string]any{
		"model":    "composer-2.5",
		"messages": []any{map[string]any{"role": "user", "content": "same prompt"}},
		"stream":   true,
	}
	first, err := RunChat(context.Background(), "tok", body, true, ClientCLI)
	if err != nil || first.Stream == nil {
		t.Fatalf("%+v %v", first, err)
	}
	<-started
	second, err := RunChat(context.Background(), "tok", body, true, ClientCLI)
	if err != nil || second.Stream == nil {
		t.Fatalf("%+v %v", second, err)
	}
	close(release)
	for range first.Stream {
	}
	for range second.Stream {
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("overlapping conversation ids %v", ids)
	}
}

func TestConnectErrorDropsConversationState(t *testing.T) {
	t.Setenv("CURSOR_AGENT_URL", "https://agentn.test.cursor.sh")
	var ids []string
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
				if proto.Unmarshal(frame[5:], &msg) != nil {
					return
				}
				if run := msg.GetRunRequest(); run != nil {
					ids = append(ids, run.GetConversationId())
					if onData != nil {
						go func() {
							onData(frameConnect([]byte(`{"error":{"code":"failed_precondition","message":"Error"}}`), connectEndStreamFlag))
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

	body := map[string]any{
		"model":    "cursor-grok-4.6-fast",
		"messages": []any{map[string]any{"role": "user", "content": "Reply with the single word pong."}},
		"stream":   true,
	}
	first, err := RunChat(context.Background(), "tok", body, true, ClientCLI)
	if err != nil || first.Stream == nil {
		t.Fatalf("%+v %v", first, err)
	}
	for range first.Stream {
	}
	second, err := RunChat(context.Background(), "tok", body, true, ClientCLI)
	if err != nil || second.Stream == nil {
		t.Fatalf("%+v %v", second, err)
	}
	for range second.Stream {
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] == ids[1] {
		t.Fatalf("conversation ids %v", ids)
	}
}

func TestRunCursorChatIdleClose(t *testing.T) {
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
									Message: &agentpb.InteractionUpdate_ThinkingDelta{ThinkingDelta: &agentpb.ThinkingDeltaUpdate{Text: "hmm"}},
								},
							},
						}), 0))
						if onClose != nil {
							onClose(closeIdle)
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
		"model": "gpt-5.6-terra-medium", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": true,
	}, true, ClientCLI)
	if err != nil || result.Stream == nil {
		t.Fatalf("%+v %v", result, err)
	}
	joined := ""
	for ev := range result.Stream {
		joined += marshalJSON(ev)
	}
	if !containsStr(joined, "went silent") || !containsStr(joined, "error") {
		t.Fatalf("%s", joined)
	}
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
	if err != nil || result.Status != 502 {
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
	processServer(context.Background(), &agentpb.AgentServerMessage{
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
	processServer(context.Background(), &agentpb.AgentServerMessage{
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

func TestCollectNonStreamMergesToolDeltas(t *testing.T) {
	result := collectNonStream("chatcmpl-x", 1, "composer-2.5", []map[string]any{
		{
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "c1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": `{"q":`},
			}}}, "finish_reason": nil}},
		},
		{
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "function": map[string]any{"arguments": `"x"}`},
			}}}, "finish_reason": nil}},
		},
		{
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
		},
	}, &streamProtoState{})
	msg := translate.AsMap(translate.AsMap(translate.AsArr(translate.AsMap(result.Body)["choices"])[0])["message"])
	calls := translate.AsArr(msg["tool_calls"])
	if len(calls) != 1 {
		t.Fatalf("%+v", msg)
	}
	fn := translate.AsMap(translate.AsMap(calls[0])["function"])
	if translate.AsStr(fn["name"]) != "lookup" || translate.AsStr(fn["arguments"]) != `{"q":"x"}` {
		t.Fatalf("%+v", calls[0])
	}
}

func TestCollectNonStreamKeepsReasoning(t *testing.T) {
	result := collectNonStream("chatcmpl-x", 1, "composer-2.5", []map[string]any{
		{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "created": int64(1), "model": "composer-2.5",
			"choices": []any{map[string]any{"index": 0, "delta": translate.ChatReasoningDelta("hmm"), "finish_reason": nil}},
		},
		{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "created": int64(1), "model": "composer-2.5",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "OK"}, "finish_reason": nil}},
		},
		{
			"id": "chatcmpl-x", "object": "chat.completion.chunk", "created": int64(1), "model": "composer-2.5",
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		},
	}, &streamProtoState{})
	if result.Status != 200 {
		t.Fatalf("%+v", result)
	}
	msg := translate.AsMap(translate.AsMap(translate.AsArr(translate.AsMap(result.Body)["choices"])[0])["message"])
	if translate.AsStr(msg["content"]) != "OK" {
		t.Fatalf("content %+v", msg)
	}
	if translate.MessageReasoning(msg) != "hmm" {
		t.Fatalf("reasoning %+v", msg)
	}
	if translate.AsStr(msg["reasoning_content"]) == "" || translate.AsStr(msg["reasoning"]) == "" {
		t.Fatalf("dual fields %+v", msg)
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

func TestRunCursorChatStreamsThinking(t *testing.T) {
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
									Message: &agentpb.InteractionUpdate_ThinkingDelta{ThinkingDelta: &agentpb.ThinkingDeltaUpdate{Text: "hmm"}},
								},
							},
						}), 0))
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

	stream, err := RunChat(context.Background(), "tok", map[string]any{
		"model": "composer-2.5", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": true,
	}, true, ClientCLI)
	if err != nil || stream.Stream == nil {
		t.Fatalf("%+v %v", stream, err)
	}
	joined := ""
	for ev := range stream.Stream {
		joined += marshalJSON(ev)
	}
	if !containsStr(joined, `"reasoning_content":"hmm"`) || !containsStr(joined, `"reasoning":"hmm"`) {
		t.Fatalf("stream %s", joined)
	}
	if !containsStr(joined, `"content":"OK"`) {
		t.Fatalf("content %s", joined)
	}

	CleanupAllSessionState()
	nostream, err := RunChat(context.Background(), "tok", map[string]any{
		"model": "composer-2.5", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "stream": false,
	}, false, ClientCLI)
	if err != nil || nostream.Status != 200 {
		t.Fatalf("%+v %v", nostream, err)
	}
	msg := translate.AsMap(translate.AsMap(translate.AsArr(translate.AsMap(nostream.Body)["choices"])[0])["message"])
	if translate.MessageReasoning(msg) != "hmm" || translate.AsStr(msg["content"]) != "OK" {
		t.Fatalf("nostream %+v", nostream.Body)
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

func TestHandleInteractionQueryDoesNotProtocolError(t *testing.T) {
	cases := []struct {
		name string
		q    *agentpb.InteractionQuery
		want func(*agentpb.InteractionResponse) bool
	}{
		{
			name: "webSearch",
			q: &agentpb.InteractionQuery{Id: 7, Query: &agentpb.InteractionQuery_WebSearchRequestQuery{
				WebSearchRequestQuery: &agentpb.WebSearchRequestQuery{Args: &agentpb.WebSearchArgs{SearchTerm: "go generics"}},
			}},
			want: func(r *agentpb.InteractionResponse) bool {
				return r.GetId() == 7 && r.GetWebSearchRequestResponse().GetApproved() != nil
			},
		},
		{
			name: "exaSearch",
			q: &agentpb.InteractionQuery{Id: 8, Query: &agentpb.InteractionQuery_ExaSearchRequestQuery{
				ExaSearchRequestQuery: &agentpb.ExaSearchRequestQuery{},
			}},
			want: func(r *agentpb.InteractionResponse) bool {
				return r.GetId() == 8 && r.GetExaSearchRequestResponse().GetApproved() != nil
			},
		},
		{
			name: "askQuestion",
			q: &agentpb.InteractionQuery{Id: 9, Query: &agentpb.InteractionQuery_AskQuestionInteractionQuery{
				AskQuestionInteractionQuery: &agentpb.AskQuestionInteractionQuery{},
			}},
			want: func(r *agentpb.InteractionResponse) bool {
				return r.GetId() == 9 && r.GetAskQuestionInteractionResponse().GetResult().GetRejected() != nil
			},
		},
		{
			name: "unknown",
			q:    &agentpb.InteractionQuery{Id: 1},
			want: func(r *agentpb.InteractionResponse) bool { return r.GetId() == 1 },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var wrote [][]byte
			var errs []string
			bridge := &Bridge{Write: func(b []byte) { wrote = append(wrote, append([]byte(nil), b...)) }, End: func() {}, Alive: func() bool { return true }}
			processServer(context.Background(), &agentpb.AgentServerMessage{
				Message: &agentpb.AgentServerMessage_InteractionQuery{InteractionQuery: tc.q},
			}, map[string][]byte{}, nil, bridge, &streamProtoState{}, nil, nil, nil, func(msg string) {
				errs = append(errs, msg)
			})
			if len(errs) != 0 {
				t.Fatalf("protocol error %v", errs)
			}
			if len(wrote) != 1 {
				t.Fatalf("wrote %d", len(wrote))
			}
			var msg agentpb.AgentClientMessage
			if err := proto.Unmarshal(wrote[0][5:], &msg); err != nil {
				t.Fatal(err)
			}
			if !tc.want(msg.GetInteractionResponse()) {
				t.Fatalf("%+v", msg.GetInteractionResponse())
			}
		})
	}
}

func TestHandleExecRejectsNewCursorTools(t *testing.T) {
	var wrote [][]byte
	bridge := &Bridge{Write: func(b []byte) { wrote = append(wrote, append([]byte(nil), b...)) }, End: func() {}, Alive: func() bool { return true }}
	cases := []*agentpb.ExecServerMessage{
		{Id: 1, ExecId: "e1", Message: &agentpb.ExecServerMessage_ListMcpResourcesExecArgs{ListMcpResourcesExecArgs: &agentpb.ListMcpResourcesExecArgs{}}},
		{Id: 2, ExecId: "e2", Message: &agentpb.ExecServerMessage_ReadMcpResourceExecArgs{ReadMcpResourceExecArgs: &agentpb.ReadMcpResourceExecArgs{Uri: "mcp://x"}}},
		{Id: 3, ExecId: "e3", Message: &agentpb.ExecServerMessage_RecordScreenArgs{RecordScreenArgs: &agentpb.RecordScreenArgs{}}},
		{Id: 4, ExecId: "e4", Message: &agentpb.ExecServerMessage_ComputerUseArgs{ComputerUseArgs: &agentpb.ComputerUseArgs{}}},
	}
	for _, exec := range cases {
		wrote = nil
		if !handleExec(exec, nil, bridge, nil) {
			t.Fatalf("unhandled %s", execKind(exec))
		}
		if len(wrote) != 1 {
			t.Fatalf("%s wrote %d", execKind(exec), len(wrote))
		}
	}
}

func TestUnknownExecThrowsInsteadOfProtocolError(t *testing.T) {
	var wrote [][]byte
	var errs []string
	bridge := &Bridge{Write: func(b []byte) { wrote = append(wrote, append([]byte(nil), b...)) }, End: func() {}, Alive: func() bool { return true }}
	exec := &agentpb.ExecServerMessage{Id: 9, ExecId: "e9"}
	raw, err := proto.MarshalOptions{}.MarshalAppend(nil, exec)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, 0x9a, 0x06, 0x01, 'x') // field 99, bytes, "x"
	if err := proto.Unmarshal(raw, exec); err != nil {
		t.Fatal(err)
	}
	if execKind(exec) == "unknown" {
		t.Fatalf("kind %s", execKind(exec))
	}
	processServer(context.Background(), &agentpb.AgentServerMessage{
		Message: &agentpb.AgentServerMessage_ExecServerMessage{ExecServerMessage: exec},
	}, map[string][]byte{}, nil, bridge, &streamProtoState{}, nil, nil, nil, func(msg string) {
		errs = append(errs, msg)
	})
	if len(errs) != 0 {
		t.Fatalf("protocol error %v", errs)
	}
	if len(wrote) != 1 {
		t.Fatalf("wrote %d", len(wrote))
	}
	var msg agentpb.AgentClientMessage
	if err := proto.Unmarshal(wrote[0][5:], &msg); err != nil {
		t.Fatal(err)
	}
	if msg.GetExecClientControlMessage() == nil || msg.GetExecClientControlMessage().GetThrow() == nil {
		t.Fatalf("%+v", msg.GetExecClientControlMessage())
	}
}
