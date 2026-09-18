package translate

const (
	blockThinking = "thinking"
	blockText     = "text"
	blockTool     = "tool"
)

func ChatReasoning(v any) string {
	if s := AsStr(v); s != "" {
		return s
	}
	m := AsMap(v)
	if s := AsStr(m["content"]); s != "" {
		return s
	}
	return AsStr(m["text"])
}

func DeltaReasoning(delta map[string]any) string {
	if s := ChatReasoning(delta["reasoning_content"]); s != "" {
		return s
	}
	return ChatReasoning(delta["reasoning"])
}

func MessageReasoning(msg map[string]any) string {
	if s := ChatReasoning(msg["reasoning_content"]); s != "" {
		return s
	}
	return ChatReasoning(msg["reasoning"])
}

func ApplyChatReasoning(dst map[string]any, text string) {
	if dst == nil || text == "" {
		return
	}
	dst["reasoning_content"] = text
	dst["reasoning"] = text
}

func ChatReasoningDelta(text string) map[string]any {
	out := map[string]any{}
	ApplyChatReasoning(out, text)
	return out
}

func reasoningOutputItem(text string) map[string]any {
	return map[string]any{
		"type":    "reasoning",
		"summary": []any{map[string]any{"type": "summary_text", "text": text}},
		"content": []any{map[string]any{"type": "reasoning_text", "text": text}},
	}
}

func thinkingBlock(text string) map[string]any {
	return map[string]any{"type": "thinking", "thinking": text}
}

func responsesReasoningDeltas(text string) []string {
	if text == "" {
		return nil
	}
	return []string{
		sse("response.reasoning_summary_text.delta", map[string]any{"type": "response.reasoning_summary_text.delta", "delta": text}),
		sse("response.reasoning_text.delta", map[string]any{"type": "response.reasoning_text.delta", "delta": text}),
	}
}

func isResponsesReasoningDelta(typ string) bool {
	switch typ {
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning.delta":
		return true
	default:
		return false
	}
}

func claudeOpenBlock(state *StreamState, kind string) []string {
	if state.BlockKind == kind {
		return nil
	}
	var lines []string
	if state.BlockKind != "" {
		lines = append(lines, sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": state.BlockIndex}))
		state.BlockIndex++
	}
	block := map[string]any{"type": "text", "text": ""}
	if kind == blockThinking {
		block = map[string]any{"type": "thinking", "thinking": ""}
	}
	lines = append(lines, sse("content_block_start", map[string]any{
		"type": "content_block_start", "index": state.BlockIndex, "content_block": block,
	}))
	state.BlockKind = kind
	return lines
}

func claudeCloseBlock(state *StreamState) []string {
	if state.BlockKind == "" {
		return nil
	}
	lines := []string{sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": state.BlockIndex})}
	state.BlockKind = ""
	return lines
}

func claudeFinishBlocks(state *StreamState) []string {
	if state.BlockKind != "" {
		return claudeCloseBlock(state)
	}
	if state.HasTools {
		return nil
	}
	return append(claudeOpenBlock(state, blockText), claudeCloseBlock(state)...)
}

func claudeOpenTool(state *StreamState, id, name string) []string {
	var lines []string
	if state.BlockKind != "" {
		lines = append(lines, sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": state.BlockIndex}))
		state.BlockIndex++
	}
	lines = append(lines, sse("content_block_start", map[string]any{
		"type": "content_block_start", "index": state.BlockIndex,
		"content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}},
	}))
	state.BlockKind = blockTool
	state.HasTools = true
	return lines
}

func claudeToolDelta(state *StreamState, args string) []string {
	if args == "" || state.BlockKind != blockTool {
		return nil
	}
	return []string{sse("content_block_delta", map[string]any{
		"type": "content_block_delta", "index": state.BlockIndex,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
	})}
}

func emitChatToolsToResponses(delta map[string]any, state *StreamState) []string {
	var lines []string
	for _, raw := range AsArr(delta["tool_calls"]) {
		tc := AsMap(raw)
		fn := AsMap(tc["function"])
		id := AsStr(tc["id"])
		name := AsStr(fn["name"])
		args := AsStr(fn["arguments"])
		if id != "" || name != "" {
			if id == "" {
				id = "call_" + itoa(int64(state.ToolIndex))
			}
			item := map[string]any{"type": "function_call", "call_id": id, "name": name, "arguments": args}
			state.Tools = append(state.Tools, item)
			state.ToolIndex++
			state.HasTools = true
			lines = append(lines, sse("response.output_item.added", map[string]any{
				"type": "response.output_item.added",
				"item": map[string]any{"type": "function_call", "call_id": id, "name": name, "arguments": ""},
			}))
		} else if args != "" && len(state.Tools) > 0 {
			last := AsMap(state.Tools[len(state.Tools)-1])
			last["arguments"] = AsStr(last["arguments"]) + args
		}
		if args != "" {
			lines = append(lines, sse("response.function_call_arguments.delta", map[string]any{
				"type": "response.function_call_arguments.delta", "delta": args,
			}))
		}
	}
	return lines
}

func emitChatToolsToClaude(delta map[string]any, state *StreamState) []string {
	var lines []string
	for _, raw := range AsArr(delta["tool_calls"]) {
		tc := AsMap(raw)
		fn := AsMap(tc["function"])
		id := AsStr(tc["id"])
		name := AsStr(fn["name"])
		args := AsStr(fn["arguments"])
		if id != "" || name != "" {
			if id == "" {
				id = "call_" + itoa(int64(state.ToolIndex))
			}
			lines = append(lines, claudeOpenTool(state, id, name)...)
			state.ToolIndex++
		}
		lines = append(lines, claudeToolDelta(state, args)...)
	}
	return lines
}

func emitResponsesToolToClaude(item map[string]any, state *StreamState) []string {
	if AsStr(item["type"]) != "function_call" {
		return nil
	}
	id := AsStr(item["call_id"])
	if id == "" {
		id = AsStr(item["id"])
	}
	return claudeOpenTool(state, id, AsStr(item["name"]))
}

func assembleResponsesOutput(state *StreamState) []any {
	var out []any
	if state.Reasoning != "" {
		out = append(out, reasoningOutputItem(state.Reasoning))
	}
	if state.Text != "" {
		out = append(out, map[string]any{
			"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": state.Text}},
		})
	}
	return append(out, state.Tools...)
}

func responsesCompletedEvent(model, status string, state *StreamState) string {
	return sse("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": state.ID, "object": "response", "model": model, "status": status,
			"output": assembleResponsesOutput(state), "usage": usageFromState(state),
		},
	})
}

func claudeStopReason(state *StreamState, finish string) string {
	if finish == "tool_calls" || state.HasTools {
		return "tool_use"
	}
	if finish == "length" {
		return "max_tokens"
	}
	return "end_turn"
}

func claudeUsageFromState(state *StreamState) map[string]any {
	return usageToClaude(map[string]any{
		"prompt_tokens":             state.PromptTokens,
		"completion_tokens":         state.CompletionTokens,
		"prompt_tokens_details":     map[string]any{"cached_tokens": state.CacheRead, "cache_write_tokens": state.CacheWrite},
		"completion_tokens_details": map[string]any{"reasoning_tokens": state.ReasoningTokens},
	})
}

func normalizeChatReasoning(rec map[string]any) {
	for _, raw := range AsArr(rec["choices"]) {
		ch := AsMap(raw)
		ApplyChatReasoning(AsMap(ch["message"]), MessageReasoning(AsMap(ch["message"])))
		ApplyChatReasoning(AsMap(ch["delta"]), DeltaReasoning(AsMap(ch["delta"])))
	}
}
