package cursor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kacperkwapisz/fob/internal/sub"
)

func TestParseCursorSub(t *testing.T) {
	g := readGolden(t, "sub-cursor.json")
	snap := ParseSub(g["usage"], g["plan"])
	if snap.Plan != "Ultra · $200/mo" {
		t.Fatalf("plan %s", snap.Plan)
	}
	if snap.Note != "" {
		t.Fatalf("note %s", snap.Note)
	}
	got := map[string]sub.Window{}
	for _, w := range snap.Windows {
		got[w.ID] = w
	}
	if got["auto"].Label != "Cursor Models" || got["auto"].UsedPercent == nil || *got["auto"].UsedPercent != 20.458 {
		t.Fatalf("auto %+v", got["auto"])
	}
	if got["api"].Label != "Other Models" || got["api"].UsedPercent == nil || *got["api"].UsedPercent != 62.94 {
		t.Fatalf("api %+v", got["api"])
	}
	if _, ok := got["included"]; ok {
		t.Fatalf("included %+v", snap.Windows)
	}
	if _, ok := got["extra"]; ok {
		t.Fatalf("extra %+v", snap.Windows)
	}
	if len(got) != 2 {
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
