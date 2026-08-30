package cursor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

func TestHTTP2IdleTimeoutIgnoresClientWrites(t *testing.T) {
	old := serverIdleTimeout
	serverIdleTimeout = 150 * time.Millisecond
	defer func() { serverIdleTimeout = old }()

	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{http2.NextProtoTLS},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	h2s := &http2.Server{}
	srv := &http.Server{
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{http2.NextProtoTLS}},
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "application/connect+proto")
			w.WriteHeader(200)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		}),
	}
	http2.ConfigureServer(srv, h2s)
	go srv.Serve(ln)
	defer srv.Close()

	addr := ln.Addr().String()
	bridge := defaultHTTP2Bridge("tok", "/agent.v1.AgentService/Run", "https://"+addr, false, ClientCLI)
	var closed atomic.Bool
	done := make(chan struct{})
	bridge.OnClose(func(int) {
		if closed.CompareAndSwap(false, true) {
			close(done)
		}
	})
	bridge.OnData(func([]byte) {})
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			if closed.Load() {
				return
			}
			bridge.Write(heartbeatBytes())
		}
	}()
	start := time.Now()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("idle timeout did not fire")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout too slow: %s", elapsed)
	}
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
