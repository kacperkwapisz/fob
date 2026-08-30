package codex

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	Originator = "codex-tui"
	UserAgent  = "codex-tui/0.146.0 (Mac OS 26.5.0; arm64) iTerm.app/3.6.10 (codex-tui; 0.146.0)"
	idLimit    = 64
)

var idPrefix = map[string]string{
	"message":                 "msg",
	"reasoning":               "rs",
	"function_call":           "fc",
	"custom_tool_call":        "ctc",
	"custom_tool_call_output": "ctco",
}

var codexHeaders = []string{
	"Version", "X-Codex-Turn-Metadata", "X-Client-Request-Id", "X-Codex-Window-Id",
	"Thread-Id", "Session-Id", "X-Openai-Internal-Codex-Responses-Lite", "X-Codex-Beta-Features",
}

func SanitizeBody(body any, inbound map[string]string) map[string]any {
	return sanitizeBody(body, inbound, false)
}

func SanitizeCompactBody(body any, inbound map[string]string) map[string]any {
	out := sanitizeBody(body, inbound, true)
	out["stream"] = false
	return out
}

func sanitizeBody(body any, inbound map[string]string, compact bool) map[string]any {
	out := translate.AsMap(translate.CloneJSON(body))
	if out["instructions"] == nil {
		out["instructions"] = ""
	}
	// ChatGPT-account Codex rejects stored responses and unary completions.
	// Codex CLI always sends store:false + stream:true (except compact).
	out["store"] = false
	out["stream"] = !compact
	delete(out, "max_output_tokens")
	delete(out, "max_tokens")
	delete(out, "temperature")
	delete(out, "top_p")
	delete(out, "top_k")
	if isLite(out, inbound) {
		out["parallel_tool_calls"] = false
	} else if out["parallel_tool_calls"] != nil && len(translate.AsArr(out["tools"])) == 0 {
		delete(out, "parallel_tool_calls")
	}
	if include := translate.AsArr(out["include"]); len(include) == 0 {
		out["include"] = []any{"reasoning.encrypted_content"}
	}
	sanitizeInputIDs(out)
	return out
}

func UpstreamHeaders(accessToken, accountID string, inbound map[string]string, stream bool, promptCacheKey string) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Accept":        "application/json",
		"Originator":    Originator,
		"User-Agent":    UserAgent,
	}
	if stream {
		headers["Accept"] = "text/event-stream"
		headers["OpenAI-Beta"] = "responses=experimental"
	}
	if v := inboundHeader(inbound, "originator"); v != "" {
		headers["Originator"] = v
	}
	if accountID != "" {
		headers["ChatGPT-Account-Id"] = accountID
	}
	for _, name := range codexHeaders {
		if v := inboundHeader(inbound, name); v != "" {
			headers[name] = v
		}
	}
	if headers["Session-Id"] == "" && promptCacheKey != "" {
		headers["Session-Id"] = promptCacheKey
	}
	if headers["Session-Id"] == "" {
		headers["Session-Id"] = newSessionID()
	}
	if headers["X-Client-Request-Id"] == "" {
		headers["X-Client-Request-Id"] = headers["Session-Id"]
	}
	return headers
}

func newSessionID() string {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "sess_fallback"
	}
	return hex.EncodeToString(buf)
}

func isLite(body map[string]any, inbound map[string]string) bool {
	if strings.ToLower(inboundHeader(inbound, "X-Openai-Internal-Codex-Responses-Lite")) == "true" {
		return true
	}
	meta := translate.AsMap(body["client_metadata"])
	v := meta["ws_request_header_x_openai_internal_codex_responses_lite"]
	if v == true {
		return true
	}
	s, _ := v.(string)
	return strings.ToLower(s) == "true"
}

const openaiAuthClaim = "https://api.openai.com/auth"

func chatgptAccountID(tokens ...string) string {
	for _, token := range tokens {
		payload := decodeJWT(token)
		if id := translate.AsStr(payload["chatgpt_account_id"]); id != "" {
			return id
		}
		if id := translate.AsStr(translate.AsMap(payload[openaiAuthClaim])["chatgpt_account_id"]); id != "" {
			return id
		}
	}
	return ""
}

func decodeJWT(token string) map[string]any {
	if token == "" {
		return map[string]any{}
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{}
	}
	return out
}

func inboundHeader(inbound map[string]string, name string) string {
	want := strings.ToLower(name)
	for k, v := range inbound {
		if strings.ToLower(k) == want && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sanitizeInputIDs(body map[string]any) {
	input := translate.AsArr(body["input"])
	if len(input) == 0 {
		return
	}
	occupied := map[string]bool{}
	preserved := map[string]bool{}
	for _, item := range input {
		rec := translate.AsMap(item)
		if dropEncryptedReasoning(rec) {
			continue
		}
		original := translate.AsStr(rec["id"])
		if original == "" {
			continue
		}
		id := normalizeID(rec, original)
		if id == original {
			preserved[id] = true
		}
		if runeLen(id) <= idLimit {
			occupied[id] = true
		}
	}
	mapped := map[string]string{}
	collision := map[string]string{}
	var rebuilt []any
	for _, item := range input {
		rec := translate.AsMap(item)
		if dropEncryptedReasoning(rec) {
			continue
		}
		original := translate.AsStr(rec["id"])
		if original == "" {
			rebuilt = append(rebuilt, item)
			continue
		}
		id := normalizeID(rec, original)
		if id != original && preserved[id] {
			hit, ok := collision[id]
			if !ok {
				for attempt := 0; ; attempt++ {
					hit = hashSuffix(id, attempt)
					if !occupied[hit] {
						break
					}
				}
				collision[id] = hit
				occupied[hit] = true
			}
			id = hit
		}
		if runeLen(id) > idLimit {
			short, ok := mapped[id]
			if !ok {
				short = hashSuffix(id, 0)
				for attempt := 1; occupied[short]; attempt++ {
					short = hashSuffix(id, attempt)
				}
				mapped[id] = short
				occupied[short] = true
			}
			id = short
		}
		if id == original {
			rebuilt = append(rebuilt, item)
		} else {
			next := translate.AsMap(translate.CloneJSON(rec))
			next["id"] = id
			rebuilt = append(rebuilt, next)
		}
	}
	body["input"] = rebuilt
}

func dropEncryptedReasoning(item map[string]any) bool {
	if translate.AsStr(item["type"]) != "reasoning" {
		return false
	}
	id := translate.AsStr(item["id"])
	if id == "" || runeLen(id) <= idLimit {
		return false
	}
	s, _ := item["encrypted_content"].(string)
	return s != ""
}

func normalizeID(item map[string]any, id string) string {
	prefix := idPrefix[translate.AsStr(item["type"])]
	if prefix == "" {
		return id
	}
	if id == "" || strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + "_" + id
}

func hashSuffix(id string, attempt int) string {
	input := id
	if attempt > 0 {
		input = id + "\x00" + itoa(attempt)
	}
	sum := sha256.Sum256([]byte(input))
	suffix := "_" + hex.EncodeToString(sum[:])[:16]
	prefixLen := idLimit - len(suffix)
	runes := []rune(id)
	if prefixLen > len(runes) {
		prefixLen = len(runes)
	}
	if prefixLen < 0 {
		prefixLen = 0
	}
	return string(runes[:prefixLen]) + suffix
}

func runeLen(s string) int { return len([]rune(s)) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
