package env

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	fcrypto "github.com/kacperkwapisz/fob/internal/crypto"
)

const (
	EmbeddedClaudeClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	EmbeddedCodexClientID  = "app_EMoamEEZ73f0CkXaXp7hrann"
	EmbeddedGrokClientID   = "b1a00492-073a-47ea-816f-4c329264a828"
	DefaultPort            = 8317
	DefaultHost            = "0.0.0.0"
)

type LogLevel string

const (
	LogDebug LogLevel = "debug"
	LogInfo  LogLevel = "info"
	LogError LogLevel = "error"
)

type Env struct {
	JWTSecret          string
	JWTKey             []byte
	DatabasePath       string
	Host               string
	Port               int
	PublicURL          string
	LogLevel           LogLevel
	ClaudeClientID     string
	ClaudeClientSecret string
	CodexClientID      string
	CodexClientSecret  string
	GrokClientID       string
	GrokClientSecret   string
	FobHome            string
	JWTGenerated       bool
}

type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

func Load(source map[string]string) (*Env, error) {
	get := func(k string) string {
		if source != nil {
			if v, ok := source[k]; ok {
				return v
			}
		}
		return os.Getenv(k)
	}

	inDocker := strings.TrimSpace(get("DATABASE_PATH")) == "/data/fob.sqlite"
	home, err := resolveHome(get("FOB_HOME"))
	if err != nil {
		return nil, err
	}

	jwtSecret := strings.TrimSpace(get("JWT_SECRET"))
	generated := false
	if jwtSecret == "" {
		if inDocker {
			return nil, &Error{Msg: "JWT_SECRET is required. Generate one with: openssl rand -hex 32"}
		}
		secretPath := filepath.Join(home, "jwt_secret")
		if existing, readErr := os.ReadFile(secretPath); readErr == nil {
			jwtSecret = strings.TrimSpace(string(existing))
		}
		if jwtSecret == "" {
			buf := make([]byte, 32)
			if _, err := io.ReadFull(rand.Reader, buf); err != nil {
				return nil, err
			}
			jwtSecret = hex.EncodeToString(buf)
			if err := os.MkdirAll(home, 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(secretPath, []byte(jwtSecret+"\n"), 0o600); err != nil {
				return nil, err
			}
			generated = true
			fmt.Fprintf(os.Stderr, "wrote JWT_SECRET to %s\n", secretPath)
		}
	}
	if len(jwtSecret) < 32 {
		return nil, &Error{Msg: "JWT_SECRET must be at least 32 characters (openssl rand -hex 32)"}
	}

	port := DefaultPort
	if raw := strings.TrimSpace(get("PORT")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 65535 {
			return nil, &Error{Msg: fmt.Sprintf("PORT must be an integer 1–65535, got %s", raw)}
		}
		port = n
	}

	logLevel := LogLevel(strings.TrimSpace(get("LOG_LEVEL")))
	if logLevel == "" {
		logLevel = LogInfo
	}
	if logLevel != LogDebug && logLevel != LogInfo && logLevel != LogError {
		return nil, &Error{Msg: "LOG_LEVEL must be debug, info, or error"}
	}

	publicURL := emptyToUndef(get("PUBLIC_URL"))
	if publicURL != "" {
		u, err := url.Parse(publicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, &Error{Msg: "PUBLIC_URL must be an absolute http(s) URL"}
		}
	}

	dbPath := strings.TrimSpace(get("DATABASE_PATH"))
	if dbPath == "" {
		if get("NODE_ENV") == "test" || get("FOB_TEST") == "1" {
			dbPath = ":memory:"
		} else if inDocker {
			dbPath = "/data/fob.sqlite"
		} else {
			dbPath = filepath.Join(home, "fob.sqlite")
		}
	}

	host := strings.TrimSpace(get("HOST"))
	if host == "" {
		host = DefaultHost
	}

	return &Env{
		JWTSecret:          jwtSecret,
		JWTKey:             fcrypto.SecretToKey(jwtSecret),
		DatabasePath:       dbPath,
		Host:               host,
		Port:               port,
		PublicURL:          publicURL,
		LogLevel:           logLevel,
		ClaudeClientID:     firstNonEmpty(strings.TrimSpace(get("CLAUDE_CLIENT_ID")), EmbeddedClaudeClientID),
		ClaudeClientSecret: emptyToUndef(get("CLAUDE_CLIENT_SECRET")),
		CodexClientID:      firstNonEmpty(strings.TrimSpace(get("CODEX_CLIENT_ID")), EmbeddedCodexClientID),
		CodexClientSecret:  emptyToUndef(get("CODEX_CLIENT_SECRET")),
		GrokClientID:       firstNonEmpty(strings.TrimSpace(get("GROK_CLIENT_ID")), EmbeddedGrokClientID),
		GrokClientSecret:   emptyToUndef(get("GROK_CLIENT_SECRET")),
		FobHome:            home,
		JWTGenerated:       generated,
	}, nil
}

func UsesEmbeddedClaude(e *Env) bool { return e.ClaudeClientID == EmbeddedClaudeClientID }
func UsesEmbeddedCodex(e *Env) bool  { return e.CodexClientID == EmbeddedCodexClientID }

func resolveHome(raw string) (string, error) {
	if strings.TrimSpace(raw) != "" {
		return filepath.Abs(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".fob"), nil
}

func emptyToUndef(v string) string {
	return strings.TrimSpace(v)
}

func firstNonEmpty(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
