package claude

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/translate"
)

func eSig() string {
	return base64.StdEncoding.EncodeToString([]byte{0x12, 0x01, 0x02, 0x03})
}

func TestDropsInvalidThinkingBlocks(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model": "claude-opus-4-7",
			"messages": []any{
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "thinking", "thinking": "secret", "signature": "not-a-claude-sig"},
						map[string]any{"type": "text", "text": "hi"},
					},
				},
			},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	msgs := translate.AsArr(translate.AsMap(prepared.Body)["messages"])
	content := translate.AsArr(translate.AsMap(msgs[0])["content"])
	if len(content) != 1 {
		t.Fatalf("content %+v", content)
	}
	if translate.AsStr(translate.AsMap(content[0])["type"]) != "text" {
		t.Fatalf("%+v", content[0])
	}
}

func TestKeepsValidThinkingSignature(t *testing.T) {
	sig := eSig()
	prepared := PrepareUpstream(
		map[string]any{
			"model": "claude-opus-4-7",
			"messages": []any{
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "thinking", "thinking": "ok", "signature": sig},
						map[string]any{"type": "text", "text": "hi"},
					},
				},
			},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	msgs := translate.AsArr(translate.AsMap(prepared.Body)["messages"])
	content := translate.AsArr(translate.AsMap(msgs[0])["content"])
	if len(content) != 2 {
		t.Fatalf("content %+v", content)
	}
	got := translate.AsStr(translate.AsMap(content[0])["signature"])
	if got != sig {
		t.Fatalf("sig %q want %q", got, sig)
	}
}

func TestStripsToolUseSignatures(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model": "claude-opus-4-7",
			"messages": []any{
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{
							"type": "tool_use", "id": "1", "name": "lookup",
							"input": map[string]any{"q": "x"}, "signature": "Eabc",
							"extra_content": map[string]any{"google": map[string]any{"thought_signature": "g"}},
						},
					},
				},
			},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	msgs := translate.AsArr(translate.AsMap(prepared.Body)["messages"])
	part := translate.AsMap(translate.AsArr(translate.AsMap(msgs[0])["content"])[0])
	if _, ok := part["signature"]; ok {
		t.Fatalf("%+v", part)
	}
	if _, ok := part["extra_content"]; ok {
		t.Fatalf("%+v", part)
	}
}

func TestDropsEmptyWebSearchDomains(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"tools": []any{
				map[string]any{"type": "web_search_20250305", "name": "web_search", "allowed_domains": []any{}, "blocked_domains": []any{}},
			},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	tool := translate.AsMap(translate.AsArr(translate.AsMap(prepared.Body)["tools"])[0])
	if _, ok := tool["allowed_domains"]; ok {
		t.Fatalf("%+v", tool)
	}
	if _, ok := tool["blocked_domains"]; ok {
		t.Fatalf("%+v", tool)
	}
}

func TestCountTokensBetasIncludeContextManagement(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, true, "key-1",
	)
	beta := prepared.Headers["anthropic-beta"]
	for _, want := range []string{codeBeta, oauthBeta, "interleaved-thinking-2025-05-14", "context-management-2025-06-27", "token-counting-2024-11-01"} {
		if !strings.Contains(beta, want) {
			t.Fatalf("missing %s in %s", want, beta)
		}
	}
	if _, ok := prepared.Headers["X-Stainless-Timeout"]; ok {
		t.Fatal("count_tokens should omit X-Stainless-Timeout")
	}
}

func TestAdvisorToolAddsBeta(t *testing.T) {
	prepared := PrepareUpstream(
		map[string]any{
			"model":    "claude-opus-4-7",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"tools":    []any{map[string]any{"type": "advisor_20260301", "name": "advisor"}},
		},
		"sk-ant-oat-test", "cred-1", "", map[string]string{}, false, false, "key-1",
	)
	if !strings.Contains(prepared.Headers["anthropic-beta"], advisorBeta) {
		t.Fatal(prepared.Headers["anthropic-beta"])
	}
}
