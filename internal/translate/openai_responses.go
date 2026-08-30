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
	for _, item := range AsArr(rec["input"]) {
		if s, ok := item.(string); ok {
			messages = append(messages, map[string]any{"role": "user", "content": s})
			continue
		}
		it := AsMap(item)
		typ := AsStr(it["type"], "message")
		if typ == "message" {
			messages = append(messages, map[string]any{"role": AsStr(it["role"], "user"), "content": flattenText(it["content"])})
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
	out = AsMap(liftPrefixForCodex(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func responsesToClaude(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	var messages []any
	var system any = rec["instructions"]
	for _, item := range AsArr(rec["input"]) {
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
			messages = append(messages, map[string]any{"role": r, "content": flattenText(it["content"])})
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
	usage := AsMap(u["usage"])
	id := AsStr(u["id"])
	if id == "" {
		id = "resp_" + itoa(nowUnix())
	}
	prompt, _ := AsNum(usage["prompt_tokens"])
	completion, _ := AsNum(usage["completion_tokens"])
	return map[string]any{
		"id": id, "object": "response", "status": "completed", "model": model, "output": output,
		"usage": map[string]any{"input_tokens": prompt, "output_tokens": completion},
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
	usage := AsMap(u["usage"])
	id := AsStr(u["id"])
	if id == "" {
		id = "resp_" + itoa(nowUnix())
	}
	in, _ := AsNum(usage["input_tokens"])
	outn, _ := AsNum(usage["output_tokens"])
	return map[string]any{
		"id": id, "object": "response", "status": "completed", "model": model, "output": output,
		"usage": map[string]any{"input_tokens": in, "output_tokens": outn},
	}
}

func codexStreamToResponses(_ string, _ any, chunk any, _ *StreamState) []string {
	ev := AsMap(chunk)
	return []string{"event: " + AsStr(ev["type"], "message") + "\ndata: " + mustJSON(chunk)}
}

func grokStreamToResponses(model string, _ any, chunk any, state *StreamState) []string {
	c := AsMap(chunk)
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
	if s, ok := delta["content"].(string); ok && s != "" {
		lines = append(lines, sse("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": s}))
	}
	if choice["finish_reason"] != nil {
		lines = append(lines, sse("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": state.ID, "object": "response", "model": model, "status": "completed", "output": []any{}}}))
		state.Finished = true
	}
	return lines
}

func claudeStreamToResponses(model string, _ any, chunk any, state *StreamState) []string {
	ev := AsMap(chunk)
	typ := AsStr(ev["type"])
	var lines []string
	if typ == "message_start" {
		state.Started = true
		if id := AsStr(AsMap(ev["message"])["id"]); id != "" {
			state.ID = id
		}
		lines = append(lines, sse("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": state.ID, "object": "response", "model": model, "status": "in_progress"}}))
	} else if typ == "content_block_delta" {
		delta := AsMap(ev["delta"])
		if AsStr(delta["type"]) == "text_delta" {
			lines = append(lines, sse("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "delta": AsStr(delta["text"])}))
		}
	} else if typ == "message_stop" {
		lines = append(lines, sse("response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": state.ID, "object": "response", "model": model, "status": "completed"}}))
		state.Finished = true
	}
	return lines
}
