package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONDoesNotHTMLEscapeOrNewline(t *testing.T) {
	res := httptest.NewRecorder()
	JSON(res, 200, map[string]any{"msg": "a<b>&c", "ok": true})
	got := res.Body.String()
	if strings.Contains(got, "\\u003c") || strings.HasSuffix(got, "\n") {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(got, `"msg":"a<b>&c"`) {
		t.Fatalf("%q", got)
	}
}

func TestModelsEmptyArrayNotNull(t *testing.T) {
	mux := NewMux()
	mux.Handle(http.MethodGet, "/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, 200, map[string]any{"object": "list", "data": []any{}})
	})
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if got := res.Body.String(); !strings.Contains(got, `"data":[]`) {
		t.Fatalf("%q", got)
	}
}
