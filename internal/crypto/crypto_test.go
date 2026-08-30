package crypto

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func goldenPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden", name)
}

func TestCryptoGoldens(t *testing.T) {
	raw, err := os.ReadFile(goldenPath(t, "crypto.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		JWTSecret          string `json:"jwtSecret"`
		JWTKeyHex          string `json:"jwtKeyHex"`
		SHA256SkFob        string `json:"sha256SkFob"`
		HMACExp            string `json:"hmacExp"`
		NonHexSecretKeyHex string `json:"nonHexSecretKeyHex"`
		EncryptDecrypt     struct {
			Plain string `json:"plain"`
		} `json:"encryptDecrypt"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	key := SecretToKey(g.JWTSecret)
	if hex.EncodeToString(key) != g.JWTKeyHex {
		t.Fatalf("hex key %s want %s", hex.EncodeToString(key), g.JWTKeyHex)
	}
	if SHA256Hex("sk-fob-"+"abababababababababababababababababababababababab") != g.SHA256SkFob {
		t.Fatalf("sha %s", SHA256Hex("sk-fob-"+"abababababababababababababababababababababababab"))
	}
	if HMACSign(key, "1710000000000") != g.HMACExp {
		t.Fatalf("hmac %s want %s", HMACSign(key, "1710000000000"), g.HMACExp)
	}
	blob, err := Encrypt(key, g.EncryptDecrypt.Plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(key, blob)
	if err != nil || got != g.EncryptDecrypt.Plain {
		t.Fatalf("roundtrip %q %v", got, err)
	}
	nonHex := SecretToKey("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if hex.EncodeToString(nonHex) != g.NonHexSecretKeyHex {
		t.Fatalf("nonhex %s", hex.EncodeToString(nonHex))
	}
}

func TestArgon2Password(t *testing.T) {
	hash, err := HashPassword("long-enough")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("long-enough", hash) {
		t.Fatal("verify failed")
	}
	if VerifyPassword("nope", hash) {
		t.Fatal("false positive")
	}
}

func TestSeedAlphabet(t *testing.T) {
	pw := GenerateSeedPassword()
	if len(pw) != 20 {
		t.Fatalf("len %d", len(pw))
	}
	for _, c := range pw {
		if !containsRune(seedAlphabet, c) {
			t.Fatalf("bad rune %q", c)
		}
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
