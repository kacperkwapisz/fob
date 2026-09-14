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

func TestResolveCursorGrok46(t *testing.T) {
	if got := resolveModelID("cursor-grok-4.6", "medium"); got != "cursor-grok-4.6-medium" {
		t.Fatalf("snapshot resolve %s", got)
	}
	sel := resolveRequestedModel("cursor-grok-4.6", "xhigh")
	if sel == nil {
		t.Fatal("nil")
	}
	if sel.ModelID != "grok-4.6" {
		t.Fatalf("model id %q", sel.ModelID)
	}
	got := ""
	for _, p := range sel.Parameters {
		if p.ID == "effort" {
			got = p.Value
		}
	}
	if got != "xhigh" {
		t.Fatalf("effort %q params %+v", got, sel.Parameters)
	}

	prevLive, prevIndex := liveIDs, variantIndex
	t.Cleanup(func() {
		liveIDs = prevLive
		variantIndex = prevIndex
	})
	variantIndex = map[string]map[string]variantPair{}
	models := []Model{
		{ID: "grok-4.6-low", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-medium", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-high", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-xhigh", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-xhigh-fast", Name: "Cursor Grok 4.6 (fast)"},
	}
	RegisterModelVariants(models)
	if got := resolveModelID("cursor-grok-4.6", "xhigh", true); got != "grok-4.6-xhigh-fast" {
		t.Fatalf("live resolve %s", got)
	}
	sel = resolveRequestedModel("cursor-grok-4.6", "medium")
	if sel == nil || sel.ModelID != "grok-4.6" {
		t.Fatalf("live requested %+v", sel)
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
