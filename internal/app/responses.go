package app

import (
	"net/http"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/proxy"
)

func registerResponses(mux *httpx.Mux, fob *proxy.Fob) {
	mux.Handle(http.MethodPost, "/v1/responses", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, fob)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundOpenAIResponses))
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			writeParseError(w, r, string(domain.InboundOpenAIResponses), err)
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		result, err := proxy.Proxy(r.Context(), fob, proxy.Request{
			Inbound: domain.InboundOpenAIResponses, Body: payload, Key: *key,
			Stream: payload["stream"] == true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			writeProxyErr(w, r, string(domain.InboundOpenAIResponses), err)
			return
		}
		writeProxy(w, r, result)
	})
	mux.Handle(http.MethodPost, "/v1/responses/compact", func(w http.ResponseWriter, r *http.Request) {
		key := requireLocalKey(r, fob)
		if key == nil {
			writeUnauthorized(w, r, string(domain.InboundOpenAIResponses))
			return
		}
		body, err := httpx.ParseBody(r)
		if err != nil {
			writeParseError(w, r, string(domain.InboundOpenAIResponses), err)
			return
		}
		payload, _ := body.(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		if payload["stream"] == true {
			f := httpx.Fail{
				Kind: httpx.KindRequest, Status: 400, Type: "invalid_request_error",
				Route: r.URL.Path, Message: "Streaming not supported for compact responses",
			}
			f.Log()
			httpx.JSON(w, 400, f.ClientBody(string(domain.InboundOpenAIResponses)))
			return
		}
		delete(payload, "stream")
		result, err := proxy.Proxy(r.Context(), fob, proxy.Request{
			Inbound: domain.InboundOpenAIResponses, Body: payload, Key: *key,
			Compact: true, InboundHeaders: httpx.InboundHeaders(r),
		})
		if err != nil {
			writeProxyErr(w, r, string(domain.InboundOpenAIResponses), err)
			return
		}
		writeProxy(w, r, result)
	})
}
