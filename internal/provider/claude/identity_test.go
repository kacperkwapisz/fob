package claude

import (
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/translate"
)

func TestUnconfirmedCallersGetCLIBetas(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"tools":    []any{map[string]any{"name": "lookup", "input_schema": map[string]any{"type": "object"}}},
		},
		"sk-ant-oat-test", "cred-1", "",
		map[string]string{"user-agent": "Cursor/1.0"},
		false, false, "key-1",
	)
	if prepared.Headers["User-Agent"] != ua {
		t.Fatalf("ua %s", prepared.Headers["User-Agent"])
	}
	beta := prepared.Headers["anthropic-beta"]
	for _, want := range []string{"claude-code-20250219", "oauth-2025-04-20", "advanced-tool-use-2025-11-20"} {
		if !strings.Contains(beta, want) {
			t.Fatalf("missing %s in %s", want, beta)
		}
	}
	body := translate.AsMap(prepared.Body)
	system := translate.AsArr(body["system"])
	if !strings.HasPrefix(translate.AsStr(translate.AsMap(system[0])["text"]), "x-anthropic-billing-header:") {
		t.Fatalf("%v", system[0])
	}
	if !strings.Contains(translate.AsStr(translate.AsMap(system[0])["text"]), "cch=") {
		t.Fatal("cch")
	}
	if translate.AsStr(translate.AsMap(system[1])["text"]) != cliIdentity {
		t.Fatalf("%v", system[1])
	}
}

func TestOmitsRedactWhenThinkingDisplay(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"thinking": map[string]any{"type": "enabled", "display": "summarized"},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	if strings.Contains(prepared.Headers["anthropic-beta"], "redact-thinking-2026-02-12") {
		t.Fatal(prepared.Headers["anthropic-beta"])
	}
}

func TestForcedToolChoiceDropsThinking(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":       "claude-opus-4-7",
			"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
			"thinking":    map[string]any{"type": "enabled"},
			"tool_choice": map[string]any{"type": "any"},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	if _, ok := translate.AsMap(prepared.Body)["thinking"]; ok {
		t.Fatal("thinking still present")
	}
}
