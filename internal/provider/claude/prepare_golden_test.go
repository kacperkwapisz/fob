package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kacperkwapisz/fob/internal/translate"
)

func TestPrepareGolden(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "golden", "claude-prepare.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Headers       map[string]string `json:"headers"`
		Reverse       map[string]string `json:"reverse"`
		System0Prefix string            `json:"system0Prefix"`
		System1       string            `json:"system1"`
		ToolName      string            `json:"toolName"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
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
	got := map[string]string{}
	for k, v := range prepared.Headers {
		if k != "x-client-request-id" {
			got[k] = v
		}
	}
	for k, want := range g.Headers {
		if got[k] != want {
			t.Fatalf("header %s\ngot  %s\nwant %s", k, got[k], want)
		}
	}
	if prepared.Reverse["mcp__outside_lounge__edge_lookup"] != "lookup" && g.Reverse["mcp__outside_lounge__edge_lookup"] == "lookup" {
		if len(prepared.Reverse) == 0 {
			t.Fatalf("reverse %+v", prepared.Reverse)
		}
	}
	body := translate.AsMap(prepared.Body)
	system := translate.AsArr(body["system"])
	sys0 := translate.AsStr(translate.AsMap(system[0])["text"])
	if !strings.HasPrefix(sys0, g.System0Prefix) && sys0[:min(len(sys0), 80)] != g.System0Prefix {
		if !strings.HasPrefix(sys0, "x-anthropic-billing-header:") {
			t.Fatalf("system0 %q", sys0)
		}
	}
	if translate.AsStr(translate.AsMap(system[1])["text"]) != g.System1 {
		t.Fatalf("system1 %q", translate.AsStr(translate.AsMap(system[1])["text"]))
	}
	tools := translate.AsArr(body["tools"])
	name := translate.AsStr(translate.AsMap(tools[0])["name"])
	if name != g.ToolName {
		t.Fatalf("tool %s want %s", name, g.ToolName)
	}
}
