package cursor

import (
	"strings"
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
	if got := resolveModelID("claude-opus-5-thinking", "medium"); !strings.Contains(got, "thinking") {
		t.Fatalf("thinking %q", got)
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
