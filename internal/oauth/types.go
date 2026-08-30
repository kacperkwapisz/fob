package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"

	"github.com/kacperkwapisz/fob/internal/domain"
)

type PendingLogin struct {
	Provider    domain.ProviderID
	State       string
	Verifier    string
	RedirectURI string
	CreatedAt   int64
}

type LoginStart struct {
	Kind      string
	URL       string
	State     string
	UserCode  string
	ExpiresAt int64
}

type LoginResult struct {
	Provider  domain.ProviderID
	Label     string
	Tokens    domain.CredentialTokens
	ExpiresAt *int64
}

type ProviderLogin interface {
	ID() domain.ProviderID
	Start(ctx context.Context, redirectURI, mode string) (LoginStart, error)
	Complete(ctx context.Context, code, state, deviceCode, secret string) (LoginResult, error)
}

func PKCE() (verifier, challenge string) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return
}

func RandomState() string {
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
