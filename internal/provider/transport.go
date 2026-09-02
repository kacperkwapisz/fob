package provider

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"

	"github.com/kacperkwapisz/fob/internal/httpx"
)

const (
	claudeSessionCacheCapacity = 32
	oauthSessionCacheCapacity  = 8
)

var (
	transportOnce sync.Once
	claudeClient  *http.Client
	oauthClient   *http.Client
	chromeClient  *http.Client

	claudeSessionCache = utls.NewLRUClientSessionCache(claudeSessionCacheCapacity)
	oauthSessionCache  = utls.NewLRUClientSessionCache(oauthSessionCacheCapacity)
)

var testJSONClient *http.Client

func SetJSONClientForTests(c *http.Client) func() {
	old := testJSONClient
	testJSONClient = c
	return func() { testJSONClient = old }
}

func clientForURL(rawURL string) *http.Client {
	if testJSONClient != nil {
		return testJSONClient
	}
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host = strings.ToLower(u.Hostname())
	}
	client := httpx.Client()
	switch host {
	case "api.anthropic.com":
		client = ClaudeHTTP()
	case "chatgpt.com":
		client = chromeHTTP()
	case "platform.claude.com":
		client = ClaudeOAuthHTTP()
	}
	return client
}

func PostJSON(ctx context.Context, rawURL string, body any, headers map[string]string) (*http.Response, error) {
	return postJSONWith(ctx, clientForURL(rawURL), rawURL, body, headers)
}

func GetJSON(ctx context.Context, rawURL string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	return clientForURL(rawURL).Do(req)
}

func ClaudeHTTP() *http.Client {
	transportOnce.Do(initImpersonated)
	return claudeClient
}

func ClaudeOAuthHTTP() *http.Client {
	transportOnce.Do(initImpersonated)
	return oauthClient
}

func chromeHTTP() *http.Client {
	transportOnce.Do(initImpersonated)
	return chromeClient
}

func initImpersonated() {
	claudeClient = &http.Client{
		Timeout: 0,
		Transport: &orderedH1Tripper{
			dial:        claudeInferenceDial,
			order:       claudeCodeRequestHeaderOrder,
			maxIdle:     8,
			idleTimeout: 30 * time.Second,
		},
	}
	oauthClient = &http.Client{
		Timeout: 0,
		Transport: &orderedH1Tripper{
			dial:        claudeOAuthDial,
			order:       claudeOAuthRequestHeaderOrder,
			maxIdle:     8,
			idleTimeout: 30 * time.Second,
		},
	}
	chromeClient = &http.Client{Timeout: 0, Transport: &chromeTripper{}}
}

func ApplyClaudeOAuthHeaders(req *http.Request) {
	if req == nil {
		return
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "axios/1.15.2")
	req.Header.Set("Accept-Encoding", "gzip, compress, deflate, br")
	req.Header.Set("Connection", "close")
	req.Close = true
}

func ClaudeOAuthDo(req *http.Request) (*http.Response, error) {
	ApplyClaudeOAuthHeaders(req)
	return ClaudeOAuthHTTP().Do(req)
}

func chromeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	raw, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	uconn := utls.UClient(raw, &utls.Config{ServerName: host}, utls.HelloChrome_Auto)
	if err := uconn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return uconn, nil
}

func claudeInferenceDial(ctx context.Context, network, addr string) (net.Conn, error) {
	return claudeDial(ctx, network, addr, claudeInferenceTLSConfig, ClaudeInferenceHelloSpec)
}

func claudeOAuthDial(ctx context.Context, network, addr string) (net.Conn, error) {
	return claudeDial(ctx, network, addr, claudeOAuthTLSConfig, ClaudeOAuthHelloSpec)
}

func claudeDial(ctx context.Context, network, addr string, cfg func(host string) *utls.Config, spec func() *utls.ClientHelloSpec) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	d := net.Dialer{Timeout: 15 * time.Second}
	raw, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	uconn := utls.UClient(raw, cfg(host), utls.HelloCustom)
	if err := uconn.ApplyPreset(spec()); err != nil {
		raw.Close()
		return nil, err
	}
	if err := uconn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return uconn, nil
}

func claudeInferenceTLSConfig(host string) *utls.Config {
	return &utls.Config{
		ServerName:                         host,
		ClientSessionCache:                 claudeSessionCache,
		OmitEmptyPsk:                       true,
		PreferSkipResumptionOnNilExtension: true,
	}
}

func claudeOAuthTLSConfig(host string) *utls.Config {
	return &utls.Config{
		ServerName:                         host,
		ClientSessionCache:                 oauthSessionCache,
		OmitEmptyPsk:                       true,
		PreferSkipResumptionOnNilExtension: true,
	}
}

// ClaudeInferenceHelloSpec is Claude Code 2.1.220's Node/OpenSSL inference-plane ClientHello.
func ClaudeInferenceHelloSpec() *utls.ClientHelloSpec {
	return &utls.ClientHelloSpec{
		CipherSuites: []uint16{
			utls.TLS_AES_128_GCM_SHA256,
			utls.TLS_AES_256_GCM_SHA384,
			utls.TLS_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
		CompressionMethods: []uint8{0},
		Extensions: []utls.TLSExtension{
			&utls.SNIExtension{},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{Renegotiation: utls.RenegotiateOnceAsClient},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{utls.X25519, utls.CurveP256, utls.CurveP384}},
			&utls.SupportedPointsExtension{SupportedPoints: []byte{0}},
			&utls.SessionTicketExtension{},
			&utls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&utls.StatusRequestExtension{},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256,
				utls.PSSWithSHA256,
				utls.PKCS1WithSHA256,
				utls.ECDSAWithP384AndSHA384,
				utls.PSSWithSHA384,
				utls.PKCS1WithSHA384,
				utls.PSSWithSHA512,
				utls.PKCS1WithSHA512,
				utls.PKCS1WithSHA1,
			}},
			&utls.SCTExtension{},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{{Group: utls.X25519}}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SupportedVersionsExtension{Versions: []uint16{utls.VersionTLS13, utls.VersionTLS12}},
			&utls.UtlsPaddingExtension{GetPaddingLen: utls.BoringPaddingStyle},
			&utls.UtlsPreSharedKeyExtension{},
		},
	}
}

// ClaudeOAuthHelloSpec is Claude Code 2.1.220's Axios OAuth control-plane ClientHello (no ALPN).
func ClaudeOAuthHelloSpec() *utls.ClientHelloSpec {
	return &utls.ClientHelloSpec{
		TLSVersMin:         utls.VersionTLS12,
		TLSVersMax:         utls.VersionTLS13,
		CompressionMethods: []uint8{0},
		CipherSuites: []uint16{
			utls.TLS_AES_128_GCM_SHA256,
			utls.TLS_AES_256_GCM_SHA384,
			utls.TLS_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			utls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			utls.TLS_RSA_WITH_AES_128_CBC_SHA,
			utls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
		Extensions: []utls.TLSExtension{
			&utls.SNIExtension{},
			&utls.ExtendedMasterSecretExtension{},
			&utls.RenegotiationInfoExtension{Renegotiation: utls.RenegotiateOnceAsClient},
			&utls.SupportedCurvesExtension{Curves: []utls.CurveID{utls.X25519, utls.CurveP256, utls.CurveP384}},
			&utls.SupportedPointsExtension{SupportedPoints: []byte{0}},
			&utls.SessionTicketExtension{},
			&utls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []utls.SignatureScheme{
				utls.ECDSAWithP256AndSHA256,
				utls.PSSWithSHA256,
				utls.PKCS1WithSHA256,
				utls.ECDSAWithP384AndSHA384,
				utls.PSSWithSHA384,
				utls.PKCS1WithSHA384,
				utls.PSSWithSHA512,
				utls.PKCS1WithSHA512,
				utls.PKCS1WithSHA1,
			}},
			&utls.KeyShareExtension{KeyShares: []utls.KeyShare{{Group: utls.X25519}}},
			&utls.PSKKeyExchangeModesExtension{Modes: []uint8{utls.PskModeDHE}},
			&utls.SupportedVersionsExtension{Versions: []uint16{utls.VersionTLS13, utls.VersionTLS12}},
			&utls.UtlsPreSharedKeyExtension{},
		},
	}
}

type chromeTripper struct{}

type closeConnectionBody struct {
	io.ReadCloser
	closeConnection func() error
	once            sync.Once
	err             error
}

func (b *closeConnectionBody) Close() error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		var connErr error
		if b.closeConnection != nil {
			connErr = b.closeConnection()
		}
		var bodyErr error
		if b.ReadCloser != nil {
			bodyErr = b.ReadCloser.Close()
		}
		if bodyErr != nil {
			b.err = bodyErr
			return
		}
		b.err = connErr
	})
	return b.err
}

func (t *chromeTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	addr := net.JoinHostPort(host, port)
	conn, err := chromeDial(req.Context(), "tcp", addr)
	if err != nil {
		return nil, err
	}
	tr := &http2.Transport{}
	h2Conn, err := tr.NewClientConn(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("chrome h2: %w", err)
	}
	resp, err := h2Conn.RoundTrip(req)
	if err != nil {
		h2Conn.Close()
		return nil, err
	}
	if resp.Body == nil {
		resp.Body = http.NoBody
	}
	resp.Body = &closeConnectionBody{ReadCloser: resp.Body, closeConnection: h2Conn.Close}
	return resp, nil
}

func claudeCodeRequestHeaderOrder(method, requestTarget string) []string {
	if strings.HasPrefix(requestTarget, "/v1/messages/count_tokens") {
		return ClaudeCountTokensHeaderOrder
	}
	if method == http.MethodGet {
		for _, target := range []string{"/api/oauth/usage", "/api/oauth/profile"} {
			if strings.HasPrefix(requestTarget, target) {
				return ClaudeOAuthInspectHeaderOrder
			}
		}
	}
	return ClaudeHeaderOrder
}

func claudeOAuthRequestHeaderOrder(method, requestTarget string) []string {
	if method == http.MethodGet {
		for _, target := range []string{"/api/oauth/profile", "/api/oauth/usage", "/api/oauth/claude_cli/roles"} {
			if strings.HasPrefix(requestTarget, target) {
				return ClaudeOAuthInspectHeaderOrder
			}
		}
	}
	return ClaudeOAuthHeaderOrder
}
