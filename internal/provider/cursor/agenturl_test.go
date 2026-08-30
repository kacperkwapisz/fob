package cursor

import "testing"

func TestAllowsCursorSH(t *testing.T) {
	if NormalizeAgentRunOrigin("https://agentn.us.api5.cursor.sh") != "https://agentn.us.api5.cursor.sh" {
		t.Fatal("normalize")
	}
	if !IsAllowedAgentHost("agentn.us.api5.cursor.sh") {
		t.Fatal("host")
	}
}

func TestRejectsBadOrigins(t *testing.T) {
	if NormalizeAgentRunOrigin("https://evil.example") != "" {
		t.Fatal("evil")
	}
	if NormalizeAgentRunOrigin("http://agentn.us.api5.cursor.sh") != "" {
		t.Fatal("http")
	}
	if NormalizeAgentRunOrigin("https://user:pass@agentn.us.api5.cursor.sh") != "" {
		t.Fatal("userinfo")
	}
}
