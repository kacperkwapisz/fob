package translate

import "encoding/json"

func responsesToCodex(model string, stream bool, body any) RequestResult {
	rec := AsMap(RewriteModel(body, model))
	out := AsMap(liftPrefixForCodex(cloneMap(rec), readPromptCacheKey(rec)))
	out["stream"] = stream
	out["model"] = model
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func responsesToGrok(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	var messages []any
	if rec["instructions"] != nil {
		messages = append(messages, map[string]any{"role": "system", "content": AsStr(rec["instructions"])})
	}
	for _, item := range inputItems(rec["input"]) {
		if s, ok := item.(string); ok {
			messages = append(messages, map[string]any{"role": "user", "content": s})
			continue
		}
		it := AsMap(item)
		typ := AsStr(it["type"], "message")
		if typ == "message" {
			messages = append(messages, map[string]any{"role": AsStr(it["role"], "user"), "content": responsesContentToOpenai(it["content"])})
		} else if typ == "reasoning" {
			msg := map[string]any{"role": "assistant", "content": nil}
			ApplyChatReasoning(msg, flattenText(firstNonNil(it["summary"], it["content"])))
			messages = append(messages, msg)
		} else if typ == "function_call" {
			id := AsStr(it["call_id"])
			if id == "" {
				id = AsStr(it["id"])
			}
			messages = append(messages, map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": AsStr(it["name"]), "arguments": AsStr(it["arguments"], "{}")}}},
			})
		} else if typ == "function_call_output" {
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": AsStr(it["call_id"]), "content": AsStr(it["output"])})
		}
	}
	var tools []any
	for _, t := range AsArr(rec["tools"]) {
		tool := AsMap(t)
		fn := AsMap(tool["function"])
		name := AsStr(tool["name"])
		if name == "" {
			name = AsStr(fn["name"])
		}
		desc := AsStr(tool["description"])
		if desc == "" {
			desc = AsStr(fn["description"])
		}
		params := tool["parameters"]
		if params == nil {
			params = fn["parameters"]
		}
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": name, "description": desc, "parameters": params}})
	}
	out := map[string]any{"model": model, "messages": messages, "stream": stream}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if rec["temperature"] != nil {
		out["temperature"] = rec["temperature"]
	}
	if n, ok := AsNum(rec["max_output_tokens"]); ok {
		out["max_tokens"] = n
	}
	if r := AsMap(rec["reasoning"]); AsStr(r["effort"]) != "" {
		out["reasoning_effort"] = AsStr(r["effort"])
	} else if effort := AsStr(rec["reasoning_effort"]); effort != "" {
		out["reasoning_effort"] = effort
	}
	if rec["tool_choice"] != nil {
		out["tool_choice"] = toolChoiceToChat(rec["tool_choice"])
	}
	out = AsMap(liftPrefixForCodex(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func responsesToClaude(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	var messages []any
	var system any = rec["instructions"]
	for _, item := range inputItems(rec["input"]) {
		if s, ok := item.(string); ok {
			messages = append(messages, map[string]any{"role": "user", "content": s})
			continue
		}
		it := AsMap(item)
		typ := AsStr(it["type"], "message")
		if typ == "message" {
			role := AsStr(it["role"], "user")
			if role == "system" {
				system = flattenText(it["content"])
				continue
			}
			r := "user"
			if role == "assistant" {
				r = "assistant"
			}
			messages = append(messages, map[string]any{"role": r, "content": openaiContentToClaude(responsesContentToOpenai(it["content"]))})
		} else if typ == "reasoning" {
			messages = append(messages, map[string]any{"role": "assistant", "content": []any{thinkingBlock(flattenText(firstNonNil(it["summary"], it["content"])))}})
		} else if typ == "function_call" {
			var input any = map[string]any{}
			_ = json.Unmarshal([]byte(AsStr(it["arguments"], "{}")), &input)
			id := AsStr(it["call_id"])
			if id == "" {
				id = AsStr(it["id"])
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": AsStr(it["name"]), "input": input}}})
		} else if typ == "function_call_output" {
			messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": AsStr(it["call_id"]), "content": AsStr(it["output"])}}})
		}
	}
	var tools []any
	for _, t := range AsArr(rec["tools"]) {
		tool := AsMap(t)
		fn := AsMap(tool["function"])
		name := AsStr(tool["name"])
		if name == "" {
			name = AsStr(fn["name"])
		}
		desc := AsStr(tool["description"])
		if desc == "" {
			desc = AsStr(fn["description"])
		}
		schema := tool["parameters"]
		if schema == nil {
			schema = fn["parameters"]
		}
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{"name": name, "description": desc, "input_schema": schema})
	}
	out := map[string]any{"model": model, "messages": messages, "stream": stream, "max_tokens": 8192}
	if n, ok := AsNum(rec["max_output_tokens"]); ok {
		out["max_tokens"] = n
	}
	if system != nil && system != "" {
		out["system"] = system
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if rec["reasoning"] != nil {
		r := AsMap(rec["reasoning"])
		out["thinking"] = map[string]any{"type": "enabled", "budget_tokens": effortToBudget(AsStr(r["effort"]))}
	}
	if rec["tool_choice"] != nil {
		out["tool_choice"] = toolChoiceToClaude(rec["tool_choice"])
	}
	out = AsMap(injectClaudeCache(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func codexToResponses(model string, _, upstream any) any {
	u := cloneMap(AsMap(upstream))
	u["model"] = model
	if u["object"] == nil {
		u["object"] = "response"
	}
	return u
}

func grokToResponses(model string, _, upstream any) any {
	u := AsMap(upstream)
	choice := AsMap(first(AsArr(u["choices"])))
	message := AsMap(choice["message"])
	var output []any
	if r := MessageReasoning(message); r != "" {
		output = append(output, reasoningOutputItem(r))
	}
	if message["content"] != nil {
		text := ""
		if s, ok := message["content"].(string); ok {
			text = s
		} else {
			text = flattenText(message["content"])
		}
		output = append(output, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}})
	}
	for _, call := range AsArr(message["tool_calls"]) {
		c := AsMap(call)
		fn := AsMap(c["function"])
		output = append(output, map[string]any{"type": "function_call", "call_id": AsStr(c["id"]), "name": AsStr(fn["name"]), "arguments": AsStr(fn["arguments"], "{}")})
	}
	id := AsStr(u["id"])
	if id == "" {
		id = "resp_" + itoa(nowUnix())
	}
	return map[string]any{
		"id": id, "object": "response", "status": "completed", "model": model, "output": output,
		"usage": usageToResponses(u["usage"]),
	}
}

func claudeToResponses(model string, _, upstream any) any {
	u := AsMap(upstream)
	var output []any
	var textParts []any
	for _, part := range AsArr(u["content"]) {
		p := AsMap(part)
		typ := AsStr(p["type"])
		if typ == "text" {
			textParts = append(textParts, map[string]any{"type": "output_text", "text": AsStr(p["text"])})
		} else if typ == "tool_use" {
			if len(textParts) > 0 {
				output = append(output, map[string]any{"type": "message", "role": "assistant", "content": textParts})
				textParts = nil
			}
			args, _ := json.Marshal(p["input"])
			if p["input"] == nil {
				args = []byte("{}")
			}
			output = append(output, map[string]any{"type": "function_call", "call_id": AsStr(p["id"]), "name": AsStr(p["name"]), "arguments": string(args)})
		} else if typ == "thinking" {
			output = append(output, map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": AsStr(p["thinking"])}}})
		}
	}
	if len(textParts) > 0 {
		output = append(output, map[string]any{"type": "message", "role": "assistant", "content": textParts})
	}
	id := AsStr(u["id"])
	if id == "" {
		id = "resp_" + itoa(nowUnix())
	}
	return map[string]any{
		"id": id, "object": "response", "status": "completed", "model": model, "output": output,
		"usage": usageToResponses(u["usage"]),
	}
}

func codexStreamToResponses(_ string, _ any, chunk any, state *StreamState) []string {
	ev := AsMap(chunk)
	typ := AsStr(ev["type"], "message")
	if typ == "response.completed" || typ == "response.done" {
		captureResponsesUsage(state, AsMap(ev["response"])["usage"])
		state.Finished = true
	}
	return []string{"event: " + typ + "\ndata: " + mustJSON(chunk)}
}

func grokStreamToResponses(model string, _ any, chunk any, state *StreamState) []string {
	c := AsMap(chunk)
	captureUsage(state, c["usage"])
	choice := AsMap(first(AsArr(c["choices"])))
	delta := AsMap(choice["delta"])
	var lines []string
	if !state.Started {
		state.Started = true
		if id := AsStr(c["id"]); id != "" {
			state.ID = id
		}
		lines = append(lines, sse("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": state.ID, "object": "response", "model": model, "status": "in_progress"}}))
	}
	if r := DeltaReasoning(delta); r != "" {
		state.Reasoning += r
		lines = append(lines, responsesReasoningDeltas(r)...)
	}
	if s, ok := delta["content"].(string); ok && s != "" {
		state.Text += s
		lines = append(lines, sse("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": s}))
	}
	lines = append(lines, emitChatToolsToResponses(delta, state)...)
	if choice["finish_reason"] != nil {
		captureStreamError(state, choice)
		status := "completed"
		if AsStr(choice["finish_reason"]) == "error" {
			status = "failed"
		}
		lines = append(lines, responsesCompletedEvent(model, status, state))
		if AsStr(choice["finish_reason"]) != "error" {
			state.Finished = true
		}
	}
	return lines
}

func claudeStreamToResponses(model string, _ any, chunk any, state *StreamState) []string {
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
		lines = append(lines, sse("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": state.ID, "object": "response", "model": model, "status": "in_progress"}}))
	case "content_block_start":
		block := AsMap(ev["content_block"])
		if AsStr(block["type"]) == "tool_use" {
			id := AsStr(block["id"])
			item := map[string]any{"type": "function_call", "call_id": id, "name": AsStr(block["name"]), "arguments": ""}
			state.Tools = append(state.Tools, item)
			state.HasTools = true
			lines = append(lines, sse("response.output_item.added", map[string]any{"type": "response.output_item.added", "item": item}))
		}
	case "content_block_delta":
		delta := AsMap(ev["delta"])
		switch AsStr(delta["type"]) {
		case "text_delta":
			text := AsStr(delta["text"])
			state.Text += text
			lines = append(lines, sse("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": text}))
		case "thinking_delta":
			text := AsStr(delta["thinking"])
			state.Reasoning += text
			lines = append(lines, responsesReasoningDeltas(text)...)
		case "input_json_delta":
			args := AsStr(delta["partial_json"])
			if args != "" && len(state.Tools) > 0 {
				last := AsMap(state.Tools[len(state.Tools)-1])
				last["arguments"] = AsStr(last["arguments"]) + args
			}
			if args != "" {
				lines = append(lines, sse("response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "delta": args}))
			}
		}
	case "message_delta":
		if n, ok := AsNum(AsMap(ev["usage"])["output_tokens"]); ok {
			state.CompletionTokens = int64(n)
		}
	case "message_stop":
		lines = append(lines, responsesCompletedEvent(model, "completed", state))
		state.Finished = true
	}
	return lines
}
