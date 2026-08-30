package store

import (
	"testing"

	"github.com/kacperkwapisz/fob/internal/db"
)

func TestPanelAuthSeedOnce(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	auth := NewPanelAuth(d)
	seed, state, err := auth.EnsureSeed()
	if err != nil {
		t.Fatal(err)
	}
	if seed == "" || len(seed) != 20 || !state.MustReset {
		t.Fatalf("seed %q %+v", seed, state)
	}
	second, _, err := auth.EnsureSeed()
	if err != nil {
		t.Fatal(err)
	}
	if second != "" {
		t.Fatalf("second seed %q", second)
	}
	ok, must := auth.Verify(seed)
	if !ok || !must {
		t.Fatalf("verify %v %v", ok, must)
	}
	ok, reason := auth.Reset(seed, "new-password-ok")
	if !ok {
		t.Fatal(reason)
	}
	ok, _ = auth.Verify(seed)
	if ok {
		t.Fatal("old seed still works")
	}
	ok, must = auth.Verify("new-password-ok")
	if !ok || must {
		t.Fatalf("new %v %v", ok, must)
	}
}
