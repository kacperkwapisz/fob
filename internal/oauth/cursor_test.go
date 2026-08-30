package oauth

import "testing"

func TestBuildCursorAuthorizeURL(t *testing.T) {
	u := BuildCursorAuthorizeURL("chal", "uuid-1")
	if !contains(u, "loginDeepControl") || !contains(u, "challenge=chal") || !contains(u, "uuid=uuid-1") {
		t.Fatalf("%s", u)
	}
}

func TestJWTExpiryMS(t *testing.T) {
	// {"exp":1710000000} base64url
	payload := "eyJleHAiOjE3MTAwMDAwMDB9"
	token := "a." + payload + ".b"
	got := JWTExpiryMS(token)
	want := int64(1710000000)*1000 - cursorRefreshSkewMS
	if got != want {
		t.Fatalf("%d want %d", got, want)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
