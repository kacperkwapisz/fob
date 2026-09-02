package panel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/kacperkwapisz/fob/internal/domain"
	"github.com/kacperkwapisz/fob/internal/store"
)

type DashboardProps struct {
	Credentials []domain.Credential
	Keys        []domain.LocalKey
	Usage       UsageProps
	SubCount    int
	Settings    SettingsProps
}

type UsageProps struct {
	Today      store.UsageTotals      `json:"today"`
	D7         store.UsageTotals      `json:"d7"`
	ByProvider []store.UsageBreakdown `json:"byProvider"`
	ByModel    []store.UsageBreakdown `json:"byModel"`
	Daily      []store.UsagePoint     `json:"daily"`
	Trends     []store.UsagePoint     `json:"trends"`
}

type SettingsProps struct {
	CursorPrefix bool
	GrokFailover bool
}

func Layout(title, meta, body string) string {
	if meta == "" {
		meta = "local proxy"
	}
	return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>` + esc(title) + `</title>
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link
      href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans:wght@400;500;600&family=IBM+Plex+Mono:wght@400;500&display=swap"
      rel="stylesheet"
    />
    <link rel="stylesheet" href="/design.css?v=0.6.3" />
    <script>document.documentElement.classList.add("js")</script>
    <script src="/alpine.min.js" defer></script>
    <script src="/panel.js?v=0.6.3" defer></script>
  </head>
  <body>
    <a class="skip" href="#main">Skip to content</a>
    <div class="shell">
      <header class="mast">
        <div class="wordmark"><span class="led" aria-hidden="true"></span>Fob</div>
        <button type="button" class="endpoint" data-copy-text="` + attr(meta) + `" title="Copy endpoint">` + esc(meta) + `</button>
        <form class="mast-lock" method="post" action="/logout">
          <button class="btn btn-ghost" type="submit">Lock</button>
        </form>
      </header>
      <main id="main">
      ` + body + `
      </main>
    </div>
    <dialog id="confirm-dialog" class="dialog">
      <form method="dialog" class="stack">
        <h3>Confirm</h3>
        <p class="lede" id="confirm-body"></p>
        <div class="actions">
          <button class="btn" value="cancel">Cancel</button>
          <button class="btn btn-danger" value="ok" id="confirm-ok">Continue</button>
        </div>
      </form>
    </dialog>
  </body>
</html>`
}

func LoginView(err string) string {
	return gate("Unlock", "Panel password. Printed to stdout on first boot if you have not set one yet.", err, `
      <form class="stack" method="post" action="/login">
        <label class="field">
          <span>Password</span>
          <input type="password" name="password" autocomplete="current-password" autofocus required />
        </label>
        <button class="btn btn-primary" type="submit">Enter</button>
      </form>`)
}

func ResetView(err string) string {
	return gate("Set a password", "The seed only works once. Pick something you will type again.", err, `
      <form class="stack" method="post" action="/password">
        <label class="field">
          <span>Current / seed</span>
          <input type="password" name="old_password" autocomplete="current-password" required />
        </label>
        <label class="field">
          <span>New password</span>
          <input type="password" name="new_password" autocomplete="new-password" minlength="8" required />
        </label>
        <button class="btn btn-primary" type="submit">Save</button>
      </form>`)
}

func DeviceView(provider, url, userCode, deviceCode, err string) string {
	lede := "Open the link, authorize, then confirm here."
	if userCode != "" {
		lede = "Open the link, enter the code, then confirm here."
	}
	var b strings.Builder
	if userCode != "" {
		fmt.Fprintf(&b, `<p class="metric user-code">%s<small>user code</small></p>`, esc(userCode))
	}
	if url != "" {
		fmt.Fprintf(&b, `<p><a href="%s" target="_blank" rel="noreferrer">Open %s</a></p>`, attr(url), esc(provider))
	}
	fmt.Fprintf(&b, `<form class="stack" method="post" action="/device/%s">
        <input type="hidden" name="device_code" value="%s" />
        <button class="btn btn-primary" type="submit">I've authorized</button>
      </form>`, attr(provider), attr(deviceCode))
	return gate("Authorize "+provider, lede, err, b.String())
}

func PasteCallbackView(provider, url, hint, err string) string {
	return PasteCallbackViewNote(provider, url, hint, err, true)
}

func PasteCallbackViewNote(provider, url, hint, note string, danger bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<p class="lede">
        Open the provider, sign in, then paste the whole address bar from the page that fails to load.
        It will look like <code>%s</code>
      </p>`, esc(hint))
	if note != "" {
		kind := "ok"
		if danger {
			kind = "danger"
		}
		b.WriteString(flash(kind, note))
	}
	if url != "" {
		fmt.Fprintf(&b, `<p><a href="%s" target="_blank" rel="noreferrer">Open %s</a></p>`, attr(url), esc(provider))
	}
	fmt.Fprintf(&b, `<form class="stack" method="post" action="/login/%s/finish">
        <label class="field">
          <span>Callback URL</span>
          <textarea name="callback" rows="3" autocomplete="off" required placeholder="%s"></textarea>
        </label>
        <button class="btn btn-primary" type="submit">Connect</button>
      </form>
      <p class="note">If the browser lands on a working Fob page, you can close it. Otherwise paste the whole failed URL here.</p>`, attr(provider), attr(hint))
	inner := fmt.Sprintf(`<h1>Finish %s login</h1>%s`, esc(provider), b.String())
	return `<section class="gate card">` + inner + `</section>`
}

func SecretView(provider, url, err string) string {
	var b strings.Builder
	if url != "" {
		fmt.Fprintf(&b, `<p><a href="%s" target="_blank" rel="noreferrer">Open Cursor Integrations</a></p>`, attr(url))
	}
	fmt.Fprintf(&b, `<form class="stack" method="post" action="/login/%s/secret">
        <label class="field">
          <span>API key</span>
          <input type="password" name="secret" autocomplete="off" required placeholder="crsr_…" />
        </label>
        <button class="btn btn-primary" type="submit">Connect</button>
      </form>`, attr(provider))
	return gate("Paste "+provider+" key", "Create a user API key in Cursor → Integrations, then paste it here.\n        If exchange fails, use Login instead — that is the CLI subscription path.", err, b.String())
}

func Dashboard(props DashboardProps) string {
	var b strings.Builder
	b.WriteString(`
    <div class="dialog-layer" id="minted" hidden x-data="{ copied: false }" @fob-minted.window="$el.hidden = false; copied = false">
      <div class="card dialog-card" role="status">
        <h2>New local key</h2>
        <p class="lede">Copy it now. Fob will not show it again.</p>
        <p class="secret"></p>
        <div class="actions">
          <button class="btn btn-primary" type="button" data-copy x-text="copied ? 'Copied' : 'Copy'" @click="copied = true">Copy</button>
          <button class="btn" type="button" data-dismiss>Done</button>
        </div>
      </div>
    </div>
    <div class="deck">`)
	b.WriteString(usageTrendsCard(props.Usage))
	b.WriteString(meterCard(props.Usage))
	b.WriteString(loginsCard(props))
	b.WriteString(keysCard(props.Keys))
	b.WriteString(subCard(props.SubCount))
	b.WriteString(cursorSettingsCard(props.Settings))
	b.WriteString(`</div>`)
	return b.String()
}

func loginsCard(props DashboardProps) string {
	byProvider := map[domain.ProviderID][]domain.Credential{}
	for _, c := range props.Credentials {
		byProvider[c.Provider] = append(byProvider[c.Provider], c)
	}
	providers := []struct {
		id    domain.ProviderID
		label string
	}{
		{domain.ProviderClaude, "Claude"},
		{domain.ProviderCodex, "Codex"},
		{domain.ProviderGrok, "Grok"},
		{domain.ProviderCursor, "Cursor"},
	}
	var b strings.Builder
	b.WriteString(`<section class="card card-logins"><div class="card-head"><h2>Logins</h2><p class="lede">OAuth into the subscriptions you already pay for. After Claude or Codex, paste the failed localhost URL back here.</p></div><ul class="provider-list">`)
	for _, p := range providers {
		creds := byProvider[p.id]
		pip := "pip"
		if len(creds) > 0 {
			pip = "pip pip-ok"
		}
		b.WriteString(`<li class="provider"><span class="` + pip + `" aria-hidden="true"></span><div class="provider-meta"><strong>`)
		b.WriteString(esc(p.label))
		b.WriteString(`</strong><div class="lede">`)
		if len(creds) == 0 {
			b.WriteString("Not connected")
		} else {
			for i, c := range creds {
				if i > 0 {
					b.WriteString(" · ")
				}
				b.WriteString(esc(c.Label))
			}
		}
		b.WriteString(`</div></div><div class="actions">`)
		for _, c := range creds {
			fmt.Fprintf(&b, `<form method="post" action="/credentials/%s/delete" data-confirm="Disconnect %s?"><button class="btn btn-danger" type="submit">Out</button></form>`, attr(c.ID), attr(c.Label))
		}
		label := "Login"
		if len(creds) > 0 {
			label = "Add"
		}
		fmt.Fprintf(&b, `<form method="post" action="/login/%s"><button class="btn btn-primary" type="submit">%s</button></form>`, attr(string(p.id)), label)
		if p.id == domain.ProviderCursor {
			b.WriteString(`<form method="post" action="/login/cursor?mode=secret"><button class="btn" type="submit">Paste key</button></form>`)
		}
		b.WriteString(`</div></li>`)
	}
	b.WriteString(`</ul></section>`)
	return b.String()
}

func keysCard(keys []domain.LocalKey) string {
	var live []domain.LocalKey
	for _, k := range keys {
		if !k.Revoked {
			live = append(live, k)
		}
	}
	var b strings.Builder
	b.WriteString(`<section class="card card-keys"><div class="card-head"><h2>Keys</h2><p class="lede">Hand a <code>sk-fob-…</code> to Cursor, Claude Code, OpenCode.</p></div>`)
	if len(live) == 0 {
		b.WriteString(`<p class="empty">No keys yet. Mint one and paste it into a tool as the OpenAI API key.</p>`)
	} else {
		b.WriteString(`<table class="table"><thead><tr><th>Name</th><th>Prefix</th><th></th></tr></thead><tbody>`)
		for _, k := range live {
			scope := "all"
			if len(k.Providers) > 0 {
				parts := make([]string, len(k.Providers))
				for i, p := range k.Providers {
					parts[i] = string(p)
				}
				scope = strings.Join(parts, " ")
			}
			fmt.Fprintf(&b, `<tr><td>%s<br /><span class="pill">%s</span></td><td><code class="key-prefix">%s…</code></td><td><form method="post" action="/keys/%s/revoke" data-confirm="Revoke %s? Tools using it will 401."><button class="btn btn-danger" type="submit">Revoke</button></form></td></tr>`,
				esc(k.Name), esc(scope), esc(k.Prefix), attr(k.ID), attr(k.Name))
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`<form class="mint" id="mint" method="post" action="/keys">
        <label class="field grow">
          <span>Name</span>
          <input type="text" name="name" placeholder="opencode" required />
        </label>
        <button class="btn btn-primary" type="submit">Mint key</button>
      </form></section>`)
	return b.String()
}

func usageTrendsCard(usage UsageProps) string {
	trends := usage.Trends
	if trends == nil {
		trends = []store.UsagePoint{}
	}
	raw, _ := json.Marshal(trends)
	var window store.UsageTotals
	var peak int64
	hasTraffic := false
	for _, day := range trends {
		window.PromptTokens += day.PromptTokens
		window.CompletionTokens += day.CompletionTokens
		tokens := day.PromptTokens + day.CompletionTokens
		if tokens > peak {
			peak = tokens
		}
		if day.Requests > 0 {
			hasTraffic = true
		}
	}
	var b strings.Builder
	b.WriteString(`<section class="card card-trends"><div class="card-head"><h2>Usage Trends</h2>`)
	if len(trends) > 0 {
		fmt.Fprintf(&b, `<p class="lede">Tokens per UTC day. %s – %s.</p></div>`, esc(shortDay(trends[0].Day)), esc(shortDay(trends[len(trends)-1].Day)))
	} else {
		b.WriteString(`<p class="lede">Tokens per UTC day. Last 14 days.</p></div>`)
	}
	b.WriteString(`<div class="kpis">`)
	fmt.Fprintf(&b, `<div class="kpi kpi-signal"><div class="kpi-value">%s</div><div class="kpi-label">%d days</div></div>`, fmtInt(window.PromptTokens+window.CompletionTokens), max(len(trends), 14))
	fmt.Fprintf(&b, `<div class="kpi"><div class="kpi-value">%s</div><div class="kpi-label">peak day</div></div>`, fmtInt(peak))
	fmt.Fprintf(&b, `<div class="kpi"><div class="kpi-value">%s</div><div class="kpi-label">prompt</div></div>`, fmtInt(window.PromptTokens))
	fmt.Fprintf(&b, `<div class="kpi"><div class="kpi-value">%s</div><div class="kpi-label">completion</div></div>`, fmtInt(window.CompletionTokens))
	b.WriteString(`</div><figure class="chart">
        <figcaption>Prompt / completion</figcaption>
        <div class="chart-frame">
          <canvas id="chart-trends" role="img" aria-label="Token usage over the last fourteen days"></canvas>
          <div class="chart-tip" id="chart-trends-tip" hidden></div>
        </div>
      </figure>`)
	if !hasTraffic {
		b.WriteString(`<p class="empty">No traffic yet. Point a tool at <code>/v1</code>.</p>`)
	}
	b.WriteString(`<script type="application/json" id="trends-data">`)
	b.Write(raw)
	b.WriteString(`</script></section>`)
	return b.String()
}

func shortDay(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return t.Format("2 Jan")
}

func meterCard(usage UsageProps) string {
	if usage.ByProvider == nil {
		usage.ByProvider = []store.UsageBreakdown{}
	}
	if usage.ByModel == nil {
		usage.ByModel = []store.UsageBreakdown{}
	}
	if usage.Daily == nil {
		usage.Daily = []store.UsagePoint{}
	}
	raw, _ := json.Marshal(usage)
	var b strings.Builder
	b.WriteString(`<section class="card card-meter"><div class="card-head"><h2>Meter</h2><p class="lede">API-equivalent $. Not your subscription bill.</p></div><div class="kpis">`)
	fmt.Fprintf(&b, `<div class="kpi kpi-signal"><div class="kpi-value">%s</div><div class="kpi-label">today</div></div>`, fmtUSD(usage.Today.USD))
	fmt.Fprintf(&b, `<div class="kpi"><div class="kpi-value">%s</div><div class="kpi-label">7 days</div></div>`, fmtUSD(usage.D7.USD))
	fmt.Fprintf(&b, `<div class="kpi"><div class="kpi-value">%s</div><div class="kpi-label">tokens today</div></div>`, fmtInt(usage.Today.PromptTokens+usage.Today.CompletionTokens))
	fmt.Fprintf(&b, `<div class="kpi"><div class="kpi-value">%s</div><div class="kpi-label">requests today</div></div>`, fmtInt(usage.Today.Requests))
	b.WriteString(`</div><div class="charts">
      <figure class="chart">
        <figcaption>7-day equivalent $</figcaption>
        <div class="chart-frame">
          <canvas id="chart-series" role="img" aria-label="Equivalent dollar spend over the last seven days"></canvas>
          <div class="chart-tip" id="chart-series-tip" hidden></div>
        </div>
      </figure>
      <figure class="chart">
        <figcaption>By provider</figcaption>
        <div class="chart-frame">
          <canvas id="chart-providers" role="img" aria-label="Equivalent dollar spend by provider"></canvas>
          <div class="chart-tip" id="chart-providers-tip" hidden></div>
        </div>
      </figure>
    </div>`)
	if len(usage.ByProvider) == 0 {
		b.WriteString(`<p class="empty">No traffic yet. Point a tool at <code>/v1</code>.</p>`)
	} else {
		b.WriteString(`<table class="table models"><thead><tr><th>Model</th><th>$</th></tr></thead><tbody>`)
		limit := len(usage.ByModel)
		if limit > 8 {
			limit = 8
		}
		for _, r := range usage.ByModel[:limit] {
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td></tr>`, esc(r.Key), fmtUSD(r.USD))
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`<script type="application/json" id="meter-data">`)
	b.Write(raw)
	b.WriteString(`</script></section>`)
	return b.String()
}

func subCard(n int) string {
	var b strings.Builder
	b.WriteString(`<section class="card card-sub" id="sub"><div class="card-head"><h2>Sub</h2><p class="lede">Remaining on the subscription. Not the meter.</p></div>`)
	if n == 0 {
		b.WriteString(`<p class="empty">Connect a login, then load.</p></section>`)
		return b.String()
	}
	b.WriteString(`<div id="sub-body"><p class="empty">Idle until you load.</p></div>
      <div class="actions" style="margin-top:1rem">
        <button class="btn btn-primary" type="button" id="sub-load">Load</button>
      </div></section>`)
	return b.String()
}

func cursorSettingsCard(settings SettingsProps) string {
	prefix := ""
	if settings.CursorPrefix {
		prefix = "checked"
	}
	grok := ""
	if settings.GrokFailover {
		grok = "checked"
	}
	return fmt.Sprintf(`<section class="card card-settings">
      <div class="card-head">
        <h2>Cursor</h2>
        <p class="lede">Prefix listed Cursor models with <code>cursor/</code> (effort/fast variants already collapse). Grok failover maps <code>grok-4.5</code> onto Cursor Grok when the Grok sub is exhausted.</p>
      </div>
      <form method="post" action="/settings/cursor" class="stack" data-autosave>
        <label class="switch-row">
          <span>
            <strong>Prefix Cursor models</strong>
            <span class="lede">List them as <code>cursor/…</code></span>
          </span>
          <input class="switch" type="checkbox" name="prefix" value="1" %s />
        </label>
        <label class="switch-row">
          <span>
            <strong>Grok ↔ Cursor failover</strong>
            <span class="lede">Fall over when the Grok sub is exhausted</span>
          </span>
          <input class="switch" type="checkbox" name="grok_failover" value="1" %s />
        </label>
        <button class="btn js-hide" type="submit">Save</button>
      </form>
    </section>`, prefix, grok)
}

func gate(title, lede, err, form string) string {
	var b strings.Builder
	b.WriteString(`<section class="gate card"><h1>`)
	b.WriteString(esc(title))
	b.WriteString(`</h1><p class="lede">`)
	b.WriteString(esc(lede))
	b.WriteString(`</p>`)
	if err != "" {
		b.WriteString(flash("danger", err))
	}
	b.WriteString(form)
	b.WriteString(`</section>`)
	return b.String()
}

func flash(kind, message string) string {
	cls := "flash"
	if kind == "danger" {
		cls = "flash flash-danger"
	} else if kind == "ok" {
		cls = "flash flash-ok"
	}
	return fmt.Sprintf(`<div class="%s" role="status">%s</div>`, cls, esc(message))
}

func esc(s string) string {
	var buf bytes.Buffer
	template.HTMLEscape(&buf, []byte(s))
	return buf.String()
}

func attr(s string) string {
	return template.HTMLEscapeString(s)
}

func fmtUSD(n float64) string {
	return "$" + strconv.FormatFloat(n, 'f', 2, 64)
}

func fmtInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	if s != "" {
		parts = append([]string{s}, parts...)
	}
	return strings.Join(parts, ",")
}
