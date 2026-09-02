package provider

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func writeKeepAliveJSON(c net.Conn, body []byte) error {
	res := &http.Response{
		Status:        "200 OK",
		StatusCode:    200,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}, "Content-Length": []string{strconv.Itoa(len(body))}},
		ContentLength: int64(len(body)),
		Body:          io.NopCloser(bytes.NewReader(body)),
		Close:         false,
	}
	return res.Write(c)
}

func serveKeepAlive(ln net.Listener, accepts *atomic.Int32) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepts.Add(1)
		go func(c net.Conn) {
			br := bufio.NewReader(c)
			for {
				req, err := http.ReadRequest(br)
				if err != nil {
					c.Close()
					return
				}
				io.Copy(io.Discard, req.Body)
				req.Body.Close()
				if err := writeKeepAliveJSON(c, []byte(`{"ok":true}`)); err != nil {
					c.Close()
					return
				}
			}
		}(conn)
	}
}

func TestOrderedH1ReusesIdleConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepts atomic.Int32
	go serveKeepAlive(ln, &accepts)

	tripper := &orderedH1Tripper{
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		order:       func(string, string) []string { return ClaudeHeaderOrder },
		maxIdle:     4,
		idleTimeout: time.Minute,
	}
	client := &http.Client{Transport: tripper}
	url := "http://" + ln.Addr().String() + "/v1/messages"
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"model":"x"}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer t")
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	if n := accepts.Load(); n != 1 {
		t.Fatalf("accepts=%d want 1", n)
	}
}

func TestOrderedH1DropsExpiredIdle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepts atomic.Int32
	go serveKeepAlive(ln, &accepts)

	tripper := &orderedH1Tripper{
		dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
		order:       func(string, string) []string { return ClaudeHeaderOrder },
		maxIdle:     4,
		idleTimeout: 20 * time.Millisecond,
	}
	client := &http.Client{Transport: tripper}
	url := "http://" + ln.Addr().String() + "/v1/messages"
	do := func() {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{"model":"x"}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	do()
	time.Sleep(50 * time.Millisecond)
	do()
	if n := accepts.Load(); n != 2 {
		t.Fatalf("accepts=%d want 2", n)
	}
}

func TestCountTokensHeaderOrderOmitsTimeout(t *testing.T) {
	for _, name := range ClaudeCountTokensHeaderOrder {
		if name == "X-Stainless-Timeout" {
			t.Fatal("count_tokens order includes X-Stainless-Timeout")
		}
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages/count_tokens", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set("anthropic-version", "2023-06-01")
	raw, err := FormatRequest(req, ClaudeCountTokensHeaderOrder)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("X-Stainless-Timeout")) {
		t.Fatalf("%s", raw)
	}
	got := claudeCodeRequestHeaderOrder("POST", "/v1/messages/count_tokens")
	if len(got) != len(ClaudeCountTokensHeaderOrder) {
		t.Fatalf("order %v", got)
	}
}
