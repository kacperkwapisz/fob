//go:build live

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/proxy"
)

type liveClient struct {
	base    string
	key     string
	http    *http.Client
	owned   bool
	booted  *Booted
	server  *httptest.Server
	ids     map[string]bool
	ownedBy map[string]string
}

func TestLiveCursorAndGrok(t *testing.T) {
	c := openLive(t)
	defer c.close(t)
	c.loadModels(t)

	if c.hasOwner("cursor") {
		t.Run("cursor/catalog", func(t *testing.T) {
			for _, id := range []string{"composer-2.5", "composer-2.5-fast", "cursor-grok-4.6", "cursor-grok-4.6-fast"} {
				if !c.ids[id] {
					t.Errorf("missing %s", id)
				}
			}
			for _, id := range []string{"cursor-grok-4.6-medium-fast", "claude-opus-5-high-fast"} {
				if c.ids[id] {
					t.Errorf("effort fast leaked %s", id)
				}
			}
		})
		t.Run("cursor/composer-chat", func(t *testing.T) { c.chat(t, "composer-2.5", false) })
		t.Run("cursor/composer-fast-stream", func(t *testing.T) { c.chat(t, "composer-2.5-fast", true) })
		t.Run("cursor/fast-body-flag", func(t *testing.T) {
			c.chatBody(t, map[string]any{
				"model": "composer-2.5", "fast": true, "stream": true,
				"messages": pingMessages(),
			}, true)
		})
		t.Run("cursor/grok-fast-stream", func(t *testing.T) { c.chat(t, "cursor-grok-4.6-fast", true) })
		t.Run("cursor/prefixed-grok-fast", func(t *testing.T) { c.chat(t, "cursor/cursor-grok-4.6-fast", true) })
		t.Run("cursor/prod-path-tools-retry", func(t *testing.T) {
			body := map[string]any{
				"model":  "cursor/cursor-grok-4.6-fast",
				"stream": true,
				"messages": []any{
					map[string]any{"role": "user", "content": "Reply with the single word pong."},
				},
				"tools": []any{
					map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        "lookup",
							"description": "Look something up",
							"parameters":  map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
						},
					},
				},
			}
			c.chatBody(t, body, true)
			c.chatBody(t, body, true)
		})
		t.Run("cursor/terra-fast-stream", func(t *testing.T) {
			if !c.ids["gpt-5.6-terra-fast"] {
				t.Skip("gpt-5.6-terra-fast not listed")
			}
			c.chat(t, "gpt-5.6-terra-fast", true)
		})
		t.Run("cursor/sol-medium-prod", func(t *testing.T) {
			id := "gpt-5.6-sol-medium"
			if !c.ids[id] && !c.ids["cursor/"+id] && !c.ids["gpt-5.6-sol"] {
				t.Skip("gpt-5.6-sol not listed")
			}
			if c.ids["cursor/"+id] {
				id = "cursor/" + id
			} else if !c.ids[id] {
				id = "gpt-5.6-sol"
			}
			c.chatBody(t, map[string]any{
				"model":  id,
				"stream": true,
				"messages": []any{
					map[string]any{"role": "user", "content": "Reply with the single word pong. Do not call tools."},
				},
				"tools": []any{
					map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        "lookup",
							"description": "Look something up",
							"parameters":  map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
						},
					},
				},
			}, true)
		})
		t.Run("cursor/claude-messages", func(t *testing.T) {
			c.messages(t, "composer-2.5", false)
		})
		t.Run("cursor/claude-messages-stream", func(t *testing.T) {
			c.messages(t, "composer-2.5-fast", true)
		})
		t.Run("cursor/responses", func(t *testing.T) {
			c.responses(t, "composer-2.5", false)
		})
		t.Run("cursor/responses-stream", func(t *testing.T) {
			c.responses(t, "composer-2.5-fast", true)
		})
		t.Run("cursor/effort", func(t *testing.T) {
			c.chatBody(t, map[string]any{
				"model": "composer-2.5", "reasoning_effort": "low", "stream": false,
				"messages": pingMessages(),
			}, false)
		})
	} else {
		t.Log("no Cursor models listed; connect Cursor in the panel")
	}

	if c.hasOwner("grok") {
		t.Run("grok/chat", func(t *testing.T) { c.chat(t, "grok-4.6", false) })
		t.Run("grok/stream", func(t *testing.T) { c.chat(t, "grok-4.6", true) })
		t.Run("grok/responses", func(t *testing.T) { c.responses(t, "grok-4.6", false) })
		t.Run("grok/claude-messages", func(t *testing.T) { c.messages(t, "grok-4.6", false) })
	} else {
		t.Log("no native Grok models listed; reconnect Grok in the panel")
	}

	t.Run("auth/invalid-key", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, c.base+"/v1/models", nil)
		req.Header.Set("authorization", "Bearer sk-fob-deadbeef")
		res, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 401 {
			t.Fatalf("status %d %s", res.StatusCode, slurp(res.Body))
		}
	})
	t.Run("unknown-model", func(t *testing.T) {
		res := c.do(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model": "definitely-not-a-model", "messages": pingMessages(),
		}, false)
		defer res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("status %d %s", res.StatusCode, slurp(res.Body))
		}
	})
}

func openLive(t *testing.T) *liveClient {
	t.Helper()
	base := strings.TrimRight(os.Getenv("FOB_LIVE_URL"), "/")
	key := strings.TrimSpace(os.Getenv("FOB_LIVE_KEY"))
	if base != "" {
		if key == "" {
			t.Fatal("FOB_LIVE_URL set but FOB_LIVE_KEY missing")
		}
		return &liveClient{base: base, key: key, http: &http.Client{Timeout: 3 * time.Minute}}
	}

	booted, err := Create(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proxy.KeepaliveCredentials(context.Background(), booted.Fob); err != nil {
		t.Logf("keepalive: %v", err)
	}
	created, err := booted.Fob.Keys.Create("live-test", nil, nil, nil)
	if err != nil {
		booted.DB.Close()
		t.Fatal(err)
	}
	srv := httptest.NewServer(booted.Handler)
	return &liveClient{
		base:   srv.URL,
		key:    created.Secret,
		http:   srv.Client(),
		owned:  true,
		booted: booted,
		server: srv,
	}
}

func (c *liveClient) close(t *testing.T) {
	t.Helper()
	if !c.owned {
		return
	}
	if c.booted != nil {
		keys, _ := c.booted.Fob.Keys.List()
		for _, k := range keys {
			if k.Name == "live-test" && !k.Revoked {
				_, _ = c.booted.Fob.Keys.Revoke(k.ID)
			}
		}
		_ = c.booted.DB.Close()
	}
	if c.server != nil {
		c.server.Close()
	}
}

func (c *liveClient) loadModels(t *testing.T) {
	t.Helper()
	res := c.do(t, http.MethodGet, "/v1/models", nil, false)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("models %d %s", res.StatusCode, slurp(res.Body))
	}
	var listed struct {
		Data []domain.ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	c.ids = map[string]bool{}
	c.ownedBy = map[string]string{}
	for _, m := range listed.Data {
		c.ids[m.ID] = true
		c.ownedBy[m.ID] = m.OwnedBy
	}
	if len(c.ids) == 0 {
		t.Fatal("GET /v1/models returned no models; connect a credential in the panel")
	}
}

func (c *liveClient) hasOwner(owner string) bool {
	for _, o := range c.ownedBy {
		if o == owner {
			return true
		}
	}
	return false
}

func (c *liveClient) do(t *testing.T, method, path string, body any, stream bool) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	client := c.http
	if stream {
		cp := *c.http
		cp.Timeout = 0
		client = &cp
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func pingMessages() []any {
	return []any{map[string]any{"role": "user", "content": "Reply with the single word pong."}}
}

func (c *liveClient) chat(t *testing.T, model string, stream bool) {
	t.Helper()
	c.chatBody(t, map[string]any{"model": model, "stream": stream, "messages": pingMessages()}, stream)
}

func (c *liveClient) chatBody(t *testing.T, body map[string]any, stream bool) {
	t.Helper()
	res := c.do(t, http.MethodPost, "/v1/chat/completions", body, stream)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, slurp(res.Body))
	}
	if !stream {
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		msg := asMap(asMap(first(asArr(asMap(payload)["choices"])))["message"])
		if strings.TrimSpace(asStr(msg["content"])) == "" && asArr(msg["tool_calls"]) == nil {
			t.Fatalf("empty completion %+v", payload)
		}
		return
	}
	assertOpenAIStream(t, res.Body)
}

func (c *liveClient) messages(t *testing.T, model string, stream bool) {
	t.Helper()
	res := c.do(t, http.MethodPost, "/v1/messages", map[string]any{
		"model": model, "max_tokens": 32, "stream": stream,
		"messages": pingMessages(),
	}, stream)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, slurp(res.Body))
	}
	if !stream {
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if asStr(payload["type"]) != "message" && asStr(payload["role"]) != "assistant" {
			if len(asArr(payload["content"])) == 0 {
				t.Fatalf("empty messages %+v", payload)
			}
		}
		return
	}
	saw := false
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "message_stop") || strings.Contains(line, "text_delta") || strings.Contains(line, "pong") {
			saw = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !saw {
		t.Fatal("claude stream produced no events")
	}
}

func (c *liveClient) responses(t *testing.T, model string, stream bool) {
	t.Helper()
	res := c.do(t, http.MethodPost, "/v1/responses", map[string]any{
		"model": model, "stream": stream,
		"input": "Reply with the single word pong.",
	}, stream)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, slurp(res.Body))
	}
	if !stream {
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if asStr(payload["object"]) != "response" && asArr(payload["output"]) == nil && asArr(payload["choices"]) == nil {
			t.Fatalf("empty responses %+v", payload)
		}
		return
	}
	saw := false
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "response.") || strings.Contains(line, "output_text") || strings.Contains(line, "pong") {
			saw = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !saw {
		t.Fatal("responses stream produced no events")
	}
}

func assertOpenAIStream(t *testing.T, body io.Reader) {
	t.Helper()
	sawChunk := false
	finished := false
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "data: [DONE]" {
			finished = true
			break
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
			continue
		}
		choice := asMap(first(asArr(chunk["choices"])))
		if asStr(choice["finish_reason"]) == "error" {
			t.Fatalf("stream error %s", asStr(asMap(choice["delta"])["content"]))
		}
		if asStr(asMap(choice["delta"])["content"]) != "" || asStr(choice["finish_reason"]) != "" {
			sawChunk = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !sawChunk {
		t.Fatal("stream produced no chunks")
	}
	if !finished {
		t.Fatal("stream ended without [DONE]")
	}
}

func slurp(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4096))
	return string(b)
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func asArr(v any) []any {
	a, _ := v.([]any)
	if a == nil {
		return []any{}
	}
	return a
}

func asStr(v any) string {
	s, _ := v.(string)
	return s
}

func first(arr []any) any {
	if len(arr) == 0 {
		return nil
	}
	return arr[0]
}
