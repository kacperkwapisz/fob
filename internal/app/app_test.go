package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/provider"
	"github.com/kacperkwapisz/fob/internal/proxy"
	"github.com/kacperkwapisz/fob/internal/store"
)

const secret = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestHealthNoKey(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/health", nil))
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["ok"] != true {
		t.Fatalf("%s", res.Body.String())
	}
}

func TestModelsEmptyArrayNotNull(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	created, err := booted.Fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, `"data":[]`) {
		t.Fatalf("%s", body)
	}
	if strings.HasSuffix(body, "\n") {
		t.Fatalf("trailing newline %q", body)
	}
}

func TestModelsDiscoveryFields(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	if _, err := booted.Fob.Vault.Save(store.SaveCredential{
		Provider: domain.ProviderGrok, Label: "grok",
		Tokens: domain.CredentialTokens{AccessToken: "t", Extra: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	created, err := booted.Fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	var body struct {
		Data []domain.ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var grok *domain.ModelInfo
	for i := range body.Data {
		if body.Data[i].ID == "grok-4.6" {
			grok = &body.Data[i]
			break
		}
	}
	if grok == nil {
		t.Fatalf("missing grok-4.6 in %s", res.Body.String())
	}
	if grok.ContextLength != 500000 || grok.MaxOutputTokens != 500000 {
		t.Fatalf("limits %+v", grok)
	}
	if grok.Cost == nil || grok.Cost.Input == nil || *grok.Cost.Input != 2 {
		t.Fatalf("cost %+v", grok.Cost)
	}
	if !grok.Reasoning || len(grok.Efforts) == 0 {
		t.Fatalf("reasoning %+v", grok)
	}
	if len(grok.InputModalities) == 0 || grok.InputModalities[0] != "text" {
		t.Fatalf("input %+v", grok.InputModalities)
	}
	var listed map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range listed["data"].([]any) {
		m := row.(map[string]any)
		if m["id"] != "grok-4.20-0309-non-reasoning" {
			continue
		}
		found = true
		if _, ok := m["reasoning"]; ok {
			t.Fatalf("non-reasoning still serializes reasoning: %+v", m)
		}
	}
	if !found {
		t.Fatal("missing non-reasoning")
	}
}

func TestModelsUnauthorized(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if res.Code != 401 {
		t.Fatalf("status %d", res.Code)
	}
}

func TestChatUnauthorizedLogsAndKeepsShape(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"grok-4.6"}`))
	req.Header.Set("content-type", "application/json")
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"type":"invalid_request_error"`) || !strings.Contains(res.Body.String(), "invalid api key") {
		t.Fatalf("%s", res.Body.String())
	}
	if !strings.Contains(buf.String(), "fob error") || !strings.Contains(buf.String(), "/v1/chat/completions") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestChatNoCredentialHumanJSON(t *testing.T) {
	var buf bytes.Buffer
	defer httpx.SetLogOutput(&buf)()
	httpx.SetLogLevel("info")
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	created, err := booted.Fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"grok-4.6","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 503 {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "<html") {
		t.Fatalf("dump %s", res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "no grok credential") || !strings.Contains(res.Body.String(), "panel") {
		t.Fatalf("%s", res.Body.String())
	}
	if !strings.Contains(buf.String(), "grok/grok-4.6") {
		t.Fatalf("log %q", buf.String())
	}
}

func TestPanelUnlockHTML(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	_, _, _ = booted.Panel.EnsureSeed()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	html := res.Body.String()
	for _, want := range []string{"IBM+Plex+Sans", "/design.css", "Set a password"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %s", want, html[:min(len(html), 400)])
		}
	}
}

func TestPanelIgnoresQueryNotices(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	_, _, _ = booted.Panel.EnsureSeed()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/?error=wrong+password&ok=Connected", nil))
	html := res.Body.String()
	if strings.Contains(html, "wrong password") || strings.Contains(html, "Connected") {
		t.Fatalf("leaked query: %s", html)
	}
}

func TestPasswordBadSeedRendersForm(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	_, _, _ = booted.Panel.EnsureSeed()
	res := httptest.NewRecorder()
	form := url.Values{"old_password": {"nope"}, "new_password": {"long-enough"}}
	req := httptest.NewRequest(http.MethodPost, "/password", strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d", res.Code)
	}
	if res.Header().Get("location") != "" || res.Header().Get("set-cookie") != "" {
		t.Fatal("unexpected redirect/cookie")
	}
	html := res.Body.String()
	if !strings.Contains(html, "old password is wrong") || !strings.Contains(html, "Set a password") {
		t.Fatalf("%s", html)
	}
}

func TestMintRedirectsWithoutSecret(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	form := url.Values{"name": {"opencode"}}
	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 303 || res.Header().Get("location") != "/" {
		t.Fatalf("status %d loc %s", res.Code, res.Header().Get("location"))
	}
	if strings.Contains(res.Header().Get("set-cookie"), "fob_flash") || strings.Contains(res.Body.String(), "sk-fob-") {
		t.Fatal("leaked secret")
	}
	pageRes := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(pageRes, pageReq)
	html := pageRes.Body.String()
	for _, want := range []string{"opencode", "sk-fob-", "Logins", "Keys", "Meter", "Usage Trends", "Cursor", "OpenAI-compatible", "Add source"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestAPIPanelKeysReturnsSecretOnce(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/panel/keys", strings.NewReader(`{"name":"cursor"}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Secret, ID, Name, Prefix string
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "cursor" || !strings.HasPrefix(body.Secret, "sk-fob-") || body.Prefix != body.Secret[:12] || body.ID == "" {
		t.Fatalf("%+v", body)
	}
}

func TestAPIPanelKeysUnauthorized(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	_, _, _ = booted.Panel.EnsureSeed()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/panel/keys", strings.NewReader(`{"name":"cursor"}`))
	req.Header.Set("content-type", "application/json")
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatalf("status %d", res.Code)
	}
}

func TestDeadPanelJSONGone(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	for _, path := range []string{"/api/panel/me", "/api/panel/credentials", "/api/panel/keys", "/api/panel/usage"} {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("cookie", session)
		booted.Handler.ServeHTTP(res, req)
		if res.Code != 404 {
			t.Fatalf("%s %d", path, res.Code)
		}
	}
}

func TestConnectedLoginsRenderFromVault(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	if _, err := booted.Fob.Vault.Save(store.SaveCredential{
		Provider: domain.ProviderClaude,
		Label:    "work claude",
		Tokens:   domain.CredentialTokens{AccessToken: "at", Extra: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	html := res.Body.String()
	if !strings.Contains(html, "work claude") || strings.Contains(html, "?ok=") {
		t.Fatalf("%s", html)
	}
	for _, want := range []string{"/alpine.min.js", "chart-series", "meter-data", "chart-trends", "trends-data"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestUsageTrendsRendersChart(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	if err := booted.Fob.Usage.Record(domain.UsageEvent{
		TS:               time.Now().UnixMilli(),
		Provider:         domain.ProviderClaude,
		Model:            "claude-opus-4-7",
		Inbound:          domain.InboundOpenAIChat,
		PromptTokens:     1200,
		CompletionTokens: 300,
		Status:           "ok",
		USD:              0.02,
	}); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	html := res.Body.String()
	for _, want := range []string{"Usage Trends", "chart-trends", "trends-data", "1,200", "1,500", "\"promptTokens\":1200", "\"completionTokens\":300"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestDesignCSS(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/design.css", nil))
	if res.Code != 200 {
		t.Fatal(res.Code)
	}
	css := res.Body.String()
	if !strings.Contains(css, "--accent:") || !strings.Contains(css, "--font-display:") || !strings.Contains(css, "--dither-bayer:") || !strings.Contains(css, "card-trends") || !strings.Contains(css, "source-form") || !strings.Contains(css, "--p-openai") {
		t.Fatal(css[:200])
	}
}

func TestPanelJS(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/panel.js", nil))
	js := res.Body.String()
	if res.Code != 200 || !strings.Contains(js, "/api/panel/keys") || !strings.Contains(js, "/api/panel/sub") || !strings.Contains(js, "closest(\"#sub-load\")") || !strings.Contains(js, "drawTrends") || !strings.Contains(js, "openai: \"--p-openai\"") {
		t.Fatal(js)
	}
	if !strings.Contains(js, `dlg.returnValue = ""`) {
		t.Fatal("confirm dialog must clear returnValue before showModal")
	}
	if !strings.Contains(js, "application/x-www-form-urlencoded") || !strings.Contains(js, "URLSearchParams") {
		t.Fatal("autosave must post urlencoded, not multipart")
	}
	if res.Header().Get("cache-control") != "no-store" {
		t.Fatalf("cache %s", res.Header().Get("cache-control"))
	}
}

func TestCursorSettingsURLEncoded(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	post := func(body url.Values) {
		t.Helper()
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/settings/cursor", strings.NewReader(body.Encode()))
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		req.Header.Set("cookie", session)
		booted.Handler.ServeHTTP(res, req)
		if res.Code != 303 {
			t.Fatalf("status %d", res.Code)
		}
	}
	page := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(page, req)
	if !strings.Contains(page.Body.String(), `name="list_fast" value="1" checked`) {
		t.Fatal("list_fast should default on")
	}
	post(url.Values{"prefix": {"1"}, "grok_failover": {"1"}, "list_fast": {"1"}})
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorPrefix); v != "1" {
		t.Fatalf("prefix %q", v)
	}
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorListFast); v != "1" {
		t.Fatalf("list_fast %q", v)
	}
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorGrokFailover); v != "1" {
		t.Fatalf("grok %q", v)
	}
	page = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(page, req)
	html := page.Body.String()
	if !strings.Contains(html, `name="prefix" value="1" checked`) || !strings.Contains(html, `name="grok_failover" value="1" checked`) || !strings.Contains(html, `name="list_fast" value="1" checked`) {
		t.Fatal("toggles not checked after save")
	}
	post(url.Values{})
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorPrefix); v != "0" {
		t.Fatalf("prefix after clear %q", v)
	}
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorListFast); v != "0" {
		t.Fatalf("list_fast after clear %q", v)
	}
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorGrokFailover); v != "0" {
		t.Fatalf("grok after clear %q", v)
	}
}

func TestPanelSubUnauthorized(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/panel/sub", nil))
	if res.Code != 401 {
		t.Fatalf("status %d", res.Code)
	}
}

func TestPanelSubJSON(t *testing.T) {
	restore := provider.SetJSONClientForTests(&http.Client{Transport: roundTrip(func(r *http.Request) *http.Response {
		if strings.Contains(r.URL.Path, "/api/oauth/usage") {
			return jsonResp(200, map[string]any{
				"five_hour": map[string]any{"utilization": 10.0, "resets_at": "2026-09-01T00:00:00Z"},
			})
		}
		if strings.Contains(r.URL.Path, "/api/oauth/profile") {
			return jsonResp(200, map[string]any{"account": map[string]any{"has_claude_pro": true}})
		}
		return jsonResp(404, map[string]any{})
	})})
	defer restore()
	booted, session := unlocked(t)
	defer booted.DB.Close()
	if _, err := booted.Fob.Vault.Save(store.SaveCredential{
		Provider: domain.ProviderClaude, Label: "work claude",
		Tokens: domain.CredentialTokens{AccessToken: "at", Extra: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/panel/sub", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Credentials []map[string]any `json:"credentials"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Credentials) != 1 || body.Credentials[0]["ok"] != true || body.Credentials[0]["plan"] != "Pro" {
		t.Fatalf("%s", res.Body.String())
	}
}

func TestPanelSubOmitsUnknownProviders(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	if _, err := booted.Fob.Vault.Save(store.SaveCredential{
		Provider: domain.ProviderClaude, Label: "work claude",
		Tokens: domain.CredentialTokens{AccessToken: "at", Extra: map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	html := res.Body.String()
	if !strings.Contains(html, ">Sub<") || !strings.Contains(html, "id=\"sub-load\"") || !strings.Contains(html, "/panel.js?v=0.8.0") {
		t.Fatalf("%s", html)
	}
}

func TestAddOpenAISourceFromPanel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o","name":"GPT-4o"}]}`))
	}))
	defer srv.Close()
	restore := provider.SetJSONClientForTests(srv.Client())
	defer restore()
	booted, session := unlocked(t)
	defer booted.DB.Close()
	form := url.Values{"label": {"OpenRouter"}, "base_url": {srv.URL + "/v1"}, "secret": {"sk-test"}}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sources/openai", strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 303 || res.Header().Get("location") != "/" {
		t.Fatalf("status %d loc %s body %s", res.Code, res.Header().Get("location"), res.Body.String())
	}
	creds, err := booted.Fob.Vault.List(domain.ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 1 || creds[0].Label != "OpenRouter" || creds[0].Tokens.AccessToken != "sk-test" {
		t.Fatalf("%+v", creds)
	}
	if creds[0].Tokens.Extra["slug"] != "openrouter" {
		t.Fatalf("slug %+v", creds[0].Tokens.Extra)
	}
	page := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(page, pageReq)
	html := page.Body.String()
	if !strings.Contains(html, "OpenRouter") || !strings.Contains(html, "openrouter") {
		t.Fatalf("%s", html)
	}

	created, err := booted.Fob.Keys.Create("t", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	models := httptest.NewRecorder()
	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsReq.Header.Set("authorization", "Bearer "+created.Secret)
	booted.Handler.ServeHTTP(models, modelsReq)
	if models.Code != 200 || !strings.Contains(models.Body.String(), "openrouter/gpt-4o") {
		t.Fatalf("%s", models.Body.String())
	}
}

func TestAddOpenAISourceRejectsBadURL(t *testing.T) {
	booted, session := unlocked(t)
	defer booted.DB.Close()
	form := url.Values{"label": {"bad"}, "base_url": {"not-a-url"}, "secret": {"sk"}}
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sources/openai", strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "Add OpenAI source") {
		t.Fatalf("status %d %s", res.Code, res.Body.String())
	}
	creds, err := booted.Fob.Vault.List(domain.ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if len(creds) != 0 {
		t.Fatalf("saved invalid source %+v", creds)
	}
}

func TestAlpineJS(t *testing.T) {
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer booted.DB.Close()
	res := httptest.NewRecorder()
	booted.Handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/alpine.min.js", nil))
	if res.Code != 200 || !strings.Contains(res.Body.String(), "Alpine") {
		t.Fatal(res.Code)
	}
}

func unlocked(t *testing.T) (*Booted, string) {
	t.Helper()
	booted, err := Create(map[string]string{"JWT_SECRET": secret, "DATABASE_PATH": ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	seed, _, err := booted.Panel.EnsureSeed()
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	form := url.Values{"old_password": {seed}, "new_password": {"long-enough"}}
	req := httptest.NewRequest(http.MethodPost, "/password", strings.NewReader(form.Encode()))
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	booted.Handler.ServeHTTP(res, req)
	if res.Code != 303 || res.Header().Get("location") != "/" {
		t.Fatalf("reset %d %s", res.Code, res.Header().Get("location"))
	}
	cookie := strings.Split(res.Header().Get("set-cookie"), ";")[0]
	if !strings.Contains(cookie, "fob_session=") {
		t.Fatalf("cookie %s", cookie)
	}
	return booted, cookie
}

type roundTrip func(*http.Request) *http.Response

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r), nil }

func jsonResp(status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	rec.Header().Set("content-type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.Write(raw)
	res := rec.Result()
	res.Body = io.NopCloser(strings.NewReader(string(raw)))
	return res
}
