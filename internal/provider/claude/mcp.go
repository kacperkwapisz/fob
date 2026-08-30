package claude

import (
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"strings"

	"github.com/kacperkwapisz/fob/internal/translate"
)

//go:embed claude-bip39.txt
var bip39File string

var words = strings.Split(strings.TrimSpace(bip39File), "\n")

var serverTypes = []string{
	"advisor_", "agent_toolset_", "bash_", "code_execution_", "computer_",
	"memory_", "text_editor_", "tool_search_tool_", "web_fetch_", "web_search_",
}

func AliasClaudeTools(body any, secret string) (map[string]any, map[string]string) {
	out := translate.AsMap(translate.CloneJSON(body))
	reverse := map[string]string{}
	reserved := map[string]bool{}
	tools := translate.AsArr(out["tools"])
	for _, tool := range tools {
		if name := translate.AsStr(translate.AsMap(tool)["name"]); name != "" {
			reserved[name] = true
		}
	}
	forward := map[string]string{}
	for _, tool := range tools {
		rec := translate.AsMap(tool)
		if isServerType(translate.AsStr(rec["type"])) {
			continue
		}
		name := translate.AsStr(rec["name"])
		if name == "" || isMcpName(name) {
			continue
		}
		alias := allocate(secret, name, reserved)
		if alias == "" {
			continue
		}
		reserved[alias] = true
		forward[name] = alias
		reverse[alias] = name
		rec["name"] = alias
	}
	if len(forward) == 0 {
		return out, reverse
	}
	renameDeep(out, forward)
	return out, reverse
}

func RestoreClaudeToolNames(value any, reverse map[string]string) any {
	if value == nil {
		return value
	}
	switch t := value.(type) {
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = RestoreClaudeToolNames(v, reverse)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, v := range t {
			if k == "name" || k == "tool_name" {
				if s, ok := v.(string); ok {
					if mapped, ok := reverse[s]; ok {
						out[k] = mapped
						continue
					}
				}
			}
			out[k] = RestoreClaudeToolNames(v, reverse)
		}
		return out
	default:
		return value
	}
}

func isServerType(typ string) bool {
	t := strings.ToLower(typ)
	for _, p := range serverTypes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func isMcpName(name string) bool {
	if len(name) == 0 || len(name) > 64 || !strings.HasPrefix(name, "mcp__") {
		return false
	}
	rest := name[5:]
	sep := strings.Index(rest, "__")
	return sep > 0 && sep+2 < len(rest)
}

func allocate(secret, original string, reserved map[string]bool) string {
	digest := aliasDigest(secret, "tool", original)
	if len(words) == 0 || len(digest) < 2 {
		return ""
	}
	base := int(digest[0])<<8 | int(digest[1])
	base %= len(words)
	server := serverComponent(secret)
	for attempt := 0; attempt < len(words); attempt++ {
		alias := aliasFor(server, words[(base+attempt)%len(words)], original)
		if !reserved[alias] {
			return alias
		}
	}
	return ""
}

func serverComponent(secret string) string {
	digest := aliasDigest(secret, "server", "")
	return word(digest, 0) + "_" + word(digest, 2)
}

func word(digest []byte, offset int) string {
	if offset+2 > len(digest) || len(words) == 0 {
		return "tool"
	}
	base := int(digest[offset])<<8 | int(digest[offset+1])
	return words[base%len(words)]
}

func aliasFor(server, toolID, original string) string {
	prefix := "mcp__" + server + "__" + toolID + "_"
	max := 64 - len(prefix)
	if max < 1 {
		max = 1
	}
	return prefix + semantic(original, max)
}

func semantic(original string, maxLength int) string {
	out := ""
	pending := false
	for _, char := range original {
		valid := (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-'
		if !valid {
			pending = len(out) > 0
			continue
		}
		if pending && len(out)+1 < maxLength {
			out += "_"
		}
		pending = false
		if len(out) >= maxLength {
			break
		}
		out += string(char)
	}
	trimmed := strings.Trim(out, "_-")
	if trimmed == "" {
		return "tool"
	}
	return trimmed
}

func aliasDigest(secret, purpose, original string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("cpa-claude-mcp-alias-v2\x00"))
	mac.Write([]byte(purpose))
	mac.Write([]byte{0})
	mac.Write([]byte(original))
	return mac.Sum(nil)
}

func renameDeep(node any, forward map[string]string) {
	switch t := node.(type) {
	case []any:
		for _, child := range t {
			renameDeep(child, forward)
		}
	case map[string]any:
		for _, key := range []string{"name", "tool_name"} {
			if v, ok := t[key].(string); ok {
				if mapped, ok := forward[v]; ok {
					t[key] = mapped
				}
			}
		}
		for _, v := range t {
			renameDeep(v, forward)
		}
	}
}
