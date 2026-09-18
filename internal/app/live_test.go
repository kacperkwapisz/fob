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
	"strconv"
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
		t.Run("cursor/agentic-tools", func(t *testing.T) {
			id := pickLiveCursorAgent(c.ids)
			if id == "" {
				t.Skip("no agentic cursor model listed")
			}
			c.agentic(t, id, true)
		})
		t.Run("cursor/agentic-nostream", func(t *testing.T) {
			id := pickLiveCursorAgent(c.ids)
			if id == "" {
				t.Skip("no agentic cursor model listed")
			}
			c.agentic(t, id, false)
		})
		t.Run("cursor/agentic-workspace", func(t *testing.T) {
			id := pickLiveCursorAgent(c.ids)
			if id == "" {
				t.Skip("no agentic cursor model listed")
			}
			c.agenticWorkspace(t, id)
		})
		t.Run("cursor/agentic-workspace-grok", func(t *testing.T) {
			id := pickLiveModel(c.ids, []string{"cursor-grok-4.6", "cursor-grok-4.6-fast", "cursor/cursor-grok-4.6"})
			if id == "" {
				t.Skip("cursor-grok-4.6 not listed")
			}
			c.agenticWorkspace(t, id)
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

func pickLiveCursorAgent(ids map[string]bool) string {
	return pickLiveModel(ids, []string{"composer-2.5", "gpt-5.6-sol", "gpt-5.6-sol-medium", "cursor/gpt-5.6-sol-medium", "composer-2.5-fast"})
}

func liveTools() []any {
	return []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup",
				"description": "Look something up. You must call this when asked.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"q": map[string]any{"type": "string"}},
					"required":   []any{"q"},
				},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "echo",
				"description": "Echo a value back. Call this with v set to a previous tool result.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"v": map[string]any{"type": "string"}},
					"required":   []any{"v"},
				},
			},
		},
	}
}

func (c *liveClient) chatTurn(t *testing.T, model string, stream bool, session string, messages, tools []any) liveStream {
	t.Helper()
	body := map[string]any{
		"model": model, "stream": stream, "messages": messages, "tools": tools,
		"pi_session_id": session,
	}
	res := c.do(t, http.MethodPost, "/v1/chat/completions", body, stream)
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, slurp(res.Body))
	}
	if stream {
		return readOpenAIStream(t, res.Body)
	}
	return readOpenAIJSON(t, res.Body)
}

func (c *liveClient) agentic(t *testing.T, model string, stream bool) {
	t.Helper()
	session := "live-agentic-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	messages := []any{
		map[string]any{"role": "user", "content": "Call lookup with q set to ping. After the tool returns, reply with only the word pong."},
	}
	called := false
	for round := 0; round < 6; round++ {
		out := c.chatTurn(t, model, stream, session, messages, liveTools()[:1])
		t.Logf("%s agentic stream=%v round %d finish=%s tools=%d content=%q", model, stream, round, out.finish, len(out.toolCalls), out.content)
		if out.finish == "error" {
			t.Fatalf("stream error %s", out.content)
		}
		if len(out.toolCalls) > 0 {
			called = true
			messages = appendToolRound(messages, out.toolCalls, func(name, args string) string {
				if name != "lookup" {
					t.Fatalf("unexpected tool %s", name)
				}
				if !strings.Contains(args, "ping") {
					t.Fatalf("lookup args %s", args)
				}
				return "pong"
			})
			continue
		}
		if !called {
			t.Fatalf("model never called lookup finish=%s content=%q", out.finish, out.content)
		}
		if strings.TrimSpace(out.content) == "" {
			t.Fatalf("empty agentic completion finish=%s", out.finish)
		}
		return
	}
	t.Fatal("agentic loop did not finish")
}

func (c *liveClient) agenticWorkspace(t *testing.T, model string) {
	t.Helper()
	fs := newLanternWorkspace()
	session := "live-workspace-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	messages := []any{
		map[string]any{"role": "system", "content": "You are a coding agent. The only files that exist are in the virtual workspace tools."},
		map[string]any{"role": "user", "content": workspacePrompt()},
	}
	var final string
	for round := 0; round < 10; round++ {
		out := c.chatTurn(t, model, true, session, messages, workspaceTools())
		t.Logf("%s workspace round %d finish=%s tools=%d calls=%v content=%q", model, round, out.finish, len(out.toolCalls), fs.calls, out.content)
		if out.finish == "error" {
			t.Fatalf("stream error %s", out.content)
		}
		if len(out.toolCalls) > 0 {
			messages = appendToolRound(messages, out.toolCalls, fs.handle)
			continue
		}
		final = strings.TrimSpace(out.content)
		break
	}
	if !fs.called("list_dir") {
		t.Fatalf("never listed the workspace; calls=%v final=%q", fs.calls, final)
	}
	if !fs.called("read_file") && !fs.called("grep_files") {
		t.Fatalf("never read/grepped files; calls=%v final=%q", fs.calls, final)
	}
	if !fs.called("write_file") {
		t.Fatalf("never wrote /out/answer.txt; calls=%v final=%q", fs.calls, final)
	}
	written := fs.files["/out/answer.txt"]
	if !strings.Contains(written, workspaceSecret) || !strings.Contains(written, workspaceVersion) {
		t.Fatalf("wrote %q want %s %s; calls=%v", written, workspaceSecret, workspaceVersion, fs.calls)
	}
	if !strings.Contains(final, workspaceSecret) || !strings.Contains(final, workspaceVersion) {
		t.Fatalf("final %q missing secret/version; calls=%v", final, fs.calls)
	}
}

func appendToolRound(messages []any, toolCalls []any, handle func(name, args string) string) []any {
	messages = append(messages, map[string]any{
		"role": "assistant", "content": nil, "tool_calls": toolCalls,
	})
	for _, tc := range toolCalls {
		m := asMap(tc)
		fn := asMap(m["function"])
		messages = append(messages, map[string]any{
			"role":         "tool",
			"tool_call_id": asStr(m["id"]),
			"content":      handle(asStr(fn["name"]), asStr(fn["arguments"])),
		})
	}
	return messages
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

type liveStream struct {
	finish    string
	content   string
	toolCalls []any
}

func readOpenAIStream(t *testing.T, body io.Reader) liveStream {
	t.Helper()
	var out liveStream
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
		delta := asMap(choice["delta"])
		if fr := asStr(choice["finish_reason"]); fr != "" {
			out.finish = fr
		}
		if s := asStr(delta["content"]); s != "" {
			out.content += s
			sawChunk = true
		}
		if tc := asArr(delta["tool_calls"]); len(tc) > 0 {
			out.toolCalls = mergeToolCallDeltas(out.toolCalls, tc)
			sawChunk = true
		}
		if out.finish != "" {
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
	return out
}

func readOpenAIJSON(t *testing.T, body io.Reader) liveStream {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	choice := asMap(first(asArr(payload["choices"])))
	msg := asMap(choice["message"])
	out := liveStream{
		finish:    asStr(choice["finish_reason"]),
		content:   asStr(msg["content"]),
		toolCalls: asArr(msg["tool_calls"]),
	}
	if out.finish == "" && len(out.toolCalls) > 0 {
		out.finish = "tool_calls"
	}
	if strings.TrimSpace(out.content) == "" && len(out.toolCalls) == 0 {
		t.Fatalf("empty completion %+v", payload)
	}
	return out
}

func mergeToolCallDeltas(dst, deltas []any) []any {
	for _, raw := range deltas {
		d := asMap(raw)
		idx := -1
		if n, ok := d["index"].(float64); ok {
			idx = int(n)
		}
		fn := asMap(d["function"])
		id := asStr(d["id"])
		if idx >= 0 {
			for idx >= len(dst) {
				dst = append(dst, map[string]any{"type": "function", "function": map[string]any{}})
			}
			cur := asMap(dst[idx])
			if id != "" {
				cur["id"] = id
			}
			if typ := asStr(d["type"]); typ != "" {
				cur["type"] = typ
			}
			curFn := asMap(cur["function"])
			if name := asStr(fn["name"]); name != "" {
				curFn["name"] = name
			}
			if args := asStr(fn["arguments"]); args != "" {
				curFn["arguments"] = asStr(curFn["arguments"]) + args
			}
			cur["function"] = curFn
			dst[idx] = cur
			continue
		}
		dst = append(dst, d)
	}
	return dst
}

func assertOpenAIStream(t *testing.T, body io.Reader) {
	t.Helper()
	out := readOpenAIStream(t, body)
	if out.finish == "error" {
		t.Fatalf("stream error %s", out.content)
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

func pickLiveModel(ids map[string]bool, want []string) string {
	for _, id := range want {
		if ids[id] {
			return id
		}
	}
	return ""
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
