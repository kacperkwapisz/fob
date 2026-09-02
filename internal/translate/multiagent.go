package translate

func FlattenCodexMultiAgent(body any) any {
	rec := AsMap(body)
	input := AsArr(rec["input"])
	if len(input) == 0 {
		return body
	}
	changed := false
	nextInput := make([]any, len(input))
	for i, item := range input {
		it := AsMap(item)
		if AsStr(it["type"]) != "agent_message" {
			nextInput[i] = item
			continue
		}
		changed = true
		content := AsArr(it["content"])
		nextContent := make([]any, len(content))
		for j, part := range content {
			p := AsMap(part)
			if AsStr(p["type"]) != "encrypted_content" {
				nextContent[j] = part
				continue
			}
			nextContent[j] = map[string]any{"type": "input_text", "text": AsStr(p["encrypted_content"])}
		}
		out := cloneMap(it)
		out["type"] = "message"
		out["role"] = "user"
		out["content"] = nextContent
		nextInput[i] = out
	}
	tools := stripCollab(AsArr(rec["tools"]))
	if !changed && sameSlice(tools, rec["tools"]) {
		return body
	}
	out := cloneMap(rec)
	out["input"] = nextInput
	if !sameSlice(tools, rec["tools"]) {
		out["tools"] = tools
	}
	return out
}

func stripCollab(tools []any) []any {
	changed := false
	var walk func(node any) any
	walk = func(node any) any {
		rec := AsMap(node)
		name := AsStr(rec["name"])
		if name == "spawn_agent" || name == "send_message" || name == "followup_task" {
			params := AsMap(rec["parameters"])
			props := AsMap(params["properties"])
			message := AsMap(props["message"])
			if _, ok := message["encrypted"]; ok {
				changed = true
				nextMsg := cloneMap(message)
				delete(nextMsg, "encrypted")
				nextParams := cloneMap(params)
				nextProps := cloneMap(props)
				nextProps["message"] = nextMsg
				nextParams["properties"] = nextProps
				out := cloneMap(rec)
				out["parameters"] = nextParams
				return out
			}
		}
		if AsStr(rec["type"]) == "namespace" {
			out := cloneMap(rec)
			inner := AsArr(rec["tools"])
			next := make([]any, len(inner))
			for i, t := range inner {
				next[i] = walk(t)
			}
			out["tools"] = next
			return out
		}
		return node
	}
	next := make([]any, len(tools))
	for i, t := range tools {
		next[i] = walk(t)
	}
	if changed {
		return next
	}
	return tools
}

func sameSlice(a []any, b any) bool {
	bb, ok := b.([]any)
	if !ok || len(a) != len(bb) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &bb[0]
}
