package httpx

import (
	"io"
	"net/http"
	"time"
)

func StreamSSE(w http.ResponseWriter, r *http.Request, src <-chan string) {
	h := w.Header()
	h.Set("content-type", "text/event-stream")
	h.Set("cache-control", "no-cache, no-transform")
	h.Set("x-accel-buffering", "no")
	ApplySecurityHeaders(h, false)
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-src:
			if !ok {
				return
			}
			if _, err := io.WriteString(w, line); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}
