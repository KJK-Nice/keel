// Join-request prompts: when someone opens a share link, the host sees
// "<name> wants to watch <session>" with Allow / Deny. Nothing is sent to
// the joiner until the host answers (internal/share/hub.go).
//
// One prompt per (session, peer); the session WebSocket can deliver the
// same request to several panes or windows, and every copy is removed when
// the hub reports it resolved.

export function createJoinPrompts({ doc = document, answer, onError }) {
  const prompts = new Map(); // `${sessionId}\n${peerId}` -> element
  let stack = null;

  const keyOf = (sessionId, peerId) => `${sessionId}\n${peerId}`;

  function container() {
    if (stack?.isConnected) return stack;
    stack = doc.createElement("div");
    stack.className = "join-prompts";
    stack.setAttribute("role", "region");
    stack.setAttribute("aria-label", "Join requests");
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
    el.className = "join-prompt";
    el.setAttribute("role", "alertdialog");

    const text = doc.createElement("p");
    text.className = "join-prompt-text";
    const who = doc.createElement("b");
    who.textContent = name || "Someone";
    text.append(who, ` wants to watch ${title || "this session"}.`);

    const note = doc.createElement("p");
    note.className = "join-prompt-note";
    note.textContent = "The name is whatever they typed. Allow only if you're expecting them; they'll see output from when you started sharing.";

    const actions = doc.createElement("div");
    actions.className = "join-prompt-actions";
    const deny = doc.createElement("button");
    deny.type = "button";
    deny.className = "btn btn-ghost";
    deny.textContent = "Deny";
    const allow = doc.createElement("button");
    allow.type = "button";
    allow.className = "btn btn-primary";
    allow.textContent = "Allow";
    actions.append(deny, allow);

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

    el.append(text, note, actions);
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
