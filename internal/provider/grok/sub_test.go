package grok

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseGrokSub(t *testing.T) {
	g := readGolden(t, "sub-grok.json")
	snap := ParseSub(g["weekly"], g["monthly"])
	if snap.Plan != "SuperGrok" {
		t.Fatalf("plan %s", snap.Plan)
	}
	if len(snap.Windows) < 3 {
		t.Fatalf("windows %+v", snap.Windows)
	}
	if snap.Windows[0].ID != "weekly" || snap.Windows[0].UsedPercent == nil || *snap.Windows[0].UsedPercent != 44 {
		t.Fatalf("%+v", snap.Windows[0])
	}
	found := false
	for _, w := range snap.Windows {
		if w.ID == "monthly" {
			found = true
			if w.Detail != "$105.00 / $150.00" {
				t.Fatalf("detail %s", w.Detail)
			}
			if w.UsedPercent == nil || *w.UsedPercent != 30 {
				t.Fatalf("%+v", w)
			}
		}
	}
	if !found {
		t.Fatalf("missing monthly %+v", snap.Windows)
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
