// The agent panel (docs/ui-redesign.md §5.4; shell in internal/ui/agent.templ).
//
// It asks keel serve (POST /api/agent/ask) about the panes in scope and
// shows the trace, the answer and command proposals. It never writes to a
// pane: Type into pane hands the proposal to term.js (deps.typeProposal),
// which opens the approve-before-type dialog and sends the approved bytes
// on the session socket, where keel serve checks them with the agent
// service before writing (§6.1, §6.5, §6.6, §6.7).

import { escapeHtml } from "../lib/html.js";
import * as A from "../lib/agent.js";
import { setText } from "../lib/dom.js";

const svg = (paths) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

const ICON = {
  ai: svg(
    '<path d="M13.427 8.08396C12.8007 5.84272 12.3226 4 11.0004 4C9.67824 4 9.19964 5.84274 8.5731 8.08399C8.31853 8.99465 8.19125 9.44998 7.82042 9.82077C7.4496 10.1916 6.99434 10.3188 6.08381 10.5732C3.84273 11.1995 2 11.6775 2 13C2 14.3225 3.84273 14.8005 6.08381 15.4268C6.99434 15.6812 7.4496 15.8084 7.82042 16.1792C8.19125 16.55 8.31853 17.0054 8.5731 17.916C9.19964 20.1573 9.67824 22 11.0004 22C12.3226 22 12.8007 20.1573 13.427 17.916C13.6815 17.0054 13.8087 16.5501 14.1795 16.1793C14.5504 15.8085 15.0056 15.6812 15.9162 15.4268C18.1573 14.8005 20 14.3225 20 13C20 11.6775 18.1573 11.1995 15.9162 10.5732C15.0056 10.3188 14.5504 10.1915 14.1795 9.8207C13.8087 9.44986 13.6815 8.99456 13.427 8.08396Z"/><path d="M19.5 2.9375V4.5M19.5 4.5V6.0625M19.5 4.5H18.25M19.5 4.5H20.75"/>',
  ),
  tick: svg('<path d="M5 14L8.5 17.5L19 6.5"/>'),
  skip: svg('<path d="M16 12H8"/><circle cx="12" cy="12" r="10"/>'),
  busy: svg(
    '<path d="M12 3V6"/><path d="M12 18V21"/><path d="M21 12L18 12"/><path d="M6 12L3 12"/><path d="M18.3635 5.63672L16.2422 7.75804"/><path d="M7.75804 16.2422L5.63672 18.3635"/><path d="M18.3635 18.3635L16.2422 16.2422"/><path d="M7.75804 7.75804L5.63672 5.63672"/>',
  ),
  x: svg('<path d="M18 6L6.00081 17.9992M17.9992 18L6 6.00085"/>'),
  at: svg(
    '<path d="M15.6 8.4V12.9C15.6 14.4 16.8 15.6 18.3 15.6C19.8 15.6 21 14.4 21 12.9V12C21 7 17 3 12 3C7 3 3 7 3 12C3 17 7 21 12 21C14 21 15.9 20.3 17.4 19.2M15.6 12C15.6 14 14 15.6 12 15.6C10 15.6 8.4 14 8.4 12C8.4 10 10 8.4 12 8.4C14 8.4 15.6 10 15.6 12Z"/>',
  ),
  cmd: svg(
    '<path d="M14 4H10C6.22876 4 4.34315 4 3.17157 5.17157C2 6.34315 2 8.22876 2 12C2 15.7712 2 17.6569 3.17157 18.8284C4.34315 20 6.22876 20 10 20H14C17.7712 20 19.6569 20 20.8284 18.8284C22 17.6569 22 15.7712 22 12C22 8.22876 22 6.34315 20.8284 5.17157C19.6569 4 17.7712 4 14 4Z"/><path d="M7 9L8.83981 10.5858C9.61327 11.2525 10 11.5858 10 12C10 12.4142 9.61327 12.7475 8.83981 13.4142L7 15"/><path d="M13 16H17"/>',
  ),
  edit: svg(
    '<path d="M16.4249 4.60509L17.4149 3.6151C18.2351 2.79497 19.5648 2.79497 20.3849 3.6151C21.205 4.43524 21.205 5.76493 20.3849 6.58507L19.3949 7.57506M16.4249 4.60509L9.76558 11.2644C9.25807 11.772 8.89804 12.4078 8.72397 13.1041L8 16L10.8959 15.276C11.5922 15.102 12.228 14.7419 12.7356 14.2344L19.3949 7.57506M16.4249 4.60509L19.3949 7.57506"/><path d="M18.9999 13.5C18.9999 16.7875 18.9999 18.4312 18.092 19.5376C17.9258 19.7401 17.7401 19.9258 17.5375 20.092C16.4312 21 14.7874 21 11.4999 21H11C7.22876 21 5.34316 21 4.17159 19.8284C3.00003 18.6569 3 16.7712 3 13V12.5C3 9.21252 3 7.56879 3.90794 6.46244C4.07417 6.2599 4.2599 6.07417 4.46244 5.90794C5.56879 5 7.21252 5 10.5 5"/>',
  ),
  copy: svg(
    '<path d="M7.5 14.5C7.5 11.2002 7.5 9.55025 8.52513 8.52513C9.55025 7.5 11.2002 7.5 14.5 7.5C17.7998 7.5 19.4497 7.5 20.4749 8.52513C21.5 9.55025 21.5 11.2002 21.5 14.5C21.5 17.7998 21.5 19.4497 20.4749 20.4749C19.4497 21.5 17.7998 21.5 14.5 21.5C11.2002 21.5 9.55025 21.5 8.52513 20.4749C7.5 19.4497 7.5 17.7998 7.5 14.5Z"/><path d="M7.5 16.5C6.10355 16.5 5.40533 16.5 4.84402 16.3036C3.83866 15.9518 3.0482 15.1613 2.69641 14.156C2.5 13.5947 2.5 12.8964 2.5 11.5V9.5C2.5 6.20017 2.5 4.55025 3.52513 3.52513C4.55025 2.5 6.20017 2.5 9.5 2.5H11.5C12.8964 2.5 13.5947 2.5 14.156 2.69641C15.1613 3.0482 15.9518 3.83866 16.3036 4.84402C16.5 5.40533 16.5 6.10355 16.5 7.5"/>',
  ),
  alert: svg(
    '<path d="M13.9248 21H10.0752C5.44476 21 3.12955 21 2.27636 19.4939C1.42317 17.9879 2.60736 15.9914 4.97574 11.9985L6.90057 8.75333C9.17559 4.91778 10.3131 3 12 3C13.6869 3 14.8244 4.91777 17.0994 8.75332L19.0243 11.9985C21.3926 15.9914 22.5768 17.9879 21.7236 19.4939C20.8704 21 18.5552 21 13.9248 21Z"/><path d="M12 9V13"/><path d="M12 16.75H12.01"/>',
  ),
  info: svg('<circle cx="12" cy="12" r="10"/><path d="M12 16V12"/><path d="M12 8.25H12.01"/>'),
  eye: svg(
    '<path d="M21.544 11.045C21.848 11.4713 22 11.6845 22 12C22 12.3155 21.848 12.5287 21.544 12.955C20.1779 14.8706 16.6892 19 12 19C7.31078 19 3.8221 14.8706 2.45604 12.955C2.15201 12.5287 2 12.3155 2 12C2 11.6845 2.15201 11.4713 2.45604 11.045C3.8221 9.12944 7.31078 5 12 5C16.6892 5 20.1779 9.12944 21.544 11.045Z"/><path d="M15 12C15 10.3431 13.6569 9 12 9C10.3431 9 9 10.3431 9 12C9 13.6569 10.3431 15 12 15C13.6569 15 15 13.6569 15 12Z"/>',
  ),
};

export const AGENT_ICON = ICON.ai;
export const AGENT_ALERT_ICON = ICON.alert;

// createAgentPanel wires the panel. deps:
//   api(path, opts)           keel serve JSON API (throws with .code, .data)
//   toast(msg, isError)
//   isMac
//   listPanes()               panes in this window (lib/agent.js shape)
//   typeProposal({proposal, conversationId, shift}) → Promise<{ok, message}>
//   copyText(text)
//   onOpenChange(open)        layout and keyboard surface follow-up
//   onResize()                the panel width changed; refit the terminals
export function createAgentPanel(deps) {
  const doc = deps.doc || document;
  const $ = (id) => doc.getElementById(id);
  const root = $("agent-panel");
  if (!root) return null;
  const el = {
    scope: $("agent-scope"),
    scopeLabel: $("agent-scope-label"),
    picker: $("agent-picker"),
    thread: $("agent-thread"),
    composer: $("agent-composer"),
    chips: $("agent-chips"),
    input: $("agent-input"),
    send: $("agent-send"),
    stop: $("agent-stop"),
    hint: $("agent-hint"),
    newBtn: $("agent-new"),
    close: $("agent-close"),
    toggle: $("btn-agent"),
    toggleKbd: $("btn-agent-kbd"),
  };
  const shiftWord = deps.isMac ? "⇧" : "Shift";

  const state = {
    open: doc.documentElement.dataset.agent === "open",
    conv: A.conversationId(),
    scope: [],
    scopeAuto: true,
    settings: null,
    keyConfigured: null,
    provider: null,
    busy: null, // AbortController of the ask in flight
    turns: 0,
    proposals: new Map(), // id → { p, el, editing, status }
  };

  if (el.toggleKbd) el.toggleKbd.textContent = deps.isMac ? "⌘I" : "Ctrl+⇧I";
  if (el.toggle) el.toggle.title = deps.isMac ? "Agent (⌘I)" : "Agent (Ctrl+Shift+I)";

  function panes() {
    return deps.listPanes() || [];
  }

  function paneById(id) {
    return panes().find((p) => p.sessionId === id) || null;
  }

  // --- open / close ---

  function applyOpen() {
    doc.documentElement.dataset.agent = state.open ? "open" : "closed";
    if (el.toggle) {
      el.toggle.setAttribute("aria-pressed", String(state.open));
      el.toggle.setAttribute("aria-expanded", String(state.open));
    }
  }

  function setOpen(open, { focus = true } = {}) {
    state.open = !!open;
    applyOpen();
    if (!state.open) closePicker();
    deps.onOpenChange?.(state.open);
    if (state.open) {
      refresh();
      loadSettings();
      if (focus) requestAnimationFrame(() => el.input?.focus({ preventScroll: true }));
    }
  }

  function toggle() {
    setOpen(!state.open);
  }

  // --- settings and the Connect Gemini state ---

  async function loadSettings() {
    try {
      const res = await deps.api("/api/agent/settings");
      state.settings = res.settings || null;
      const pid = state.settings?.provider || "gemini";
      state.keyConfigured = !!res.keys?.[pid]?.configured;
      if (state.scopeAuto) state.scope = A.defaultScope(state.settings?.defaultScope, panes());
      if (!state.keyConfigured && !state.provider) {
        const list = await deps.api("/api/agent/providers");
        state.provider = (list || []).find((d) => d.id === pid) || null;
      }
    } catch (err) {
      if (err?.code === 423) return;
      state.keyConfigured = null;
    }
    refresh();
    paintEmpty();
  }

  function connectHTML() {
    const d = state.provider || { name: "Google Gemini", keyLabel: "Gemini API key" };
    const warn = d.warning
      ? `<div class="k-agent-warning" role="note">${ICON.alert}<p>${escapeHtml(d.warning)}${
          d.termsUrl ? ` <a href="${escapeHtml(d.termsUrl)}" target="_blank" rel="noopener noreferrer">Gemini API terms</a>` : ""
        }</p></div>`
      : "";
    const help = d.keyHelpUrl
      ? `<a href="${escapeHtml(d.keyHelpUrl)}" target="_blank" rel="noopener noreferrer">Get a key</a>`
      : "";
    return `<form class="k-agent-connect" id="agent-connect" autocomplete="off">
      <h2 class="k-agent-connect-title">Connect Gemini</h2>
      <p class="k-help">When you ask, the agent sends the panes in scope, redacted, and your question to ${escapeHtml(d.name || "Google Gemini")}. The key is kept in the vault.</p>
      ${warn}
      <label class="k-field"><span class="k-label">${escapeHtml(d.keyLabel || "API key")}</span>
        <div class="k-agent-connect-row">
          <input class="k-input" type="password" id="agent-key" name="key" spellcheck="false" autocomplete="off" aria-label="${escapeHtml(d.keyLabel || "API key")}" />
          <button type="submit" class="k-btn k-btn--primary k-btn--sm">Connect</button>
        </div>
      </label>
      <p class="k-agent-field-error" id="agent-key-error" role="alert"></p>
      <p class="k-help">${help}</p>
    </form>`;
  }

  function paintEmpty() {
    if (state.turns > 0) return;
    if (state.keyConfigured === false) {
      el.thread.innerHTML = connectHTML();
      return;
    }
    const lines = state.settings?.scrollbackLines || 200;
    el.thread.innerHTML = `<div class="k-agent-empty">${ICON.ai}
      <p class="k-agent-empty-title">Ask about your sessions</p>
      <p class="k-help">When you send, the agent reads the last ${lines} lines of each pane in scope, redacts secrets, and answers. Every read is written to the log.</p>
    </div>`;
  }

  async function saveKey(form) {
    const input = form.querySelector("#agent-key");
    const errEl = form.querySelector("#agent-key-error");
    const key = (input?.value || "").trim();
    if (!key) {
      if (errEl) errEl.textContent = "Paste a key first.";
      input?.focus();
      return;
    }
    const pid = state.settings?.provider || "gemini";
    try {
      await deps.api(`/api/agent/keys/${encodeURIComponent(pid)}`, { method: "PUT", body: JSON.stringify({ key }) });
      if (input) input.value = "";
      state.keyConfigured = true;
      deps.toast("Gemini connected. The key is in the vault.");
      paintEmpty();
      el.input?.focus({ preventScroll: true });
    } catch (err) {
      if (errEl) errEl.textContent = err?.message || String(err);
    }
  }

  // --- scope ---

  function scopePanes() {
    return state.scope.map(paneById).filter(Boolean);
  }

  function refresh() {
    const all = panes();
    if (state.scopeAuto && state.settings) state.scope = A.defaultScope(state.settings.defaultScope, all);
    else state.scope = A.reconcileScope(state.scope, all);
    const m = A.candidatePanes(all).length;
    setText(el.scopeLabel, A.scopeLabel(state.scope.length, m));
    paintChips();
    if (!el.picker.hidden) paintPicker();
    for (const rec of state.proposals.values()) if (!rec.editing) paintCard(rec);
  }

  function paintChips() {
    el.chips.innerHTML = scopePanes()
      .map(
        (p) =>
          `<span class="k-agent-chip" data-session="${escapeHtml(p.sessionId)}">${ICON.at}<span>${escapeHtml(p.label)}</span><button type="button" data-agent-unscope="${escapeHtml(
            p.sessionId,
          )}" aria-label="Stop reading ${escapeHtml(p.label)}">${ICON.x}</button></span>`,
      )
      .join("");
  }

  function paintPicker() {
    const all = A.candidatePanes(panes());
    const rows = all.map((p) => {
      const on = state.scope.includes(p.sessionId);
      const sub = [p.sub, p.focused ? "focused" : ""].filter(Boolean).join(" · ");
      const badge = !p.readable
        ? `<span class="k-badge k-badge--outline">Agent can't read</span>`
        : p.production
          ? `<span class="k-badge k-badge--signal">Production</span>`
          : "";
      return `<button type="button" class="k-agent-pick" role="menuitemcheckbox" aria-checked="${on}" ${
        p.readable ? "" : 'aria-disabled="true"'
      } data-agent-pick="${escapeHtml(p.sessionId)}">
        <span class="k-check" aria-hidden="true"><span class="k-i">${ICON.tick}</span></span>
        <span class="k-agent-pick-main"><span class="k-agent-pick-name">${escapeHtml(p.label)}</span>${
          sub ? `<span class="k-agent-pick-sub">${escapeHtml(sub)}</span>` : ""
        }</span>${badge}</button>`;
    });
    el.picker.innerHTML = rows.length
      ? `<div class="k-menu-label">Panes the agent reads</div>${rows.join("")}<div class="k-agent-picker-foot">
          <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-agent-preset="focused">Focused pane</button>
          <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-agent-preset="tab">Every pane in tab</button>
          <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-agent-preset="none">None</button>
        </div>`
      : `<p class="k-help k-agent-picker-empty">Open a terminal to let the agent read it.</p>`;
  }

  function openPicker() {
    paintPicker();
    el.picker.hidden = false;
    el.scope.setAttribute("aria-expanded", "true");
    (el.picker.querySelector('[aria-checked]:not([aria-disabled="true"])') || el.picker).focus({ preventScroll: true });
  }

  function closePicker({ refocus = false } = {}) {
    if (el.picker.hidden) return;
    el.picker.hidden = true;
    el.scope.setAttribute("aria-expanded", "false");
    if (refocus) el.input?.focus({ preventScroll: true });
  }

  function setScope(ids) {
    state.scopeAuto = false;
    state.scope = A.reconcileScope(ids, panes());
    refresh();
  }

  // --- thread ---

  function scrollDown() {
    el.thread.scrollTop = el.thread.scrollHeight;
  }

  function startThread() {
    if (state.turns === 0) el.thread.innerHTML = "";
    state.turns++;
  }

  function appendHTML(html) {
    const t = doc.createElement("template");
    t.innerHTML = html.trim();
    const node = t.content.firstElementChild;
    el.thread.appendChild(node);
    return node;
  }

  function stepHTML(step) {
    const cls = step.status === "read" ? "k-agent-step--read" : "k-agent-step--skip";
    const icon = step.status === "read" ? ICON.tick : ICON.skip;
    return `<div class="k-agent-step ${cls}">${icon}<span>${escapeHtml(A.traceText(step))}</span></div>`;
  }

  function errorHTML(err) {
    if (err?.code === 412 || err?.data?.reason === "no-key") {
      return `<div class="k-agent-error" role="alert"><b>Gemini is not connected.</b><span>Add a key to ask the agent.</span></div>`;
    }
    if (err?.code === 423) return `<div class="k-agent-error" role="alert"><b>The vault is locked.</b><span>Unlock it, then ask again.</span></div>`;
    const msg = err?.message || String(err);
    return `<div class="k-agent-error" role="alert"><b>The agent could not answer.</b><span>${escapeHtml(msg)}</span></div>`;
  }

  async function ask(question) {
    const q = String(question || "").trim();
    if (!q || state.busy) return;
    if (state.keyConfigured === false) {
      paintEmpty();
      root.querySelector("#agent-key")?.focus();
      return;
    }
    closePicker();
    startThread();
    const inScope = scopePanes();
    appendHTML(`<div class="k-agent-turn"><div class="k-agent-who">You</div><p class="k-agent-msg">${escapeHtml(q)}</p></div>`);
    const turn = appendHTML(`<div class="k-agent-turn"><div class="k-agent-who">${ICON.ai}Agent</div>
      <div class="k-agent-trace"><div class="k-agent-step k-agent-step--busy">${ICON.busy}<span>${
        inScope.length ? `Reading ${inScope.length} ${inScope.length === 1 ? "pane" : "panes"} and asking` : "Asking without reading a pane"
      }</span></div></div></div>`);
    scrollDown();
    const ctl = new AbortController();
    state.busy = ctl;
    setBusy(true);
    try {
      const res = await deps.api("/api/agent/ask", {
        method: "POST",
        body: JSON.stringify({ conversationId: state.conv, question: q, panes: inScope.map((p) => p.sessionId) }),
        signal: ctl.signal,
      });
      if (res.conversationId !== state.conv) return; // a new conversation started meanwhile
      const trace = turn.querySelector(".k-agent-trace");
      trace.innerHTML =
        (res.trace || []).map(stepHTML).join("") || `<div class="k-agent-step k-agent-step--skip">${ICON.skip}<span>Read no pane</span></div>`;
      if (res.testProvider) {
        trace.insertAdjacentHTML("beforeend", `<div class="k-agent-step k-agent-step--skip">${ICON.info}<span>Test provider · nothing left this machine</span></div>`);
      }
      if (res.answer) turn.insertAdjacentHTML("beforeend", `<p class="k-agent-msg">${A.answerHTML(res.answer)}</p>`);
      for (const p of res.proposals || []) addProposal(p);
    } catch (err) {
      if (err?.name === "AbortError") {
        turn.querySelector(".k-agent-trace").innerHTML = `<div class="k-agent-step k-agent-step--skip">${ICON.skip}<span>Stopped</span></div>`;
      } else {
        turn.querySelector(".k-agent-trace").innerHTML = "";
        turn.insertAdjacentHTML("beforeend", errorHTML(err));
        if (err?.code === 412) {
          state.keyConfigured = false;
          state.provider = null;
          loadSettings();
        }
      }
    } finally {
      if (state.busy === ctl) {
        state.busy = null;
        setBusy(false);
      }
      scrollDown();
    }
  }

  function setBusy(on) {
    if (el.stop) el.stop.hidden = !on;
    if (el.send) el.send.disabled = on;
  }

  // --- proposals ---

  function addProposal(p) {
    const node = doc.createElement("div");
    node.className = "k-prop";
    node.setAttribute("role", "group");
    node.setAttribute("aria-label", "Proposal");
    node.dataset.proposalId = p.id;
    el.thread.appendChild(node);
    const rec = { p, el: node, editing: false, status: null };
    state.proposals.set(p.id, rec);
    paintCard(rec);
  }

  function noteHTML(kind, icon, text) {
    return `<p class="k-prop-note k-prop-note--${kind}">${icon}<span>${text}</span></p>`;
  }

  function paintCard(rec) {
    const { p } = rec;
    const pane = paneById(p.sessionId);
    const offer = A.typeOffer(p, pane);
    const badge = A.riskBadge(p.risk);
    rec.el.dataset.risk = p.risk;
    const notes = [];
    if (offer === "shift" && !rec.status) notes.push(noteHTML("flare", ICON.alert, `${escapeHtml(A.shiftReason(p))}. Hold ${shiftWord} when you press Type.`));
    if (offer === "following") notes.push(noteHTML("haze", ICON.eye, "You follow this pane. The agent cannot type into it."));
    if (offer === "closed") notes.push(noteHTML("haze", ICON.info, "That pane is not open in this window. Copy the command instead."));
    if (offer === "none") notes.push(noteHTML("haze", ICON.info, "No pane named for this command. Copy it instead."));
    if (p.placeholders) notes.push(noteHTML("signal", ICON.alert, "It holds a redacted placeholder such as [secret:1]. Edit it before you type it."));
    if (rec.status) notes.push(noteHTML(rec.status.ok ? "tide" : "flare", rec.status.ok ? ICON.tick : ICON.alert, escapeHtml(rec.status.text)));
    const canType = offer === "type" || offer === "shift";
    const typeKbd = offer === "shift" ? `${shiftWord === "⇧" ? "⇧↵" : "Shift+↵"}` : "↵";
    const body = rec.editing
      ? `<textarea class="k-textarea k-textarea--mono" data-prop-edit aria-label="Edit command" spellcheck="false">${escapeHtml(p.command)}</textarea>`
      : `<pre class="k-bytes" aria-label="Exact bytes">${A.bytesHTML(p.bytes)}</pre>`;
    const foot = rec.editing
      ? `<span class="k-spacer"></span>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-prop-cancel>Cancel</button>
         <button type="button" class="k-btn k-btn--primary k-btn--sm" data-prop-save>Save command</button>`
      : `<span class="k-prop-count">${escapeHtml(A.countLabel(p.bytes))}</span>
         <span class="k-spacer"></span>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-prop-edit-btn aria-label="Edit command" title="Edit">${ICON.edit}</button>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-prop-copy aria-label="Copy command" title="Copy">${ICON.copy}</button>
         ${canType ? `<button type="button" class="k-btn k-btn--primary k-btn--sm" data-prop-type>Type into pane<span class="k-kbd">${typeKbd}</span></button>` : ""}`;
    rec.el.innerHTML = `<div class="k-prop-head">${ICON.cmd}<span>Run on</span><span class="k-mono">${escapeHtml(p.host || pane?.label || "no pane")}</span>${
      p.production ? '<span class="k-badge k-badge--signal">Production</span>' : ""
    }<span class="k-badge ${badge.cls} k-prop-risk">${badge.label}</span></div>
      <div class="k-prop-body">${body}${p.why ? `<p class="k-prop-why">${escapeHtml(p.why)}</p>` : ""}${notes.join("")}</div>
      <div class="k-prop-foot">${foot}</div>`;
    if (rec.editing) {
      const ta = rec.el.querySelector("[data-prop-edit]");
      ta?.focus({ preventScroll: true });
    }
  }

  async function typeIt(rec, shift) {
    const pane = paneById(rec.p.sessionId);
    const gate = A.typeGate(rec.p, pane, shift);
    if (!gate.ok) {
      if (gate.reason === "shift") {
        rec.status = { ok: false, text: `Not typed. ${A.shiftReason(rec.p)}: hold ${shiftWord} and press Type.` };
        paintCard(rec);
        rec.el.querySelector("[data-prop-type]")?.focus({ preventScroll: true });
      }
      return;
    }
    const res = await deps.typeProposal({ proposal: rec.p, conversationId: state.conv, shift: !!shift });
    if (!res) return;
    if (res.ok) rec.status = { ok: true, text: `Typed into ${rec.p.host || "the pane"}.` };
    else if (res.cancelled) rec.status = null;
    else rec.status = { ok: false, text: res.message || "Not typed." };
    paintCard(rec);
  }

  async function saveEdit(rec) {
    const ta = rec.el.querySelector("[data-prop-edit]");
    const command = ta?.value || "";
    try {
      const p = await deps.api(`/api/agent/conversations/${encodeURIComponent(state.conv)}/proposals/${encodeURIComponent(rec.p.id)}`, {
        method: "PUT",
        body: JSON.stringify({ command }),
      });
      rec.p = p;
      rec.editing = false;
      rec.status = null;
      paintCard(rec);
    } catch (err) {
      deps.toast(err?.message || String(err), true);
    }
  }

  // --- new conversation ---

  function newConversation() {
    const old = state.conv;
    state.busy?.abort();
    state.conv = A.conversationId();
    state.turns = 0;
    state.proposals.clear();
    state.scopeAuto = true;
    if (state.settings) state.scope = A.defaultScope(state.settings.defaultScope, panes());
    deps.api(`/api/agent/conversations/${encodeURIComponent(old)}`, { method: "DELETE" }).catch(() => {});
    refresh();
    paintEmpty();
    el.input?.focus({ preventScroll: true });
  }

  // --- events ---

  el.toggle?.addEventListener("click", () => toggle());
  el.close?.addEventListener("click", () => setOpen(false));
  el.newBtn?.addEventListener("click", () => newConversation());
  el.scope?.addEventListener("click", () => (el.picker.hidden ? openPicker() : closePicker({ refocus: true })));
  el.stop?.addEventListener("click", () => state.busy?.abort());

  el.picker.addEventListener("click", (ev) => {
    const pick = ev.target.closest("[data-agent-pick]");
    if (pick) {
      if (pick.getAttribute("aria-disabled") === "true") return;
      setScope(A.toggleScope(state.scope, paneById(pick.dataset.agentPick)));
      el.picker.querySelector(`[data-agent-pick="${CSS.escape(pick.dataset.agentPick)}"]`)?.focus({ preventScroll: true });
      return;
    }
    const preset = ev.target.closest("[data-agent-preset]")?.dataset.agentPreset;
    if (preset) {
      setScope(preset === "none" ? [] : A.defaultScope(preset, panes()));
      closePicker({ refocus: true });
    }
  });
  el.picker.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      closePicker({ refocus: true });
      return;
    }
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      const items = [...el.picker.querySelectorAll("button")];
      const i = items.indexOf(doc.activeElement);
      const next = items[(i + (ev.key === "ArrowDown" ? 1 : -1) + items.length) % items.length];
      next?.focus({ preventScroll: true });
      ev.preventDefault();
    }
  });
  doc.addEventListener("pointerdown", (ev) => {
    if (el.picker.hidden) return;
    if (el.picker.contains(ev.target) || el.scope.contains(ev.target)) return;
    closePicker();
  });

  el.chips.addEventListener("click", (ev) => {
    const id = ev.target.closest("[data-agent-unscope]")?.dataset.agentUnscope;
    if (id) setScope(state.scope.filter((s) => s !== id));
  });

  el.composer.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const q = el.input.value;
    if (!q.trim()) return;
    el.input.value = "";
    ask(q);
  });
  el.input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing && !ev.altKey && !ev.metaKey && !ev.ctrlKey) {
      ev.preventDefault();
      el.composer.requestSubmit();
    }
  });
  el.input.addEventListener("input", (ev) => {
    if (ev.inputType !== "insertText" || ev.data !== "@") return;
    const caret = el.input.selectionStart;
    if (!A.atTrigger(el.input.value, caret)) return;
    el.input.value = el.input.value.slice(0, caret - 1) + el.input.value.slice(caret);
    el.input.setSelectionRange(caret - 1, caret - 1);
    openPicker();
  });

  el.thread.addEventListener("submit", (ev) => {
    if (ev.target.id !== "agent-connect") return;
    ev.preventDefault();
    saveKey(ev.target);
  });
  el.thread.addEventListener("click", (ev) => {
    const card = ev.target.closest(".k-prop");
    const rec = card && state.proposals.get(card.dataset.proposalId);
    if (!rec) return;
    if (ev.target.closest("[data-prop-type]")) typeIt(rec, ev.shiftKey);
    else if (ev.target.closest("[data-prop-copy]")) deps.copyText(rec.p.command);
    else if (ev.target.closest("[data-prop-edit-btn]")) {
      rec.editing = true;
      paintCard(rec);
    } else if (ev.target.closest("[data-prop-cancel]")) {
      rec.editing = false;
      paintCard(rec);
    } else if (ev.target.closest("[data-prop-save]")) saveEdit(rec);
  });
  el.thread.addEventListener("keydown", (ev) => {
    const card = ev.target.closest(".k-prop");
    const rec = card && state.proposals.get(card.dataset.proposalId);
    if (!rec) return;
    // ↵ / ⇧↵ on Type into pane: handled here so ⇧ is read from the key,
    // whatever the engine puts on a keyboard-made click.
    if (ev.key === "Enter" && ev.target.closest("[data-prop-type]")) {
      ev.preventDefault();
      typeIt(rec, ev.shiftKey);
    }
    if (rec.editing && ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      rec.editing = false;
      paintCard(rec);
    }
    if (rec.editing && ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
      ev.preventDefault();
      saveEdit(rec);
    }
  });

  // --- width (the sash on the panel's left edge) ---
  // Min is --size-agent, max 50vw, step --size-agent-step. The width lives in
  // --agent-width on the panel and in localStorage; null means the default.
  const WIDTH_KEY = "keel-agent-width";
  const sash = $("agent-sash");
  const tokenPx = (name, fallback) => {
    const n = parseFloat(doc.defaultView?.getComputedStyle(doc.documentElement).getPropertyValue(name));
    return n > 0 ? n : fallback;
  };
  const minW = () => tokenPx("--size-agent", 380);
  const maxW = () => Math.max(minW(), Math.floor((doc.defaultView?.innerWidth || 1280) * 0.5));
  let width = null;

  function readWidth() {
    try {
      const n = Number(localStorage.getItem(WIDTH_KEY));
      return n > 0 ? n : null;
    } catch {
      return null;
    }
  }
  function writeWidth() {
    try {
      if (width == null) localStorage.removeItem(WIDTH_KEY);
      else localStorage.setItem(WIDTH_KEY, String(width));
    } catch {}
  }
  function applyWidth(notify) {
    const lo = minW();
    const hi = maxW();
    if (width != null) width = Math.min(hi, Math.max(lo, Math.round(width)));
    if (width == null) root.style.removeProperty("--agent-width");
    else root.style.setProperty("--agent-width", `${width}px`);
    if (sash) {
      sash.setAttribute("aria-valuemin", String(lo));
      sash.setAttribute("aria-valuemax", String(hi));
      sash.setAttribute("aria-valuenow", String(width ?? lo));
    }
    if (notify) deps.onResize?.();
  }
  const currentWidth = () => width ?? minW();
  function setWidth(w, persist) {
    width = w;
    applyWidth(true);
    if (persist) writeWidth();
  }

  if (sash) {
    width = readWidth();
    applyWidth(false);
    const html = doc.documentElement;
    sash.addEventListener("pointerdown", (ev) => {
      if (ev.button > 0) return;
      ev.preventDefault();
      sash.setPointerCapture?.(ev.pointerId);
      sash.classList.add("is-dragging");
      html.classList.add("is-agent-resizing");
      const move = (e) => setWidth(root.getBoundingClientRect().right - e.clientX, false);
      const up = () => {
        sash.removeEventListener("pointermove", move);
        sash.removeEventListener("pointerup", up);
        sash.removeEventListener("pointercancel", up);
        sash.classList.remove("is-dragging");
        html.classList.remove("is-agent-resizing");
        writeWidth();
      };
      sash.addEventListener("pointermove", move);
      sash.addEventListener("pointerup", up);
      sash.addEventListener("pointercancel", up);
    });
    sash.addEventListener("keydown", (ev) => {
      const step = tokenPx("--size-agent-step", 24) * (ev.shiftKey ? 4 : 1);
      // The panel is on the right: ArrowLeft widens it.
      if (ev.key === "ArrowLeft") setWidth(currentWidth() + step, true);
      else if (ev.key === "ArrowRight") setWidth(currentWidth() - step, true);
      else if (ev.key === "Home") setWidth(minW(), true);
      else if (ev.key === "End") setWidth(maxW(), true);
      else return;
      ev.preventDefault();
    });
    sash.addEventListener("dblclick", () => setWidth(null, true));
    doc.defaultView?.addEventListener("resize", () => applyWidth(false));
  }

  applyOpen();
  if (state.open) loadSettings();
  paintEmpty();

  return {
    isOpen: () => state.open,
    setOpen,
    toggle,
    // prefill opens the panel with text in the composer. It does not send:
    // the user presses ↵ (the palette's ⌘↵ row, §5.2).
    prefill(text) {
      setOpen(true, { focus: false });
      if (el.input) {
        el.input.value = String(text || "");
        requestAnimationFrame(() => {
          el.input.focus({ preventScroll: true });
          const end = el.input.value.length;
          el.input.setSelectionRange(end, end);
        });
      }
    },
    refresh,
    focused: () => root.contains(doc.activeElement),
    ask,
    // test seam for node: the current scope ids
    scope: () => state.scope.slice(),
  };
}
