// Toasts with actions (k-toast): "<name> asks for control" with Hand over /
// Keep driving, and "Typing is off while you follow" with Ask for control.
// One stack per call site; one toast per key, so a repeated event updates
// nothing and a second key stacks below. Text is set as text, never markup.
//
// Actions only call what the caller passes in; this module sends nothing.

// className names the stack; a stack with that class already in the page
// (the join prompts' one, say) is shared rather than drawn over.
export function createActionToasts({ doc = document, className = "", label = "Notifications" }) {
  const toasts = new Map(); // key -> { el, timer }
  let stack = null;

  function container() {
    if (stack?.isConnected) return stack;
    const sel = `.k-toasts${className.split(/\s+/).filter(Boolean).map((c) => `.${c}`).join("")}`;
    stack = doc.querySelector?.(sel) || null;
    if (stack) return stack;
    stack = doc.createElement("div");
    stack.className = `k-toasts ${className}`.trim();
    stack.setAttribute("role", "region");
    stack.setAttribute("aria-label", label);
    doc.body.appendChild(stack);
    return stack;
  }

  function dismiss(key) {
    const t = toasts.get(key);
    toasts.delete(key);
    if (!t) return;
    clearTimeout(t.timer);
    t.el.remove();
  }

  // show({ key, tone, icon, title, desc, actions: [{ label, primary, run }],
  // timeout, role }): icon is trusted SVG markup from lib/share.js glyph().
  // Returns false when a toast with that key is already showing.
  function show({ key, tone = "", icon = "", title, desc = "", actions = [], timeout = 0, role = "status" }) {
    if (toasts.has(key)) {
      const t = toasts.get(key);
      if (timeout) {
        clearTimeout(t.timer);
        t.timer = setTimeout(() => dismiss(key), timeout);
      }
      return false;
    }
    const el = doc.createElement("div");
    el.className = `k-toast ${tone ? `k-toast--${tone}` : ""}`.trim();
    el.setAttribute("role", role);
    const ico = doc.createElement("span");
    ico.className = "k-i";
    ico.setAttribute("aria-hidden", "true");
    ico.innerHTML = icon;
    const head = doc.createElement("div");
    head.className = "k-toast-title";
    head.textContent = title;
    el.append(ico, head);
    if (desc) {
      const d = doc.createElement("div");
      d.className = "k-toast-desc";
      d.textContent = desc;
      el.append(d);
    }
    if (actions.length) {
      const row = doc.createElement("div");
      row.className = "k-toast-actions";
      for (const a of actions) {
        const b = doc.createElement("button");
        b.type = "button";
        b.className = a.primary ? "k-btn k-btn--sm" : "k-btn k-btn--sm k-btn--ghost";
        b.textContent = a.label;
        b.addEventListener("click", () => {
          dismiss(key);
          a.run?.();
        });
        row.append(b);
      }
      el.append(row);
    }
    container().appendChild(el);
    const t = { el, timer: 0 };
    if (timeout) t.timer = setTimeout(() => dismiss(key), timeout);
    toasts.set(key, t);
    return true;
  }

  // dismissWhere drops every toast whose key starts with prefix, except keep.
  function dismissWhere(prefix, keep = "") {
    for (const key of [...toasts.keys()]) if (key.startsWith(prefix) && key !== keep) dismiss(key);
  }

  return { show, dismiss, dismissWhere, has: (key) => toasts.has(key) };
}
