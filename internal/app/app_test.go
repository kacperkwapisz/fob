package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
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
	for _, want := range []string{"opencode", "sk-fob-", "Logins", "Keys", "Meter", "Usage Trends", "Cursor"} {
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
	if !strings.Contains(css, "--accent:") || !strings.Contains(css, "--font-display:") || !strings.Contains(css, "--dither-bayer:") || !strings.Contains(css, "card-trends") {
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
	if res.Code != 200 || !strings.Contains(js, "/api/panel/keys") || !strings.Contains(js, "/api/panel/sub") || !strings.Contains(js, "closest(\"#sub-load\")") || !strings.Contains(js, "drawTrends") {
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
	post(url.Values{"prefix": {"1"}, "grok_failover": {"1"}})
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorPrefix); v != "1" {
		t.Fatalf("prefix %q", v)
	}
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorGrokFailover); v != "1" {
		t.Fatalf("grok %q", v)
	}
	page := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("cookie", session)
	booted.Handler.ServeHTTP(page, req)
	html := page.Body.String()
	if !strings.Contains(html, `name="prefix" value="1" checked`) || !strings.Contains(html, `name="grok_failover" value="1" checked`) {
		t.Fatal("toggles not checked after save")
	}
	post(url.Values{})
	if v, _ := booted.Fob.Settings.Get(proxy.SettingCursorPrefix); v != "0" {
		t.Fatalf("prefix after clear %q", v)
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
	if !strings.Contains(html, ">Sub<") || !strings.Contains(html, "id=\"sub-load\"") || !strings.Contains(html, "/panel.js?v=0.7.1") {
		t.Fatalf("%s", html)
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
