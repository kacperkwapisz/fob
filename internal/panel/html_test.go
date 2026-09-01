package panel

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/store"
)

func TestLayoutShell(t *testing.T) {
	html := Layout("Fob — unlock", "local proxy", "<p>hi</p>")
	for _, want := range []string{
		"Fraunces",
		"/design.css",
		"/alpine.js",
		"/panel.js",
		`href="#main"`,
		`x-data="fob"`,
		"<p>hi</p>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, ">Lock<") {
		t.Fatal("unauthed layout should not lock")
	}
}

func TestAuthedLayoutLock(t *testing.T) {
	html := AuthedLayout("Fob", "local", "<p>panel</p>")
	if !strings.Contains(html, `action="/logout"`) || !strings.Contains(html, ">Lock<") {
		t.Fatal(html)
	}
}

func TestLoginUnlocks(t *testing.T) {
	html := LoginView("wrong password")
	for _, want := range []string{"Unlock", "wrong password", `name="password"`, "Show"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestDashboardStructure(t *testing.T) {
	html := Dashboard(DashboardProps{
		Credentials: []domain.Credential{
			{ID: "c1", Provider: domain.ProviderClaude, Label: "work claude"},
		},
		Keys: []domain.LocalKey{
			{ID: "k1", Name: "opencode", Prefix: "sk-fob-9f3a"},
		},
		Usage: UsageProps{
			Today: store.UsageTotals{USD: 1.2, PromptTokens: 100, CompletionTokens: 20, Requests: 3},
			D7:    store.UsageTotals{USD: 8},
			ByProvider: []store.UsageBreakdown{
				{Key: "claude", UsageTotals: store.UsageTotals{USD: 5}},
			},
			ByModel: []store.UsageBreakdown{
				{Key: "claude-opus-4-6", UsageTotals: store.UsageTotals{USD: 5}},
			},
		},
		Settings: SettingsProps{CursorPrefix: true},
	})
	for _, want := range []string{
		"Logins", "Keys", "Meter", "Cursor",
		"work claude", "Disconnect", "opencode", "sk-fob-9f3a",
		"$1.20", "$8.00", "lamp-on", "fob-tag", "bar-fill",
		`checked`, "Mint key",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, ">Out<") {
		t.Fatal("old Out label still present")
	}
	if !strings.Contains(html, ">Claude<") && !strings.Contains(strings.ToLower(html), "claude") {
		t.Fatal("provider missing")
	}
}

func TestEmptyMeter(t *testing.T) {
	html := Dashboard(DashboardProps{})
	if !strings.Contains(html, "$0.00") || !strings.Contains(html, "No traffic yet") || !strings.Contains(html, "No keys yet") {
		t.Fatal(html)
	}
}
