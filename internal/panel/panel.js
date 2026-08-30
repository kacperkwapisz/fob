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
  }
})
