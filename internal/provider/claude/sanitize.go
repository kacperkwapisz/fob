package claude

import (
	"encoding/base64"
	"strings"

	"github.com/kacperkwapisz/fob/internal/translate"
)

func sanitizeClaudeMessages(body map[string]any) map[string]any {
	messages := translate.AsArr(body["messages"])
	if len(messages) == 0 {
		return body
	}
	var kept []any
	changed := false
	for _, raw := range messages {
		msg := translate.AsMap(raw)
		content, ok := msg["content"].([]any)
		if !ok {
			kept = append(kept, raw)
			continue
		}
		var parts []any
		partChanged := false
		for _, partRaw := range content {
			part := translate.AsMap(partRaw)
			switch translate.AsStr(part["type"]) {
			case "thinking":
				sig := translate.AsStr(part["signature"])
				if validClaudeThinkingSignature(sig) {
					if native := nativeThinkingSignature(sig); native != "" && native != sig {
						next := translate.AsMap(translate.CloneJSON(part))
						next["signature"] = native
						parts = append(parts, next)
						partChanged = true
						continue
					}
					parts = append(parts, partRaw)
					continue
				}
				partChanged = true
			case "tool_use":
				cleaned, stripped := stripToolUseSignatures(part)
				if stripped {
					parts = append(parts, cleaned)
					partChanged = true
					continue
				}
				parts = append(parts, partRaw)
			default:
				parts = append(parts, partRaw)
			}
		}
		if !partChanged {
			kept = append(kept, raw)
			continue
		}
		changed = true
		if len(parts) == 0 {
			continue
		}
		next := translate.AsMap(translate.CloneJSON(msg))
		next["content"] = parts
		kept = append(kept, next)
	}
	if !changed {
		return body
	}
	body["messages"] = kept
	return body
}

func sanitizeWebSearchDomains(body map[string]any) map[string]any {
	tools := translate.AsArr(body["tools"])
	if len(tools) == 0 {
		return body
	}
	changed := false
	for _, toolRaw := range tools {
		tool := translate.AsMap(toolRaw)
		if !strings.HasPrefix(translate.AsStr(tool["type"]), "web_search_") {
			continue
		}
		for _, field := range []string{"allowed_domains", "blocked_domains"} {
			arr, ok := tool[field].([]any)
			if ok && len(arr) == 0 {
				delete(tool, field)
				changed = true
			}
		}
	}
	if !changed {
		return body
	}
	body["tools"] = tools
	return body
}

func stripToolUseSignatures(part map[string]any) (map[string]any, bool) {
	changed := false
	next := translate.AsMap(translate.CloneJSON(part))
	for _, key := range []string{"signature", "thoughtSignature", "thought_signature", "model"} {
		if _, ok := next[key]; ok {
			delete(next, key)
			changed = true
		}
	}
	if extra, ok := next["extra_content"].(map[string]any); ok {
		if google, ok := extra["google"].(map[string]any); ok {
			if _, has := google["thought_signature"]; has {
				delete(google, "thought_signature")
				changed = true
			}
			if len(google) == 0 {
				delete(extra, "google")
			} else {
				extra["google"] = google
			}
		}
		if len(extra) == 0 {
			delete(next, "extra_content")
		} else {
			next["extra_content"] = extra
		}
	}
	return next, changed
}

func validClaudeThinkingSignature(raw string) bool {
	sig := stripThinkingPrefix(raw)
	if sig == "" {
		return false
	}
	switch sig[0] {
	case 'E', 'R':
		return validERThinkingSignature(sig)
	case 'C':
		return validCAISThinkingSignature(sig)
	default:
		return false
	}
}

func nativeThinkingSignature(raw string) string {
	sig := stripThinkingPrefix(raw)
	if sig == "" {
		return ""
	}
	switch sig[0] {
	case 'E', 'C':
		return sig
	case 'R':
		decoded, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			return sig
		}
		return string(decoded)
	default:
		return sig
	}
}

func stripThinkingPrefix(raw string) string {
	sig := strings.TrimSpace(raw)
	if sig == "" {
		return ""
	}
	if i := strings.IndexByte(sig, '#'); i >= 0 {
		sig = strings.TrimSpace(sig[i+1:])
	}
	return sig
}

func validERThinkingSignature(sig string) bool {
	switch sig[0] {
	case 'E':
		decoded, err := base64.StdEncoding.DecodeString(sig)
		return err == nil && len(decoded) > 0 && decoded[0] == 0x12
	case 'R':
		decoded, err := base64.StdEncoding.DecodeString(sig)
		if err != nil || len(decoded) == 0 || decoded[0] != 'E' {
			return false
		}
		inner, err := base64.StdEncoding.DecodeString(string(decoded))
		return err == nil && len(inner) > 0 && inner[0] == 0x12
	default:
		return false
	}
}

func validCAISThinkingSignature(sig string) bool {
	if sig == "" || sig[0] != 'C' {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(sig)
	return err == nil && len(decoded) > 0 && decoded[0] == 0x08
}
