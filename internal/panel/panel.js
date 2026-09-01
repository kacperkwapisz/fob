document.addEventListener("alpine:init", () => {
  Alpine.data("fob", () => ({
    toast: "",
    ping(msg) {
      this.toast = msg
      clearTimeout(this._t)
      this._t = setTimeout(() => {
        this.toast = ""
      }, 2200)
    },
  }))
})

function toast(msg) {
  const root = document.querySelector("[x-data]")
  if (root && root._x_dataStack && root._x_dataStack[0] && typeof root._x_dataStack[0].ping === "function") {
    root._x_dataStack[0].ping(msg)
    return
  }
}

function openConfirm(message, form) {
  const dlg = document.getElementById("confirm-dlg")
  if (!dlg || typeof dlg.showModal !== "function") {
    return window.confirm(message)
  }
  const msg = dlg.querySelector("#confirm-msg")
  if (msg) msg.textContent = message
  return new Promise((resolve) => {
    const ok = dlg.querySelector("[data-confirm-ok]")
    const cancel = dlg.querySelector("[data-confirm-cancel]")
    const done = (value) => {
      ok?.removeEventListener("click", onOk)
      cancel?.removeEventListener("click", onCancel)
      dlg.removeEventListener("close", onClose)
      if (dlg.open) dlg.close()
      resolve(value)
    }
    const onOk = () => done(true)
    const onCancel = () => done(false)
    const onClose = () => done(false)
    ok?.addEventListener("click", onOk, { once: true })
    cancel?.addEventListener("click", onCancel, { once: true })
    dlg.addEventListener("close", onClose, { once: true })
    dlg.showModal()
    ok?.focus()
  }).then((ok) => {
    if (ok) {
      form.dataset.confirmed = "1"
      form.requestSubmit()
    }
    return ok
  })
}

document.addEventListener("submit", (e) => {
  const form = e.target
  if (!(form instanceof HTMLFormElement)) return
  const confirmMsg = form.getAttribute("data-confirm")
  if (confirmMsg && form.dataset.confirmed !== "1") {
    e.preventDefault()
    openConfirm(confirmMsg, form)
    return
  }
  if (form.hasAttribute("data-ajax")) {
    e.preventDefault()
    const btn = form.querySelector("button[type=submit]")
    if (btn) btn.disabled = true
    fetch(form.action, {
      method: "POST",
      body: new URLSearchParams(new FormData(form)),
      redirect: "follow",
    })
      .then((res) => {
        if (!res.ok) throw new Error("save failed")
        toast("Saved")
      })
      .catch(() => toast("Could not save"))
      .finally(() => {
        if (btn) btn.disabled = false
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
        if (typeof slot.showModal === "function") slot.showModal()
        else slot.hidden = false
      }
      form.reset()
    })
    .catch(() => toast("Could not mint key"))
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
      toast("Copied")
    })
    return
  }
  if (t.hasAttribute("data-dismiss")) {
    location.reload()
    return
  }
  const code = t.closest(".user-code")
  if (code instanceof HTMLElement && code.dataset.code) {
    navigator.clipboard.writeText(code.dataset.code).then(() => toast("Copied"))
  }
})
