package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/oauth"
	"github.com/kacperkwapisz/fob/internal/panel"
	"github.com/kacperkwapisz/fob/internal/proxy"
	"github.com/kacperkwapisz/fob/internal/session"
	"github.com/kacperkwapisz/fob/internal/store"
)

const dayMS = 24 * 60 * 60 * 1000

func registerPanel(mux *httpx.Mux, fob *proxy.Fob, e *env.Env, panelAuth *store.PanelAuth, settings *store.SettingsStore, logins map[domain.ProviderID]oauth.ProviderLogin) {
	mux.Handle(http.MethodGet, "/design.css", func(w http.ResponseWriter, _ *http.Request) {
		httpx.Bytes(w, 200, "text/css; charset=utf-8", panel.DesignCSS)
	})
	mux.Handle(http.MethodGet, "/panel.js", func(w http.ResponseWriter, _ *http.Request) {
		httpx.Bytes(w, 200, "text/javascript; charset=utf-8", panel.PanelJS)
	})
	mux.Handle(http.MethodGet, "/alpine.js", func(w http.ResponseWriter, _ *http.Request) {
		httpx.Bytes(w, 200, "text/javascript; charset=utf-8", panel.AlpineJS)
	})
	mux.Handle(http.MethodGet, "/", func(w http.ResponseWriter, r *http.Request) {
		if panelAuth.State().MustReset {
			page(w, panel.ResetView(""), "Fob — set password", "")
			return
		}
		if !panelAuthed(r, e, panelAuth) {
			page(w, panel.LoginView(""), "Fob — unlock", "")
			return
		}
		meta := e.PublicURL
		if meta == "" {
			meta = "local"
		}
		pageAuthed(w, dashboard(fob, settings), "Fob", meta)
	})
	mux.Handle(http.MethodPost, "/login", func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		password := httpx.FormString(body, "password")
		ok, mustReset := panelAuth.Verify(password)
		if !ok {
			page(w, panel.LoginView("wrong password"), "Fob — unlock", "")
			return
		}
		if mustReset {
			page(w, panel.ResetView("reset required"), "Fob — set password", "")
			return
		}
		httpx.SeeOther(w, "/", session.Issue(e.JWTKey, isSecure(e.PublicURL)))
	})
	mux.Handle(http.MethodPost, "/password", func(w http.ResponseWriter, r *http.Request) {
		body, _ := httpx.ParseBody(r)
		ok, reason := panelAuth.Reset(httpx.FormString(body, "old_password"), httpx.FormString(body, "new_password"))
		if !ok {
			page(w, panel.ResetView(reason), "Fob — set password", "")
			return
		}
		httpx.SeeOther(w, "/", session.Issue(e.JWTKey, isSecure(e.PublicURL)))
	})
	mux.Handle(http.MethodPost, "/logout", func(w http.ResponseWriter, _ *http.Request) {
		httpx.SeeOther(w, "/", session.Clear(isSecure(e.PublicURL)))
	})
	mux.Handle(http.MethodPost, "/login/{provider}", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		providerID := domain.ProviderID(httpx.Param(r, "provider"))
		if !domain.IsProviderID(string(providerID)) {
			httpx.SeeOther(w, "/", "")
			return
		}
		mode := r.URL.Query().Get("mode")
		start, err := logins[providerID].Start(r.Context(), "", mode)
		if err != nil {
			httpx.SeeOther(w, "/", "")
			return
		}
		if start.Kind == "secret" {
			page(w, panel.SecretView(string(providerID), start.URL, ""), "Fob — "+string(providerID), "")
			return
		}
		if start.Kind == "device" {
			page(w, panel.DeviceView(string(providerID), start.URL, start.UserCode, or(start.State, ""), ""), "Fob — "+string(providerID), "")
			return
		}
		hint := oauth.ClaudeRedirectURI
		if providerID == domain.ProviderCodex {
			hint = oauth.CodexRedirectURI
		}
		note := ""
		danger := false
		if providerID == domain.ProviderClaude || providerID == domain.ProviderCodex {
			login := logins[providerID]
			bind, _ := oauth.BindCallback(providerID, func(code, state string) {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				result, err := login.Complete(ctx, code, state, "", "")
				if err != nil {
					return
				}
				_, _ = fob.Vault.Save(store.SaveCredential{Provider: result.Provider, Label: result.Label, Tokens: result.Tokens, ExpiresAt: result.ExpiresAt})
			})
			if bind.Busy {
				note = oauth.BindErrorNote(true)
				danger = true
			} else {
				note = "Listening on " + hint + " — finish sign-in and this page will connect. Paste remains a fallback."
			}
		}
		view := panel.PasteCallbackViewNote(string(providerID), start.URL, hint, note, danger)
		page(w, view, "Fob — "+string(providerID), "")
	})
	mux.Handle(http.MethodPost, "/login/{provider}/finish", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		providerID := domain.ProviderID(httpx.Param(r, "provider"))
		if providerID != domain.ProviderClaude && providerID != domain.ProviderCodex {
			httpx.SeeOther(w, "/", "")
			return
		}
		hint := oauth.ClaudeRedirectURI
		if providerID == domain.ProviderCodex {
			hint = oauth.CodexRedirectURI
		}
		body, _ := httpx.ParseBody(r)
		parsed := oauth.ParseCallback(httpx.FormString(body, "callback"))
		fail := func(err string) {
			page(w, panel.PasteCallbackView(string(providerID), "", hint, err), "Fob — "+string(providerID), "")
		}
		if parsed.Error != "" {
			msg := parsed.ErrorDescription
			if msg == "" {
				msg = parsed.Error
			}
			fail(msg)
			return
		}
		if parsed.Code == "" {
			fail("paste the whole callback URL")
			return
		}
		result, err := logins[providerID].Complete(r.Context(), parsed.Code, parsed.State, "", "")
		if err != nil {
			fail(err.Error())
			return
		}
		oauth.UnbindCallback(providerID)
		_, _ = fob.Vault.Save(store.SaveCredential{Provider: result.Provider, Label: result.Label, Tokens: result.Tokens, ExpiresAt: result.ExpiresAt})
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/login/{provider}/secret", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		providerID := domain.ProviderID(httpx.Param(r, "provider"))
		if providerID != domain.ProviderCursor {
			httpx.SeeOther(w, "/", "")
			return
		}
		body, _ := httpx.ParseBody(r)
		result, err := logins[providerID].Complete(r.Context(), "", "", "", httpx.FormString(body, "secret"))
		if err != nil {
			page(w, panel.SecretView("cursor", "https://cursor.com/dashboard?tab=integrations", err.Error()), "Fob — cursor", "")
			return
		}
		_, _ = fob.Vault.Save(store.SaveCredential{Provider: result.Provider, Label: result.Label, Tokens: result.Tokens, ExpiresAt: result.ExpiresAt})
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/device/{provider}", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		providerID := domain.ProviderID(httpx.Param(r, "provider"))
		if !domain.IsProviderID(string(providerID)) {
			httpx.SeeOther(w, "/", "")
			return
		}
		body, _ := httpx.ParseBody(r)
		deviceCode := httpx.FormString(body, "device_code")
		ctx := r.Context()
		if providerID == domain.ProviderGrok || providerID == domain.ProviderCursor {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 16*time.Minute)
			defer cancel()
		}
		result, err := logins[providerID].Complete(ctx, "", "", deviceCode, "")
		if err != nil {
			page(w, panel.DeviceView(string(providerID), "", "", deviceCode, err.Error()), "Fob — "+string(providerID), "")
			return
		}
		_, _ = fob.Vault.Save(store.SaveCredential{Provider: result.Provider, Label: result.Label, Tokens: result.Tokens, ExpiresAt: result.ExpiresAt})
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/settings/cursor", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		body, _ := httpx.ParseBody(r)
		prefix := "0"
		if httpx.FormString(body, "prefix") == "1" {
			prefix = "1"
		}
		grok := "0"
		if httpx.FormString(body, "grok_failover") == "1" {
			grok = "1"
		}
		_ = settings.Set(proxy.SettingCursorPrefix, prefix)
		_ = settings.Set(proxy.SettingCursorGrokFailover, grok)
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/credentials/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		_, _ = fob.Vault.Remove(httpx.Param(r, "id"))
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/keys", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		body, _ := httpx.ParseBody(r)
		name := strings.TrimSpace(httpx.FormString(body, "name"))
		if name == "" {
			name = "tool"
		}
		_, _ = fob.Keys.Create(name, nil, nil, nil)
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/keys/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.SeeOther(w, "/", "")
			return
		}
		_, _ = fob.Keys.Revoke(httpx.Param(r, "id"))
		httpx.SeeOther(w, "/", "")
	})
	mux.Handle(http.MethodPost, "/api/panel/keys", func(w http.ResponseWriter, r *http.Request) {
		if !panelAuthed(r, e, panelAuth) {
			httpx.PanelUnauthorized(w, "")
			return
		}
		body, _ := httpx.ParseBody(r)
		name := strings.TrimSpace(httpx.JSONField(body, "name"))
		if name == "" {
			name = "tool"
		}
		created, err := fob.Keys.Create(name, nil, nil, nil)
		if err != nil {
			httpx.JSON(w, 500, map[string]any{"error": err.Error()})
			return
		}
		httpx.JSON(w, 200, map[string]any{
			"secret": created.Secret,
			"id":     created.Key.ID,
			"name":   created.Key.Name,
			"prefix": created.Key.Prefix,
		})
	})
}

func dashboard(fob *proxy.Fob, settings *store.SettingsStore) string {
	creds, _ := fob.Vault.List()
	keys, _ := fob.Keys.List()
	today, _ := fob.Usage.Since(dayMS)
	d7, _ := fob.Usage.Since(7 * dayMS)
	byProvider, _ := fob.Usage.GroupBy(7*dayMS, "provider")
	byModel, _ := fob.Usage.GroupBy(7*dayMS, "model")
	prefix, _ := settings.Get(proxy.SettingCursorPrefix)
	grok, _ := settings.Get(proxy.SettingCursorGrokFailover)
	return panel.Dashboard(panel.DashboardProps{
		Credentials: creds,
		Keys:        keys,
		Usage: panel.UsageProps{
			Today: today, D7: d7, ByProvider: byProvider, ByModel: byModel,
		},
		Settings: panel.SettingsProps{CursorPrefix: prefix == "1", GrokFailover: grok == "1"},
	})
}

func panelAuthed(r *http.Request, e *env.Env, panelAuth *store.PanelAuth) bool {
	if session.Read(r, e.JWTKey) {
		return true
	}
	password := session.BearerPassword(r)
	if password == "" {
		return false
	}
	ok, _ := panelAuth.Verify(password)
	return ok
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
