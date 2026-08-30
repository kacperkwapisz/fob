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
		return
	}
	if sel.ModelID == "" {
		t.Fatal("empty model id")
	}
}
