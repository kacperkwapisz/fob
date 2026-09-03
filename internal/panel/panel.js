document.documentElement.classList.add("js")

// 8×8 Bayer. Canvas fills use density 0.5 (series) and 0.55 (bars).
// CSS wells mask the same matrix at threshold 20/64 via --dither-bayer.
const BAYER = [
  [0, 32, 8, 40, 2, 34, 10, 42],
  [48, 16, 56, 24, 50, 18, 58, 26],
  [12, 44, 4, 36, 14, 46, 6, 38],
  [60, 28, 52, 20, 62, 30, 54, 22],
  [3, 35, 11, 43, 1, 33, 9, 41],
  [51, 19, 59, 27, 49, 17, 57, 25],
  [15, 47, 7, 39, 13, 45, 5, 37],
  [63, 31, 55, 23, 61, 29, 53, 21],
]

const PROVIDER_COLORS = {
  claude: "--p-claude",
  codex: "--p-codex",
  grok: "--p-grok",
  cursor: "--p-cursor",
}

document.addEventListener("alpine:init", () => {
  if (!window.Alpine) return
  Alpine.data("fob", () => ({ copied: false }))
})

document.addEventListener("submit", (e) => {
  const form = e.target
  if (!(form instanceof HTMLFormElement)) return
  const confirmMsg = form.getAttribute("data-confirm")
  if (confirmMsg) {
    e.preventDefault()
    askConfirm(confirmMsg).then((ok) => {
      if (!ok) return
      form.removeAttribute("data-confirm")
      form.requestSubmit()
    })
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
        slot.querySelector("[data-copy]")?.focus()
      }
      window.dispatchEvent(new CustomEvent("fob-minted", { detail: body }))
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
  if (t.hasAttribute("data-copy-text")) {
    navigator.clipboard.writeText(t.getAttribute("data-copy-text") || t.textContent.trim()).then(() => {
      const prev = t.textContent
      t.textContent = "Copied"
      setTimeout(() => {
        t.textContent = prev
      }, 1200)
    })
    return
  }
  if (t.hasAttribute("data-dismiss")) {
    location.reload()
    return
  }
  const load = t.closest("#sub-load")
  if (load instanceof HTMLButtonElement) {
    loadSub(load)
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
  const bits = []
  if (remain !== null) bits.push(`<b>${Math.round(remain)}%</b>`)
  if (w.resets_at) bits.push(relativeReset(w.resets_at).replace(/^ · /, ""))
  if (w.detail) bits.push(escapeHTML(w.detail))
  const bar = remain === null ? "" : `<div class="sub-bar"><span style="width:${remain}%"></span></div>`
  return `<div class="sub-row"><div class="sub-row-meta"><span>${escapeHTML(w.label || w.id)}</span><span>${bits.join(" · ")}</span></div>${bar}</div>`
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

document.querySelectorAll("form[data-autosave]").forEach((form) => {
  form.addEventListener("change", () => {
    fetch(form.action, {
      method: "POST",
      headers: { "content-type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams(new FormData(form)).toString(),
      credentials: "same-origin",
      redirect: "manual",
    })
  })
})

function askConfirm(message) {
  const dlg = document.getElementById("confirm-dialog")
  const body = document.getElementById("confirm-body")
  if (!(dlg instanceof HTMLDialogElement) || !body) {
    return Promise.resolve(window.confirm(message))
  }
  body.textContent = message
  dlg.returnValue = ""
  dlg.showModal()
  return new Promise((resolve) => {
    const onClose = () => {
      dlg.removeEventListener("close", onClose)
      resolve(dlg.returnValue === "ok")
    }
    dlg.addEventListener("close", onClose)
  })
}

function cssColor(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

function parseHex(color) {
  const hex = color.replace("#", "").trim()
  if (hex.length !== 6) return [230, 237, 242]
  return [parseInt(hex.slice(0, 2), 16), parseInt(hex.slice(2, 4), 16), parseInt(hex.slice(4, 6), 16)]
}

function makeDither(width, height, rgb, variant, density) {
  const canvas = document.createElement("canvas")
  canvas.width = width
  canvas.height = height
  const ctx = canvas.getContext("2d")
  const img = ctx.createImageData(width, height)
  const [r, g, b] = rgb
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      let on = false
      if (variant === "hatch") {
        on = (x + y) % 3 === 0
      } else if (variant === "dotted") {
        on = x % 3 === 0 && y % 3 === 0
      } else {
        on = BAYER[y & 7][x & 7] < density * 64
      }
      if (!on) continue
      const i = (y * width + x) * 4
      img.data[i] = r
      img.data[i + 1] = g
      img.data[i + 2] = b
      img.data[i + 3] = 255
    }
  }
  ctx.putImageData(img, 0, 0)
  return canvas
}

function sizeCanvas(canvas) {
  const dpr = Math.max(1, window.devicePixelRatio || 1)
  const rect = canvas.getBoundingClientRect()
  const w = Math.max(1, Math.round(rect.width * dpr))
  const h = Math.max(1, Math.round(rect.height * dpr))
  if (canvas.width !== w || canvas.height !== h) {
    canvas.width = w
    canvas.height = h
  }
  return { ctx: canvas.getContext("2d"), w, h, dpr, cssW: rect.width, cssH: rect.height }
}

function fmtMoney(n) {
  return "$" + (Number(n) || 0).toFixed(2)
}

function fmtTokens(n) {
  const v = Number(n) || 0
  if (v >= 1_000_000_000) return (v / 1_000_000_000).toFixed(1).replace(/\.0$/, "") + "B"
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1).replace(/\.0$/, "") + "M"
  if (v >= 10_000) return Math.round(v / 1_000) + "k"
  if (v >= 1_000) return (v / 1_000).toFixed(1).replace(/\.0$/, "") + "k"
  return String(Math.round(v))
}

function labelProvider(key) {
  if (!key) return ""
  return key.charAt(0).toUpperCase() + key.slice(1)
}

function shortDay(iso) {
  if (!iso) return ""
  const d = new Date(iso + "T00:00:00Z")
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })
}

function drawSeries(canvas, daily) {
  const { ctx, w, h, dpr } = sizeCanvas(canvas)
  ctx.clearRect(0, 0, w, h)
  const pad = { t: 18 * dpr, r: 14 * dpr, b: 28 * dpr, l: 58 * dpr }
  const points = Array.isArray(daily) && daily.length ? daily : [{ day: "", usd: 0 }]
  const max = Math.max(0.01, ...points.map((p) => Number(p.usd) || 0))
  const innerW = w - pad.l - pad.r
  const innerH = h - pad.t - pad.b
  const accent = parseHex(cssColor("--accent") || "#5eead4")
  const mute = cssColor("--ink-mute") || "#8e99a6"
  const grid = cssColor("--line") || "#2a343e"

  ctx.strokeStyle = grid
  ctx.lineWidth = dpr
  for (let i = 0; i < 3; i++) {
    const y = pad.t + (innerH * i) / 2
    ctx.beginPath()
    ctx.moveTo(pad.l, y)
    ctx.lineTo(w - pad.r, y)
    ctx.stroke()
  }

  const xy = points.map((p, i) => {
    const x = pad.l + (points.length === 1 ? innerW / 2 : (innerW * i) / (points.length - 1))
    const y = pad.t + innerH - (Number(p.usd) / max) * innerH
    return { x, y, p }
  })

  const path = new Path2D()
  path.moveTo(xy[0].x, pad.t + innerH)
  xy.forEach((pt, i) => {
    if (i === 0) path.lineTo(pt.x, pt.y)
    else path.lineTo(pt.x, pt.y)
  })
  path.lineTo(xy[xy.length - 1].x, pad.t + innerH)
  path.closePath()

  ctx.save()
  ctx.clip(path)
  ctx.drawImage(makeDither(w, h, accent, "bayer", 0.5), 0, 0)
  ctx.restore()

  ctx.beginPath()
  xy.forEach((pt, i) => (i === 0 ? ctx.moveTo(pt.x, pt.y) : ctx.lineTo(pt.x, pt.y)))
  ctx.strokeStyle = cssColor("--accent") || "#5eead4"
  ctx.lineWidth = 1.5 * dpr
  ctx.stroke()

  const last = xy[xy.length - 1]
  ctx.fillStyle = cssColor("--accent") || "#5eead4"
  ctx.fillRect(last.x - 2 * dpr, last.y - 2 * dpr, 4 * dpr, 4 * dpr)

  ctx.fillStyle = mute
  ctx.font = `${11 * dpr}px "IBM Plex Mono", ui-monospace, monospace`
  ctx.textBaseline = "top"
  ctx.textAlign = "center"
  const labelEvery = points.length > 7 ? 2 : 1
  points.forEach((p, i) => {
    if (i % labelEvery !== 0 && i !== points.length - 1) return
    ctx.textAlign = i === 0 ? "left" : i === points.length - 1 ? "right" : "center"
    ctx.fillText(shortDay(p.day), xy[i].x, h - 18 * dpr)
  })
  ctx.textAlign = "right"
  ctx.textBaseline = "middle"
  ctx.fillText(fmtMoney(max), pad.l - 8 * dpr, pad.t)
  ctx.fillText("$0.00", pad.l - 8 * dpr, pad.t + innerH)

  return { xy, pad, dpr }
}

function drawProviders(canvas, rows) {
  const { ctx, w, h, dpr } = sizeCanvas(canvas)
  ctx.clearRect(0, 0, w, h)
  const data = Array.isArray(rows) ? rows.filter((r) => r && r.key) : []
  const mute = cssColor("--ink-mute") || "#8e99a6"
  if (!data.length) {
    ctx.fillStyle = mute
    ctx.font = `${12 * dpr}px "IBM Plex Sans", sans-serif`
    ctx.textAlign = "center"
    ctx.textBaseline = "middle"
    ctx.fillText("No traffic yet", w / 2, h / 2)
    return { rows: [] }
  }
  const pad = { t: 10 * dpr, r: 72 * dpr, b: 10 * dpr, l: 72 * dpr }
  const innerH = h - pad.t - pad.b
  const rowH = innerH / data.length
  const max = Math.max(0.01, ...data.map((r) => Number(r.usd) || 0))
  const variants = ["bayer", "hatch", "dotted", "bayer"]
  const layout = []
  data.forEach((row, i) => {
    const y = pad.t + i * rowH
    const trackY = y + rowH * 0.28
    const trackH = rowH * 0.44
    const barW = Math.max(2 * dpr, ((Number(row.usd) || 0) / max) * (w - pad.l - pad.r))
    const colorVar = PROVIDER_COLORS[row.key] || "--accent"
    const rgb = parseHex(cssColor(colorVar) || cssColor("--accent"))
    ctx.fillStyle = "rgba(255,255,255,0.04)"
    ctx.fillRect(pad.l, trackY, w - pad.l - pad.r, trackH)
    ctx.save()
    ctx.beginPath()
    ctx.rect(pad.l, trackY, barW, trackH)
    ctx.clip()
    ctx.drawImage(makeDither(w, h, rgb, variants[i % variants.length], 0.55), 0, 0)
    ctx.restore()
    ctx.fillStyle = mute
    ctx.font = `${11 * dpr}px "IBM Plex Sans", sans-serif`
    ctx.textAlign = "right"
    ctx.textBaseline = "middle"
    ctx.fillText(labelProvider(row.key), pad.l - 8 * dpr, y + rowH / 2)
    ctx.textAlign = "left"
    ctx.font = `${11 * dpr}px "IBM Plex Mono", ui-monospace, monospace`
    ctx.fillText(fmtMoney(row.usd), pad.l + barW + 8 * dpr, y + rowH / 2)
    layout.push({ row, x: pad.l, y: trackY, w: barW, h: trackH })
  })
  return { rows: layout, dpr }
}

function tokensOf(p) {
  return (Number(p?.promptTokens) || 0) + (Number(p?.completionTokens) || 0)
}

function drawTrends(canvas, daily) {
  const { ctx, w, h, dpr } = sizeCanvas(canvas)
  ctx.clearRect(0, 0, w, h)
  const pad = { t: 18 * dpr, r: 14 * dpr, b: 28 * dpr, l: 52 * dpr }
  const points = Array.isArray(daily) && daily.length ? daily : [{ day: "", promptTokens: 0, completionTokens: 0 }]
  const max = Math.max(1, ...points.map(tokensOf))
  const innerW = w - pad.l - pad.r
  const innerH = h - pad.t - pad.b
  const mute = cssColor("--ink-mute") || "#8e99a6"
  const grid = cssColor("--line") || "#2a343e"
  const promptRGB = parseHex(mute)
  const completionRGB = parseHex(cssColor("--accent") || "#5eead4")
  const gap = Math.max(dpr, innerW / points.length * 0.22)
  const barW = Math.max(2 * dpr, innerW / points.length - gap)

  ctx.strokeStyle = grid
  ctx.lineWidth = dpr
  for (let i = 0; i < 3; i++) {
    const y = pad.t + (innerH * i) / 2
    ctx.beginPath()
    ctx.moveTo(pad.l, y)
    ctx.lineTo(w - pad.r, y)
    ctx.stroke()
  }

  const layout = []
  points.forEach((p, i) => {
    const prompt = Number(p.promptTokens) || 0
    const completion = Number(p.completionTokens) || 0
    const total = prompt + completion
    const x = pad.l + (innerW / points.length) * i + gap / 2
    const barH = total <= 0 ? 0 : Math.max(2 * dpr, (total / max) * innerH)
    const y = pad.t + innerH - barH
    const promptH = total <= 0 ? 0 : (prompt / total) * barH
    const completionH = barH - promptH
    if (promptH > 0) {
      ctx.save()
      ctx.beginPath()
      ctx.rect(x, y + completionH, barW, promptH)
      ctx.clip()
      ctx.drawImage(makeDither(w, h, promptRGB, "hatch", 0.45), 0, 0)
      ctx.restore()
    }
    if (completionH > 0) {
      ctx.save()
      ctx.beginPath()
      ctx.rect(x, y, barW, completionH)
      ctx.clip()
      ctx.drawImage(makeDither(w, h, completionRGB, "bayer", 0.55), 0, 0)
      ctx.restore()
    }
    layout.push({ x, y, w: barW, h: Math.max(barH, 8 * dpr), p })
  })

  ctx.fillStyle = mute
  ctx.font = `${11 * dpr}px "IBM Plex Mono", ui-monospace, monospace`
  ctx.textBaseline = "top"
  const labelEvery = points.length > 8 ? 2 : 1
  points.forEach((p, i) => {
    if (i % labelEvery !== 0 && i !== points.length - 1) return
    const x = pad.l + (innerW / points.length) * i + gap / 2 + barW / 2
    ctx.textAlign = i === 0 ? "left" : i === points.length - 1 ? "right" : "center"
    ctx.fillText(shortDay(p.day), i === 0 ? pad.l : i === points.length - 1 ? w - pad.r : x, h - 18 * dpr)
  })
  ctx.textAlign = "right"
  ctx.textBaseline = "middle"
  ctx.fillText(fmtTokens(max), pad.l - 8 * dpr, pad.t)
  ctx.fillText("0", pad.l - 8 * dpr, pad.t + innerH)

  return { bars: layout, dpr }
}

function bindTip(canvas, tip, locate) {
  if (!canvas || !tip) return
  const onMove = (ev) => {
    const rect = canvas.getBoundingClientRect()
    const x = ev.clientX - rect.left
    const y = ev.clientY - rect.top
    const hit = locate(x, y, rect)
    if (!hit) {
      tip.hidden = true
      return
    }
    tip.hidden = false
    tip.textContent = hit.label
    const tx = Math.min(rect.width - 12, Math.max(12, x + 12))
    const ty = Math.min(rect.height - 12, Math.max(12, y - 28))
    tip.style.transform = `translate(${tx}px, ${ty}px)`
  }
  canvas.addEventListener("pointermove", onMove)
  canvas.addEventListener("pointerleave", () => {
    tip.hidden = true
  })
}

function parseJSON(id, fallback) {
  const node = document.getElementById(id)
  if (!node) return fallback
  try {
    return JSON.parse(node.textContent || "null") ?? fallback
  } catch {
    return fallback
  }
}

function bootCharts() {
  const data = parseJSON("meter-data", { daily: [], byProvider: [] })
  const trends = parseJSON("trends-data", [])
  const series = document.getElementById("chart-series")
  const providers = document.getElementById("chart-providers")
  const trendCanvas = document.getElementById("chart-trends")
  let seriesLayout = { xy: [] }
  let providerLayout = { rows: [] }
  let trendLayout = { bars: [] }
  const redraw = () => {
    if (series) seriesLayout = drawSeries(series, data.daily || []) || { xy: [] }
    if (providers) providerLayout = drawProviders(providers, data.byProvider || []) || { rows: [] }
    if (trendCanvas) trendLayout = drawTrends(trendCanvas, Array.isArray(trends) ? trends : []) || { bars: [] }
  }
  redraw()
  if (series) {
    bindTip(series, document.getElementById("chart-series-tip"), (x, y, rect) => {
      const xy = seriesLayout.xy || []
      if (!xy.length) return null
      const dpr = series.width / rect.width
      const px = x * dpr
      let best = xy[0]
      let dist = Infinity
      xy.forEach((pt) => {
        const d = Math.abs(pt.x - px)
        if (d < dist) {
          dist = d
          best = pt
        }
      })
      if (!best?.p) return null
      return { label: `${shortDay(best.p.day)}  ${fmtMoney(best.p.usd)}` }
    })
  }
  if (providers) {
    bindTip(providers, document.getElementById("chart-providers-tip"), (x, y, rect) => {
      const dpr = providers.width / rect.width
      const px = x * dpr
      const py = y * dpr
      const hit = (providerLayout.rows || []).find((r) => px >= r.x && px <= r.x + Math.max(r.w, 8) && py >= r.y && py <= r.y + r.h)
      if (!hit) return null
      return { label: `${hit.row.key}  ${fmtMoney(hit.row.usd)}` }
    })
  }
  if (trendCanvas) {
    bindTip(trendCanvas, document.getElementById("chart-trends-tip"), (x, y, rect) => {
      const dpr = trendCanvas.width / rect.width
      const px = x * dpr
      const py = y * dpr
      const hit = (trendLayout.bars || []).find((r) => px >= r.x && px <= r.x + r.w && py >= r.y && py <= r.y + r.h)
      if (!hit?.p) return null
      return {
        label: `${shortDay(hit.p.day)}  ${fmtTokens(tokensOf(hit.p))}  (${fmtTokens(hit.p.promptTokens)} / ${fmtTokens(hit.p.completionTokens)})`,
      }
    })
  }
  const ro = new ResizeObserver(redraw)
  if (series) ro.observe(series)
  if (providers) ro.observe(providers)
  if (trendCanvas) ro.observe(trendCanvas)
}

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", bootCharts)
} else {
  bootCharts()
}
