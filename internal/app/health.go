package app

import (
	"net/http"

	"github.com/kacperkwapisz/fob/internal/httpx"
)

func registerHealth(mux *httpx.Mux) {
	mux.Handle(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}
