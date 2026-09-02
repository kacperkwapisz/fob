package provider

import (
	"strings"
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestClaudeInferenceHelloHasALPNPaddingAndPSK(t *testing.T) {
	spec := ClaudeInferenceHelloSpec()
	var (
		alpn    bool
		padding bool
		psk     bool
		status  bool
		alpnVal []string
	)
	for i, ext := range spec.Extensions {
		switch e := ext.(type) {
		case *utls.ALPNExtension:
			alpn = true
			alpnVal = e.AlpnProtocols
		case *utls.UtlsPaddingExtension:
			padding = true
		case *utls.UtlsPreSharedKeyExtension:
			psk = true
			if i != len(spec.Extensions)-1 {
				t.Fatal("PSK must be last")
			}
		case *utls.StatusRequestExtension:
			status = true
		}
	}
	if !alpn || len(alpnVal) != 1 || alpnVal[0] != "http/1.1" {
		t.Fatalf("alpn=%v %v", alpn, alpnVal)
	}
	if !padding {
		t.Fatal("missing padding")
	}
	if !psk {
		t.Fatal("missing PSK")
	}
	if !status {
		t.Fatal("missing status_request")
	}
}

func TestClaudeOAuthHelloHasNoALPN(t *testing.T) {
	spec := ClaudeOAuthHelloSpec()
	var psk bool
	for i, ext := range spec.Extensions {
		switch ext.(type) {
		case *utls.ALPNExtension:
			t.Fatal("OAuth ClientHello must not advertise ALPN")
		case *utls.UtlsPreSharedKeyExtension:
			psk = true
			if i != len(spec.Extensions)-1 {
				t.Fatal("PSK must be last")
			}
		}
	}
	if !psk {
		t.Fatal("missing PSK")
	}
}

func TestClaudeOAuthHeaderOrderHasNoALPNNames(t *testing.T) {
	joined := strings.Join(ClaudeOAuthHeaderOrder, ",")
	if strings.Contains(joined, "Authorization") {
		// POST token exchange is unauthenticated; Authorization belongs on inspect GETs.
		t.Fatalf("refresh order should not include Authorization: %s", joined)
	}
	if ClaudeOAuthInspectHeaderOrder[2] != "Authorization" {
		t.Fatalf("%v", ClaudeOAuthInspectHeaderOrder)
	}
}
