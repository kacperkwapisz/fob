package cursor

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/kacperkwapisz/fob/internal/provider/cursor/agentpb"
)

func TestResolveModelIDDoesNotDoubleEffort(t *testing.T) {
	cases := []struct{ model, effort, want string }{
		{"gpt-5.6-terra-medium", "", "gpt-5.6-terra-medium"},
		{"gpt-5.6-terra-medium", "medium", "gpt-5.6-terra-medium"},
		{"gpt-5.6-terra", "medium", "gpt-5.6-terra-medium"},
		{"gpt-5.6-terra-medium", "high", "gpt-5.6-terra-high"},
		{"claude-opus-5", "", "claude-opus-5-medium"},
		{"claude-opus-5-medium", "medium", "claude-opus-5-medium"},
		{"composer-2.5-fast", "", "composer-2.5-fast"},
	}
	for _, c := range cases {
		got := resolveModelID(c.model, c.effort)
		if got != c.want {
			t.Errorf("resolveModelID(%q,%q)=%q want %q", c.model, c.effort, got, c.want)
		}
	}
}

func TestResolveRequestedModelStripsExistingEffort(t *testing.T) {
	sel := resolveRequestedModel("gpt-5.6-terra-medium", "medium")
	if sel == nil {
		t.Fatal("nil selection")
	}
	if sel.ModelID != "gpt-5.6-terra" {
		t.Fatalf("model id %q", sel.ModelID)
	}
	got := ""
	for _, p := range sel.Parameters {
		if p.ID == "effort" {
			got = p.Value
		}
	}
	if got != "medium" {
		t.Fatalf("effort %q params %+v", got, sel.Parameters)
	}
}

func TestResolveModelIDKeepsThinkingTwin(t *testing.T) {
	if got := resolveModelID("claude-opus-5-medium", "medium"); got != "claude-opus-5-medium" {
		t.Fatalf("non-thinking %q", got)
	}
	if got := resolveModelID("claude-opus-5-thinking", "medium"); got != "claude-opus-5-thinking-medium" {
		t.Fatalf("thinking %q", got)
	}
}

func selParam(sel *requestedModelSelection, id string) string {
	if sel == nil {
		return ""
	}
	for _, p := range sel.Parameters {
		if p.ID == id {
			return p.Value
		}
	}
	return ""
}

func TestResolveRequestedModelThinkingTwin(t *testing.T) {
	non := resolveRequestedModel("claude-opus-5", "medium")
	if non == nil || non.ModelID != "claude-opus-5" {
		t.Fatalf("non-thinking %+v", non)
	}
	if selParam(non, "thinking") != "false" || selParam(non, "effort") != "medium" {
		t.Fatalf("non-thinking params %+v", non.Parameters)
	}

	th := resolveRequestedModel("claude-opus-5-thinking", "medium")
	if th == nil || th.ModelID != "claude-opus-5" {
		t.Fatalf("thinking %+v", th)
	}
	if selParam(th, "thinking") != "true" || selParam(th, "effort") != "medium" {
		t.Fatalf("thinking params %+v", th.Parameters)
	}

	xhigh := resolveRequestedModel("claude-opus-5-thinking", "xhigh")
	if selParam(xhigh, "thinking") != "true" || selParam(xhigh, "effort") != "xhigh" {
		t.Fatalf("xhigh params %+v", xhigh.Parameters)
	}

	if resolveModelID("claude-opus-5", "medium") != "claude-opus-5-medium" {
		t.Fatalf("wire non-thinking %q", resolveModelID("claude-opus-5", "medium"))
	}
	if resolveModelID("claude-opus-5-thinking", "xhigh") != "claude-opus-5-thinking-xhigh" {
		t.Fatalf("wire thinking %q", resolveModelID("claude-opus-5-thinking", "xhigh"))
	}
}

func TestSelectionFromBodyThinkingBlock(t *testing.T) {
	sel := selectionFromBody(map[string]any{
		"model":    "claude-opus-5",
		"thinking": map[string]any{"type": "enabled", "budget_tokens": 16000.0},
	})
	if selParam(sel, "thinking") != "true" || selParam(sel, "effort") != "high" {
		t.Fatalf("enabled thinking %+v", sel)
	}

	off := selectionFromBody(map[string]any{"model": "claude-opus-5", "reasoning_effort": "high"})
	if selParam(off, "thinking") != "false" || selParam(off, "effort") != "high" {
		t.Fatalf("effort only %+v", off)
	}

	grok := selectionFromBody(map[string]any{
		"model":    "cursor-grok-4.6",
		"thinking": map[string]any{"type": "enabled"},
	})
	if selParam(grok, "thinking") != "" {
		t.Fatalf("grok should not grow a thinking flag %+v", grok)
	}
}

func TestVariantIndexKeepsThinkingSeparate(t *testing.T) {
	non := lookupVariants("claude-opus-5")
	if non["medium"].standard != "claude-opus-5-medium" {
		t.Fatalf("non-thinking slot %+v", non["medium"])
	}
	th := lookupVariants("claude-opus-5-thinking")
	if th["medium"].standard != "claude-opus-5-thinking-medium" {
		t.Fatalf("thinking slot %+v", th["medium"])
	}
	if lookupVariants("claude-opus-5")["xhigh"].standard != "" {
		t.Fatalf("non-thinking picked up xhigh %+v", lookupVariants("claude-opus-5")["xhigh"])
	}
}

func TestSuffixedEffortUsesRequestedFamily(t *testing.T) {
	sel := resolveRequestedModel("gpt-5.6-terra-medium", "medium")
	payload := buildCursorRequest(resolveModelID("gpt-5.6-terra-medium", "medium"), "", "hi", nil, "c1", nil, nil, sel, nil, false, nil)
	var msg agentpb.AgentClientMessage
	if err := proto.Unmarshal(payload.requestBytes, &msg); err != nil {
		t.Fatal(err)
	}
	run := msg.GetRunRequest()
	if run == nil || run.GetRequestedModel() == nil {
		t.Fatalf("run=%+v", run)
	}
	if run.GetRequestedModel().GetModelId() != "gpt-5.6-terra" {
		t.Fatalf("requested=%+v", run.GetRequestedModel())
	}
	if run.GetModelDetails() != nil {
		t.Fatalf("must not send modelDetails when requestedModel is set: %+v", run.GetModelDetails())
	}
}
