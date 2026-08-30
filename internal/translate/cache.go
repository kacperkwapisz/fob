package translate

var ephemeral = map[string]any{"type": "ephemeral"}

func injectClaudeCache(body any, promptCacheKey string) any {
	rec := AsMap(body)
	system := rec["system"]
	if s, ok := system.(string); ok && s != "" {
		system = []any{map[string]any{"type": "text", "text": s, "cache_control": cloneMap(ephemeral)}}
	} else if arr, ok := system.([]any); ok {
		next := make([]any, len(arr))
		for i, block := range arr {
			b := AsMap(block)
			if i == len(arr)-1 && b["cache_control"] == nil {
				nb := cloneMap(b)
				nb["cache_control"] = cloneMap(ephemeral)
				next[i] = nb
			} else {
				next[i] = b
			}
		}
		system = next
	}
	tools := AsArr(rec["tools"])
	if len(tools) > 0 {
		next := make([]any, len(tools))
		for i, tool := range tools {
			t := AsMap(tool)
			if i == len(tools)-1 && t["cache_control"] == nil {
				nt := cloneMap(t)
				nt["cache_control"] = cloneMap(ephemeral)
				next[i] = nt
			} else {
				next[i] = t
			}
		}
		tools = next
	}
	out := cloneMap(rec)
	if system != nil {
		out["system"] = system
	}
	if len(tools) > 0 {
		out["tools"] = tools
	}
	if promptCacheKey != "" && out["metadata"] == nil {
		out["metadata"] = map[string]any{"user_id": promptCacheKey}
	}
	return out
}

func liftPrefixForCodex(body any, promptCacheKey string) any {
	rec := AsMap(body)
	if promptCacheKey == "" {
		return rec
	}
	if rec["prompt_cache_key"] != nil {
		return rec
	}
	out := cloneMap(rec)
	out["prompt_cache_key"] = promptCacheKey
	return out
}

func readPromptCacheKey(body any) string {
	s, _ := AsMap(body)["prompt_cache_key"].(string)
	return s
}
