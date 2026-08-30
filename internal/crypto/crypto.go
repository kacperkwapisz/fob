package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
)

const seedAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func Encrypt(key []byte, plaintext string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("aes key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(plaintext), nil)
	tagSize := gcm.Overhead()
	enc := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]
	out := make([]byte, 0, 12+len(tag)+len(enc))
	out = append(out, iv...)
	out = append(out, tag...)
	out = append(out, enc...)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func Decrypt(key []byte, blob string) (string, error) {
	buf, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	if len(buf) < 12+16 {
		return "", fmt.Errorf("ciphertext too short")
	}
	iv := buf[:12]
	tag := buf[12:28]
	enc := buf[28:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed := append(append([]byte{}, enc...), tag...)
	plain, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func HMACSign(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func HMACVerify(key []byte, payload, sig string) bool {
	expected := HMACSign(key, payload)
	a := []byte(expected)
	b := []byte(sig)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func RandomID(n int) string {
	if n <= 0 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func SHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func GenerateSeedPassword() string {
	bytes := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, bytes); err != nil {
		panic(err)
	}
	out := make([]byte, 20)
	for i := 0; i < 20; i++ {
		out[i] = seedAlphabet[int(bytes[i])%len(seedAlphabet)]
	}
	return string(out)
}

func SecretToKey(secret string) []byte {
	if isHex64(secret) {
		out, err := hex.DecodeString(secret)
		if err == nil && len(out) == 32 {
			return out
		}
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < 64; i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}
