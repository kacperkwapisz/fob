package env

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestLoadRefusesMissingSecretInDocker(t *testing.T) {
	_, err := Load(map[string]string{"DATABASE_PATH": "/data/fob.sqlite"})
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRefusesShortSecret(t *testing.T) {
	_, err := Load(map[string]string{"JWT_SECRET": "tiny"})
	if err == nil || !strings.Contains(err.Error(), "at least 32") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadParsesDefaults(t *testing.T) {
	e, err := Load(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 8317 || e.Host != "0.0.0.0" || e.LogLevel != LogInfo {
		t.Fatalf("%+v", e)
	}
	if len(e.JWTKey) != 32 {
		t.Fatalf("jwt key %d", len(e.JWTKey))
	}
	if e.PublicURL != "" {
		t.Fatalf("public url %q", e.PublicURL)
	}
}

func TestLoadHonorsPortAndPublicURL(t *testing.T) {
	e, err := Load(map[string]string{
		"JWT_SECRET":    secret,
		"PORT":          "9000",
		"PUBLIC_URL":    "http://localhost:9000",
		"DATABASE_PATH": ":memory:",
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 9000 || e.PublicURL != "http://localhost:9000" || e.DatabasePath != ":memory:" {
		t.Fatalf("%+v", e)
	}
}

func TestLoadRejectsBadPublicURL(t *testing.T) {
	_, err := Load(map[string]string{"JWT_SECRET": secret, "PUBLIC_URL": "not-a-url"})
	if err == nil || !strings.Contains(err.Error(), "PUBLIC_URL") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadWritesJWTFileWhenUnset(t *testing.T) {
	dir := t.TempDir()
	e, err := Load(map[string]string{"FOB_HOME": dir})
	if err != nil {
		t.Fatal(err)
	}
	if !e.JWTGenerated {
		t.Fatal("expected generated secret")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "jwt_secret"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != e.JWTSecret {
		t.Fatalf("secret mismatch")
	}
	if e.DatabasePath != filepath.Join(dir, "fob.sqlite") {
		t.Fatalf("db path %s", e.DatabasePath)
	}
}
