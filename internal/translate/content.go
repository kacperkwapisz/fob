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
			url := AsStr(AsMap(p["image_url"])["url"])
			if parsed := parseDataURL(url); parsed != nil {
				blocks = append(blocks, map[string]any{
					"type":   "image",
					"source": map[string]any{"type": "base64", "media_type": parsed.media, "data": parsed.data},
				})
			} else if url != "" {
				blocks = append(blocks, map[string]any{
					"type":   "image",
					"source": map[string]any{"type": "base64", "media_type": "image/png", "data": url},
				})
			}
			continue
		}
		if typ == "image" {
			source := AsMap(p["source"])
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": AsStr(source["media_type"], "image/png"),
					"data":       AsStr(source["data"]),
				},
			})
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
			source := AsMap(p["source"])
			media := AsStr(source["media_type"], "image/png")
			data := AsStr(source["data"])
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + media + ";base64," + data}})
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

type dataURL struct{ media, data string }

var dataURLRe = regexp.MustCompile(`^data:([^;]+);base64,(.+)$`)

func parseDataURL(url string) *dataURL {
	m := dataURLRe.FindStringSubmatch(url)
	if m == nil {
		return nil
	}
	return &dataURL{media: m[1], data: m[2]}
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
