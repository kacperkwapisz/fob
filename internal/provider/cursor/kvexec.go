package cursor

import (
	"encoding/hex"
	"encoding/json"

	"google.golang.org/protobuf/proto"

	"github.com/kacperkwapisz/fob/internal/provider/cursor/agentpb"
)

const rejectReason = "Tool not available in this environment. Use the MCP tools provided instead."

type pendingExec struct {
	execID      string
	execMsgID   uint32
	toolCallID  string
	toolName    string
	decodedArgs string
}

type streamProtoState struct {
	toolIndex          int
	pending            []pendingExec
	outputTokens       int
	totalTokens        int
	turnEnded          bool
	terminalCheckpoint bool
	turnUsage          *turnUsage
}

type turnUsage struct {
	input, output, cacheRead, cacheWrite, reasoning int
}

func sendClient(bridge *Bridge, msg *agentpb.AgentClientMessage) {
	b, err := proto.Marshal(msg)
	if err != nil {
		return
	}
	bridge.Write(frameConnect(b, 0))
}

func handleKv(kv *agentpb.KvServerMessage, blobStore map[string][]byte, bridge *Bridge) bool {
	if args := kv.GetGetBlobArgs(); args != nil {
		key := hex.EncodeToString(args.GetBlobId())
		res := &agentpb.GetBlobResult{}
		if data, ok := blobStore[key]; ok {
			res.BlobData = data
		}
		sendClient(bridge, &agentpb.AgentClientMessage{
			Message: &agentpb.AgentClientMessage_KvClientMessage{KvClientMessage: &agentpb.KvClientMessage{
				Id:      kv.GetId(),
				Message: &agentpb.KvClientMessage_GetBlobResult{GetBlobResult: res},
			}},
		})
		return true
	}
	if args := kv.GetSetBlobArgs(); args != nil {
		blobStore[hex.EncodeToString(args.GetBlobId())] = append([]byte(nil), args.GetBlobData()...)
		sendClient(bridge, &agentpb.AgentClientMessage{
			Message: &agentpb.AgentClientMessage_KvClientMessage{KvClientMessage: &agentpb.KvClientMessage{
				Id:      kv.GetId(),
				Message: &agentpb.KvClientMessage_SetBlobResult{SetBlobResult: &agentpb.SetBlobResult{}},
			}},
		})
		return true
	}
	return false
}

func sendExec(bridge *Bridge, exec *agentpb.ExecServerMessage, client *agentpb.ExecClientMessage) {
	client.Id = exec.GetId()
	client.ExecId = exec.GetExecId()
	sendClient(bridge, &agentpb.AgentClientMessage{
		Message: &agentpb.AgentClientMessage_ExecClientMessage{ExecClientMessage: client},
	})
}

func handleExec(exec *agentpb.ExecServerMessage, mcpTools []*agentpb.McpToolDefinition, bridge *Bridge, onMcp func(pendingExec)) bool {
	if exec.GetRequestContextArgs() != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_RequestContextResult{RequestContextResult: &agentpb.RequestContextResult{
			Result: &agentpb.RequestContextResult_Success{Success: &agentpb.RequestContextSuccess{
				RequestContext: &agentpb.RequestContext{
					Tools:        mcpTools,
					FileContents: map[string]string{},
				},
			}},
		}}})
		return true
	}
	if args := exec.GetMcpArgs(); args != nil {
		decoded := decodeMcpArgs(args.GetArgs())
		raw, _ := json.Marshal(decoded)
		id := args.GetToolCallId()
		if id == "" {
			id = randomUUID()
		}
		name := args.GetToolName()
		if name == "" {
			name = args.GetName()
		}
		onMcp(pendingExec{
			execID: exec.GetExecId(), execMsgID: exec.GetId(),
			toolCallID: id, toolName: name, decodedArgs: string(raw),
		})
		return true
	}
	if args := exec.GetReadArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_ReadResult{ReadResult: &agentpb.ReadResult{
			Result: &agentpb.ReadResult_Rejected{Rejected: &agentpb.ReadRejected{Path: args.GetPath(), Reason: rejectReason}},
		}}})
		return true
	}
	if args := exec.GetLsArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_LsResult{LsResult: &agentpb.LsResult{
			Result: &agentpb.LsResult_Rejected{Rejected: &agentpb.LsRejected{Path: args.GetPath(), Reason: rejectReason}},
		}}})
		return true
	}
	if exec.GetGrepArgs() != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_GrepResult{GrepResult: &agentpb.GrepResult{
			Result: &agentpb.GrepResult_Error{Error: &agentpb.GrepError{Error: rejectReason}},
		}}})
		return true
	}
	if args := exec.GetWriteArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_WriteResult{WriteResult: &agentpb.WriteResult{
			Result: &agentpb.WriteResult_Rejected{Rejected: &agentpb.WriteRejected{Path: args.GetPath(), Reason: rejectReason}},
		}}})
		return true
	}
	if args := exec.GetDeleteArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_DeleteResult{DeleteResult: &agentpb.DeleteResult{
			Result: &agentpb.DeleteResult_Rejected{Rejected: &agentpb.DeleteRejected{Path: args.GetPath(), Reason: rejectReason}},
		}}})
		return true
	}
	if args := exec.GetShellArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_ShellResult{ShellResult: &agentpb.ShellResult{
			Result: &agentpb.ShellResult_Rejected{Rejected: &agentpb.ShellRejected{
				Command: args.GetCommand(), WorkingDirectory: args.GetWorkingDirectory(), Reason: rejectReason,
			}},
		}}})
		return true
	}
	if args := exec.GetShellStreamArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_ShellStream{ShellStream: &agentpb.ShellStream{
			Event: &agentpb.ShellStream_Rejected{Rejected: &agentpb.ShellRejected{
				Command: args.GetCommand(), WorkingDirectory: args.GetWorkingDirectory(), Reason: rejectReason,
			}},
		}}})
		return true
	}
	if args := exec.GetBackgroundShellSpawnArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_BackgroundShellSpawnResult{BackgroundShellSpawnResult: &agentpb.BackgroundShellSpawnResult{
			Result: &agentpb.BackgroundShellSpawnResult_Rejected{Rejected: &agentpb.ShellRejected{
				Command: args.GetCommand(), WorkingDirectory: args.GetWorkingDirectory(), Reason: rejectReason,
			}},
		}}})
		return true
	}
	if exec.GetWriteShellStdinArgs() != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_WriteShellStdinResult{WriteShellStdinResult: &agentpb.WriteShellStdinResult{
			Result: &agentpb.WriteShellStdinResult_Error{Error: &agentpb.WriteShellStdinError{Error: rejectReason}},
		}}})
		return true
	}
	if args := exec.GetFetchArgs(); args != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_FetchResult{FetchResult: &agentpb.FetchResult{
			Result: &agentpb.FetchResult_Error{Error: &agentpb.FetchError{Url: args.GetUrl(), Error: rejectReason}},
		}}})
		return true
	}
	if exec.GetDiagnosticsArgs() != nil {
		sendExec(bridge, exec, &agentpb.ExecClientMessage{Message: &agentpb.ExecClientMessage_DiagnosticsResult{DiagnosticsResult: &agentpb.DiagnosticsResult{}}})
		return true
	}
	return false
}

func processServer(
	msg *agentpb.AgentServerMessage,
	blobStore map[string][]byte,
	mcpTools []*agentpb.McpToolDefinition,
	bridge *Bridge,
	state *streamProtoState,
	onText func(text string, thinking bool),
	onMcp func(pendingExec),
	onCheckpoint func([]byte),
	onProtocolError func(string),
) {
	if upd := msg.GetInteractionUpdate(); upd != nil {
		if td := upd.GetTextDelta(); td != nil && td.GetText() != "" {
			onText(td.GetText(), false)
		}
		if th := upd.GetThinkingDelta(); th != nil && th.GetText() != "" {
			onText(th.GetText(), true)
		}
		if tok := upd.GetTokenDelta(); tok != nil {
			state.outputTokens += int(tok.GetTokens())
		}
		if te := upd.GetTurnEnded(); te != nil {
			state.turnEnded = true
			state.turnUsage = readTurnUsage(te)
		}
		return
	}
	if kv := msg.GetKvServerMessage(); kv != nil {
		if !handleKv(kv, blobStore, bridge) {
			onProtocolError("Unsupported Cursor KV message")
		}
		return
	}
	if exec := msg.GetExecServerMessage(); exec != nil {
		if !handleExec(exec, mcpTools, bridge, onMcp) {
			onProtocolError("Unsupported Cursor exec message")
		}
		return
	}
	if cp := msg.GetConversationCheckpointUpdate(); cp != nil {
		if td := cp.GetTokenDetails(); td != nil {
			state.totalTokens = int(td.GetUsedTokens())
		}
		if onCheckpoint != nil {
			b, _ := proto.Marshal(cp)
			onCheckpoint(b)
		}
		return
	}
	if msg.GetInteractionQuery() != nil {
		onProtocolError("Unsupported Cursor server message: interactionQuery")
	}
}

func readTurnUsage(te *agentpb.TurnEndedUpdate) *turnUsage {
	u := &turnUsage{}
	if te == nil {
		return u
	}
	unknown := te.ProtoReflect().GetUnknown()
	fields := map[int]int{}
	for len(unknown) > 0 {
		fieldNum, wire, n := consumeTag(unknown)
		unknown = unknown[n:]
		if wire != 0 {
			break
		}
		val, n := consumeVarint(unknown)
		unknown = unknown[n:]
		fields[fieldNum] = int(val)
	}
	if v, ok := fields[1]; ok {
		u.input = v
	}
	if v, ok := fields[2]; ok {
		u.output = v
	}
	if v, ok := fields[3]; ok {
		u.cacheRead = v
	}
	if v, ok := fields[4]; ok {
		u.cacheWrite = v
	}
	if v, ok := fields[5]; ok {
		u.reasoning = v
	}
	return u
}

func consumeTag(b []byte) (field, wire, n int) {
	v, n := consumeVarint(b)
	return int(v >> 3), int(v & 7), n
}

func consumeVarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
		if s > 63 {
			return 0, i + 1
		}
	}
	return 0, len(b)
}

func computeUsage(state *streamProtoState) map[string]any {
	if state.turnUsage != nil {
		u := state.turnUsage
		prompt := u.input + u.cacheRead + u.cacheWrite
		completion := u.output + u.reasoning
		return map[string]any{
			"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion,
			"prompt_tokens_details":     map[string]any{"cached_tokens": u.cacheRead, "cache_write_tokens": u.cacheWrite},
			"completion_tokens_details": map[string]any{"reasoning_tokens": u.reasoning},
		}
	}
	completion := state.outputTokens
	total := state.totalTokens
	if total == 0 {
		total = completion
	}
	prompt := total - completion
	if prompt < 0 {
		prompt = 0
	}
	return map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": total}
}

func sendMcpResult(bridge *Bridge, exec pendingExec, content string) {
	sendClient(bridge, &agentpb.AgentClientMessage{
		Message: &agentpb.AgentClientMessage_ExecClientMessage{ExecClientMessage: &agentpb.ExecClientMessage{
			Id:     exec.execMsgID,
			ExecId: exec.execID,
			Message: &agentpb.ExecClientMessage_McpResult{McpResult: &agentpb.McpResult{
				Result: &agentpb.McpResult_Success{Success: &agentpb.McpSuccess{
					Content: []*agentpb.McpToolResultContentItem{{
						Content: &agentpb.McpToolResultContentItem_Text{Text: &agentpb.McpTextContent{Text: content}},
					}},
				}},
			}},
		}},
	})
}
