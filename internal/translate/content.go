package translate

import (
	"encoding/json"
	"regexp"
	"strings"
)

func openaiContentToClaude(content any) any {
	if s, ok := content.(string); ok {
		return s
	}
	var blocks []any
	for _, part := range AsArr(content) {
		p := AsMap(part)
		typ := AsStr(p["type"], "text")
		if typ == "text" {
			blocks = append(blocks, map[string]any{"type": "text", "text": AsStr(p["text"])})
			continue
		}
		if typ == "image_url" {
			if url := AsStr(AsMap(p["image_url"])["url"]); url != "" {
				blocks = append(blocks, claudeImageFromURL(url))
			}
			continue
		}
		if typ == "image" {
			if b := claudeImageFromSource(AsMap(p["source"])); b != nil {
				blocks = append(blocks, b)
			}
		}
	}
	if len(blocks) == 1 && AsStr(AsMap(blocks[0])["type"]) == "text" {
		return AsMap(blocks[0])["text"]
	}
	return blocks
}

func claudeContentToOpenai(content any) map[string]any {
	if s, ok := content.(string); ok {
		return map[string]any{"content": s}
	}
	var parts []any
	var toolCalls []any
	thinking := ""
	for _, part := range AsArr(content) {
		p := AsMap(part)
		typ := AsStr(p["type"])
		switch typ {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": AsStr(p["text"])})
		case "image":
			if part := openaiImageFromClaude(AsMap(p["source"])); part != nil {
				parts = append(parts, part)
			}
		case "tool_use":
			args, _ := json.Marshal(p["input"])
			if p["input"] == nil {
				args = []byte("{}")
			}
			toolCalls = append(toolCalls, map[string]any{
				"id": AsStr(p["id"]), "type": "function",
				"function": map[string]any{"name": AsStr(p["name"]), "arguments": string(args)},
			})
		case "thinking":
			thinking += AsStr(p["thinking"])
		}
	}
	if len(parts) == 1 && AsStr(AsMap(parts[0])["type"]) == "text" && len(toolCalls) == 0 {
		out := map[string]any{"content": AsMap(parts[0])["text"]}
		if thinking != "" {
			out["reasoning"] = thinking
		}
		return out
	}
	var contentOut any
	if len(parts) > 0 {
		contentOut = parts
	} else if thinking != "" {
		contentOut = nil
	} else {
		contentOut = ""
	}
	out := map[string]any{"content": contentOut}
	if len(toolCalls) > 0 {
		out["tool_calls"] = toolCalls
	}
	if thinking != "" {
		out["reasoning"] = thinking
	}
	return out
}

func claudeImageFromURL(url string) map[string]any {
	if parsed := parseDataURL(url); parsed != nil {
		return map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": parsed.media, "data": parsed.data},
		}
	}
	return map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": url},
	}
}

func claudeImageFromSource(source map[string]any) map[string]any {
	if AsStr(source["type"]) == "url" || AsStr(source["url"]) != "" {
		return map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": AsStr(source["url"])}}
	}
	if data := AsStr(source["data"]); data != "" {
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": AsStr(source["media_type"], "image/png"),
				"data":       data,
			},
		}
	}
	return nil
}

func openaiImageFromClaude(source map[string]any) map[string]any {
	if url := claudeImageURL(source); url != "" {
		return map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
	}
	return nil
}

func claudeImageURL(source map[string]any) string {
	if AsStr(source["type"]) == "url" || AsStr(source["url"]) != "" {
		return AsStr(source["url"])
	}
	data := AsStr(source["data"])
	if data == "" {
		return ""
	}
	return "data:" + AsStr(source["media_type"], "image/png") + ";base64," + data
}

type dataURL struct{ media, data string }

var dataURLRe = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

func parseDataURL(url string) *dataURL {
	m := dataURLRe.FindStringSubmatch(url)
	if m == nil {
		return nil
	}
	return &dataURL{media: m[1], data: m[2]}
}

func toolChoiceName(v any) (typ, name string) {
	if s, ok := v.(string); ok {
		return s, ""
	}
	m := AsMap(v)
	typ = AsStr(m["type"])
	name = AsStr(m["name"])
	if name == "" {
		name = AsStr(AsMap(m["function"])["name"])
	}
	return typ, name
}

func toolChoiceToChat(v any) any {
	typ, name := toolChoiceName(v)
	switch typ {
	case "auto", "none", "required":
		return typ
	case "any":
		return "required"
	case "function", "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}
	default:
		if name != "" {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
		return v
	}
}

func toolChoiceToClaude(v any) any {
	typ, name := toolChoiceName(v)
	switch typ {
	case "none":
		return map[string]any{"type": "none"}
	case "required", "any":
		return map[string]any{"type": "any"}
	case "function", "tool":
		return map[string]any{"type": "tool", "name": name}
	default:
		if name != "" {
			return map[string]any{"type": "tool", "name": name}
		}
		return map[string]any{"type": "auto"}
	}
}

func toolChoiceToResponses(v any) any {
	typ, name := toolChoiceName(v)
	switch typ {
	case "auto", "none", "required":
		return typ
	case "any":
		return "required"
	case "function", "tool":
		return map[string]any{"type": "function", "name": name}
	default:
		if name != "" {
			return map[string]any{"type": "function", "name": name}
		}
		return v
	}
}

func MergeToolCallDeltas(dst []any, deltas []any) []any {
	for _, raw := range deltas {
		tc := AsMap(raw)
		idx := -1
		if n, ok := AsNum(tc["index"]); ok {
			idx = int(n)
		}
		if idx < 0 {
			dst = append(dst, tc)
			continue
		}
		for len(dst) <= idx {
			dst = append(dst, map[string]any{"type": "function", "function": map[string]any{"name": "", "arguments": ""}})
		}
		cur := AsMap(dst[idx])
		fn := AsMap(cur["function"])
		srcFn := AsMap(tc["function"])
		if id := AsStr(tc["id"]); id != "" {
			cur["id"] = id
		}
		if typ := AsStr(tc["type"]); typ != "" {
			cur["type"] = typ
		}
		if name := AsStr(srcFn["name"]); name != "" {
			fn["name"] = name
		}
		if args := AsStr(srcFn["arguments"]); args != "" {
			fn["arguments"] = AsStr(fn["arguments"]) + args
		}
		cur["function"] = fn
		if n, ok := AsNum(tc["index"]); ok {
			cur["index"] = n
		}
		dst[idx] = cur
	}
	return dst
}

func FlattenTextForCursor(content any) string { return flattenText(content) }

func flattenText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var parts []string
	for _, p := range AsArr(content) {
		rec := AsMap(p)
		if t := AsStr(rec["text"]); t != "" {
			parts = append(parts, t)
		} else if t := AsStr(rec["thinking"]); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}
