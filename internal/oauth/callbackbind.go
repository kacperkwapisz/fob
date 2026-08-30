package oauth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
)

type BindResult struct {
	Busy bool
}

var (
	bindMu      sync.Mutex
	activeBinds = map[domain.ProviderID]*http.Server{}
)

func CallbackPort(provider domain.ProviderID) string {
	switch provider {
	case domain.ProviderClaude:
		return "127.0.0.1:54545"
	case domain.ProviderCodex:
		return "127.0.0.1:1455"
	default:
		return ""
	}
}

func BindCallback(provider domain.ProviderID, onCode func(code, state string)) (BindResult, error) {
	addr := CallbackPort(provider)
	if addr == "" {
		return BindResult{}, nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return BindResult{Busy: true}, nil
	}
	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code, state := q.Get("code"), q.Get("state")
		w.Header().Set("content-type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>Fob</title><p>Login captured. You can close this tab and return to Fob.</p>")
		if onCode != nil && code != "" {
			go onCode(code, state)
		}
		go func() {
			time.Sleep(200 * time.Millisecond)
			UnbindCallback(provider)
		}()
	}
	mux.HandleFunc("/callback", handler)
	mux.HandleFunc("/auth/callback", handler)
	mux.HandleFunc("/", handler)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	bindMu.Lock()
	if old, ok := activeBinds[provider]; ok {
		_ = old.Shutdown(context.Background())
	}
	activeBinds[provider] = srv
	bindMu.Unlock()
	go func() {
		_ = srv.Serve(ln)
	}()
	return BindResult{}, nil
}

func UnbindCallback(provider domain.ProviderID) {
	bindMu.Lock()
	srv := activeBinds[provider]
	delete(activeBinds, provider)
	bindMu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

func BindErrorNote(busy bool) string {
	if !busy {
		return ""
	}
	return fmt.Sprintf("localhost callback port is in use — paste the failed URL instead")
}
