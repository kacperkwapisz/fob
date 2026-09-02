package panel

import (
	"bytes"
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
	Settings    SettingsProps
}

type UsageProps struct {
	Today      store.UsageTotals
	D7         store.UsageTotals
	ByProvider []store.UsageBreakdown
	ByModel    []store.UsageBreakdown
	Trends     []store.UsageDay
}

type SettingsProps struct {
	CursorPrefix bool
	GrokFailover bool
}

func Layout(title, meta, body string) string {
	if meta == "" {
		meta = "local proxy"
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%s</title>
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link
      href="https://fonts.googleapis.com/css2?family=Fraunces:opsz,wght@9..144,500;9..144,600&family=IBM+Plex+Mono:wght@400;500&display=swap"
      rel="stylesheet"
    />
    <link rel="stylesheet" href="/design.css" />
    <script src="/panel.js" defer></script>
  </head>
  <body>
    <div class="shell">
      <header class="mast">
        <div class="wordmark">Fob<span>.</span></div>
        <div class="mast-meta">%s</div>
      </header>
      %s
    </div>
  </body>
</html>`, esc(title), esc(meta), body)
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
		fmt.Fprintf(&b, `<p class="metric">%s<small>user code</small></p>`, esc(userCode))
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
	if err != "" {
		b.WriteString(flash("danger", err))
	}
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
    <div class="flash" id="minted" hidden>
      <strong>New local key</strong>
      <p>Copy it now. Fob will not show it again.</p>
      <p class="secret"></p>
      <div class="actions">
        <button class="btn" type="button" data-copy>Copy</button>
        <button class="btn" type="button" data-dismiss>Done</button>
      </div>
    </div>
    <div class="deck">`)
	b.WriteString(loginsCard(props))
	b.WriteString(keysCard(props.Keys))
	b.WriteString(meterCard(props.Usage))
	b.WriteString(usageTrendsCard(props.Usage))
	b.WriteString(cursorSettingsCard(props.Settings))
	b.WriteString(`</div>
    <form method="post" action="/logout" style="margin-top:2rem">
      <button class="btn" type="submit">Lock panel</button>
    </form>`)
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
	b.WriteString(`<section class="card"><h2>Logins</h2><p class="lede">OAuth into the subscriptions you already pay for. After Claude or Codex, paste the failed localhost URL back here.</p>`)
	for _, p := range providers {
		creds := byProvider[p.id]
		b.WriteString(`<div class="row"><div><strong>`)
		b.WriteString(esc(p.label))
		b.WriteString(`</strong><div class="lede" style="margin:0">`)
		if len(creds) == 0 {
			b.WriteString("Not connected")
		} else {
			for _, c := range creds {
				b.WriteString(esc(c.Label))
				b.WriteString(" ")
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
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`</section>`)
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
	b.WriteString(`<section class="card"><h2>Keys</h2><p class="lede">Hand a <code>sk-fob-…</code> to Cursor, Claude Code, OpenCode.</p>`)
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
			fmt.Fprintf(&b, `<tr><td>%s<br /><span class="pill">%s</span></td><td>%s…</td><td><form method="post" action="/keys/%s/revoke" data-confirm="Revoke %s? Tools using it will 401."><button class="btn btn-danger" type="submit">Revoke</button></form></td></tr>`,
				esc(k.Name), esc(scope), esc(k.Prefix), attr(k.ID), attr(k.Name))
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`<form class="stack" id="mint" method="post" action="/keys" style="margin-top:1rem">
        <label class="field">
          <span>Name</span>
          <input type="text" name="name" placeholder="opencode" required />
        </label>
        <button class="btn btn-primary" type="submit">Mint key</button>
      </form></section>`)
	return b.String()
}

func meterCard(usage UsageProps) string {
	var b strings.Builder
	b.WriteString(`<section class="card"><h2>Meter</h2><p class="lede">API-equivalent $. Not your subscription bill.</p><div class="metrics">`)
	fmt.Fprintf(&b, `<div class="metric">%s<small>today</small></div>`, fmtUSD(usage.Today.USD))
	fmt.Fprintf(&b, `<div class="metric">%s<small>7 days</small></div>`, fmtUSD(usage.D7.USD))
	fmt.Fprintf(&b, `<div class="metric">%s<small>tokens today</small></div>`, fmtInt(usage.Today.PromptTokens+usage.Today.CompletionTokens))
	fmt.Fprintf(&b, `<div class="metric">%s<small>requests today</small></div>`, fmtInt(usage.Today.Requests))
	b.WriteString(`</div>`)
	if len(usage.ByProvider) == 0 {
		b.WriteString(`<p class="empty">No traffic yet. Point a tool at <code>/v1</code>.</p>`)
	} else {
		b.WriteString(`<table class="table"><thead><tr><th>Provider</th><th>$</th></tr></thead><tbody>`)
		for _, r := range usage.ByProvider {
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td></tr>`, esc(r.Key), fmtUSD(r.USD))
		}
		b.WriteString(`</tbody></table><table class="table" style="margin-top:1rem"><thead><tr><th>Model</th><th>$</th></tr></thead><tbody>`)
		limit := len(usage.ByModel)
		if limit > 8 {
			limit = 8
		}
		for _, r := range usage.ByModel[:limit] {
			fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td></tr>`, esc(r.Key), fmtUSD(r.USD))
		}
		b.WriteString(`</tbody></table>`)
	}
	b.WriteString(`</section>`)
	return b.String()
}

func usageTrendsCard(usage UsageProps) string {
	var b strings.Builder
	b.WriteString(`<section class="card card-wide"><h2>Usage Trends</h2>`)
	if !trendHasTraffic(usage.Trends) {
		b.WriteString(`<p class="lede">Tokens per day. Last 14 days.</p><p class="empty">No traffic yet. Point a tool at <code>/v1</code>.</p></section>`)
		return b.String()
	}
	from := time.UnixMilli(usage.Trends[0].Start).In(time.Local)
	to := time.UnixMilli(usage.Trends[len(usage.Trends)-1].Start).In(time.Local)
	fmt.Fprintf(&b, `<p class="lede">Tokens per day. %s – %s.</p>`, esc(from.Format("2 Jan")), esc(to.Format("2 Jan")))
	var window store.UsageTotals
	var peak int64
	for _, day := range usage.Trends {
		window.PromptTokens += day.PromptTokens
		window.CompletionTokens += day.CompletionTokens
		tokens := day.PromptTokens + day.CompletionTokens
		if tokens > peak {
			peak = tokens
		}
	}
	b.WriteString(`<div class="metrics">`)
	fmt.Fprintf(&b, `<div class="metric">%s<small>%d days</small></div>`, fmtInt(window.PromptTokens+window.CompletionTokens), len(usage.Trends))
	fmt.Fprintf(&b, `<div class="metric">%s<small>peak day</small></div>`, fmtInt(peak))
	fmt.Fprintf(&b, `<div class="metric">%s<small>prompt</small></div>`, fmtInt(window.PromptTokens))
	fmt.Fprintf(&b, `<div class="metric">%s<small>completion</small></div>`, fmtInt(window.CompletionTokens))
	b.WriteString(`</div>`)
	fmt.Fprintf(&b, `<div class="trend" style="grid-template-columns:repeat(%d,minmax(0,1fr))" role="img" aria-label="Token usage over %d days, %s total">`,
		len(usage.Trends), len(usage.Trends), fmtInt(window.PromptTokens+window.CompletionTokens))
	for i, day := range usage.Trends {
		tokens := day.PromptTokens + day.CompletionTokens
		height := 0.0
		if peak > 0 {
			height = float64(tokens) / float64(peak) * 100
		}
		if tokens > 0 && height < 3 {
			height = 3
		}
		when := time.UnixMilli(day.Start).In(time.Local)
		label := when.Format("2")
		if i == 0 || i == len(usage.Trends)-1 || when.Day() == 1 {
			label = when.Format("2 Jan")
		}
		title := fmt.Sprintf("%s · %s prompt · %s completion",
			when.Format("2 Jan"), fmtInt(day.PromptTokens), fmtInt(day.CompletionTokens))
		fmt.Fprintf(&b, `<div class="trend-col" title="%s"><div class="trend-track">`, attr(title))
		if tokens > 0 {
			fmt.Fprintf(&b, `<div class="trend-stack" style="height:%.1f%%">`, height)
			fmt.Fprintf(&b, `<div class="trend-prompt" style="flex:%d"></div>`, day.PromptTokens)
			fmt.Fprintf(&b, `<div class="trend-completion" style="flex:%d"></div>`, day.CompletionTokens)
			b.WriteString(`</div>`)
		}
		fmt.Fprintf(&b, `</div><span class="trend-day">%s</span></div>`, esc(label))
	}
	b.WriteString(`</div><p class="trend-legend"><span><span class="swatch swatch-prompt"></span>prompt</span><span><span class="swatch swatch-completion"></span>completion</span></p></section>`)
	return b.String()
}

func trendHasTraffic(days []store.UsageDay) bool {
	for _, day := range days {
		if day.Requests > 0 {
			return true
		}
	}
	return false
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
	return fmt.Sprintf(`<section class="card">
      <h2>Cursor</h2>
      <p class="lede">Prefix listed Cursor models with <code>cursor/</code>. Grok failover maps <code>grok-4.5</code> onto Cursor Grok when the Grok sub is exhausted.</p>
      <form method="post" action="/settings/cursor" class="stack">
        <label class="field" style="flex-direction:row;gap:.6rem;align-items:center">
          <input type="checkbox" name="prefix" value="1" %s />
          <span>Prefix Cursor models</span>
        </label>
        <label class="field" style="flex-direction:row;gap:.6rem;align-items:center">
          <input type="checkbox" name="grok_failover" value="1" %s />
          <span>Grok ↔ Cursor failover</span>
        </label>
        <button class="btn" type="submit">Save</button>
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
	return "$" + strconv.FormatFloat(n, 'f', -1, 64)
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
