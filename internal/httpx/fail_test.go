package httpx

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestSanitizeUpstreamDropsHTMLAndProto(t *testing.T) {
	msg, body := SanitizeUpstream(502, []byte("<!DOCTYPE html><html><body>cloudflare</body></html>"))
	if msg != "" || body != nil {
		t.Fatalf("html leaked %q %+v", msg, body)
	}
	msg, body = SanitizeUpstream(500, []byte{0x00, 0x01, 0x02, 0xff})
	if msg != "" || body != nil {
		t.Fatalf("binary leaked %q %+v", msg, body)
	}
}

func TestSanitizeUpstreamExtractsJSONMessage(t *testing.T) {
	msg, body := SanitizeUpstream(429, []byte(`{"error":{"message":"rate limited by anthropic","type":"rate_limit_error"}}`))
	if msg != "rate limited by anthropic" {
		t.Fatalf("msg %q", msg)
	}
	errMap := body.(map[string]any)["error"].(map[string]any)
	if errMap["message"] != "rate limited by anthropic" || errMap["type"] != "rate_limit_exceeded" {
		t.Fatalf("%+v", body)
	}
}

func TestClientBodyShapes(t *testing.T) {
	f := Fail{Status: 503, Type: "server_error", Message: "no grok credential is connected", Hint: "open the panel and login to grok"}
	oa := f.ClientBody("openai.chat")
	err := oa["error"].(map[string]any)
	if err["type"] != "server_error" || !strings.Contains(err["message"].(string), "open the panel") {
		t.Fatalf("%+v", oa)
	}
	cl := f.ClientBody(InboundClaude)
	cerr := cl["error"].(map[string]any)
	if cerr["type"] != "api_error" || cerr["message"] != err["message"] {
		t.Fatalf("%+v", cl)
	}
	if _, ok := cerr["code"]; ok {
		t.Fatalf("claude shape has code: %+v", cl)
	}
}

func TestHumanizePrefersExtractedThenStatus(t *testing.T) {
	_, typ, msg, hint := Humanize(429, "claude", "you've hit your limit")
	if typ != "rate_limit_exceeded" || msg != "you've hit your limit" || !strings.Contains(hint, "Claude") {
		t.Fatalf("%s %q %q", typ, msg, hint)
	}
	_, _, msg, hint = Humanize(429, "claude", "<html>nope</html>")
	if !strings.Contains(msg, "rate-limited") || !strings.Contains(hint, "wait") {
		t.Fatalf("%q %q", msg, hint)
	}
	_, _, msg, _ = Humanize(502, "cursor", "upstream 502")
	if !strings.Contains(msg, "Cursor") || strings.Contains(msg, "upstream") {
		t.Fatalf("%q", msg)
	}
}

func TestClassifyErr(t *testing.T) {
	status, kind, msg := ClassifyErr(context.DeadlineExceeded)
	if status != 504 || kind != KindTimeout || !strings.Contains(msg, "timed out") {
		t.Fatalf("%d %s %q", status, kind, msg)
	}
	status, kind, msg = ClassifyErr(context.Canceled)
	if status != 499 || kind != KindCancel {
		t.Fatalf("%d %s %q", status, kind, msg)
	}
	status, kind, msg = ClassifyErr(&net.DNSError{Err: "no such host", Name: "api.anthropic.com", IsNotFound: true})
	if status != 502 || kind != KindConnect || msg != "cannot reach provider" {
		t.Fatalf("%d %s %q", status, kind, msg)
	}
	status, kind, msg = ClassifyErr(errors.New(`Post "https://api.anthropic.com/v1/messages?beta=true": EOF`))
	if status != 502 || kind != KindConnect {
		t.Fatalf("%d %s %q", status, kind, msg)
	}
}

func TestFailLineAndLog(t *testing.T) {
	var buf bytes.Buffer
	defer SetLogOutput(&buf)()
	SetLogLevel("info")
	Fail{
		Kind: KindNoCredential, Status: 503, Provider: "grok", Model: "grok-4.6",
		Route: "openai.chat", Message: "no grok credential is connected", Hint: "open the panel and login to grok",
	}.Log()
	got := buf.String()
	for _, want := range []string{"fob error", "503", "grok/grok-4.6", "openai.chat", "no grok credential", "open the panel"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<html") {
		t.Fatalf("dump in log: %q", got)
	}
}

func TestSuccessQuietUnlessDebug(t *testing.T) {
	var buf bytes.Buffer
	defer SetLogOutput(&buf)()
	SetLogLevel("info")
	LogOK("claude", "claude-opus-4-7", "openai.chat")
	if buf.Len() != 0 {
		t.Fatalf("info leaked ok: %q", buf.String())
	}
	SetLogLevel("debug")
	LogOK("claude", "claude-opus-4-7", "openai.chat")
	if !strings.Contains(buf.String(), "fob  200  claude/claude-opus-4-7") {
		t.Fatalf("%q", buf.String())
	}
	SetLogLevel("info")
}

func TestCancelIsDebugOnly(t *testing.T) {
	var buf bytes.Buffer
	defer SetLogOutput(&buf)()
	SetLogLevel("info")
	Fail{Kind: KindCancel, Status: 499, Message: "request cancelled"}.Log()
	if buf.Len() != 0 {
		t.Fatalf("cancel at info: %q", buf.String())
	}
}
