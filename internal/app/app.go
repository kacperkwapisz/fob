package app

import (
	"net/http"

	"github.com/kacperkwapisz/fob/internal/db"
	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/env"
	"github.com/kacperkwapisz/fob/internal/httpx"
	"github.com/kacperkwapisz/fob/internal/oauth"
	"github.com/kacperkwapisz/fob/internal/panel"
	"github.com/kacperkwapisz/fob/internal/provider"
	claudeexec "github.com/kacperkwapisz/fob/internal/provider/claude"
	codexexec "github.com/kacperkwapisz/fob/internal/provider/codex"
	cursorexec "github.com/kacperkwapisz/fob/internal/provider/cursor"
	grokexec "github.com/kacperkwapisz/fob/internal/provider/grok"
	"github.com/kacperkwapisz/fob/internal/proxy"
	"github.com/kacperkwapisz/fob/internal/store"
)

type Booted struct {
	Handler http.Handler
	Env     *env.Env
	Fob     *proxy.Fob
	Panel   *store.PanelAuth
	DB      *db.DB
}

func Create(source map[string]string) (*Booted, error) {
	e, err := env.Load(source)
	if err != nil {
		return nil, err
	}
	d, err := db.Open(e.DatabasePath)
	if err != nil {
		return nil, err
	}
	vault := store.NewVault(d, e.JWTKey)
	keys := store.NewKeyStore(d)
	usage := store.NewUsageStore(d)
	prices, err := store.NewPriceStore(d)
	if err != nil {
		d.Close()
		return nil, err
	}
	panelAuth := store.NewPanelAuth(d)
	settings := store.NewSettingsStore(d)
	fob := &proxy.Fob{
		Env:      e,
		Vault:    vault,
		Keys:     keys,
		Usage:    usage,
		Prices:   prices,
		Settings: settings,
		Executors: map[domain.ProviderID]provider.Executor{
			domain.ProviderClaude: claudeexec.Executor{ClientID: e.ClaudeClientID},
			domain.ProviderCodex:  codexexec.Executor{ClientID: e.CodexClientID},
			domain.ProviderGrok:   grokexec.Executor{ClientID: e.GrokClientID},
			domain.ProviderCursor: &cursorexec.Executor{},
		},
	}
	logins := oauth.Logins(e)
	mux := httpx.NewMux()
	registerHealth(mux)
	registerOpenAI(mux, fob)
	registerAnthropic(mux, fob)
	registerResponses(mux, fob)
	registerPanel(mux, fob, e, panelAuth, settings, logins)
	return &Booted{Handler: httpx.Guard(mux), Env: e, Fob: fob, Panel: panelAuth, DB: d}, nil
}

func page(w http.ResponseWriter, body, title, meta string) {
	httpx.HTML(w, http.StatusOK, panel.Layout(title, meta, body))
}

func isSecure(publicURL string) bool {
	return len(publicURL) >= 8 && publicURL[:8] == "https://"
}
