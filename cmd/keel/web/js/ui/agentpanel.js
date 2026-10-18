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
  trash: svg(
    '<path d="M19.5 5.5L18.8803 15.5251C18.7219 18.0864 18.6428 19.3671 18.0008 20.2879C17.6833 20.7431 17.2747 21.1273 16.8007 21.416C15.8421 22 14.559 22 11.9927 22C9.42312 22 8.1383 22 7.17905 21.4149C6.7048 21.1257 6.296 20.7408 5.97868 20.2848C5.33688 19.3626 5.25945 18.0801 5.10461 15.5152L4.5 5.5"/><path d="M3 5.5H21M16.0557 5.5L15.3731 4.09173C14.9196 3.15626 14.6928 2.68852 14.3017 2.39681C14.215 2.3321 14.1231 2.27454 14.027 2.2247C13.5939 2 13.0741 2 12.0345 2C10.9688 2 10.436 2 9.99568 2.23412C9.8981 2.28601 9.80498 2.3459 9.71729 2.41317C9.32164 2.7167 9.10063 3.20155 8.65861 4.17126L8.05292 5.5"/><path d="M9.5 16.5L9.5 10.5"/><path d="M14.5 16.5L14.5 10.5"/>',
  ),
  redo: svg(
    '<path d="M21 8H9C5.68629 8 3 10.6863 3 14C3 17.3137 5.68629 20 9 20H13"/><path d="M17 4L18.1538 4.87652C20.0513 6.31801 21 7.03875 21 8C21 8.96125 20.0513 9.68199 18.1538 11.1235L17 12"/>',
  ),
  history: svg(
    '<path d="M3.49902 14.9656C4.72475 18.4791 8.06749 21 11.999 21C16.9696 21 20.999 16.9706 20.999 12C20.999 7.02944 16.9696 3 11.999 3C8.29827 3 4.8984 5.6756 3.68943 8.5"/><path d="M11.999 7V12L14.999 14"/><path d="M7.49751 8.74363C7.49751 8.74363 3.81388 9.3026 3.25487 8.7436C2.69585 8.1846 3.25488 4.50098 3.25488 4.50098"/>',
  ),
  shield: svg(
    '<path d="M18.7088 3.49534C16.8165 2.55382 14.5009 2 12 2C9.4991 2 7.1835 2.55382 5.29116 3.49534C4.36318 3.95706 3.89919 4.18792 3.4496 4.91378C3 5.63965 3 6.34248 3 7.74814V11.2371C3 16.9205 7.54236 20.0804 10.173 21.4338C10.9067 21.8113 11.2735 22 12 22C12.7265 22 13.0933 21.8113 13.8269 21.4338C16.4576 20.0804 21 16.9205 21 11.2371L21 7.74814C21 6.34249 21 5.63966 20.5504 4.91378C20.1008 4.18791 19.6368 3.95706 18.7088 3.49534Z"/>',
  ),
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
//   confirm(msg, opts)        → Promise<boolean> (the alertdialog), for Delete all
//
// Saved conversations (§5.4.1): the title button in the header opens the
// session list (GET /api/agent/conversations). Picking one loads its
// transcript (GET /api/agent/conversations/{id}) and the next ask
// continues it; keel serve reads the panes in scope again then. Proposals
// from an earlier run come back Expired: Copy, or Propose again, which asks
// keel serve for a fresh proposal that goes through the normal Type path.
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
    sessBtn: $("agent-sessions-btn"),
    title: $("agent-title"),
    saved: $("agent-saved"),
    sessions: $("agent-sessions"),
    sessFilter: $("agent-sessions-filter"),
    sessList: $("agent-sessions-list"),
    sessNote: $("agent-sessions-note"),
    sessClear: $("agent-sessions-clear"),
  };
  const NEW_TITLE = "New conversation";
  const INPUT_HINT = el.input?.getAttribute("placeholder") || "Ask about these sessions…";
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
    title: "",
    historyDays: null, // from the settings; 0 keeps conversations only while open
    sessions: [], // the last session list
    renaming: null, // id of the session row being renamed
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
    if (!state.open) {
      closePicker();
      closeSessions();
    }
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
      state.historyDays = state.settings?.historyDays ?? null;
      paintTitle();
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

  // --- title and the saved note ---

  function paintTitle() {
    setText(el.title, state.title || NEW_TITLE);
    if (el.sessBtn) el.sessBtn.title = state.title ? `${state.title} · Recent conversations` : "Recent conversations";
    if (el.saved && state.historyDays != null) setText(el.saved, A.savedLabel(state.historyDays));
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
    closeSessions();
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
    closeSessions();
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
      if (res.title) {
        state.title = res.title;
        paintTitle();
      }
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
    if (p.typedAt && !p.expired) rec.status = { ok: true, text: `Typed into ${p.host || "the pane"} at ${A.stampLabel(p.typedAt)}.` };
    state.proposals.set(p.id, rec);
    paintCard(rec);
    return node;
  }

  function noteHTML(kind, icon, text) {
    return `<p class="k-prop-note k-prop-note--${kind}">${icon}<span>${text}</span></p>`;
  }

  // An expired card is a proposal saved from an earlier run: the panes may
  // have changed, so it cannot be typed. Copy, or Propose again (a fresh
  // proposal through the normal path).
  function paintExpired(rec) {
    const { p } = rec;
    const badge = A.riskBadge(p.risk);
    rec.el.classList.add("k-prop--expired");
    rec.el.dataset.risk = p.risk;
    const when = p.proposedAt ? A.stampLabel(p.proposedAt) : "";
    const typed = p.typedAt ? `typed ${A.stampLabel(p.typedAt)}` : "not typed";
    rec.el.innerHTML = `<div class="k-prop-head">${ICON.cmd}<span>Run on</span><span class="k-mono">${escapeHtml(p.host || "no pane")}</span>${
      p.production ? '<span class="k-badge k-badge--signal">Production</span>' : ""
    }<span class="k-badge ${badge.cls} k-prop-risk">${badge.label}</span><span class="k-badge">Expired</span></div>
      <div class="k-prop-body"><pre class="k-bytes" aria-label="Exact bytes">${A.bytesHTML(p.bytes)}</pre>${p.why ? `<p class="k-prop-why">${escapeHtml(p.why)}</p>` : ""}${noteHTML(
        "muted",
        ICON.history,
        "Panes may have changed since this was proposed.",
      )}</div>
      <div class="k-prop-foot"><span class="k-prop-count">${escapeHtml([when ? `Proposed ${when}` : "", typed].filter(Boolean).join(" · "))}</span>
        <span class="k-spacer"></span>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-prop-copy>${ICON.copy}Copy</button>
        <button type="button" class="k-btn k-btn--sm" data-prop-again>${ICON.redo}Propose again</button></div>`;
  }

  async function proposeAgain(rec) {
    const btn = rec.el.querySelector("[data-prop-again]");
    if (btn) btn.disabled = true;
    try {
      const p = await deps.api(
        `/api/agent/conversations/${encodeURIComponent(state.conv)}/proposals/${encodeURIComponent(rec.p.id)}/again`,
        { method: "POST" },
      );
      const node = addProposal(p);
      node.scrollIntoView?.({ block: "nearest" });
      node.querySelector("[data-prop-type], [data-prop-copy]")?.focus({ preventScroll: true });
    } catch (err) {
      deps.toast(err?.message || String(err), true);
    } finally {
      if (btn) btn.disabled = false;
    }
  }

  function paintCard(rec) {
    const { p } = rec;
    if (p.expired) {
      paintExpired(rec);
      return;
    }
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

  // newConversation starts a fresh thread. The old one stays: it is saved
  // in the vault (with history on) and listed under the title button.
  function newConversation({ focus = true } = {}) {
    state.busy?.abort();
    state.conv = A.conversationId();
    state.turns = 0;
    state.title = "";
    state.proposals.clear();
    state.scopeAuto = true;
    if (state.settings) state.scope = A.defaultScope(state.settings.defaultScope, panes());
    if (el.input) el.input.placeholder = INPUT_HINT;
    paintTitle();
    refresh();
    paintEmpty();
    if (focus) el.input?.focus({ preventScroll: true });
  }

  // --- saved conversations (the title button's list) ---

  function sessionsOpen() {
    return !!el.sessions && !el.sessions.hidden;
  }

  async function openSessions() {
    if (!el.sessions) return;
    closePicker();
    el.sessions.hidden = false;
    el.sessBtn?.setAttribute("aria-expanded", "true");
    if (el.sessFilter) el.sessFilter.value = "";
    state.renaming = null;
    el.sessList.innerHTML = `<p class="k-help k-agent-sessions-empty">Loading…</p>`;
    el.sessFilter?.focus({ preventScroll: true });
    await loadSessions();
  }

  function closeSessions({ refocus = false } = {}) {
    if (!sessionsOpen()) return;
    el.sessions.hidden = true;
    state.renaming = null;
    el.sessBtn?.setAttribute("aria-expanded", "false");
    if (refocus) el.sessBtn?.focus({ preventScroll: true });
  }

  async function loadSessions() {
    try {
      const res = await deps.api("/api/agent/conversations");
      state.sessions = res?.sessions || [];
      if (res?.historyDays != null) state.historyDays = res.historyDays;
      paintTitle();
    } catch (err) {
      state.sessions = [];
      el.sessList.innerHTML = `<p class="k-help k-agent-sessions-empty">${
        err?.code === 423 ? "Unlock the vault to see saved conversations." : escapeHtml(err?.message || String(err))
      }</p>`;
      return;
    }
    paintSessions();
  }

  function sessionRowHTML(s) {
    const current = s.id === state.conv;
    const hosts = (s.hosts || [])
      .slice(0, 3)
      .map((h) => `<span class="k-badge k-badge--mono">${escapeHtml(h)}</span>`)
      .join("");
    if (state.renaming === s.id) {
      return `<form class="k-agent-sess-row k-agent-sess-row--edit" data-sess-rename-form="${escapeHtml(s.id)}" aria-current="${current}">
        <input class="k-input" data-sess-title value="${escapeHtml(s.title || "")}" maxlength="120" aria-label="Conversation title" spellcheck="false" autocomplete="off" />
        <button type="submit" class="k-btn k-btn--primary k-btn--sm">Save</button>
      </form>`;
    }
    return `<div class="k-agent-sess-row" aria-current="${current}" data-sess="${escapeHtml(s.id)}">
      <button type="button" class="k-agent-sess-pick" data-sess-pick="${escapeHtml(s.id)}">
        <span class="k-agent-sess-name">${escapeHtml(s.title || NEW_TITLE)}</span>
        <span class="k-agent-sess-meta">${hosts}<span>${escapeHtml(A.sessionWhen(s.updatedAt))}</span><span>· ${escapeHtml(A.proposalsLabel(s.proposals))}</span></span>
      </button>
      <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-sess-rename="${escapeHtml(s.id)}" aria-label="Rename ${escapeHtml(
        s.title || NEW_TITLE,
      )}" title="Rename">${ICON.edit}</button>
      <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-sess-delete="${escapeHtml(s.id)}" aria-label="Delete ${escapeHtml(
        s.title || NEW_TITLE,
      )}" title="Delete">${ICON.trash}</button>
    </div>`;
  }

  function paintSessions() {
    const days = state.historyDays;
    setText(el.sessNote, days == null ? "" : A.retentionNote(days));
    if (el.sessClear) el.sessClear.disabled = !state.sessions.length;
    const groups = A.sessionGroups(state.sessions, el.sessFilter?.value || "");
    if (!groups.length) {
      const msg = state.sessions.length
        ? "No conversation matches."
        : days === 0
          ? "History is off: conversations are kept only while the panel is open. Turn it on in Settings · Agent."
          : "No saved conversations.";
      el.sessList.innerHTML = `<p class="k-help k-agent-sessions-empty">${escapeHtml(msg)}</p>`;
      return;
    }
    el.sessList.innerHTML = groups
      .map(
        (g) =>
          `<div class="k-agent-sess-grp" role="presentation"><span>${escapeHtml(g.label)}</span><span>${g.rows.length}</span></div>${g.rows
            .map(sessionRowHTML)
            .join("")}`,
      )
      .join("");
    if (state.renaming) {
      const input = el.sessList.querySelector("[data-sess-title]");
      input?.focus({ preventScroll: true });
      input?.select();
    }
  }

  function threadTurnHTML(t) {
    if (t.again) return "";
    const who = t.at ? ` · ${escapeHtml(A.stampLabel(t.at))}` : "";
    const trace = (t.trace || []).map(stepHTML).join("");
    return `<div class="k-agent-turn"><div class="k-agent-who">You${who}</div><p class="k-agent-msg">${escapeHtml(t.question || "")}</p></div>
      <div class="k-agent-turn"><div class="k-agent-who">${ICON.ai}Agent</div>${trace ? `<div class="k-agent-trace">${trace}</div>` : ""}${
        t.answer ? `<p class="k-agent-msg">${A.answerHTML(t.answer)}</p>` : ""
      }</div>`;
  }

  // resume opens a saved conversation. Nothing is read or sent: the
  // transcript comes from keel serve, and the next ask reads the panes in
  // scope as they are then.
  async function resume(id) {
    if (!id) return;
    if (id === state.conv && state.turns > 0) {
      closeSessions();
      el.input?.focus({ preventScroll: true });
      return;
    }
    let t;
    try {
      t = await deps.api(`/api/agent/conversations/${encodeURIComponent(id)}`);
    } catch (err) {
      deps.toast(err?.code === 404 ? "That conversation is gone." : err?.message || String(err), true);
      loadSessions();
      return;
    }
    closeSessions();
    state.busy?.abort();
    state.conv = t.id;
    state.title = t.title || "";
    state.proposals.clear();
    state.turns = 0;
    el.thread.innerHTML = "";
    for (const turn of t.turns || []) {
      const html = threadTurnHTML(turn);
      if (html) {
        el.thread.insertAdjacentHTML("beforeend", html);
        state.turns++;
      }
      for (const p of turn.proposals || []) addProposal(p);
    }
    const anyExpired = (t.turns || []).some((turn) => (turn.proposals || []).some((p) => p.expired));
    if (anyExpired || state.turns) {
      el.thread.insertAdjacentHTML(
        "beforeend",
        `<div class="k-agent-divider" role="separator">Resumed · ${escapeHtml(A.agoLabel(t.updatedAt))}</div>
         <div class="k-agent-resumed">${ICON.shield}<span class="k-help">Earlier reads stay in this conversation as text. New reads use only the panes in scope now, on hosts that still allow the agent.</span></div>`,
      );
    }
    if (!state.turns) state.turns = 1; // keep the transcript when nothing new is asked
    if (el.input) el.input.placeholder = "Continue this conversation…";
    paintTitle();
    refresh();
    scrollDown();
    el.input?.focus({ preventScroll: true });
  }

  async function renameSession(id, title) {
    const t = String(title || "").trim();
    if (!t) {
      state.renaming = null;
      paintSessions();
      return;
    }
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ title: t }) });
      const s = state.sessions.find((x) => x.id === id);
      if (s) s.title = res.title;
      if (id === state.conv) {
        state.title = res.title;
        paintTitle();
      }
    } catch (err) {
      deps.toast(err?.message || String(err), true);
    }
    state.renaming = null;
    paintSessions();
    el.sessList.querySelector(`[data-sess-rename="${CSS.escape(id)}"]`)?.focus({ preventScroll: true });
  }

  async function deleteSession(id) {
    try {
      await deps.api(`/api/agent/conversations/${encodeURIComponent(id)}`, { method: "DELETE" });
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      return;
    }
    state.sessions = state.sessions.filter((s) => s.id !== id);
    if (id === state.conv) newConversation({ focus: false });
    paintSessions();
    (el.sessList.querySelector("[data-sess-pick]") || el.sessFilter)?.focus({ preventScroll: true });
    deps.toast("Conversation deleted");
  }

  async function deleteAllSessions() {
    if (deps.confirm) {
      const ok = await deps.confirm("Every saved agent conversation is deleted from the vault, in every window. The log entries that say what the agent read stay in Logs.", {
        title: "Delete all conversations?",
        action: "Delete all",
      });
      if (!ok) return;
    }
    try {
      await deps.api("/api/agent/conversations", { method: "DELETE" });
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      return;
    }
    state.sessions = [];
    newConversation({ focus: false });
    paintSessions();
    deps.toast("Agent history deleted");
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
    if (sessionsOpen() && !el.sessions.contains(ev.target) && !el.sessBtn?.contains(ev.target)) closeSessions();
    if (el.picker.hidden) return;
    if (el.picker.contains(ev.target) || el.scope.contains(ev.target)) return;
    closePicker();
  });

  el.sessBtn?.addEventListener("click", () => (sessionsOpen() ? closeSessions({ refocus: true }) : openSessions()));
  el.sessFilter?.addEventListener("input", () => paintSessions());
  el.sessClear?.addEventListener("click", () => deleteAllSessions());
  el.sessList?.addEventListener("click", (ev) => {
    const pick = ev.target.closest("[data-sess-pick]")?.dataset.sessPick;
    if (pick) return void resume(pick);
    const ren = ev.target.closest("[data-sess-rename]")?.dataset.sessRename;
    if (ren) {
      state.renaming = ren;
      paintSessions();
      return;
    }
    const del = ev.target.closest("[data-sess-delete]")?.dataset.sessDelete;
    if (del) deleteSession(del);
  });
  el.sessList?.addEventListener("submit", (ev) => {
    const form = ev.target.closest("[data-sess-rename-form]");
    if (!form) return;
    ev.preventDefault();
    renameSession(form.dataset.sessRenameForm, form.querySelector("[data-sess-title]")?.value);
  });
  el.sessions?.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      if (state.renaming) {
        const id = state.renaming;
        state.renaming = null;
        paintSessions();
        el.sessList.querySelector(`[data-sess-rename="${CSS.escape(id)}"]`)?.focus({ preventScroll: true });
        return;
      }
      closeSessions({ refocus: true });
      return;
    }
    if ((ev.key === "ArrowDown" || ev.key === "ArrowUp") && !state.renaming) {
      const items = [el.sessFilter, ...el.sessList.querySelectorAll("[data-sess-pick]")].filter(Boolean);
      const i = items.indexOf(doc.activeElement);
      const next = items[(i + (ev.key === "ArrowDown" ? 1 : -1) + items.length) % items.length];
      next?.focus({ preventScroll: true });
      ev.preventDefault();
    }
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
    else if (ev.target.closest("[data-prop-again]")) proposeAgain(rec);
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
    resume,
    // test seam for node: the current scope ids
    scope: () => state.scope.slice(),
  };
}
