package panel

import (
	"bytes"
	"fmt"
	"html/template"
	"strconv"
	"strings"

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
}

type SettingsProps struct {
	CursorPrefix bool
	GrokFailover bool
}

func Layout(title, meta, body string) string {
	return shell(title, meta, body, false)
}

func AuthedLayout(title, meta, body string) string {
	return shell(title, meta, body, true)
}

func shell(title, meta, body string, authed bool) string {
	if meta == "" {
		meta = "local proxy"
	}
	lock := ""
	if authed {
		lock = `<form method="post" action="/logout"><button class="btn btn-ghost" type="submit">Lock</button></form>`
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
    <script defer src="/alpine.js"></script>
    <script defer src="/panel.js"></script>
  </head>
  <body x-data="fob">
    <a class="skip" href="#main">Skip to panel</a>
    <div class="grain" aria-hidden="true"></div>
    <div class="shell">
      <header class="mast">
        <a class="wordmark" href="/">Fob<span>.</span></a>
        <div class="mast-end">
          <p class="mast-meta">%s</p>
          %s
        </div>
      </header>
      <main id="main">
      %s
      </main>
    </div>
    <dialog id="confirm-dlg" class="sheet" aria-labelledby="confirm-title">
      <h2 id="confirm-title">Please confirm</h2>
      <p class="lede" id="confirm-msg"></p>
      <div class="actions">
        <button class="btn" type="button" data-confirm-cancel>Cancel</button>
        <button class="btn btn-danger" type="button" data-confirm-ok>Confirm</button>
      </div>
    </dialog>
    <dialog id="minted" class="sheet" aria-labelledby="minted-title">
      <h2 id="minted-title">New local key</h2>
      <p class="lede">Copy it now. Fob will not show it again.</p>
      <p class="secret"></p>
      <div class="actions">
        <button class="btn btn-primary" type="button" data-copy>Copy</button>
        <button class="btn" type="button" data-dismiss>Done</button>
      </div>
    </dialog>
    <div class="toast" role="status" aria-live="polite" x-cloak x-show="toast" x-text="toast"></div>
  </body>
</html>`, esc(title), esc(meta), lock, body)
}

func LoginView(err string) string {
	return gate("Unlock", "Panel password. Printed to stdout on first boot if you have not set one yet.", err, `
      <form class="stack" method="post" action="/login">
        <label class="field">
          <span>Password</span>
          <div class="field-row" x-data="{ show: false }">
            <input :type="show ? 'text' : 'password'" type="password" name="password" autocomplete="current-password" autofocus required />
            <button class="btn btn-ghost" type="button" x-cloak @click="show = !show" :aria-pressed="show.toString()" x-text="show ? 'Hide' : 'Show'">Show</button>
          </div>
        </label>
        <button class="btn btn-primary" type="submit">Unlock</button>
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
		fmt.Fprintf(&b, `<button type="button" class="user-code" data-code="%s" aria-label="Copy user code">%s<small>user code · click to copy</small></button>`, attr(userCode), esc(userCode))
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
	return `<section class="gate drawer">` + inner + `</section>`
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
	return gate("Paste "+provider+" key", "Create a user API key in Cursor → Integrations, then paste it here. If exchange fails, use Login instead — that is the CLI subscription path.", err, b.String())
}

func Dashboard(props DashboardProps) string {
	var b strings.Builder
	b.WriteString(`<h1 class="sr-only">Panel</h1><div class="deck">`)
	b.WriteString(meterCard(props.Usage))
	b.WriteString(loginsCard(props))
	b.WriteString(keysCard(props.Keys))
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
	b.WriteString(`<section class="drawer logins-drawer"><header class="drawer-h"><h2>Logins</h2><p class="lede">OAuth into the subscriptions you already pay for. After Claude or Codex, paste the failed localhost URL back here.</p></header><ul class="roster">`)
	for _, p := range providers {
		creds := byProvider[p.id]
		lamp := "lamp-off"
		status := "Not connected"
		if len(creds) > 0 {
			lamp = "lamp-on"
			labels := make([]string, 0, len(creds))
			for _, c := range creds {
				labels = append(labels, c.Label)
			}
			status = strings.Join(labels, " · ")
		}
		fmt.Fprintf(&b, `<li class="roster-row"><div class="roster-id"><span class="lamp %s" aria-hidden="true"></span><div class="roster-copy"><strong>%s</strong><span class="meta">%s</span></div></div><div class="actions">`, lamp, esc(p.label), esc(status))
		for _, c := range creds {
			fmt.Fprintf(&b, `<form method="post" action="/credentials/%s/delete" data-confirm="Disconnect %s?"><button class="btn btn-ghost btn-danger" type="submit">Disconnect</button></form>`, attr(c.ID), attr(c.Label))
		}
		label := "Login"
		if len(creds) > 0 {
			label = "Add"
		}
		kind := "btn-primary"
		if len(creds) > 0 {
			kind = "btn-ghost"
		}
		fmt.Fprintf(&b, `<form method="post" action="/login/%s"><button class="btn %s" type="submit">%s</button></form>`, attr(string(p.id)), kind, label)
		if p.id == domain.ProviderCursor {
			b.WriteString(`<form method="post" action="/login/cursor?mode=secret"><button class="btn btn-ghost" type="submit">Paste key</button></form>`)
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
	b.WriteString(`<section class="drawer keys-drawer"><header class="drawer-h"><h2>Keys</h2><p class="lede">Hand a <code>sk-fob-…</code> to Cursor, Claude Code, OpenCode.</p></header>`)
	if len(live) == 0 {
		b.WriteString(`<p class="empty">No keys yet. Mint one and paste it into a tool as the OpenAI API key.</p>`)
	} else {
		b.WriteString(`<ul class="fobs">`)
		for _, k := range live {
			scope := "all"
			if len(k.Providers) > 0 {
				parts := make([]string, len(k.Providers))
				for i, p := range k.Providers {
					parts[i] = string(p)
				}
				scope = strings.Join(parts, " ")
			}
			fmt.Fprintf(&b, `<li class="fob-tag"><span class="fob-hole" aria-hidden="true"></span><div class="fob-copy"><strong>%s</strong><span class="mono">%s…</span> <span class="pill">%s</span></div><form method="post" action="/keys/%s/revoke" data-confirm="Revoke %s? Tools using it will 401."><button class="btn btn-ghost btn-danger" type="submit">Revoke</button></form></li>`,
				esc(k.Name), esc(k.Prefix), esc(scope), attr(k.ID), attr(k.Name))
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`<form class="stack mint" id="mint" method="post" action="/keys">
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
	b.WriteString(`<section class="drawer meter-board"><header class="drawer-h"><h2>Meter</h2><p class="lede">API-equivalent $. Not your subscription bill.</p></header><div class="dials">`)
	fmt.Fprintf(&b, `<div class="dial"><span class="dial-value">%s</span><span class="dial-label">today</span></div>`, fmtUSD(usage.Today.USD))
	fmt.Fprintf(&b, `<div class="dial"><span class="dial-value">%s</span><span class="dial-label">7 days</span></div>`, fmtUSD(usage.D7.USD))
	fmt.Fprintf(&b, `<div class="dial"><span class="dial-value">%s</span><span class="dial-label">tokens today</span></div>`, fmtInt(usage.Today.PromptTokens+usage.Today.CompletionTokens))
	fmt.Fprintf(&b, `<div class="dial"><span class="dial-value">%s</span><span class="dial-label">requests today</span></div>`, fmtInt(usage.Today.Requests))
	b.WriteString(`</div>`)
	if len(usage.ByProvider) == 0 {
		b.WriteString(`<p class="empty">No traffic yet. Point a tool at <code>/v1</code>.</p>`)
	} else {
		max := 0.0
		for _, r := range usage.ByProvider {
			if r.USD > max {
				max = r.USD
			}
		}
		b.WriteString(`<div class="bars">`)
		for _, r := range usage.ByProvider {
			scale := 0.0
			if max > 0 {
				scale = r.USD / max
			}
			fmt.Fprintf(&b, `<div class="bar-row"><span class="bar-label">%s</span><span class="bar-track"><span class="bar-fill" style="transform:scaleX(%.4f)"></span></span><span class="bar-val">%s</span></div>`, esc(prettyKey(r.Key)), scale, fmtUSD(r.USD))
		}
		b.WriteString(`</div><table class="table"><thead><tr><th>Model</th><th>$</th></tr></thead><tbody>`)
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

func cursorSettingsCard(settings SettingsProps) string {
	prefix := ""
	if settings.CursorPrefix {
		prefix = "checked"
	}
	grok := ""
	if settings.GrokFailover {
		grok = "checked"
	}
	return fmt.Sprintf(`<section class="drawer settings-drawer">
      <header class="drawer-h">
        <h2>Cursor</h2>
        <p class="lede">Prefix listed Cursor models with <code>cursor/</code>. Grok failover maps <code>grok-4.5</code> onto Cursor Grok when the Grok sub is exhausted.</p>
      </header>
      <form method="post" action="/settings/cursor" class="stack" data-ajax>
        <label class="switch">
          <input type="checkbox" name="prefix" value="1" %s />
          <span class="track" aria-hidden="true"></span>
          <span class="switch-copy">
            <strong>Prefix Cursor models</strong>
            <span>List them as cursor/…</span>
          </span>
        </label>
        <label class="switch">
          <input type="checkbox" name="grok_failover" value="1" %s />
          <span class="track" aria-hidden="true"></span>
          <span class="switch-copy">
            <strong>Grok ↔ Cursor failover</strong>
            <span>Fall through when Grok is exhausted</span>
          </span>
        </label>
        <button class="btn" type="submit">Save</button>
      </form>
    </section>`, prefix, grok)
}

func gate(title, lede, err, form string) string {
	var b strings.Builder
	b.WriteString(`<section class="gate drawer"><h1>`)
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

func prettyKey(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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
