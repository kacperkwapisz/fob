package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFailFromResponseDropsHTML(t *testing.T) {
	res := &http.Response{
		StatusCode: 502,
		Body:       io.NopCloser(strings.NewReader("<html><h1>Bad Gateway</h1></html>")),
	}
	got := FailFromResponse(res)
	if got.OK || got.Status != 502 || !got.Retryable {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Message, "<html") || strings.Contains(fmtBody(got.Body), "<html") {
		t.Fatalf("html leaked %+v", got)
	}
}

func TestFailFromResponseKeepsHumanJSON(t *testing.T) {
	res := &http.Response{
		StatusCode: 429,
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"too many requests","type":"rate_limit_error"}}`)),
	}
	got := FailFromResponse(res)
	if got.Message != "too many requests" {
		t.Fatalf("%+v", got)
	}
}

func fmtBody(v any) string {
	m, _ := v.(map[string]any)
	if m == nil {
		return ""
	}
	err, _ := m["error"].(map[string]any)
	s, _ := err["message"].(string)
	if s == "" {
		s, _ = m["error"].(string)
	}
	return s
}
