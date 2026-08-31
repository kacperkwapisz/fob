package httpx

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const DefaultRequestTimeout = 30 * time.Second

type Mux struct {
	exact   map[string]http.HandlerFunc
	dynamic []dyn
}

type dyn struct {
	method  string
	pattern string
	names   []string
	h       http.HandlerFunc
}

func NewMux() *Mux {
	return &Mux{exact: map[string]http.HandlerFunc{}}
}

func (m *Mux) Handle(method, path string, h http.HandlerFunc) {
	if strings.Contains(path, "{") {
		names := []string{}
		pat := path
		for {
			i := strings.Index(pat, "{")
			if i < 0 {
				break
			}
			j := strings.Index(pat[i:], "}")
			if j < 0 {
				break
			}
			j += i
			names = append(names, pat[i+1:j])
			pat = pat[:i] + "\x00" + pat[j+1:]
		}
		m.dynamic = append(m.dynamic, dyn{method: method, pattern: pat, names: names, h: h})
		return
	}
	m.exact[method+" "+path] = h
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h, ok := m.exact[r.Method+" "+r.URL.Path]; ok {
		h(w, r)
		return
	}
	for _, d := range m.dynamic {
		if d.method != r.Method {
			continue
		}
		params, ok := match(d.pattern, d.names, r.URL.Path)
		if !ok {
			continue
		}
		q := r.URL.Query()
		for k, v := range params {
			q.Set(":"+k, v)
		}
		r2 := r.Clone(r.Context())
		r2.URL.RawQuery = q.Encode()
		d.h(w, r2)
		return
	}
	JSON(w, http.StatusNotFound, map[string]any{
		"error": map[string]any{
			"status":  404,
			"message": "No route for " + r.Method + " " + r.URL.Path,
		},
	})
}

func match(pattern string, names []string, path string) (map[string]string, bool) {
	pi, si := 0, 0
	out := map[string]string{}
	ni := 0
	for pi < len(pattern) {
		if pattern[pi] == 0 {
			start := si
			for si < len(path) && path[si] != '/' {
				si++
			}
			if ni >= len(names) {
				return nil, false
			}
			out[names[ni]] = path[start:si]
			ni++
			pi++
			continue
		}
		if si >= len(path) || path[si] != pattern[pi] {
			return nil, false
		}
		pi++
		si++
	}
	if si != len(path) || ni != len(names) {
		return nil, false
	}
	return out, true
}

func Param(r *http.Request, name string) string {
	return r.URL.Query().Get(":" + name)
}

func Server(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       255 * time.Second,
	}
}

var methodOverrideHeaders = []string{
	"x-http-method-override",
	"x-method-override",
	"x-http-method",
}

func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if which := methodOverrideKey(r); which != "" {
			JSON(w, http.StatusBadRequest, map[string]any{
				"error": map[string]any{
					"status":  400,
					"code":    "method_override_rejected",
					"message": "Hyper refuses to honor method override via '" + which + "'.",
					"why":     "Method overrides are a CSRF/verb-smuggling vector and are disabled by default.",
					"fix":     "Call the endpoint with the real HTTP verb. If you really need overrides, set `app({ security: { rejectMethodOverride: false } })`.",
				},
			})
			return
		}
		if noTimeout(r) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), DefaultRequestTimeout)
		defer cancel()
		r = r.WithContext(ctx)
		tw := &timeoutWriter{ResponseWriter: w, header: make(http.Header)}
		done := make(chan struct{})
		go func() {
			defer func() {
				_ = recover()
				close(done)
			}()
			next.ServeHTTP(tw, r)
		}()
		select {
		case <-done:
			tw.flushTo(w)
		case <-ctx.Done():
			if tw.started() {
				tw.flushTo(w)
				return
			}
			JSON(w, http.StatusGatewayTimeout, map[string]any{
				"error": map[string]any{
					"status":  504,
					"code":    "request_timeout",
					"message": "Handler exceeded 30000ms timeout.",
					"why":     "The handler did not produce a response in time.",
					"fix":     "Make the handler faster, raise `security.requestTimeoutMs`, or set `.meta({ timeoutMs })` per-route.",
				},
			})
		}
	})
}

func methodOverrideKey(r *http.Request) string {
	for _, h := range methodOverrideHeaders {
		if r.Header.Get(h) != "" {
			return h
		}
	}
	if r.URL.Query().Has("_method") {
		return "_method"
	}
	return ""
}

func noTimeout(r *http.Request) bool {
	p, m := r.URL.Path, r.Method
	if m == http.MethodGet && (p == "/v1/models" || p == "/api/panel/sub") {
		return true
	}
	if m != http.MethodPost {
		return false
	}
	switch p {
	case "/v1/chat/completions", "/v1/messages", "/v1/messages/count_tokens", "/v1/responses", "/v1/responses/compact":
		return true
	}
	if strings.HasPrefix(p, "/device/") {
		return true
	}
	if strings.HasPrefix(p, "/login/") && !strings.HasSuffix(p, "/finish") {
		return true
	}
	return false
}

type timeoutWriter struct {
	http.ResponseWriter
	mu      sync.Mutex
	header  http.Header
	buf     []byte
	status  int
	wrote   bool
	flushed bool
}

func (t *timeoutWriter) Header() http.Header { return t.header }

func (t *timeoutWriter) WriteHeader(status int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.wrote || t.flushed {
		return
	}
	t.status = status
	t.wrote = true
}

func (t *timeoutWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.flushed {
		return 0, http.ErrHandlerTimeout
	}
	if !t.wrote {
		t.status = http.StatusOK
		t.wrote = true
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

func (t *timeoutWriter) started() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.wrote
}

func (t *timeoutWriter) flushTo(w http.ResponseWriter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.flushed {
		return
	}
	t.flushed = true
	dst := w.Header()
	for k, vs := range t.header {
		dst[k] = vs
	}
	status := t.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(t.buf) > 0 {
		_, _ = w.Write(t.buf)
	}
}
