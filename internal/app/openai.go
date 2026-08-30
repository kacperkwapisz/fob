package app

import (
	"net/http"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/proxy"
	"github.com/kacperkwapisz/fob/internal/session"
)

func registerOpenAI(mux *httpx.Mux, fob *proxy.Fob) {
	mux.Handle(http.MethodGet, "/v1/models", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, fob)
		if key == nil {
			httpx.OpenAIUnauthorized(w)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"object": "list", "data": proxy.ListModels(fob)})
	})
	mux.Handle(http.MethodPost, "/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, fob)
		if key == nil {
			httpx.OpenAIUnauthorized(w)
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			httpx.JSON(w, 400, httpx.OpenAIError("invalid_request_error", err.Error()))
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), fob, proxy.Request{
			Inbound: domain.InboundOpenAIChat, Body: payload, Key: *key,
			Stream: payload["stream"] == true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			httpx.JSON(w, 500, httpx.OpenAIError("server_error", err.Error()))
			return
		}
		writeProxy(w, r, result)
	})
}

func requireLocalKey(r *http.Request, fob *proxy.Fob) *domain.LocalKey {
	secret := session.BearerLocalKey(r)
	if secret == "" {
		return nil
	}
	key, _ := fob.Keys.Verify(secret)
	return key
}

func writeProxy(w http.ResponseWriter, r *http.Request, result proxy.Result) {
	if !result.OK {
		httpx.JSON(w, result.Status, result.Body)
		return
	}
	if result.Stream != nil {
		httpx.StreamSSE(w, r, result.Stream)
		return
	}
	httpx.JSON(w, http.StatusOK, result.Body)
}
