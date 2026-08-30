package httpx

import "net/http"

var securityHeaderOrder = []struct{ k, v string }{
	{"X-Content-Type-Options", "nosniff"},
	{"X-Frame-Options", "DENY"},
	{"Referrer-Policy", "strict-origin-when-cross-origin"},
	{"Cross-Origin-Opener-Policy", "same-origin"},
	{"Cross-Origin-Resource-Policy", "same-origin"},
	{"Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()"},
}

func ApplySecurityHeaders(h http.Header, https bool) {
	for _, e := range securityHeaderOrder {
		if h.Get(e.k) == "" {
			h.Set(e.k, e.v)
		}
	}
	if https && h.Get("strict-transport-security") == "" {
		h.Set("strict-transport-security", "max-age=15552000; includeSubDomains")
	}
	h.Del("Server")
}

func JSON(w http.ResponseWriter, status int, body any) {
	h := w.Header()
	h.Set("content-type", "application/json; charset=utf-8")
	ApplySecurityHeaders(h, false)
	w.WriteHeader(status)
	_ = writeJSON(w, body)
}

func HTML(w http.ResponseWriter, status int, html string) {
	h := w.Header()
	h.Set("content-type", "text/html; charset=utf-8")
	ApplySecurityHeaders(h, false)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(html))
}

func Bytes(w http.ResponseWriter, status int, contentType string, body []byte) {
	h := w.Header()
	h.Set("content-type", contentType)
	ApplySecurityHeaders(h, false)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func SeeOther(w http.ResponseWriter, location, setCookie string) {
	h := w.Header()
	h.Set("location", location)
	ApplySecurityHeaders(h, false)
	if setCookie != "" {
		h.Set("set-cookie", setCookie)
	}
	w.WriteHeader(http.StatusSeeOther)
}
