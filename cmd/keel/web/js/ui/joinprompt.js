// Join-request prompts: when someone opens a share link, the host sees
// "<name> wants to watch <session>" with Allow / Deny. Nothing is sent to
// the joiner until the host answers (internal/share/hub.go).
//
// One prompt per (session, peer); the session WebSocket can deliver the
// same request to several panes or windows, and every copy is removed when
// the hub reports it resolved. Shown as a k-toast (haze); the redesign
// changed only the look, not what Allow and Deny send.

import { glyph } from "../lib/share.js";

export function createJoinPrompts({ doc = document, answer, onError }) {
  const prompts = new Map(); // `${sessionId}\n${peerId}` -> element
  let stack = null;

  const keyOf = (sessionId, peerId) => `${sessionId}\n${peerId}`;

  function container() {
    if (stack?.isConnected) return stack;
    // Shared with the control request toasts (ui/actiontoast.js).
    stack = doc.querySelector?.(".k-toasts.join-prompts") || null;
    if (stack) return stack;
    stack = doc.createElement("div");
    stack.className = "k-toasts join-prompts";
    stack.setAttribute("role", "region");
    stack.setAttribute("aria-label", "Share requests");
    doc.body.appendChild(stack);
    return stack;
  }

  function remove(key) {
    const el = prompts.get(key);
    prompts.delete(key);
    el?.remove();
  }

  // request shows a prompt for a joiner; safe to call more than once.
  function request({ sessionId, peerId, name, title }) {
    if (!sessionId || !peerId) return;
    const key = keyOf(sessionId, peerId);
    if (prompts.has(key)) return;
    const el = doc.createElement("div");
    el.className = "k-toast k-toast--haze join-prompt";
    el.setAttribute("role", "alertdialog");

    const icon = doc.createElement("span");
    icon.className = "k-i";
    icon.setAttribute("aria-hidden", "true");
    icon.innerHTML = glyph("eye");

    const text = doc.createElement("div");
    text.className = "k-toast-title";
    const who = doc.createElement("b");
    who.textContent = name || "Someone";
    text.append(who, ` wants to watch ${title || "this session"}.`);

    const note = doc.createElement("div");
    note.className = "k-toast-desc";
    note.textContent = "The name is whatever they typed. Allow only if you're expecting them; they'll see output from when you started sharing.";

    const actions = doc.createElement("div");
    actions.className = "k-toast-actions";
    const allow = doc.createElement("button");
    allow.type = "button";
    allow.className = "k-btn k-btn--sm";
    allow.textContent = "Allow";
    const deny = doc.createElement("button");
    deny.type = "button";
    deny.className = "k-btn k-btn--sm k-btn--ghost";
    deny.textContent = "Deny";
    actions.append(allow, deny);

    const reply = async (ok) => {
      allow.disabled = true;
      deny.disabled = true;
      try {
        await answer(sessionId, peerId, ok);
        remove(key);
      } catch (err) {
        remove(key);
        onError?.(err);
      }
    };
    allow.addEventListener("click", () => reply(true));
    deny.addEventListener("click", () => reply(false));

    el.append(icon, text, note, actions);
    container().appendChild(el);
    prompts.set(key, el);
  }

  // resolve drops the prompt once the hub has answered it (here, in another
  // window, by timeout, or because the joiner left).
  function resolve({ sessionId, peerId }) {
    remove(keyOf(sessionId, peerId));
  }

  // forget drops every prompt for a session (share stopped, tab closed).
  function forget(sessionId) {
    for (const key of [...prompts.keys()]) {
      if (key.startsWith(`${sessionId}\n`)) remove(key);
    }
  }

  return { request, resolve, forget };
}
