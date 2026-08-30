package cursor

import "testing"

func TestPublicIDs(t *testing.T) {
	if PublicID("claude-opus-5-medium") != "claude-opus-5-medium" {
		t.Fatal("keep id")
	}
	if PublicID("auto") != "cursor-auto" {
		t.Fatal("auto")
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
