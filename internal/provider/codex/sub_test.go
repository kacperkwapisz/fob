package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseCodexSub(t *testing.T) {
	nowMs = func() int64 { return time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC).UnixMilli() }
	g := readGolden(t, "sub-codex.json")
	snap := ParseSub(g)
	if snap.Plan != "Plus" {
		t.Fatalf("plan %s", snap.Plan)
	}
	if snap.Note != "2 reset credits" {
		t.Fatalf("note %s", snap.Note)
	}
	if len(snap.Windows) != 2 {
		t.Fatalf("windows %+v", snap.Windows)
	}
	if snap.Windows[0].ID != "five-hour" || snap.Windows[0].UsedPercent == nil || *snap.Windows[0].UsedPercent != 31.2 {
		t.Fatalf("%+v", snap.Windows[0])
	}
	if snap.Windows[1].ID != "weekly" {
		t.Fatalf("%+v", snap.Windows[1])
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
