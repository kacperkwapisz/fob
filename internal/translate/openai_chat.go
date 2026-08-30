package translate

import (
	"encoding/json"
)

func openaiChatToClaude(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	messages := AsArr(rec["messages"])
	var system any
	var claudeMessages []any
	for _, raw := range messages {
		m := AsMap(raw)
		role := AsStr(m["role"])
		if role == "system" {
			block := openaiContentToClaude(m["content"])
			if system == nil {
				system = block
			} else {
				prev, ok := system.([]any)
				if !ok {
					prev = []any{map[string]any{"type": "text", "text": toString(system)}}
				}
				next, ok := block.([]any)
				if !ok {
					next = []any{map[string]any{"type": "text", "text": toString(block)}}
				}
				system = append(prev, next...)
			}
			continue
		}
		if role == "tool" {
			content := m["content"]
			if _, ok := content.(string); !ok {
				content = flattenText(content)
			}
			claudeMessages = append(claudeMessages, map[string]any{
				"role": "user",
				"content": []any{map[string]any{
					"type": "tool_result", "tool_use_id": AsStr(m["tool_call_id"]), "content": content,
				}},
			})
			continue
		}
		if role == "assistant" {
			content := openaiContentToClaude(m["content"])
			var blocks []any
			if arr, ok := content.([]any); ok {
				blocks = append(blocks, arr...)
			} else if content != nil && content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": content})
			}
			for _, call := range AsArr(m["tool_calls"]) {
				c := AsMap(call)
				fn := AsMap(c["function"])
				var input any = map[string]any{}
				if err := json.Unmarshal([]byte(AsStr(fn["arguments"], "{}")), &input); err != nil {
					input = map[string]any{"raw": AsStr(fn["arguments"])}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": AsStr(c["id"]), "name": AsStr(fn["name"]), "input": input})
			}
			if s, ok := m["reasoning"].(string); ok {
				blocks = append([]any{map[string]any{"type": "thinking", "thinking": s}}, blocks...)
			}
			var outContent any = blocks
			if len(blocks) == 1 && AsStr(AsMap(blocks[0])["type"]) == "text" {
				outContent = AsMap(blocks[0])["text"]
			}
			claudeMessages = append(claudeMessages, map[string]any{"role": "assistant", "content": outContent})
			continue
		}
		claudeMessages = append(claudeMessages, map[string]any{"role": "user", "content": openaiContentToClaude(m["content"])})
	}
	var tools []any
	for _, t := range AsArr(rec["tools"]) {
		tool := AsMap(t)
		fn := AsMap(tool["function"])
		name := AsStr(fn["name"])
		if name == "" {
			name = AsStr(tool["name"])
		}
		desc := AsStr(fn["description"])
		if desc == "" {
			desc = AsStr(tool["description"])
		}
		schema := fn["parameters"]
		if schema == nil {
			schema = tool["input_schema"]
		}
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{"name": name, "description": desc, "input_schema": schema})
	}
	out := map[string]any{"model": model, "messages": claudeMessages, "stream": stream, "max_tokens": 8192}
	if n, ok := AsNum(rec["max_tokens"]); ok {
		out["max_tokens"] = n
	} else if n, ok := AsNum(rec["max_completion_tokens"]); ok {
		out["max_tokens"] = n
	}
	if system != nil {
		out["system"] = system
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if rec["temperature"] != nil {
		out["temperature"] = rec["temperature"]
	}
	if rec["stop"] != nil {
		if arr, ok := rec["stop"].([]any); ok {
			out["stop_sequences"] = arr
		} else {
			out["stop_sequences"] = []any{rec["stop"]}
		}
	}
	if rec["thinking"] != nil {
		out["thinking"] = rec["thinking"]
	} else if effort := AsStr(rec["reasoning_effort"]); effort != "" {
		out["thinking"] = map[string]any{"type": "enabled", "budget_tokens": effortToBudget(effort)}
	}
	out = AsMap(injectClaudeCache(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func openaiChatToCodex(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	var input []any
	var instructions string
	for _, raw := range AsArr(rec["messages"]) {
		m := AsMap(raw)
		role := AsStr(m["role"])
		if role == "system" {
			text := flattenText(m["content"])
			if instructions != "" {
				instructions += "\n\n" + text
			} else {
				instructions = text
			}
			continue
		}
		if role == "tool" {
			out := m["content"]
			if _, ok := out.(string); !ok {
				b, _ := json.Marshal(out)
				out = string(b)
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": AsStr(m["tool_call_id"]), "output": out})
			continue
		}
		if role == "assistant" {
			content := openaiPartsToResponses(m["content"])
			if len(content) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "assistant", "content": content})
			}
			for _, call := range AsArr(m["tool_calls"]) {
				c := AsMap(call)
				fn := AsMap(c["function"])
				input = append(input, map[string]any{
					"type": "function_call", "call_id": AsStr(c["id"]), "name": AsStr(fn["name"]),
					"arguments": AsStr(fn["arguments"], "{}"),
				})
			}
			continue
		}
		input = append(input, map[string]any{"type": "message", "role": "user", "content": openaiPartsToResponses(m["content"])})
	}
	var tools []any
	for _, t := range AsArr(rec["tools"]) {
		tool := AsMap(t)
		fn := AsMap(tool["function"])
		name := AsStr(fn["name"])
		if name == "" {
			name = AsStr(tool["name"])
		}
		desc := AsStr(fn["description"])
		if desc == "" {
			desc = AsStr(tool["description"])
		}
		params := fn["parameters"]
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{"type": "function", "name": name, "description": desc, "parameters": params})
	}
	out := map[string]any{"model": model, "input": input, "stream": stream}
	if instructions != "" {
		out["instructions"] = instructions
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if rec["temperature"] != nil {
		out["temperature"] = rec["temperature"]
	}
	if rec["max_tokens"] != nil || rec["max_completion_tokens"] != nil {
		n, ok := AsNum(rec["max_tokens"])
		if !ok {
			n, _ = AsNum(rec["max_completion_tokens"])
		}
		out["max_output_tokens"] = n
	}
	if rec["reasoning"] != nil {
		out["reasoning"] = rec["reasoning"]
	} else if effort := AsStr(rec["reasoning_effort"]); effort != "" {
		out["reasoning"] = map[string]any{"effort": effort}
	}
	out = AsMap(liftPrefixForCodex(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func claudeToOpenaiChat(model string, _, upstream any) any {
	u := AsMap(upstream)
	converted := claudeContentToOpenai(u["content"])
	stop := mapClaudeStop(AsStr(u["stop_reason"]))
	usage := mapClaudeUsage(u["usage"])
	id := AsStr(u["id"])
	if id == "" {
		id = "chatcmpl_" + itoa(nowUnix())
	}
	msg := map[string]any{"role": "assistant", "content": converted["content"]}
	if converted["tool_calls"] != nil {
		msg["tool_calls"] = converted["tool_calls"]
	}
	if converted["reasoning"] != nil {
		msg["reasoning_content"] = converted["reasoning"]
	}
	return map[string]any{
		"id": id, "object": "chat.completion", "created": nowUnix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": stop}},
		"usage":   usage,
	}
}

func grokToOpenaiChat(model string, _, upstream any) any {
	u := AsMap(upstream)
	if AsStr(u["object"]) == "chat.completion" || u["choices"] != nil {
		out := cloneMap(u)
		out["model"] = model
		return out
	}
	return claudeToOpenaiChat(model, nil, upstream)
}

func codexToOpenaiChat(model string, _, upstream any) any {
	u := AsMap(upstream)
	var content any = ""
	var toolCalls []any
	reasoning := ""
	for _, item := range AsArr(u["output"]) {
		it := AsMap(item)
		typ := AsStr(it["type"])
		if typ == "message" {
			content = responsesContentToOpenai(it["content"])
		} else if typ == "function_call" {
			id := AsStr(it["call_id"])
			if id == "" {
				id = AsStr(it["id"])
			}
			toolCalls = append(toolCalls, map[string]any{
				"id": id, "type": "function",
				"function": map[string]any{"name": AsStr(it["name"]), "arguments": AsStr(it["arguments"], "{}")},
			})
		} else if typ == "reasoning" {
			reasoning += flattenText(firstNonNil(it["summary"], it["content"]))
		}
	}
	usage := mapResponsesUsage(u["usage"])
	id := AsStr(u["id"])
	if id == "" {
		id = "chatcmpl_" + itoa(nowUnix())
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	return map[string]any{
		"id": id, "object": "chat.completion", "created": nowUnix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   usage,
	}
}

func claudeStreamToOpenaiChat(model string, _ any, chunk any, state *StreamState) []string {
	ev := AsMap(chunk)
	typ := AsStr(ev["type"])
	var lines []string
	switch typ {
	case "message_start":
		state.Started = true
		msg := AsMap(ev["message"])
		if id := AsStr(msg["id"]); id != "" {
			state.ID = id
		}
		u := mapClaudeUsage(msg["usage"])
		state.PromptTokens = int64(numOf(u["prompt_tokens"]))
		state.CompletionTokens = int64(numOf(u["completion_tokens"]))
		lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{"role": "assistant"}, nil, nil)))
	case "content_block_start":
		block := AsMap(ev["content_block"])
		if AsStr(block["type"]) == "tool_use" {
			lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{
				"tool_calls": []any{map[string]any{
					"index": state.ToolIndex, "id": AsStr(block["id"]), "type": "function",
					"function": map[string]any{"name": AsStr(block["name"]), "arguments": ""},
				}},
			}, nil, nil)))
		}
	case "content_block_delta":
		delta := AsMap(ev["delta"])
		dt := AsStr(delta["type"])
		if dt == "text_delta" {
			lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{"content": AsStr(delta["text"])}, nil, nil)))
		} else if dt == "input_json_delta" {
			lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{
				"tool_calls": []any{map[string]any{"index": state.ToolIndex, "function": map[string]any{"arguments": AsStr(delta["partial_json"])}}},
			}, nil, nil)))
		} else if dt == "thinking_delta" {
			lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{"reasoning_content": AsStr(delta["thinking"])}, nil, nil)))
		}
	case "content_block_stop":
		if AsStr(AsMap(ev["content_block"])["type"]) == "tool_use" {
			state.ToolIndex++
		}
	case "message_delta":
		usage := AsMap(ev["usage"])
		if n, ok := AsNum(usage["output_tokens"]); ok {
			state.CompletionTokens = int64(n)
		}
		stop := mapClaudeStop(AsStr(AsMap(ev["delta"])["stop_reason"]))
		lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{}, stop, state)))
	case "message_stop":
		if !state.Finished {
			state.Finished = true
			lines = append(lines, "data: [DONE]")
		}
	}
	return lines
}

func grokStreamToOpenaiChat(model string, _ any, chunk any, state *StreamState) []string {
	c := AsMap(chunk)
	if c["choices"] != nil {
		out := cloneMap(c)
		out["model"] = model
		if AsStr(c["id"]) == "" {
			out["id"] = state.ID
		}
		return []string{chunkLine(out)}
	}
	return nil
}

func codexStreamToOpenaiChat(model string, _ any, chunk any, state *StreamState) []string {
	ev := AsMap(chunk)
	typ := AsStr(ev["type"])
	var lines []string
	switch typ {
	case "response.created", "response.in_progress":
		if id := AsStr(AsMap(ev["response"])["id"]); id != "" {
			state.ID = id
		}
		if !state.Started {
			state.Started = true
			lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{"role": "assistant"}, nil, nil)))
		}
	case "response.output_text.delta":
		lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{"content": AsStr(ev["delta"])}, nil, nil)))
	case "response.reasoning_summary_text.delta", "response.reasoning.delta":
		lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{"reasoning_content": AsStr(ev["delta"])}, nil, nil)))
	case "response.output_item.added":
		item := AsMap(ev["item"])
		if AsStr(item["type"]) == "function_call" {
			id := AsStr(item["call_id"])
			if id == "" {
				id = AsStr(item["id"])
			}
			lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{
				"tool_calls": []any{map[string]any{
					"index": state.ToolIndex, "id": id, "type": "function",
					"function": map[string]any{"name": AsStr(item["name"]), "arguments": ""},
				}},
			}, nil, nil)))
		}
	case "response.function_call_arguments.delta":
		lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{
			"tool_calls": []any{map[string]any{"index": state.ToolIndex, "function": map[string]any{"arguments": AsStr(ev["delta"])}}},
		}, nil, nil)))
	case "response.output_item.done":
		if AsStr(AsMap(ev["item"])["type"]) == "function_call" {
			state.ToolIndex++
		}
	case "response.completed":
		resp := AsMap(ev["response"])
		u := mapResponsesUsage(resp["usage"])
		state.PromptTokens = int64(numOf(u["prompt_tokens"]))
		state.CompletionTokens = int64(numOf(u["completion_tokens"]))
		hasTools := false
		for _, i := range AsArr(resp["output"]) {
			if AsStr(AsMap(i)["type"]) == "function_call" {
				hasTools = true
			}
		}
		finish := "stop"
		if hasTools {
			finish = "tool_calls"
		}
		lines = append(lines, chunkLine(openaiChatChunk(state.ID, model, map[string]any{}, finish, state)))
		if !state.Finished {
			state.Finished = true
			lines = append(lines, "data: [DONE]")
		}
	}
	return lines
}

func openaiPartsToResponses(content any) []any {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []any{map[string]any{"type": "input_text", "text": s}}
	}
	var out []any
	for _, part := range AsArr(content) {
		p := AsMap(part)
		typ := AsStr(p["type"], "text")
		if typ == "text" {
			out = append(out, map[string]any{"type": "input_text", "text": AsStr(p["text"])})
		} else if typ == "image_url" {
			out = append(out, map[string]any{"type": "input_image", "image_url": AsStr(AsMap(p["image_url"])["url"])})
		}
	}
	return out
}

func responsesContentToOpenai(content any) any {
	if s, ok := content.(string); ok {
		return s
	}
	var texts []string
	var parts []any
	for _, part := range AsArr(content) {
		p := AsMap(part)
		typ := AsStr(p["type"])
		if typ == "output_text" || typ == "text" || typ == "input_text" {
			texts = append(texts, AsStr(p["text"]))
		} else if typ == "output_image" || typ == "input_image" {
			url := AsStr(p["image_url"])
			if url == "" {
				url = AsStr(AsMap(p["image_url"])["url"])
			}
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		}
	}
	if len(parts) == 0 {
		return join(texts, "")
	}
	var out []any
	for _, t := range texts {
		out = append(out, map[string]any{"type": "text", "text": t})
	}
	return append(out, parts...)
}

func openaiChatChunk(id, model string, delta map[string]any, finish any, state *StreamState) any {
	out := map[string]any{
		"id": id, "object": "chat.completion.chunk", "created": nowUnix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	}
	if finish != nil && state != nil {
		out["usage"] = map[string]any{
			"prompt_tokens": state.PromptTokens, "completion_tokens": state.CompletionTokens,
			"total_tokens": state.PromptTokens + state.CompletionTokens,
		}
	}
	return out
}

func chunkLine(payload any) string { return "data: " + mustJSON(payload) }

func mapClaudeStop(reason string) any {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	}
	if reason == "" {
		return nil
	}
	return reason
}

func mapClaudeUsage(usage any) map[string]any {
	u := AsMap(usage)
	prompt, _ := AsNum(u["input_tokens"])
	completion, _ := AsNum(u["output_tokens"])
	cacheRead, _ := AsNum(u["cache_read_input_tokens"])
	cacheWrite, _ := AsNum(u["cache_creation_input_tokens"])
	out := map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
	if cacheRead != 0 || cacheWrite != 0 {
		out["prompt_tokens_details"] = map[string]any{"cached_tokens": cacheRead, "cache_write_tokens": cacheWrite}
	}
	return out
}

func mapResponsesUsage(usage any) map[string]any {
	u := AsMap(usage)
	prompt, ok := AsNum(u["input_tokens"])
	if !ok {
		prompt, _ = AsNum(u["prompt_tokens"])
	}
	completion, ok := AsNum(u["output_tokens"])
	if !ok {
		completion, _ = AsNum(u["completion_tokens"])
	}
	details := AsMap(u["input_tokens_details"])
	cacheRead, _ := AsNum(details["cached_tokens"])
	out := map[string]any{"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion}
	if cacheRead != 0 {
		out["prompt_tokens_details"] = map[string]any{"cached_tokens": cacheRead}
	}
	return out
}

func effortToBudget(effort string) int {
	switch effort {
	case "low":
		return 2048
	case "high", "xhigh":
		return 16384
	default:
		return 8192
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return AsStr(v)
}

func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

func numOf(v any) float64 {
	n, _ := AsNum(v)
	return n
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
