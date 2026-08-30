package grok

import (
	"testing"

	"github.com/kacperkwapisz/fob/internal/translate"
)

func TestSanitizeGrokBody(t *testing.T) {
	out := SanitizeBody(map[string]any{
		"model": "grok-4.5",
		"tools": []any{
			map[string]any{
				"type": "namespace", "name": "codex_app",
				"tools": []any{
					map[string]any{"type": "function", "name": "automation_update", "parameters": map[string]any{"type": "object", "oneOf": []any{map[string]any{"$ref": "#/$defs/Huge"}}, "$defs": map[string]any{"Huge": map[string]any{"type": "object"}}}},
					map[string]any{"type": "function", "name": "other", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}},
				},
			},
			map[string]any{"type": "custom", "name": "apply_patch", "parameters": map[string]any{"type": "object"}},
		},
		"stop": []any{"\n"}, "prompt_cache_retention": "24h", "previous_response_id": "resp_old",
	}, "grok-4.5")
	tools := translate.AsArr(out["tools"])
	if len(tools) != 2 {
		t.Fatalf("%d %+v", len(tools), tools)
	}
	if translate.AsStr(translate.AsMap(tools[0])["name"]) != "codex_app__automation_update" {
		t.Fatalf("%v", tools[0])
	}
	if _, ok := out["stop"]; ok {
		t.Fatal("stop")
	}
}

func TestStripReasoningOnGrokBuild(t *testing.T) {
	out := SanitizeBody(map[string]any{"model": "grok-build-0.1", "reasoning": map[string]any{"effort": "high"}}, "grok-build-0.1")
	if _, ok := out["reasoning"]; ok {
		t.Fatalf("%+v", out)
	}
}

func TestKeepReasoningOnGrok45(t *testing.T) {
	out := SanitizeBody(map[string]any{"model": "grok-4.5", "reasoning": map[string]any{"effort": "high"}}, "grok-4.5")
	if translate.AsStr(translate.AsMap(out["reasoning"])["effort"]) != "high" {
		t.Fatalf("%+v", out["reasoning"])
	}
}

func TestKeepReasoningOnGrok46(t *testing.T) {
	out := SanitizeBody(map[string]any{"model": "grok-4.6", "reasoning": map[string]any{"effort": "low"}}, "grok-4.6")
	if translate.AsStr(translate.AsMap(out["reasoning"])["effort"]) != "low" {
		t.Fatalf("%+v", out["reasoning"])
	}
}

func TestRewriteAgentMessage(t *testing.T) {
	out := SanitizeBody(map[string]any{
		"model": "grok-4.5",
		"input": []any{map[string]any{"type": "agent_message", "content": []any{map[string]any{"type": "encrypted_content", "encrypted_content": "hello"}}}},
	}, "grok-4.5")
	item := translate.AsMap(translate.AsArr(out["input"])[0])
	if translate.AsStr(item["type"]) != "message" || translate.AsStr(item["role"]) != "user" {
		t.Fatalf("%+v", item)
	}
	if translate.AsStr(translate.AsMap(translate.AsArr(item["content"])[0])["type"]) != "input_text" {
		t.Fatalf("%+v", item["content"])
	}
}
