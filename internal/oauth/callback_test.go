package oauth

import (
	"testing"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestParseCallbackClaudeURL(t *testing.T) {
	p := ParseCallback("http://localhost:54545/callback?code=abc&state=xyz")
	if p.Code != "abc" || p.State != "xyz" {
		t.Fatalf("%+v", p)
	}
}

func TestParseCallbackCodexURL(t *testing.T) {
	p := ParseCallback("http://localhost:1455/auth/callback?code=tok")
	if p.Code != "tok" {
		t.Fatalf("%+v", p)
	}
}

func TestParseCallbackBareCode(t *testing.T) {
	p := ParseCallback("just-a-code")
	if p.Code != "just-a-code" {
		t.Fatalf("%+v", p)
	}
}

func TestParseCallbackErrorURL(t *testing.T) {
	p := ParseCallback("http://localhost:54545/callback?error=access_denied&error_description=nope")
	if p.Error != "access_denied" || p.ErrorDescription != "nope" {
		t.Fatalf("%+v", p)
	}
}

func TestTakePendingOneShot(t *testing.T) {
	now := time.Now().UnixMilli()
	PutPending(PendingLogin{Provider: domain.ProviderClaude, State: "s1", Verifier: "v", CreatedAt: now})
	PutPending(PendingLogin{Provider: domain.ProviderClaude, State: "s2", Verifier: "v2", CreatedAt: now + 1})
	first := TakePending("s1")
	if first == nil || first.State != "s1" {
		t.Fatalf("%+v", first)
	}
	if TakePending("s1") != nil {
		t.Fatal("not one-shot")
	}
	latest := TakePendingForProvider(domain.ProviderClaude)
	if latest == nil || latest.State != "s2" {
		t.Fatalf("%+v", latest)
	}
}
