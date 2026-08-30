package provider

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestFormatClaudeRequestHeaderOrder(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages?beta=true", bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-cli/2.1.220 (external, cli)")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("x-app", "cli")
	req.Header.Set("Content-Type", "application/json")
	raw, err := FormatClaudeRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	head, _, ok := strings.Cut(string(raw), "\r\n\r\n")
	if !ok {
		t.Fatal("no header terminator")
	}
	lines := strings.Split(head, "\r\n")
	if lines[0] != "POST /v1/messages?beta=true HTTP/1.1" {
		t.Fatalf("request line %q", lines[0])
	}
	var names []string
	for _, line := range lines[1:] {
		name, _, found := strings.Cut(line, ":")
		if !found {
			t.Fatalf("bad header %q", line)
		}
		names = append(names, name)
	}
	pos := map[string]int{}
	for i, n := range names {
		pos[n] = i
	}
	prev := -1
	for _, want := range ClaudeHeaderOrder {
		i, ok := pos[want]
		if !ok {
			continue
		}
		if i < prev {
			t.Fatalf("order: %v", names)
		}
		prev = i
	}
	if names[pos["Host"]] == "" || !strings.Contains(string(raw), "Content-Length: 13\r\n") {
		t.Fatalf("%s", raw)
	}
}
