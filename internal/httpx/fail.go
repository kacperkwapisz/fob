package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	KindAuth         = "auth"
	KindNoCredential = "no_credential"
	KindRefresh      = "refresh"
	KindUpstream     = "upstream"
	KindConnect      = "connect"
	KindTimeout      = "timeout"
	KindCancel       = "cancel"
	KindModel        = "model"
	KindProvider     = "provider"
	KindPermission   = "permission"
	KindCap          = "cap"
	KindRequest      = "request"
	KindInternal     = "internal"

	InboundClaude = "claude.messages"
)

type Fail struct {
	Kind     string
	Status   int
	Type     string
	Provider string
	Model    string
	Route    string
	Message  string
	Hint     string
}

func (f Fail) Display() string {
	msg := strings.TrimSpace(f.Message)
	hint := strings.TrimSpace(f.Hint)
	if msg == "" {
		return hint
	}
	if hint == "" || strings.Contains(strings.ToLower(msg), strings.ToLower(hint)) {
		return msg
	}
	return msg + " — " + hint
}

func (f Fail) Line() string {
	var b strings.Builder
	b.WriteString("fob error")
	if f.Status > 0 {
		b.WriteString("  ")
		b.WriteString(strconv.Itoa(f.Status))
	} else if f.Kind != "" {
		b.WriteString("  ")
		b.WriteString(f.Kind)
	}
	if f.Provider != "" {
		b.WriteString("  ")
		b.WriteString(f.Provider)
		if f.Model != "" {
			b.WriteByte('/')
			b.WriteString(f.Model)
		}
	} else if f.Model != "" {
		b.WriteString("  ")
		b.WriteString(f.Model)
	}
	if f.Route != "" {
		b.WriteString("  ")
		b.WriteString(f.Route)
	}
	if msg := f.Display(); msg != "" {
		b.WriteString("  ")
		b.WriteString(msg)
	}
	return b.String()
}

func (f Fail) Log() {
	if f.Kind == KindCancel {
		logln(levelDebug, f.Line())
		return
	}
	logln(levelError, f.Line())
}

func (f Fail) ClientBody(inbound string) map[string]any {
	typ := f.Type
	if typ == "" {
		typ = TypeFor(f.Status, inbound)
	}
	msg := f.Display()
	if msg == "" {
		msg = "request failed"
	}
	if inbound == InboundClaude {
		return ClaudeError(claudeType(typ), msg)
	}
	return OpenAIError(typ, msg)
}

func ClaudeError(typ, message string) map[string]any {
	if typ == "" {
		typ = "api_error"
	}
	return map[string]any{"error": map[string]any{"type": typ, "message": message}}
}

func TypeFor(status int, inbound string) string {
	if inbound == InboundClaude {
		switch status {
		case 400:
			return "invalid_request_error"
		case 401:
			return "authentication_error"
		case 403:
			return "permission_error"
		case 404:
			return "not_found_error"
		case 429:
			return "rate_limit_error"
		default:
			return "api_error"
		}
	}
	switch status {
	case 400, 404:
		return "invalid_request_error"
	case 401:
		return "invalid_request_error"
	case 403:
		return "permission_denied"
	case 429:
		return "rate_limit_exceeded"
	default:
		return "server_error"
	}
}

func claudeType(typ string) string {
	switch typ {
	case "permission_denied":
		return "permission_error"
	case "rate_limit_exceeded":
		return "rate_limit_error"
	case "server_error":
		return "api_error"
	default:
		return typ
	}
}

func Humanize(status int, provider, extracted string) (kind, typ, message, hint string) {
	extracted = CleanMessage(extracted)
	kind = kindForStatus(status)
	typ = TypeFor(status, "")
	name := displayProvider(provider)
	hint = hintFor(status, name)
	if extracted != "" && !IsOpaque(extracted) {
		message = extracted
		return
	}
	switch status {
	case 400:
		message = "invalid request"
	case 401:
		if name != "" {
			message = name + " rejected the credential"
		} else {
			message = "invalid api key"
		}
	case 403:
		if name != "" {
			message = "not allowed to use " + name
		} else {
			message = "not allowed"
		}
	case 404:
		message = "not found"
	case 408, 504:
		if name != "" {
			message = name + " timed out"
		} else {
			message = "provider timed out"
		}
	case 429:
		if name != "" {
			message = name + " rate-limited this request"
		} else {
			message = "rate limited"
		}
	case 499:
		message = "request cancelled"
	case 500:
		if name != "" {
			message = name + " returned an internal error"
		} else {
			message = "provider returned an internal error"
		}
	case 502, 503:
		if name != "" {
			message = name + " is unavailable"
		} else {
			message = "provider is unavailable"
		}
	default:
		if name != "" {
			message = name + " request failed"
		} else {
			message = "request failed"
		}
		if status > 0 {
			message += " (" + strconv.Itoa(status) + ")"
		}
	}
	return
}

func kindForStatus(status int) string {
	switch status {
	case 400:
		return KindRequest
	case 401:
		return KindAuth
	case 403:
		return KindPermission
	case 404:
		return KindModel
	case 408, 504:
		return KindTimeout
	case 429:
		return KindUpstream
	case 499:
		return KindCancel
	default:
		return KindUpstream
	}
}

func hintFor(status int, name string) string {
	who := name
	if who == "" {
		who = "the provider"
	}
	switch status {
	case 401:
		return "reconnect " + who + " in the panel"
	case 403:
		return "check the key's provider/model scope"
	case 408, 504:
		return "retry; if this persists the upstream is slow or unreachable"
	case 429:
		return "wait, or add another " + who + " credential"
	case 500, 502, 503:
		return "retry, or try another credential"
	default:
		return ""
	}
}

func displayProvider(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "grok":
		return "Grok"
	case "cursor":
		return "Cursor"
	default:
		return strings.TrimSpace(id)
	}
}

func ClassifyErr(err error) (status int, kind, message string) {
	if err == nil {
		return 500, KindInternal, "internal error"
	}
	if errors.Is(err, context.Canceled) {
		return 499, KindCancel, "request cancelled"
	}
	var timeout timeoutErr
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		return 504, KindTimeout, "provider timed out"
	}
	var netErr net.Error
	msg := err.Error()
	if errors.As(err, &netErr) || looksLikeConnect(msg) {
		return 502, KindConnect, "cannot reach provider"
	}
	cleaned := CleanMessage(msg)
	if cleaned == "" {
		return 502, KindUpstream, "provider request failed"
	}
	return 502, KindUpstream, cleaned
}

type timeoutErr interface {
	error
	Timeout() bool
}

func looksLikeConnect(msg string) bool {
	low := strings.ToLower(msg)
	for _, n := range []string{
		"connection refused", "no such host", "network is unreachable",
		"tls:", "handshake", "connect:", "i/o timeout", "connection reset",
		"broken pipe", "unexpected eof", "eof",
	} {
		if strings.Contains(low, n) {
			return true
		}
	}
	return false
}

func ExtractErrorMessage(body any) string {
	switch t := body.(type) {
	case nil:
		return ""
	case string:
		return CleanMessage(t)
	case map[string]any:
		if errMap, ok := t["error"].(map[string]any); ok {
			if s := asCleanStr(errMap["message"]); s != "" {
				return s
			}
			if s := asCleanStr(errMap["msg"]); s != "" {
				return s
			}
		}
		if s := asCleanStr(t["message"]); s != "" {
			return s
		}
		if s, ok := t["error"].(string); ok {
			return CleanMessage(s)
		}
	}
	return ""
}

func SanitizeUpstream(status int, raw []byte) (message string, body any) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || looksLikeHTML(raw) || !utf8.Valid(raw) || looksBinary(raw) {
		return "", nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		if msg := CleanMessage(string(raw)); msg != "" && !IsOpaque(msg) {
			return msg, nil
		}
		return "", nil
	}
	msg := ExtractErrorMessage(v)
	if msg == "" || IsOpaque(msg) {
		return "", nil
	}
	return msg, OpenAIError(TypeFor(status, ""), msg)
}

func CleanMessage(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = stripURLs(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if looksLikeHTML([]byte(s)) || !utf8.ValidString(s) || IsOpaque(s) {
		return ""
	}
	if utf8.RuneCountInString(s) > 240 {
		r := []rune(s)
		s = string(r[:240]) + "…"
	}
	return s
}

func IsOpaque(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return true
	}
	if looksLikeHTML([]byte(s)) {
		return true
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "<html") || strings.Contains(low, "<!doctype") {
		return true
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return true
	}
	if strings.HasPrefix(low, "upstream ") {
		rest := strings.TrimSpace(s[len("upstream "):])
		if _, err := strconv.Atoi(rest); err == nil {
			return true
		}
	}
	letters := 0
	other := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			letters++
			continue
		}
		if strings.ContainsRune(".,;:!?()[]'\"-_/—–", r) {
			continue
		}
		other++
	}
	return other > 8 && other*2 > letters
}

func looksLikeHTML(raw []byte) bool {
	s := bytes.TrimSpace(bytes.ToLower(raw))
	return bytes.HasPrefix(s, []byte("<!doctype")) || bytes.HasPrefix(s, []byte("<html")) ||
		bytes.HasPrefix(s, []byte("<head")) || bytes.Contains(s[:min(len(s), 256)], []byte("<html"))
}

func looksBinary(raw []byte) bool {
	n := min(len(raw), 256)
	nul := 0
	for i := 0; i < n; i++ {
		if raw[i] == 0 {
			nul++
		}
	}
	return nul > 0
}

func stripURLs(s string) string {
	out := s
	for _, prefix := range []string{"https://", "http://"} {
		for {
			i := strings.Index(strings.ToLower(out), prefix)
			if i < 0 {
				break
			}
			j := i + len(prefix)
			for j < len(out) && !unicode.IsSpace(rune(out[j])) && out[j] != '"' && out[j] != '\'' {
				j++
			}
			out = strings.TrimSpace(out[:i] + out[j:])
			out = strings.TrimSpace(strings.Trim(out, ":-"))
		}
	}
	return strings.Join(strings.Fields(out), " ")
}

func asCleanStr(v any) string {
	s, _ := v.(string)
	return CleanMessage(s)
}
