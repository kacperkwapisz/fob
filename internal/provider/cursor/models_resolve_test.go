package cursor

import "testing"

func TestResolveModelIDUsesCatalog(t *testing.T) {
	if got := resolveModelID("claude-opus-5", ""); got != "claude-opus-5-medium" {
		t.Fatalf("got %s", got)
	}
	if got := resolveModelID("composer-2.5-fast", ""); !containsStr(got, "composer-2.5") {
		t.Fatalf("got %s", got)
	}
}

func TestResolveRequestedModelFromSnapshot(t *testing.T) {
	sel := resolveRequestedModel("claude-opus-5", "medium")
	if sel == nil {
		t.Fatal("nil")
	}
	if sel.ModelID != "claude-opus-5" {
		t.Fatalf("model id %q", sel.ModelID)
	}
}

func TestResolveRequestedModelAliasesXhigh(t *testing.T) {
	sel := resolveRequestedModel("gpt-5.5", "xhigh")
	if sel == nil {
		t.Fatal("nil")
	}
	got := ""
	for _, p := range sel.Parameters {
		if p.ID == "effort" {
			got = p.Value
		}
	}
	if got != "extra-high" {
		t.Fatalf("effort %q params %+v", got, sel.Parameters)
	}
}
