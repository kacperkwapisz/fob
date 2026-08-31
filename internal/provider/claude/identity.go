package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/translate"
)

const (
	fingerprintSalt = "59cf53e54c78"
	cliVersion      = "2.1.220"
	cliIdentity     = "You are Claude Code, Anthropic's official CLI for Claude."
	ua              = "claude-cli/2.1.220 (external, cli)"
	packageVersion  = "0.94.0"
	runtimeVersion  = "v26.3.0"
	osName          = "MacOS"
	arch            = "arm64"
	codeBeta        = "claude-code-20250219"
	oauthBeta       = "oauth-2025-04-20"
	redact          = "redact-thinking-2026-02-12"
	fast            = "fast-mode-2026-02-01"
	advisorBeta     = "advisor-tool-2026-03-01"
)

var nativeEntry = map[string]bool{"cli": true, "sdk-cli": true, "claude-vscode": true}
var uaNative = regexp.MustCompile(`(?i)^claude-cli/\d+\.\d+\.\d+\s+\(external,\s*[^,)]+(?:,\s*agent-sdk/\d+\.\d+\.\d+)?\)$`)
var constantBetas = []string{"interleaved-thinking-2025-05-14", redact, "thinking-token-count-2026-05-13", "context-management-2025-06-27", "prompt-caching-scope-2026-01-05"}
var trailingBetas = []string{"server-side-fallback-2026-06-01", "fallback-credit-2026-06-01", "structured-outputs-2025-12-15"}

type Prepare struct {
	Body    any
	Headers map[string]string
	Reverse map[string]string
}

var now = time.Now
var newUUID = func() string {
	// randomUUID equivalent used only for x-client-request-id when inbound lacks one.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", now().UnixNano())))
	return fmt.Sprintf("%x-%x-4%x-8%x-%x", sum[:4], sum[4:6], sum[6:8][1:], sum[8:10][1:], sum[10:16])
}

func PrepareUpstream(body any, accessToken, credentialID, accountID string, inbound map[string]string, stream, countTokens bool, callerKey string) Prepare {
	if inbound == nil {
		inbound = map[string]string{}
	}
	confirmed := detectNative(inbound, body, countTokens)
	b := translate.AsMap(translate.CloneJSON(body))
	b = sanitizeClaudeMessages(b)
	b = sanitizeWebSearchDomains(b)
	liftMidSystem(b)
	disableThinkingIfForced(b)
	normalizeSampling(b, confirmed)
	cloaked := !confirmed
	var reverse map[string]string
	if cloaked {
		b = applyCloak(b)
		aliased, rev := AliasClaudeTools(b, or(callerKey, "cpa-claude-mcp-default-caller"))
		b = aliased
		reverse = rev
	}
	sessionID := sessionFrom(b, inbound, credentialID)
	applyMetadata(b, credentialID, accountID, sessionID)
	raw, _ := json.Marshal(b)
	signed := SignCch(string(raw), billingHeader(b, true))
	var parsed map[string]any
	_ = json.Unmarshal([]byte(signed), &parsed)
	headers := claudeHeaders(accessToken, confirmed, cloaked, countTokens, stream, inbound, parsed, sessionID)
	return Prepare{Body: parsed, Headers: headers, Reverse: reverse}
}

func RestoreStreamChunk(chunk any, reverse map[string]string) any {
	if reverse == nil {
		return chunk
	}
	return RestoreClaudeToolNames(chunk, reverse)
}

func detectNative(inbound map[string]string, body any, countTokens bool) bool {
	uaVal := header(inbound, "user-agent")
	entry := parseEntry(uaVal)
	xApp := header(inbound, "x-app") == "cli"
	uaOk := uaNative.MatchString(strings.TrimSpace(uaVal))
	betas := header(inbound, "anthropic-beta")
	betasOk := false
	for _, b := range strings.Split(betas, ",") {
		if strings.TrimSpace(b) == codeBeta {
			betasOk = true
		}
	}
	userID := translate.AsStr(translate.AsMap(translate.AsMap(body)["metadata"])["user_id"])
	metaOk := countTokens || len(userID) > 0
	return nativeEntry[entry] && xApp && uaOk && betasOk && metaOk
}

func parseEntry(uaVal string) string {
	re := regexp.MustCompile(`(?i)^claude-cli/\S+\s+\(external,\s*([^,)]+)`)
	m := re.FindStringSubmatch(uaVal)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func header(inbound map[string]string, name string) string {
	want := strings.ToLower(name)
	for k, v := range inbound {
		if strings.ToLower(k) == want {
			return v
		}
	}
	return ""
}

func claudeBetas(body map[string]any, requested string, countTokens bool) string {
	asked := map[string]bool{}
	for _, s := range strings.Split(requested, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			asked[s] = true
		}
	}
	if countTokens {
		out := []string{codeBeta, oauthBeta, "interleaved-thinking-2025-05-14", "context-management-2025-06-27", "token-counting-2024-11-01"}
		if asked[advisorBeta] || hasAdvisorTool(body) {
			out = append(out, advisorBeta)
		}
		return strings.Join(out, ",")
	}
	out := []string{codeBeta, oauthBeta}
	if asked["context-1m-2025-08-07"] {
		out = append(out, "context-1m-2025-08-07")
	}
	redactOn := !thinkingDisplay(body)
	for _, beta := range constantBetas {
		if beta == redact && !redactOn {
			continue
		}
		out = append(out, beta)
	}
	if hasRoleSystem(body) {
		out = append(out, "mid-conversation-system-2026-04-07")
	}
	if asked[advisorBeta] || hasAdvisorTool(body) {
		out = append(out, advisorBeta)
	}
	if len(translate.AsArr(body["tools"])) > 0 {
		out = append(out, "advanced-tool-use-2025-11-20")
	}
	out = append(out, "effort-2025-11-24")
	if !asked["fallback-credit-2026-06-01"] {
		out = append(out, "fallback-credit-2026-06-01")
	}
	for _, beta := range trailingBetas {
		if asked[beta] {
			out = append(out, beta)
		}
	}
	if translate.AsStr(body["speed"]) == "fast" || asked[fast] {
		out = append(out, fast)
	}
	out = append(out, "extended-cache-ttl-2025-04-11")
	if asked["cache-diagnosis-2026-04-07"] {
		out = append(out, "cache-diagnosis-2026-04-07")
	}
	seen := map[string]bool{}
	var uniq []string
	for _, b := range out {
		if seen[b] {
			continue
		}
		seen[b] = true
		uniq = append(uniq, b)
	}
	return strings.Join(uniq, ",")
}

func thinkingDisplay(body map[string]any) bool {
	display := translate.AsMap(body["thinking"])["display"]
	return display != nil && display != ""
}

func hasRoleSystem(body map[string]any) bool {
	for _, m := range translate.AsArr(body["messages"]) {
		if translate.AsStr(translate.AsMap(m)["role"]) == "system" {
			return true
		}
	}
	return false
}

func hasAdvisorTool(body map[string]any) bool {
	for _, tool := range translate.AsArr(body["tools"]) {
		if strings.HasPrefix(strings.ToLower(translate.AsStr(translate.AsMap(tool)["type"])), "advisor_") {
			return true
		}
	}
	return false
}

func liftMidSystem(body map[string]any) {
	messages := translate.AsArr(body["messages"])
	var moved []string
	var kept []any
	for _, raw := range messages {
		m := translate.AsMap(raw)
		if translate.AsStr(m["role"]) == "system" {
			if text := flatten(m["content"]); text != "" {
				moved = append(moved, text)
			}
			continue
		}
		kept = append(kept, raw)
	}
	if len(moved) == 0 {
		return
	}
	existing := flatten(body["system"])
	var parts []string
	if existing != "" {
		parts = append(parts, existing)
	}
	parts = append(parts, moved...)
	sys := make([]any, len(parts))
	for i, text := range parts {
		sys[i] = map[string]any{"type": "text", "text": text}
	}
	body["system"] = sys
	body["messages"] = kept
}

func flatten(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var parts []string
	for _, p := range translate.AsArr(content) {
		if t := translate.AsStr(translate.AsMap(p)["text"]); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

func disableThinkingIfForced(body map[string]any) {
	typ := strings.ToLower(translate.AsStr(translate.AsMap(body["tool_choice"])["type"]))
	if typ != "any" && typ != "tool" {
		return
	}
	delete(body, "thinking")
	oc := translate.AsMap(body["output_config"])
	if _, ok := oc["effort"]; ok {
		next := translate.AsMap(translate.CloneJSON(oc))
		delete(next, "effort")
		if len(next) == 0 {
			delete(body, "output_config")
		} else {
			body["output_config"] = next
		}
	}
}

func normalizeSampling(body map[string]any, native bool) {
	thinkingType := strings.ToLower(translate.AsStr(translate.AsMap(body["thinking"])["type"]))
	thinking := thinkingType == "enabled" || thinkingType == "adaptive" || thinkingType == "auto"
	if !native {
		delete(body, "temperature")
		delete(body, "top_p")
		if thinking {
			delete(body, "top_k")
		}
		return
	}
	if thinking {
		if n, ok := body["temperature"].(float64); ok && n != 1 {
			delete(body, "temperature")
		}
		if n, ok := body["top_p"].(float64); ok && n < 0.95 {
			delete(body, "top_p")
		}
		delete(body, "top_k")
		return
	}
	if body["temperature"] != nil && body["top_p"] != nil {
		delete(body, "top_p")
	}
}

func applyCloak(body map[string]any) map[string]any {
	forwarded := collectSystem(body["system"])
	billing := billingHeader(body, true)
	body["system"] = []any{
		map[string]any{"type": "text", "text": billing},
		map[string]any{"type": "text", "text": cliIdentity, "cache_control": map[string]any{"type": "ephemeral"}},
	}
	if len(forwarded) > 0 {
		insertMidSystem(body, forwarded)
	}
	injectDate(body)
	return body
}

func collectSystem(system any) []string {
	var out []string
	push := func(text string) {
		t := strings.TrimSpace(text)
		if t == "" || t == cliIdentity || strings.HasPrefix(t, "x-anthropic-billing-header:") {
			return
		}
		out = append(out, text)
	}
	if s, ok := system.(string); ok {
		push(s)
	} else {
		for _, part := range translate.AsArr(system) {
			push(translate.AsStr(translate.AsMap(part)["text"]))
		}
	}
	return out
}

func insertMidSystem(body map[string]any, texts []string) {
	messages := translate.AsArr(body["messages"])
	firstUser := -1
	for i, m := range messages {
		if translate.AsStr(translate.AsMap(m)["role"]) == "user" {
			firstUser = i
			break
		}
	}
	if firstUser < 0 {
		return
	}
	insertAt := firstUser + 1
	for insertAt < len(messages) && translate.AsStr(translate.AsMap(messages[insertAt])["role"]) == "user" {
		insertAt++
	}
	blocks := make([]any, len(texts))
	for i, text := range texts {
		blocks[i] = map[string]any{"role": "system", "content": []any{map[string]any{"type": "text", "text": text, "cache_control": map[string]any{"type": "ephemeral"}}}}
	}
	next := append([]any{}, messages[:insertAt]...)
	next = append(next, blocks...)
	next = append(next, messages[insertAt:]...)
	body["messages"] = next
}

func injectDate(body map[string]any) {
	messages := translate.AsArr(body["messages"])
	idx := -1
	for i, m := range messages {
		if translate.AsStr(translate.AsMap(m)["role"]) == "user" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	msg := translate.AsMap(messages[idx])
	block := map[string]any{"type": "text", "text": dateReminder()}
	if s, ok := msg["content"].(string); ok {
		messages[idx] = mapMerge(msg, map[string]any{"content": []any{block, map[string]any{"type": "text", "text": s, "cache_control": map[string]any{"type": "ephemeral"}}}})
	} else if _, ok := msg["content"].([]any); ok {
		var parts []any
		for _, p := range translate.AsArr(msg["content"]) {
			if !strings.HasPrefix(translate.AsStr(translate.AsMap(p)["text"]), "<system-reminder>\nAs you answer the user's questions") {
				parts = append(parts, p)
			}
		}
		messages[idx] = mapMerge(msg, map[string]any{"content": append([]any{block}, parts...)})
	}
	body["messages"] = messages
}

func dateReminder() string {
	n := now()
	return fmt.Sprintf("<system-reminder>\nAs you answer the user's questions, you can use the following context:\n# currentDate\nToday's date is %04d-%02d-%02d.\n\n      IMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.\n</system-reminder>\n\n", n.Year(), n.Month(), n.Day())
}

func billingHeader(body map[string]any, cch bool) string {
	text := lastUserText(body)
	hash := fingerprint(text, cliVersion)
	extra := ""
	if cch {
		extra = " cch=00000;"
	}
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli;%s", cliVersion, hash, extra)
}

func lastUserText(body map[string]any) string {
	text := ""
	for _, raw := range translate.AsArr(body["messages"]) {
		m := translate.AsMap(raw)
		if translate.AsStr(m["role"]) != "user" {
			continue
		}
		if s, ok := m["content"].(string); ok {
			text = s
			continue
		}
		for _, part := range translate.AsArr(m["content"]) {
			p := translate.AsMap(part)
			if translate.AsStr(p["type"]) == "text" {
				text = translate.AsStr(p["text"])
			}
		}
	}
	return text
}

func fingerprint(messageText, version string) string {
	runes := []rune(messageText)
	chars := ""
	for _, i := range []int{4, 7, 20} {
		if i < len(runes) {
			chars += string(runes[i])
		} else {
			chars += "0"
		}
	}
	sum := sha256.Sum256([]byte(fingerprintSalt + chars + version))
	return hex.EncodeToString(sum[:])[:3]
}

func sessionFrom(body map[string]any, inbound map[string]string, credentialID string) string {
	if fromHeader := header(inbound, "x-claude-code-session-id"); fromHeader != "" {
		return fromHeader
	}
	cache := translate.AsStr(body["prompt_cache_key"])
	if cache == "" {
		cache = translate.AsStr(translate.AsMap(body["metadata"])["user_id"])
	}
	if cache != "" && looksUUID(cache) {
		return cache
	}
	return uuidFrom(credentialID + ":session")
}

func applyMetadata(body map[string]any, credentialID, accountID, sessionID string) {
	metadata := translate.AsMap(body["metadata"])
	deviceID := uuidFrom(credentialID + ":device")
	accountUUID := uuidFrom(credentialID + ":account")
	if accountID != "" && looksUUID(accountID) {
		accountUUID = accountID
	}
	existing := map[string]any{}
	if raw, ok := metadata["user_id"].(string); ok && strings.HasPrefix(strings.TrimSpace(raw), "{") {
		_ = json.Unmarshal([]byte(raw), &existing)
	}
	delete(existing, "device_id")
	delete(existing, "account_uuid")
	delete(existing, "session_id")
	user := map[string]any{"device_id": deviceID, "account_uuid": accountUUID, "session_id": sessionID}
	for k, v := range existing {
		user[k] = v
	}
	raw, _ := json.Marshal(user)
	next := translate.AsMap(translate.CloneJSON(metadata))
	next["user_id"] = string(raw)
	body["metadata"] = next
}

func looksUUID(v string) bool {
	ok, _ := regexp.MatchString(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`, v)
	return ok
}

func uuidFrom(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	hexs := hex.EncodeToString(sum[:])
	return hexs[0:8] + "-" + hexs[8:12] + "-4" + hexs[13:16] + "-8" + hexs[17:20] + "-" + hexs[20:32]
}

func claudeHeaders(accessToken string, confirmed, cloaked, countTokens, stream bool, inbound map[string]string, body map[string]any, sessionID string) map[string]string {
	requested := header(inbound, "anthropic-beta")
	beta := requested
	if !(confirmed && requested != "") {
		beta = claudeBetas(body, requested, countTokens)
	}
	headers := map[string]string{
		"Authorization":     "Bearer " + accessToken,
		"anthropic-version": "2023-06-01",
		"anthropic-beta":    beta,
		"anthropic-dangerous-direct-browser-access": "true",
		"x-app":                       "cli",
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Lang":            "js",
		"X-Stainless-Timeout":         "600",
		"X-Claude-Code-Session-Id":    sessionID,
		"x-client-request-id":         or(header(inbound, "x-client-request-id"), newUUID()),
		"Connection":                  "keep-alive",
		"Accept":                      "application/json",
		"Accept-Encoding":             "gzip, deflate, br, zstd",
		"User-Agent":                  ua,
		"X-Stainless-Package-Version": packageVersion,
		"X-Stainless-Runtime-Version": runtimeVersion,
		"X-Stainless-OS":              osName,
		"X-Stainless-Arch":            arch,
	}
	if countTokens {
		delete(headers, "X-Stainless-Timeout")
	}
	if confirmed {
		if v := header(inbound, "user-agent"); v != "" {
			headers["User-Agent"] = v
		}
		for _, name := range []string{"X-Claude-Code-Agent-Id", "X-Claude-Code-Parent-Agent-Id", "X-Claude-Remote-Container-Id", "X-Claude-Remote-Session-Id", "X-Client-App", "X-Anthropic-Additional-Protection"} {
			if v := header(inbound, name); v != "" {
				headers[name] = v
			}
		}
		if uaVal := header(inbound, "user-agent"); uaNative.MatchString(uaVal) {
			headers["User-Agent"] = uaVal
		}
	}
	for k, v := range headers {
		if v == "" {
			delete(headers, k)
		}
	}
	_ = cloaked
	_ = stream
	return headers
}

func mapMerge(base, extra map[string]any) map[string]any {
	out := translate.AsMap(translate.CloneJSON(base))
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
