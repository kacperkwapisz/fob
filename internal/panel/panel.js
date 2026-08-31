document.addEventListener("submit", (e) => {
  const form = e.target
  if (!(form instanceof HTMLFormElement)) return
  const confirmMsg = form.getAttribute("data-confirm")
  if (confirmMsg && !window.confirm(confirmMsg)) {
    e.preventDefault()
    return
  }
  if (form.id !== "mint") return
  e.preventDefault()
  const name = String(new FormData(form).get("name") ?? "").trim() || "tool"
  const btn = form.querySelector("button[type=submit]")
  if (btn) btn.disabled = true
  fetch("/api/panel/keys", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name }),
  })
    .then((res) => {
      if (!res.ok) throw new Error("mint failed")
      return res.json()
    })
    .then((body) => {
      const slot = document.querySelector("#minted")
      const secret = slot?.querySelector(".secret")
      if (slot && secret) {
        secret.textContent = body.secret
        slot.hidden = false
      }
      form.reset()
    })
    .finally(() => {
      if (btn) btn.disabled = false
    })
})

document.addEventListener("click", (e) => {
  const t = e.target
  if (!(t instanceof HTMLElement)) return
  if (t.hasAttribute("data-copy")) {
    const root = t.closest("#minted") ?? document
    const el = root.querySelector(".secret")
    if (!el) return
    navigator.clipboard.writeText(el.textContent.trim()).then(() => {
      t.textContent = "Copied"
    })
    return
  }
  if (t.hasAttribute("data-dismiss")) {
    location.reload()
    return
  }
  if (t.id === "sub-load") {
    loadSub(t)
  }
})

function loadSub(btn) {
  const body = document.querySelector("#sub-body")
  if (!body) return
  btn.disabled = true
  body.innerHTML = `<p class="empty">Loading…</p>`
  fetch("/api/panel/sub")
    .then((res) => {
      if (!res.ok) throw new Error("sub failed")
      return res.json()
    })
    .then((data) => {
      const list = Array.isArray(data.credentials) ? data.credentials : []
      if (list.length === 0) {
        body.innerHTML = `<p class="empty">No subscription windows.</p>`
        return
      }
      body.innerHTML = list.map(renderSubCred).join("")
      btn.textContent = "Reload"
    })
    .catch(() => {
      body.innerHTML = `<p class="empty">Could not load sub.</p>`
    })
    .finally(() => {
      btn.disabled = false
    })
}

function renderSubCred(c) {
  const title = escapeHTML(c.label || c.provider || "login")
  const plan = c.plan ? `<span class="pill">${escapeHTML(c.plan)}</span>` : ""
  if (!c.ok) {
    return `<div class="sub-cred"><div class="sub-head"><strong>${title}</strong>${plan}</div><p class="empty">${escapeHTML(c.error || "unavailable")}</p></div>`
  }
  const note = c.note ? `<p class="note" style="margin:0 0 .5rem">${escapeHTML(c.note)}</p>` : ""
  const rows = (c.windows || []).map(renderSubWindow).join("")
  const empty = rows ? rows : `<p class="empty">No windows.</p>`
  return `<div class="sub-cred"><div class="sub-head"><strong>${title}</strong>${plan}</div>${note}${empty}</div>`
}

function renderSubWindow(w) {
  const used = typeof w.used_percent === "number" ? Math.max(0, Math.min(100, w.used_percent)) : null
  const remain = used === null ? null : Math.max(0, 100 - used)
  const pct = remain === null ? "—" : `${Math.round(remain)}%`
  const reset = w.resets_at ? relativeReset(w.resets_at) : ""
  const detail = w.detail ? ` · ${escapeHTML(w.detail)}` : ""
  const width = remain === null ? 0 : remain
  return `<div class="sub-row"><div class="sub-row-meta"><span>${escapeHTML(w.label || w.id)}</span><span><b>${pct}</b>${reset}${detail}</span></div><div class="sub-bar"><span style="width:${width}%"></span></div></div>`
}

function relativeReset(ms) {
  const n = Number(ms)
  if (!Number.isFinite(n) || n <= 0) return ""
  const delta = n - Date.now()
  if (delta <= 0) return " · now"
  const min = Math.round(delta / 60000)
  if (min < 60) return ` · ${min}m`
  const hr = Math.round(min / 60)
  if (hr < 48) return ` · ${hr}h`
  return ` · ${Math.round(hr / 24)}d`
}

function escapeHTML(s) {
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
}
