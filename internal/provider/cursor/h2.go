package cursor

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

type Bridge struct {
	Write   func([]byte)
	End     func()
	OnData  func(func([]byte))
	OnClose func(func(int))
	Alive   func() bool
}

type BridgeFactory func(accessToken, rpcPath, agentURL string, unary bool, client ClientKind) *Bridge

var (
	bridgeMu          sync.Mutex
	bridgeFactory     BridgeFactory = defaultHTTP2Bridge
	serverIdleTimeout               = 30 * time.Second
)

func SetBridgeFactoryForTests(f BridgeFactory) {
	bridgeMu.Lock()
	defer bridgeMu.Unlock()
	if f == nil {
		bridgeFactory = defaultHTTP2Bridge
		return
	}
	bridgeFactory = f
}

func currentFactory() BridgeFactory {
	bridgeMu.Lock()
	defer bridgeMu.Unlock()
	return bridgeFactory
}

func defaultHTTP2Bridge(accessToken, rpcPath, agentURL string, unary bool, client ClientKind) *Bridge {
	u, err := url.Parse(agentURL)
	if err != nil {
		return deadBridge()
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	tlsCfg := &tls.Config{ServerName: u.Hostname(), NextProtos: []string{http2.NextProtoTLS}}
	hostname := u.Hostname()
	if hostname == "127.0.0.1" || hostname == "localhost" {
		tlsCfg.InsecureSkipVerify = true
	}
	dialer := &tls.Dialer{Config: tlsCfg}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, err := dialer.DialContext(ctx, "tcp", host)
	cancel()
	if err != nil {
		return deadBridge()
	}
	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(conn)
	if err != nil {
		conn.Close()
		return deadBridge()
	}
	pr, pw := io.Pipe()
	contentType := "application/connect+proto"
	if unary {
		contentType = "application/proto"
	}
	req, err := http.NewRequest(http.MethodPost, agentURL+rpcPath, pr)
	if err != nil {
		conn.Close()
		return deadBridge()
	}
	req.Header.Set("content-type", contentType)
	req.Header.Set("te", "trailers")
	req.Header.Set("authorization", "Bearer "+accessToken)
	req.Header.Set("x-ghost-mode", "true")
	req.Header.Set("x-cursor-client-type", string(client))
	if client == ClientSDK {
		req.Header.Set("x-cursor-client-version", SDKVersion)
	} else {
		req.Header.Set("x-cursor-client-version", CLIVersion)
	}
	req.Header.Set("x-request-id", newRequestID())
	if !unary {
		req.Header.Set("connect-protocol-version", "1")
	}
	var (
		mu     sync.Mutex
		alive  = true
		onData func([]byte)
		onCls  func(int)
		queue  [][]byte
	)
	deliverClose := func(code int) {
		mu.Lock()
		if !alive {
			mu.Unlock()
			return
		}
		alive = false
		cb := onCls
		mu.Unlock()
		if cb != nil {
			cb(code)
		}
	}
	go func() {
		res, err := cc.RoundTrip(req)
		if err != nil {
			deliverClose(1)
			return
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			deliverClose(1)
			return
		}
		idle := time.NewTimer(serverIdleTimeout)
		defer idle.Stop()
		buf := make([]byte, 32*1024)
		for {
			type read struct {
				n   int
				err error
			}
			ch := make(chan read, 1)
			go func() {
				n, err := res.Body.Read(buf)
				ch <- read{n, err}
			}()
			select {
			case <-idle.C:
				deliverClose(1)
				_ = conn.Close()
				return
			case r := <-ch:
				if r.err != nil {
					if r.err == io.EOF {
						deliverClose(0)
					} else {
						deliverClose(1)
					}
					return
				}
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(serverIdleTimeout)
				payload := append([]byte(nil), buf[:r.n]...)
				mu.Lock()
				cb := onData
				if cb == nil {
					queue = append(queue, payload)
					mu.Unlock()
					continue
				}
				mu.Unlock()
				cb(payload)
			}
		}
	}()
	return &Bridge{
		Write: func(data []byte) { _, _ = pw.Write(data) },
		End:   func() { _ = pw.Close() },
		OnData: func(cb func([]byte)) {
			mu.Lock()
			onData = cb
			q := queue
			queue = nil
			mu.Unlock()
			for _, p := range q {
				cb(p)
			}
		},
		OnClose: func(cb func(int)) {
			mu.Lock()
			onCls = cb
			a := alive
			mu.Unlock()
			if !a {
				go cb(1)
			}
		},
		Alive: func() bool {
			mu.Lock()
			defer mu.Unlock()
			return alive
		},
	}
}

func deadBridge() *Bridge {
	return &Bridge{
		Write:   func([]byte) {},
		End:     func() {},
		OnData:  func(func([]byte)) {},
		OnClose: func(cb func(int)) { go cb(1) },
		Alive:   func() bool { return false },
	}
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	hex := hex.EncodeToString(buf)
	return hex[0:8] + "-" + hex[8:12] + "-4" + hex[13:16] + "-8" + hex[17:20] + "-" + hex[20:32]
}
