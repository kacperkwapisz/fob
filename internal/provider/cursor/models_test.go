package cursor

import (
	"testing"

	"github.com/kacperkwapisz/fob/internal/domain"
)

func TestPublicIDs(t *testing.T) {
	if PublicID("claude-opus-5-medium") != "claude-opus-5-medium" {
		t.Fatal("keep id")
	}
	if PublicID("auto") != "cursor-auto" {
		t.Fatal("auto")
	}
	if PublicID("grok-4.6-medium") != "cursor-grok-4.6-medium" {
		t.Fatal(PublicID("grok-4.6-medium"))
	}
	if PublicID("cursor-grok-4.5-medium") != "cursor-grok-4.5-medium" {
		t.Fatal("keep cursor grok")
	}
	if WireID("cursor-auto") != "default" || WireID("auto") != "default" || WireID("composer-2.5") != "composer-2.5" {
		t.Fatal("wire")
	}
	if WireID("cursor-grok-4.6") != "grok-4.6" || WireID("cursor-grok-4.6-medium") != "grok-4.6-medium" {
		t.Fatal("wire grok")
	}
}

func TestLookupWireID(t *testing.T) {
	if LookupWireID("Composer 2.5") != "composer-2.5" {
		t.Fatal(LookupWireID("Composer 2.5"))
	}
	if LookupWireID("composer-2.5-fast") != "composer-2.5-fast" {
		t.Fatal(LookupWireID("composer-2.5-fast"))
	}
	if LookupWireID("Auto (default)") != "auto" {
		t.Fatal(LookupWireID("Auto (default)"))
	}
	if LookupWireID("not-a-model") != "" {
		t.Fatal(LookupWireID("not-a-model"))
	}
}

func TestPricedRoutedID(t *testing.T) {
	if pricedRoutedID("Composer 2.5") != "composer-2.5" {
		t.Fatal(pricedRoutedID("Composer 2.5"))
	}
	if pricedRoutedID("Auto (default)") != "" || pricedRoutedID("cursor-auto") != "" {
		t.Fatal("auto must not price")
	}
	if pricedRoutedID("mystery-router") != "mystery-router" {
		t.Fatal(pricedRoutedID("mystery-router"))
	}
}

func TestStripPrefix(t *testing.T) {
	if StripPublicPrefix("cursor/composer-2.5") != "composer-2.5" {
		t.Fatal("strip")
	}
}

func TestRestoreGrokAlias(t *testing.T) {
	known := KnownIDs()
	if RestoreWirePrefix("grok-4.5-medium", known) != "cursor-grok-4.5-medium" {
		t.Fatal(RestoreWirePrefix("grok-4.5-medium", known))
	}
	if RestoreWirePrefix("grok-4.6", known) != "grok-4.6" {
		t.Fatal(RestoreWirePrefix("grok-4.6", known))
	}
	if RestoreWirePrefix("grok-4.6-medium", known) != "cursor-grok-4.6-medium" {
		t.Fatal(RestoreWirePrefix("grok-4.6-medium", known))
	}
	if RestoreWirePrefix("cursor-grok-4.6", known) != "cursor-grok-4.6" {
		t.Fatal(RestoreWirePrefix("cursor-grok-4.6", known))
	}
}

func TestRestoreLiveGrokWithoutCursorPrefix(t *testing.T) {
	prev := liveIDs
	t.Cleanup(func() { liveIDs = prev })
	liveIDs = []string{"grok-4.6-low", "grok-4.6-medium", "grok-4.6-high", "grok-4.6-xhigh", "grok-4.6-xhigh-fast"}
	if RestoreWirePrefix("grok-4.6", liveIDs) != "grok-4.6" {
		t.Fatal(RestoreWirePrefix("grok-4.6", liveIDs))
	}
	if RestoreWirePrefix("cursor-grok-4.6", liveIDs) != "cursor-grok-4.6" {
		t.Fatal(RestoreWirePrefix("cursor-grok-4.6", liveIDs))
	}
	if got := MapNativeToWire("grok-4.6", liveIDs); got != "grok-4.6-medium" {
		t.Fatalf("map %s", got)
	}
}

func TestMapClaudeOpus5(t *testing.T) {
	if MapNativeToWire("claude-opus-5", KnownIDs()) != "claude-opus-5-medium" {
		t.Fatal(MapNativeToWire("claude-opus-5", KnownIDs()))
	}
}

func TestExpandKeepsVariantIDs(t *testing.T) {
	prev := liveIDs
	t.Cleanup(func() { liveIDs = prev })
	models := ExpandAvailableModels(map[string]any{
		"models": []any{map[string]any{
			"name": "composer-2.5", "displayName": "Composer 2.5",
			"variantIds": map[string]any{
				"":     map[string]any{"standard": "composer-2.5", "fast": "composer-2.5-fast"},
				"high": map[string]any{"standard": "composer-2.5-high"},
			},
		}},
	})
	if len(models) != 1 || models[0].VariantIDs[""].fast != "composer-2.5-fast" {
		t.Fatalf("%+v", models)
	}
	RegisterModelVariants(models)
	if got := resolveModelID("composer-2.5", "", true); got != "composer-2.5-fast" {
		t.Fatalf("got %s", got)
	}
	listed := ToModelInfo(models, false)
	if len(listed) != 2 || listed[0].ID != "composer-2.5" || listed[1].ID != "composer-2.5-fast" {
		t.Fatalf("listed %+v", listed)
	}
	if !equalStrings(listed[0].Efforts, []string{"high"}) || !equalStrings(listed[1].Efforts, []string{"high"}) {
		t.Fatalf("live variantIds efforts %+v", listed)
	}
}

func TestExpandVariants(t *testing.T) {
	models := ExpandAvailableModels(map[string]any{
		"models": []any{map[string]any{
			"name": "composer-2.5", "displayName": "Composer 2.5",
			"variants": []any{
				map[string]any{"parameterValues": []any{}},
				map[string]any{"parameterValues": []any{map[string]any{"id": "fast", "value": "true"}}},
			},
		}},
	})
	if len(models) != 2 || models[0].ID != "composer-2.5" || models[1].ID != "composer-2.5-fast" {
		t.Fatalf("%+v", models)
	}
}

func TestPublicFamilyID(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-7-high-fast":           "claude-opus-4-7",
		"claude-opus-4-7-thinking-xhigh-fast": "claude-opus-4-7-thinking",
		"claude-fable-5-thinking-high":        "claude-fable-5-thinking",
		"claude-4.6-opus-high-thinking":       "claude-4.6-opus-thinking",
		"gpt-5.5-extra-high-fast":             "gpt-5.5",
		"gpt-5.4-mini-high":                   "gpt-5.4-mini",
		"gemini-3.6-flash-minimal":            "gemini-3.6-flash",
		"composer-2.5-fast":                   "composer-2.5",
		"cursor-grok-4.5-medium-fast":         "cursor-grok-4.5",
		"grok-4.6-xhigh-fast":                 "grok-4.6",
		"cursor-grok-4.6-medium":              "cursor-grok-4.6",
		"gpt-5.2":                             "gpt-5.2",
		"auto":                                "auto",
	}
	for in, want := range cases {
		if got := publicFamilyID(in); got != want {
			t.Fatalf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestCollapseForListDropsEffortAndFast(t *testing.T) {
	collapsed := CollapseForList(Snapshot())
	ids := map[string]string{}
	for _, m := range collapsed {
		ids[m.ID] = m.Name
	}
	for _, id := range []string{
		"claude-opus-4-7-high", "claude-opus-4-7-high-fast", "claude-opus-5-medium",
		"composer-2.5-fast", "gpt-5.2-high", "cursor-grok-4.5-medium", "cursor-grok-4.6-xhigh",
	} {
		if _, ok := ids[id]; ok {
			t.Fatalf("variant still listed %s", id)
		}
	}
	for _, id := range []string{
		"claude-opus-4-7", "claude-opus-4-7-thinking", "claude-opus-5", "claude-opus-5-thinking",
		"composer-2.5", "gpt-5.2", "gpt-5.4", "gpt-5.4-mini", "cursor-grok-4.5", "cursor-grok-4.6", "auto",
	} {
		if _, ok := ids[id]; !ok {
			t.Fatalf("missing family %s", id)
		}
	}
	listed := ToModelInfo(Snapshot(), false)
	listedIDs := map[string]bool{}
	for _, m := range listed {
		listedIDs[m.ID] = true
	}
	if !listedIDs["cursor-auto"] {
		t.Fatal("cursor-auto")
	}
	for _, id := range []string{"composer-2.5-fast", "claude-opus-5-fast", "claude-opus-5-thinking-fast", "cursor-grok-4.6-fast"} {
		if !listedIDs[id] {
			t.Fatalf("missing fast twin %s", id)
		}
	}
	if listedIDs["claude-opus-5-high-fast"] || listedIDs["composer-2.5-fast-fast"] {
		t.Fatal("effort fast still listed")
	}
	if ids["claude-opus-4-7"] != "Opus 4.7 1M" {
		t.Fatalf("opus 4.7 name %q", ids["claude-opus-4-7"])
	}
	if ids["claude-opus-5"] != "Opus 5 1M" {
		t.Fatalf("opus 5 name %q", ids["claude-opus-5"])
	}
	if ids["composer-2.5"] != "Composer 2.5" {
		t.Fatalf("composer name %q", ids["composer-2.5"])
	}
	if n := len(collapsed); n > 50 || n < 20 {
		t.Fatalf("collapsed len %d", n)
	}
	byID := map[string]domain.ModelInfo{}
	for _, m := range listed {
		byID[m.ID] = m
	}
	opus := byID["claude-opus-5"]
	if opus.ContextLength != 1000000 || opus.MaxOutputTokens != 128000 {
		t.Fatalf("opus limits %+v", opus)
	}
	if !equalStrings(opus.Efforts, []string{"low", "medium", "high"}) {
		t.Fatalf("opus efforts %v", opus.Efforts)
	}
	thinking := byID["claude-opus-5-thinking"]
	if !thinking.Reasoning || !equalStrings(thinking.Efforts, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("thinking ladder %+v", thinking)
	}
	gpt52 := byID["gpt-5.2"]
	if gpt52.ContextLength != 400000 || gpt52.MaxOutputTokens != 128000 {
		t.Fatalf("gpt-5.2 limits %+v", gpt52)
	}
	if gpt52.Cost == nil || gpt52.Cost.Input == nil || *gpt52.Cost.Input != 1.75 {
		t.Fatalf("gpt-5.2 cost %+v", gpt52.Cost)
	}
	gpt := byID["gpt-5.5"]
	if !equalStrings(gpt.Efforts, []string{"none", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("gpt-5.5 efforts %v", gpt.Efforts)
	}
	composer := byID["composer-2.5"]
	if composer.Cost == nil || composer.Cost.Input == nil || *composer.Cost.Input != 0.5 {
		t.Fatalf("composer cost %+v", composer.Cost)
	}
	if len(composer.Efforts) != 0 {
		t.Fatalf("composer efforts %v", composer.Efforts)
	}
	grok46 := byID["cursor-grok-4.6"]
	if grok46.ID == "" {
		t.Fatal("missing cursor-grok-4.6")
	}
	if grok46.Name != "Cursor Grok 4.6" {
		t.Fatalf("grok 4.6 name %q", grok46.Name)
	}
	if !equalStrings(grok46.Efforts, []string{"low", "medium", "high", "xhigh"}) {
		t.Fatalf("grok 4.6 efforts %v", grok46.Efforts)
	}
}

func TestLiveGrokListsAsCursorPrefixed(t *testing.T) {
	listed := ToModelInfo([]Model{
		{ID: "grok-4.6-low", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-medium", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-high", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-xhigh", Name: "Cursor Grok 4.6"},
		{ID: "grok-4.6-xhigh-fast", Name: "Cursor Grok 4.6 (fast)"},
	}, false)
	if len(listed) != 2 || listed[0].ID != "cursor-grok-4.6" || listed[1].ID != "cursor-grok-4.6-fast" {
		t.Fatalf("%+v", listed)
	}
	if listed[0].OwnedBy != "cursor" {
		t.Fatalf("owned %s", listed[0].OwnedBy)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestMapNativeToWireThinking(t *testing.T) {
	got := MapNativeToWire("claude-opus-4-7-thinking", KnownIDs())
	if !stringsContains(got, "thinking") || publicFamilyID(got) != "claude-opus-4-7-thinking" {
		t.Fatalf("thinking mapped to %s", got)
	}
	if MapNativeToWire("claude-opus-5", KnownIDs()) != "claude-opus-5-medium" {
		t.Fatal(MapNativeToWire("claude-opus-5", KnownIDs()))
	}
	if MapNativeToWire("gpt-5.4", KnownIDs()) != "gpt-5.4-medium" {
		t.Fatal(MapNativeToWire("gpt-5.4", KnownIDs()))
	}
	if stripEffort("gpt-5.5-extra-high") != "gpt-5.5" {
		t.Fatal(stripEffort("gpt-5.5-extra-high"))
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
