package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseCursorSub(t *testing.T) {
	g := readGolden(t, "sub-cursor.json")
	snap := ParseSub(g["usage"], g["plan"])
	if snap.Plan != "Ultra · $200/mo" {
		t.Fatalf("plan %s", snap.Plan)
	}
	if snap.Note != "You've hit your usage limit" {
		t.Fatalf("note %s", snap.Note)
	}
	ids := map[string]float64{}
	for _, w := range snap.Windows {
		if w.UsedPercent != nil {
			ids[w.ID] = *w.UsedPercent
		}
	}
	if ids["auto"] != 20.458 || ids["api"] != 62.94 || ids["included"] != 100 {
		t.Fatalf("%+v", snap.Windows)
	}
	if snap.Windows[0].ResetsAt == nil || *snap.Windows[0].ResetsAt != 1789582434000 {
		t.Fatalf("reset %+v", snap.Windows[0].ResetsAt)
	}
}

func TestParseCursorSubDisabled(t *testing.T) {
	snap := ParseSub(map[string]any{"enabled": false, "displayMessage": "hidden"}, map[string]any{})
	if snap.Note != "hidden" || len(snap.Windows) != 0 {
		t.Fatalf("%+v", snap)
	}
}

func readGolden(t *testing.T, name string) map[string]any {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]any
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}
