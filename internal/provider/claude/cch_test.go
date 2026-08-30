package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cespare/xxhash/v2"
)

func TestCchGoldens(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "golden", "cch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Simple struct {
			Input  string `json:"input"`
			Output string `json:"output"`
		} `json:"simple"`
		InjectFallback struct {
			Input  string `json:"input"`
			Output string `json:"output"`
		} `json:"injectFallback"`
		XXHash struct {
			InputUTF8 string `json:"inputUtf8"`
			HashHex   string `json:"hashHex"`
		} `json:"xxhashSeedKnown"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	h := xxhash.NewWithSeed(cchSeed)
	_, _ = h.Write([]byte(g.XXHash.InputUTF8))
	got := fmt.Sprintf("%x", h.Sum64())
	if got != g.XXHash.HashHex {
		t.Fatalf("xxhash %s want %s", got, g.XXHash.HashHex)
	}
	if out := SignCch(g.Simple.Input, ""); out != g.Simple.Output {
		t.Fatalf("simple\ngot  %s\nwant %s", out, g.Simple.Output)
	}
	billing := "x-anthropic-billing-header: cc_version=2.1.220.abc; cc_entrypoint=cli; cch=00000;"
	if out := SignCch(g.InjectFallback.Input, billing); out != g.InjectFallback.Output {
		t.Fatalf("inject\ngot  %s\nwant %s", out, g.InjectFallback.Output)
	}
}
