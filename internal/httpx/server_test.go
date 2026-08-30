package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMuxJSON404(t *testing.T) {
	mux := NewMux()
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if res.Code != 404 {
		t.Fatalf("status %d", res.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["status"] != float64(404) {
		t.Fatalf("%s", res.Body.String())
	}
}

func TestGuardRejectsMethodOverride(t *testing.T) {
	mux := NewMux()
	mux.Handle(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, 200, map[string]any{"ok": true})
	})
	h := Guard(mux)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-HTTP-Method-Override", "POST")
	h.ServeHTTP(res, req)
	if res.Code != 400 {
		t.Fatalf("status %d", res.Code)
	}
	if !contains(res.Body.String(), "method_override_rejected") {
		t.Fatalf("%s", res.Body.String())
	}
}

func TestGuardHonorsHealth(t *testing.T) {
	mux := NewMux()
	mux.Handle(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, 200, map[string]any{"ok": true})
	})
	res := httptest.NewRecorder()
	Guard(mux).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health", nil))
	if res.Code != 200 {
		t.Fatalf("status %d body %s", res.Code, res.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
