package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseClaudeSub(t *testing.T) {
	g := readGolden(t, "sub-claude.json")
	snap := ParseSub(g["usage"], g["profile"])
	if snap.Plan != "Max" {
		t.Fatalf("plan %s", snap.Plan)
	}
	if snap.Note != "$12.34 / $50.00 extra" {
		t.Fatalf("note %s", snap.Note)
	}
	if len(snap.Windows) < 2 {
		t.Fatalf("windows %+v", snap.Windows)
	}
	if snap.Windows[0].ID != "five-hour" || snap.Windows[0].UsedPercent == nil || *snap.Windows[0].UsedPercent != 42.5 {
		t.Fatalf("%+v", snap.Windows[0])
	}
	if snap.Windows[0].ResetsAt == nil {
		t.Fatal("missing reset")
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
