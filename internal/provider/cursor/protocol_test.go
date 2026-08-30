package cursor

import (
	"context"
	"testing"

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
