package translate

import "encoding/json"

func claudeToClaude(model string, stream bool, body any) RequestResult {
	rec := AsMap(RewriteModel(body, model))
	out := AsMap(injectClaudeCache(cloneMap(rec), readPromptCacheKey(rec)))
	out["stream"] = stream
	out["model"] = model
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func claudeToGrok(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	var messages []any
	if rec["system"] != nil {
		messages = append(messages, map[string]any{"role": "system", "content": flattenText(rec["system"])})
	}
	for _, raw := range AsArr(rec["messages"]) {
		m := AsMap(raw)
		role := AsStr(m["role"])
		content := m["content"]
		if role == "assistant" {
			var toolCalls []any
			var texts []any
			parts := AsArr(content)
			if _, ok := content.([]any); !ok {
				parts = []any{map[string]any{"type": "text", "text": content}}
			}
			for _, part := range parts {
				p := AsMap(part)
				if AsStr(p["type"]) == "tool_use" {
					args, _ := json.Marshal(p["input"])
					if p["input"] == nil {
						args = []byte("{}")
					}
					toolCalls = append(toolCalls, map[string]any{
						"id": AsStr(p["id"]), "type": "function",
						"function": map[string]any{"name": AsStr(p["name"]), "arguments": string(args)},
					})
				} else if AsStr(p["type"]) == "thinking" {
					continue
				} else {
					texts = append(texts, p)
				}
			}
			msg := map[string]any{"role": "assistant"}
			if len(texts) == 1 && AsStr(AsMap(texts[0])["type"]) == "text" {
				msg["content"] = AsMap(texts[0])["text"]
			} else {
				msg["content"] = texts
			}
			if len(toolCalls) > 0 {
				msg["tool_calls"] = toolCalls
			}
			messages = append(messages, msg)
			continue
		}
		if role == "user" {
			if arr, ok := content.([]any); ok {
				has := false
				for _, part := range arr {
					if AsStr(AsMap(part)["type"]) == "tool_result" {
						has = true
						break
					}
				}
				if has {
					for _, part := range arr {
						p := AsMap(part)
						if AsStr(p["type"]) == "tool_result" {
							c := p["content"]
							if _, ok := c.(string); !ok {
								c = flattenText(c)
							}
							messages = append(messages, map[string]any{"role": "tool", "tool_call_id": AsStr(p["tool_use_id"]), "content": c})
						}
					}
					continue
				}
			}
		}
		if role == "" {
			role = "user"
		}
		messages = append(messages, map[string]any{"role": role, "content": claudeContentToChat(content)})
	}
	var tools []any
	for _, t := range AsArr(rec["tools"]) {
		tool := AsMap(t)
		schema := tool["input_schema"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": AsStr(tool["name"]), "description": AsStr(tool["description"]), "parameters": schema},
		})
	}
	out := map[string]any{"model": model, "messages": messages, "stream": stream}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if rec["temperature"] != nil {
		out["temperature"] = rec["temperature"]
	}
	if n, ok := AsNum(rec["max_tokens"]); ok {
		out["max_tokens"] = n
	}
	out = AsMap(liftPrefixForCodex(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func claudeToCodex(model string, stream bool, body any) RequestResult {
	rec := AsMap(body)
	var input []any
	var instructions string
	if rec["system"] != nil {
		instructions = flattenText(rec["system"])
	}
	for _, raw := range AsArr(rec["messages"]) {
		m := AsMap(raw)
		role := AsStr(m["role"])
		content := m["content"]
		if role == "assistant" {
			var parts []any
			src := AsArr(content)
			if _, ok := content.([]any); !ok {
				src = []any{map[string]any{"type": "text", "text": content}}
			}
			for _, part := range src {
				p := AsMap(part)
				typ := AsStr(p["type"])
				if typ == "tool_use" {
					if len(parts) > 0 {
						input = append(input, map[string]any{"type": "message", "role": "assistant", "content": parts})
						parts = nil
					}
					args, _ := json.Marshal(p["input"])
					if p["input"] == nil {
						args = []byte("{}")
					}
					input = append(input, map[string]any{"type": "function_call", "call_id": AsStr(p["id"]), "name": AsStr(p["name"]), "arguments": string(args)})
				} else if typ == "thinking" {
					input = append(input, map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": AsStr(p["thinking"])}}})
				} else {
					parts = append(parts, map[string]any{"type": "output_text", "text": AsStr(p["text"])})
				}
			}
			if len(parts) > 0 {
				input = append(input, map[string]any{"type": "message", "role": "assistant", "content": parts})
			}
			continue
		}
		if role == "user" {
			if arr, ok := content.([]any); ok {
				var textParts []any
				for _, part := range arr {
					p := AsMap(part)
					if AsStr(p["type"]) == "tool_result" {
						if len(textParts) > 0 {
							input = append(input, map[string]any{"type": "message", "role": "user", "content": textParts})
							textParts = nil
						}
						c := p["content"]
						if _, ok := c.(string); !ok {
							c = flattenText(c)
						}
						input = append(input, map[string]any{"type": "function_call_output", "call_id": AsStr(p["tool_use_id"]), "output": c})
					} else if AsStr(p["type"]) == "image" {
						source := AsMap(p["source"])
						textParts = append(textParts, map[string]any{
							"type":      "input_image",
							"image_url": "data:" + AsStr(source["media_type"], "image/png") + ";base64," + AsStr(source["data"]),
						})
					} else {
						textParts = append(textParts, map[string]any{"type": "input_text", "text": AsStr(p["text"])})
					}
				}
				if len(textParts) > 0 {
					input = append(input, map[string]any{"type": "message", "role": "user", "content": textParts})
				}
				continue
			}
		}
		r := "user"
		partType := "input_text"
		if role == "assistant" {
			r = "assistant"
			partType = "output_text"
		}
		input = append(input, map[string]any{"type": "message", "role": r, "content": []any{map[string]any{"type": partType, "text": flattenText(content)}}})
	}
	var tools []any
	for _, t := range AsArr(rec["tools"]) {
		tool := AsMap(t)
		schema := tool["input_schema"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{"type": "function", "name": AsStr(tool["name"]), "description": AsStr(tool["description"]), "parameters": schema})
	}
	out := map[string]any{"model": model, "input": input, "stream": stream}
	if instructions != "" {
		out["instructions"] = instructions
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if n, ok := AsNum(rec["max_tokens"]); ok {
		out["max_output_tokens"] = n
	}
	if rec["thinking"] != nil {
		thinking := AsMap(rec["thinking"])
		n, _ := AsNum(thinking["budget_tokens"])
		out["reasoning"] = map[string]any{"effort": budgetToEffort(n)}
	}
	out = AsMap(liftPrefixForCodex(out, readPromptCacheKey(rec)))
	return RequestResult{Model: model, Stream: stream, Body: out}
}

func claudeFromClaude(model string, _, upstream any) any {
	u := cloneMap(AsMap(upstream))
	u["model"] = model
	return u
}

func grokToClaude(model string, _, upstream any) any {
	u := AsMap(upstream)
	choice := AsMap(first(AsArr(u["choices"])))
	message := AsMap(choice["message"])
	var content []any
	if message["content"] != nil {
		if s, ok := message["content"].(string); ok {
			content = append(content, map[string]any{"type": "text", "text": s})
		} else {
			content = append(content, AsArr(message["content"])...)
		}
	}
	for _, call := range AsArr(message["tool_calls"]) {
		c := AsMap(call)
		fn := AsMap(c["function"])
		var input any = map[string]any{}
		_ = json.Unmarshal([]byte(AsStr(fn["arguments"], "{}")), &input)
		content = append(content, map[string]any{"type": "tool_use", "id": AsStr(c["id"]), "name": AsStr(fn["name"]), "input": input})
	}
	usage := AsMap(u["usage"])
	stop := "end_turn"
	if AsStr(choice["finish_reason"]) == "tool_calls" {
		stop = "tool_use"
	} else if AsStr(choice["finish_reason"]) == "length" {
		stop = "max_tokens"
	}
	id := AsStr(u["id"])
	if id == "" {
		id = "msg_" + itoa(nowUnix())
	}
	prompt, _ := AsNum(usage["prompt_tokens"])
	completion, _ := AsNum(usage["completion_tokens"])
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": stop,
		"usage": map[string]any{"input_tokens": prompt, "output_tokens": completion},
	}
}

func codexToClaude(model string, _, upstream any) any {
	u := AsMap(upstream)
	var content []any
	for _, item := range AsArr(u["output"]) {
		it := AsMap(item)
		typ := AsStr(it["type"])
		if typ == "message" {
			for _, part := range AsArr(it["content"]) {
				content = append(content, map[string]any{"type": "text", "text": AsStr(AsMap(part)["text"])})
			}
		} else if typ == "function_call" {
			var input any = map[string]any{}
			_ = json.Unmarshal([]byte(AsStr(it["arguments"], "{}")), &input)
			id := AsStr(it["call_id"])
			if id == "" {
				id = AsStr(it["id"])
			}
			content = append(content, map[string]any{"type": "tool_use", "id": id, "name": AsStr(it["name"]), "input": input})
		} else if typ == "reasoning" {
			content = append(content, map[string]any{"type": "thinking", "thinking": flattenText(firstNonNil(it["summary"], it["content"]))})
		}
	}
	usage := AsMap(u["usage"])
	hasTools := false
	for _, c := range content {
		if AsStr(AsMap(c)["type"]) == "tool_use" {
			hasTools = true
		}
	}
	stop := "end_turn"
	if hasTools {
		stop = "tool_use"
	}
	id := AsStr(u["id"])
	if id == "" {
		id = "msg_" + itoa(nowUnix())
	}
	in, ok := AsNum(usage["input_tokens"])
	if !ok {
		in, _ = AsNum(usage["prompt_tokens"])
	}
	outn, ok := AsNum(usage["output_tokens"])
	if !ok {
		outn, _ = AsNum(usage["completion_tokens"])
	}
	return map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": model, "content": content, "stop_reason": stop,
		"usage": map[string]any{"input_tokens": in, "output_tokens": outn},
	}
}

func claudeStreamIdentity(_ string, _ any, chunk any, _ *StreamState) []string {
	return []string{"event: " + AsStr(AsMap(chunk)["type"], "message") + "\ndata: " + mustJSON(chunk)}
}

func grokStreamToClaude(model string, _ any, chunk any, state *StreamState) []string {
	c := AsMap(chunk)
	choice := AsMap(first(AsArr(c["choices"])))
	delta := AsMap(choice["delta"])
	var lines []string
	if !state.Started {
		state.Started = true
		if id := AsStr(c["id"]); id != "" {
			state.ID = id
		}
		lines = append(lines, sse("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": state.ID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}},
		}))
		lines = append(lines, sse("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}))
	}
	if s, ok := delta["content"].(string); ok && s != "" {
		lines = append(lines, sse("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": s}}))
	}
	if choice["finish_reason"] != nil {
		stop := "end_turn"
		if AsStr(choice["finish_reason"]) == "tool_calls" {
			stop = "tool_use"
		}
		lines = append(lines, sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}))
		lines = append(lines, sse("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}, "usage": map[string]any{"output_tokens": 0}}))
		lines = append(lines, sse("message_stop", map[string]any{"type": "message_stop"}))
		state.Finished = true
	}
	return lines
}

func codexStreamToClaude(model string, _ any, chunk any, state *StreamState) []string {
	ev := AsMap(chunk)
	typ := AsStr(ev["type"])
	var lines []string
	if typ == "response.created" && !state.Started {
		state.Started = true
		if id := AsStr(AsMap(ev["response"])["id"]); id != "" {
			state.ID = id
		}
		lines = append(lines, sse("message_start", map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": state.ID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}},
		}))
		lines = append(lines, sse("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}))
	} else if typ == "response.output_text.delta" {
		lines = append(lines, sse("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": AsStr(ev["delta"])}}))
	} else if typ == "response.completed" {
		lines = append(lines, sse("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0}))
		lines = append(lines, sse("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 0}}))
		lines = append(lines, sse("message_stop", map[string]any{"type": "message_stop"}))
		state.Finished = true
	}
	return lines
}

func claudeContentToChat(content any) any {
	if s, ok := content.(string); ok {
		return s
	}
	var parts []any
	for _, part := range AsArr(content) {
		p := AsMap(part)
		if AsStr(p["type"]) == "text" {
			parts = append(parts, map[string]any{"type": "text", "text": AsStr(p["text"])})
		} else if AsStr(p["type"]) == "image" {
			source := AsMap(p["source"])
			parts = append(parts, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "data:" + AsStr(source["media_type"], "image/png") + ";base64," + AsStr(source["data"])},
			})
		}
	}
	if len(parts) == 1 && AsStr(AsMap(parts[0])["type"]) == "text" {
		return AsMap(parts[0])["text"]
	}
	return parts
}

func sse(event string, data any) string {
	return "event: " + event + "\ndata: " + mustJSON(data)
}

func budgetToEffort(budget float64) string {
	if budget == 0 {
		return "medium"
	}
	if budget <= 2048 {
		return "low"
	}
	if budget >= 16000 {
		return "high"
	}
	return "medium"
}

func first(arr []any) any {
	if len(arr) == 0 {
		return nil
	}
	return arr[0]
}
