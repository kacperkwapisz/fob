package app

import (
	"net/http"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/proxy"
)

func registerAnthropic(mux *httpx.Mux, fob *proxy.Fob) {
	mux.Handle(http.MethodPost, "/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, fob)
		if key == nil {
			httpx.ClaudeUnauthorized(w)
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			httpx.JSON(w, 400, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": err.Error()}})
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), fob, proxy.Request{
			Inbound: domain.InboundClaudeMessages, Body: payload, Key: *key,
			Stream: payload["stream"] == true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			httpx.JSON(w, 500, map[string]any{"error": map[string]any{"type": "server_error", "message": err.Error()}})
			return
		}
		writeProxy(w, r, result)
	})
	mux.Handle(http.MethodPost, "/v1/messages/count_tokens", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, fob)
		if key == nil {
			httpx.ClaudeUnauthorized(w)
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			httpx.JSON(w, 400, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": err.Error()}})
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), fob, proxy.Request{
			Inbound: domain.InboundClaudeMessages, Body: payload, Key: *key,
			CountTokens: true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			httpx.JSON(w, 500, map[string]any{"error": map[string]any{"type": "server_error", "message": err.Error()}})
			return
		}
		writeProxy(w, r, result)
	})
}
