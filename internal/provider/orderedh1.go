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

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// ClaudeHeaderOrder is the wire header order Claude Code 2.1.220 sends on HTTP/1.1.
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

type orderedH1Tripper struct {
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
	mu   sync.Mutex
	idle *persistConn
	addr string
}

type persistConn struct {
	conn net.Conn
	br   *bufio.Reader
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
	pc, err := t.take(req.Context(), addr)
	if err != nil {
		return nil, err
	}
	raw, err := FormatClaudeRequest(req)
	if err != nil {
		pc.conn.Close()
		return nil, err
	}
	if _, err := pc.conn.Write(raw); err != nil {
		pc.conn.Close()
		return nil, err
	}
	res, err := http.ReadResponse(pc.br, req)
	if err != nil {
		pc.conn.Close()
		return nil, err
	}
	res.Body = &connBody{ReadCloser: decodeBody(res), pc: pc, t: t, addr: addr, closeConn: res.Close}
	return res, nil
}

func (t *orderedH1Tripper) take(ctx context.Context, addr string) (*persistConn, error) {
	t.mu.Lock()
	if t.idle != nil && t.addr == addr {
		pc := t.idle
		t.idle = nil
		t.mu.Unlock()
		return pc, nil
	}
	t.mu.Unlock()
	conn, err := t.dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &persistConn{conn: conn, br: bufio.NewReader(conn)}, nil
}

func (t *orderedH1Tripper) put(pc *persistConn, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.idle != nil {
		t.idle.conn.Close()
	}
	t.idle = pc
	t.addr = addr
}

type connBody struct {
	io.ReadCloser
	pc        *persistConn
	t         *orderedH1Tripper
	addr      string
	closeConn bool
	once      sync.Once
}

func (b *connBody) Close() error {
	var err error
	b.once.Do(func() {
		err = b.ReadCloser.Close()
		if b.closeConn {
			b.pc.conn.Close()
			return
		}
		b.t.put(b.pc, b.addr)
	})
	return err
}

func FormatClaudeRequest(req *http.Request) ([]byte, error) {
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
	for _, name := range ClaudeHeaderOrder {
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
