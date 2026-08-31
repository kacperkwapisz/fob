package provider

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// ClaudeHeaderOrder is the wire header order Claude Code 2.1.220 sends on /v1/messages.
var ClaudeHeaderOrder = []string{
	"Accept",
	"Authorization",
	"Content-Type",
	"User-Agent",
	"X-Claude-Code-Session-Id",
	"X-Stainless-Arch",
	"X-Stainless-Lang",
	"X-Stainless-OS",
	"X-Stainless-Package-Version",
	"X-Stainless-Retry-Count",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
	"X-Stainless-Timeout",
	"anthropic-beta",
	"anthropic-dangerous-direct-browser-access",
	"anthropic-version",
	"x-app",
	"x-client-request-id",
	"Connection",
	"Host",
	"Accept-Encoding",
	"Content-Length",
}

// ClaudeCountTokensHeaderOrder drops X-Stainless-Timeout, matching native count_tokens.
var ClaudeCountTokensHeaderOrder = []string{
	"Accept",
	"Authorization",
	"Content-Type",
	"User-Agent",
	"X-Claude-Code-Session-Id",
	"X-Stainless-Arch",
	"X-Stainless-Lang",
	"X-Stainless-OS",
	"X-Stainless-Package-Version",
	"X-Stainless-Retry-Count",
	"X-Stainless-Runtime",
	"X-Stainless-Runtime-Version",
	"anthropic-beta",
	"anthropic-dangerous-direct-browser-access",
	"anthropic-version",
	"x-app",
	"x-client-request-id",
	"Connection",
	"Host",
	"Accept-Encoding",
	"Content-Length",
}

// ClaudeOAuthHeaderOrder is Axios refresh/token POST order on platform.claude.com.
var ClaudeOAuthHeaderOrder = []string{
	"Accept",
	"Content-Type",
	"User-Agent",
	"Content-Length",
	"Accept-Encoding",
	"Host",
	"Connection",
}

// ClaudeOAuthInspectHeaderOrder is Axios GET order for OAuth profile/roles.
var ClaudeOAuthInspectHeaderOrder = []string{
	"Accept",
	"Content-Type",
	"Authorization",
	"Cache-Control",
	"User-Agent",
	"Accept-Encoding",
	"Host",
	"Connection",
}

type headerOrderFunc func(method, requestTarget string) []string

type orderedH1Tripper struct {
	dial        func(ctx context.Context, network, addr string) (net.Conn, error)
	order       headerOrderFunc
	maxIdle     int
	idleTimeout time.Duration

	mu    sync.Mutex
	idles map[string][]*persistConn
}

type persistConn struct {
	conn    net.Conn
	br      *bufio.Reader
	idleAt  time.Time
	closeCh chan struct{}
}

func (t *orderedH1Tripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, fmt.Errorf("missing url")
	}
	addr := req.URL.Host
	if !strings.Contains(addr, ":") {
		if req.URL.Scheme == "http" {
			addr += ":80"
		} else {
			addr += ":443"
		}
	}
	raw, err := FormatRequest(req, t.headerOrder(req))
	if err != nil {
		return nil, err
	}
	pc, err := t.take(req.Context(), addr)
	if err != nil {
		return nil, err
	}
	if _, err := pc.conn.Write(raw); err != nil {
		pc.close()
		pc, err = t.take(req.Context(), addr)
		if err != nil {
			return nil, err
		}
		if _, err = pc.conn.Write(raw); err != nil {
			pc.close()
			return nil, err
		}
	}
	res, err := http.ReadResponse(pc.br, req)
	if err != nil {
		pc.close()
		return nil, err
	}
	reuse := !res.Close && !req.Close && res.ProtoAtLeast(1, 1)
	res.Body = &connBody{ReadCloser: decodeBody(res), pc: pc, t: t, addr: addr, reuse: reuse}
	return res, nil
}

func (t *orderedH1Tripper) headerOrder(req *http.Request) []string {
	if t.order == nil {
		return ClaudeHeaderOrder
	}
	method := req.Method
	if method == "" {
		method = http.MethodPost
	}
	target := "/"
	if req.URL != nil {
		target = req.URL.RequestURI()
		if target == "" {
			target = "/"
		}
	}
	return t.order(method, target)
}

func (t *orderedH1Tripper) take(ctx context.Context, addr string) (*persistConn, error) {
	for {
		t.mu.Lock()
		pool := t.idles[addr]
		if len(pool) == 0 {
			t.mu.Unlock()
			break
		}
		pc := pool[len(pool)-1]
		t.idles[addr] = pool[:len(pool)-1]
		t.mu.Unlock()
		if pc.expired(t.idleTimeout) {
			pc.close()
			continue
		}
		return pc, nil
	}
	conn, err := t.dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &persistConn{conn: conn, br: bufio.NewReader(conn), closeCh: make(chan struct{})}, nil
}

func (t *orderedH1Tripper) put(pc *persistConn, addr string) {
	if t.maxIdle <= 0 {
		pc.close()
		return
	}
	pc.idleAt = time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idles == nil {
		t.idles = map[string][]*persistConn{}
	}
	pool := t.idles[addr]
	if len(pool) >= t.maxIdle {
		oldest := pool[0]
		t.idles[addr] = pool[1:]
		oldest.close()
		pool = t.idles[addr]
	}
	t.idles[addr] = append(pool, pc)
}

func (pc *persistConn) expired(idleTimeout time.Duration) bool {
	if idleTimeout <= 0 || pc.idleAt.IsZero() {
		return false
	}
	return time.Since(pc.idleAt) > idleTimeout
}

func (pc *persistConn) close() {
	if pc == nil || pc.conn == nil {
		return
	}
	select {
	case <-pc.closeCh:
	default:
		close(pc.closeCh)
		pc.conn.Close()
	}
}

type connBody struct {
	io.ReadCloser
	pc    *persistConn
	t     *orderedH1Tripper
	addr  string
	reuse bool
	once  sync.Once
}

func (b *connBody) Close() error {
	var err error
	b.once.Do(func() {
		if b.reuse {
			_, _ = io.Copy(io.Discard, b.ReadCloser)
		}
		err = b.ReadCloser.Close()
		if !b.reuse {
			b.pc.close()
			return
		}
		b.t.put(b.pc, b.addr)
	})
	return err
}

func FormatClaudeRequest(req *http.Request) ([]byte, error) {
	return FormatRequest(req, ClaudeHeaderOrder)
}

func FormatRequest(req *http.Request, order []string) ([]byte, error) {
	body, err := readRequestBody(req)
	if err != nil {
		return nil, err
	}
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	path := "/"
	if req.URL != nil {
		path = req.URL.RequestURI()
		if path == "" {
			path = "/"
		}
	}
	if len(order) == 0 {
		order = ClaudeHeaderOrder
	}
	var b strings.Builder
	method := req.Method
	if method == "" {
		method = http.MethodPost
	}
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\n", method, path)
	written := map[string]bool{}
	headerOf := func(name string) string {
		if strings.EqualFold(name, "Host") {
			return host
		}
		if strings.EqualFold(name, "Content-Length") {
			return strconv.Itoa(len(body))
		}
		if strings.EqualFold(name, "Content-Type") {
			if v := req.Header.Get(name); v != "" {
				return v
			}
			if len(body) > 0 {
				return "application/json"
			}
			return ""
		}
		return req.Header.Get(name)
	}
	for _, name := range order {
		v := headerOf(name)
		if v == "" && !strings.EqualFold(name, "Host") && !strings.EqualFold(name, "Content-Length") {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\r\n", name, v)
		written[http.CanonicalHeaderKey(name)] = true
	}
	for k, vs := range req.Header {
		if written[k] {
			continue
		}
		for _, v := range vs {
			if v == "" {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\r\n", k, v)
		}
	}
	b.WriteString("\r\n")
	out := append([]byte(b.String()), body...)
	return out, nil
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	defer req.Body.Close()
	return io.ReadAll(req.Body)
}

func decodeBody(res *http.Response) io.ReadCloser {
	enc := strings.ToLower(strings.TrimSpace(res.Header.Get("Content-Encoding")))
	if enc == "" {
		return res.Body
	}
	var r io.ReadCloser
	switch enc {
	case "gzip":
		gz, err := gzip.NewReader(res.Body)
		if err != nil {
			return res.Body
		}
		r = gz
	case "deflate":
		r = flate.NewReader(res.Body)
	case "br":
		r = io.NopCloser(brotli.NewReader(res.Body))
	case "zstd":
		dec, err := zstd.NewReader(res.Body)
		if err != nil {
			return res.Body
		}
		r = dec.IOReadCloser()
	default:
		return res.Body
	}
	res.Header.Del("Content-Encoding")
	res.Header.Del("Content-Length")
	res.ContentLength = -1
	res.Uncompressed = true
	return r
}
