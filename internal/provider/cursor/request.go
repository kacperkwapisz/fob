package cursor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/kacperkwapisz/fob/internal/provider/cursor/agentpb"
	"github.com/kacperkwapisz/fob/internal/translate"
)

type requestPayload struct {
	requestBytes []byte
	blobStore    map[string][]byte
	mcpTools     []*agentpb.McpToolDefinition
}

type requestedModelSelection struct {
	ModelID    string
	MaxMode    bool
	Parameters []struct{ ID, Value string }
}

func storeAsBlob(data []byte, store map[string][]byte) []byte {
	sum := sha256.Sum256(data)
	id := append([]byte(nil), sum[:]...)
	store[hex.EncodeToString(id)] = append([]byte(nil), data...)
	return id
}

func buildSelectedContextBlob(rootPromptBlobIDs [][]byte, clientName string) []byte {
	var parts []byte
	for _, blobID := range rootPromptBlobIDs {
		parts = append(parts, 0x0A, byte(len(blobID)))
		parts = append(parts, blobID...)
	}
	client := []byte(clientName)
	parts = append(parts, 0xB2, 0x01, byte(len(client)))
	parts = append(parts, client...)
	return parts
}

func createUserMessage(text string, selectedContextBlob []byte, images []ImagePart) *agentpb.UserMessage {
	id := randomUUID()
	var selected []*agentpb.SelectedImage
	for _, image := range images {
		selected = append(selected, &agentpb.SelectedImage{
			Uuid:     image.UUID,
			Path:     "",
			MimeType: image.MimeType,
			DataOrBlobId: &agentpb.SelectedImage_Data{
				Data: image.Data,
			},
		})
	}
	return &agentpb.UserMessage{
		Text:                text,
		MessageId:           id,
		SelectedContext:     &agentpb.SelectedContext{SelectedImages: selected},
		Mode:                1,
		SelectedContextBlob: selectedContextBlob,
		CorrelationId:       id,
	}
}

func encodeMcpArgs(args map[string]any) map[string][]byte {
	out := map[string][]byte{}
	for k, v := range args {
		out[k] = encodeProtoValue(v)
	}
	return out
}

func encodeProtoValue(v any) []byte {
	pv, err := structpb.NewValue(v)
	if err != nil {
		return []byte(translate.AsStr(v))
	}
	b, err := proto.Marshal(pv)
	if err != nil {
		return []byte(translate.AsStr(v))
	}
	return b
}

func decodeProtoValue(raw []byte) any {
	var pv structpb.Value
	if err := proto.Unmarshal(raw, &pv); err != nil {
		return string(raw)
	}
	return pv.AsInterface()
}

func decodeMcpArgs(args map[string][]byte) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = decodeProtoValue(v)
	}
	return out
}

func buildMcpTools(tools []any) []*agentpb.McpToolDefinition {
	var out []*agentpb.McpToolDefinition
	for _, t := range tools {
		tool := translate.AsMap(t)
		fn := translate.AsMap(tool["function"])
		name := translate.AsStr(fn["name"])
		if name == "" {
			name = translate.AsStr(tool["name"])
		}
		params := fn["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}}
		}
		out = append(out, &agentpb.McpToolDefinition{
			Name:               name,
			Description:        translate.AsStr(fn["description"]),
			ProviderIdentifier: MCPProvider,
			ToolName:           name,
			InputSchema:        encodeProtoValue(params),
		})
	}
	return out
}

func buildTurnStepBytes(step ParsedTurnStep) []byte {
	if text, ok := step.(ParsedAssistantTextStep); ok {
		b, _ := proto.Marshal(&agentpb.ConversationStep{
			Message: &agentpb.ConversationStep_AssistantMessage{
				AssistantMessage: &agentpb.AssistantMessage{Text: text.Text},
			},
		})
		return b
	}
	tc, ok := step.(*ParsedToolCallStep)
	if !ok {
		if v, ok := step.(ParsedToolCallStep); ok {
			tc = &v
		} else {
			return nil
		}
	}
	toolName := tc.ToolName
	if toolName == "" {
		toolName = "tool"
	}
	call := &agentpb.McpToolCall{
		Args: &agentpb.McpArgs{
			Name:               toolName,
			Args:               encodeMcpArgs(tc.Arguments),
			ToolCallId:         tc.ToolCallID,
			ProviderIdentifier: MCPProvider,
			ToolName:           toolName,
		},
	}
	if tc.Result != nil {
		if tc.Result.IsError {
			call.Result = &agentpb.McpToolResult{Result: &agentpb.McpToolResult_Error{Error: &agentpb.McpToolError{Error: tc.Result.Content}}}
		} else {
			call.Result = &agentpb.McpToolResult{Result: &agentpb.McpToolResult_Success{Success: &agentpb.McpSuccess{
				Content: []*agentpb.McpToolResultContentItem{{
					Content: &agentpb.McpToolResultContentItem_Text{Text: &agentpb.McpTextContent{Text: tc.Result.Content}},
				}},
			}}}
		}
	}
	b, _ := proto.Marshal(&agentpb.ConversationStep{
		Message: &agentpb.ConversationStep_ToolCall{ToolCall: &agentpb.ToolCall{
			Tool: &agentpb.ToolCall_McpToolCall{McpToolCall: call},
		}},
	})
	return b
}

func buildCursorRequest(
	modelID, systemPrompt, userText string,
	turns []ParsedTurn,
	conversationID string,
	checkpoint []byte,
	existing map[string][]byte,
	selection *requestedModelSelection,
	mcpTools []*agentpb.McpToolDefinition,
	resume bool,
	images []ImagePart,
) requestPayload {
	blobStore := map[string][]byte{}
	for k, v := range existing {
		blobStore[k] = append([]byte(nil), v...)
	}
	systemBytes, _ := json.Marshal(map[string]any{"role": "system", "content": systemPrompt})
	systemBlobID := storeAsBlob(systemBytes, blobStore)
	selectedCtxBlob := storeAsBlob(buildSelectedContextBlob([][]byte{systemBlobID}, MCPProvider), blobStore)

	var state *agentpb.ConversationStateStructure
	if len(checkpoint) > 0 {
		state = &agentpb.ConversationStateStructure{}
		_ = proto.Unmarshal(checkpoint, state)
	} else {
		cwd, _ := os.Getwd()
		mode := int32(1)
		var turnBlobIDs [][]byte
		for _, turn := range turns {
			userMsg := createUserMessage(turn.UserText, selectedCtxBlob, nil)
			userBytes, _ := proto.Marshal(userMsg)
			userBlob := storeAsBlob(userBytes, blobStore)
			var stepBlobs [][]byte
			for _, s := range turn.Steps {
				stepBlobs = append(stepBlobs, storeAsBlob(buildTurnStepBytes(s), blobStore))
			}
			reqID := randomUUID()
			agentTurn := &agentpb.AgentConversationTurnStructure{
				UserMessage: userBlob,
				Steps:       stepBlobs,
				RequestId:   &reqID,
			}
			turnStruct := &agentpb.ConversationTurnStructure{
				Turn: &agentpb.ConversationTurnStructure_AgentConversationTurn{AgentConversationTurn: agentTurn},
			}
			tb, _ := proto.Marshal(turnStruct)
			turnBlobIDs = append(turnBlobIDs, storeAsBlob(tb, blobStore))
		}
		state = &agentpb.ConversationStateStructure{
			RootPromptMessagesJson: [][]byte{systemBlobID},
			Turns:                  turnBlobIDs,
			Todos:                  [][]byte{},
			PendingToolCalls:       []string{},
			PreviousWorkspaceUris:  []string{"file://" + cwd},
			Mode:                   &mode,
			FileStates:             map[string][]byte{},
			FileStatesV2:           map[string]*agentpb.FileStateStructure{},
			SummaryArchives:        [][]byte{},
			TurnTimings:            []*agentpb.StepTiming{},
			SubagentStates:         map[string]*agentpb.SubagentPersistedState{},
			ReadPaths:              []string{},
			ClientName:             MCPProvider,
		}
	}

	userMessage := createUserMessage(userText, selectedCtxBlob, images)
	action := &agentpb.ConversationAction{}
	if resume && len(checkpoint) > 0 {
		action.Action = &agentpb.ConversationAction_ResumeAction{ResumeAction: &agentpb.ResumeAction{}}
	} else {
		action.Action = &agentpb.ConversationAction_UserMessageAction{UserMessageAction: &agentpb.UserMessageAction{UserMessage: userMessage}}
	}

	run := &agentpb.AgentRunRequest{
		ConversationState: state,
		Action:            action,
		McpTools:          &agentpb.McpTools{McpTools: mcpTools},
		ConversationId:    proto.String(conversationID),
	}
	if selection != nil {
		var params []*agentpb.RequestedModelModelParameterbytes
		for _, p := range selection.Parameters {
			params = append(params, &agentpb.RequestedModelModelParameterbytes{Id: p.ID, Value: p.Value})
		}
		run.RequestedModel = &agentpb.RequestedModel{ModelId: selection.ModelID, MaxMode: selection.MaxMode, Parameters: params}
	} else {
		run.ModelDetails = &agentpb.ModelDetails{ModelId: modelID, DisplayModelId: modelID, DisplayName: modelID}
	}
	clientMsg := &agentpb.AgentClientMessage{Message: &agentpb.AgentClientMessage_RunRequest{RunRequest: run}}
	bytes, _ := proto.Marshal(clientMsg)
	return requestPayload{requestBytes: bytes, blobStore: blobStore, mcpTools: mcpTools}
}

func requestHashOf(model string, messages, tools, requested any) string {
	raw, _ := json.Marshal(map[string]any{"model": model, "messages": messages, "tools": tools, "requestedModel": requested})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func heartbeatBytes() []byte {
	b, _ := proto.Marshal(&agentpb.AgentClientMessage{
		Message: &agentpb.AgentClientMessage_ClientHeartbeat{ClientHeartbeat: &agentpb.ClientHeartbeat{}},
	})
	return frameConnect(b, 0)
}

func cancelBytes() []byte {
	b, _ := proto.Marshal(&agentpb.AgentClientMessage{
		Message: &agentpb.AgentClientMessage_ConversationAction{
			ConversationAction: &agentpb.ConversationAction{
				Action: &agentpb.ConversationAction_CancelAction{CancelAction: &agentpb.CancelAction{}},
			},
		},
	})
	return frameConnect(b, 0)
}
