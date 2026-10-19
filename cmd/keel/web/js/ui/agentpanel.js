// The agent panel (docs/ui-redesign.md §5.4; shell in internal/ui/agent.templ)
// and the window's one agent conversation, which the Agent tab page
// (ui/agentpage.js, §5.4.2) shows too.
//
// It asks keel serve (POST /api/agent/ask) about the panes in scope and
// shows the trace, the answer and command proposals. It never writes to a
// pane: Type into pane hands the proposal to term.js (deps.typeProposal),
// which opens the approve-before-type dialog and sends the approved bytes
// on the session socket, where keel serve checks them with the agent
// service before writing (§6.1, §6.5, §6.6, §6.7).
//
// The conversation (its id, title, turns, proposals, scope and the saved
// list) lives here once. Every thread view (the panel's, the page's) is
// drawn from it and every change goes through it, so the panel and the
// page always show the same conversation.

import { escapeHtml } from "../lib/html.js";
import * as A from "../lib/agent.js";
import * as R from "../lib/runbooks.js";
import * as NB from "../lib/notebooks.js";
import { setText } from "../lib/dom.js";
import { ICON, createThreadView, sessionGroupsHTML } from "./agentthread.js";

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
//   openPage()                opens the Agent tab page ("Open all in a tab")
//   openRunbooks()            opens the Runbooks tab page (§16)
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
    attach: $("agent-attach"),
    nbPicker: $("agent-nb-picker"),
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
    sessPage: $("agent-sessions-page"),
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
    turns: 0, // turns shown (a resumed transcript counts as one at least)
    title: "",
    historyDays: null, // from the settings; 0 keeps conversations only while open
    saveError: "", // why keel serve is not saving conversations, "" when it is
    unreadable: false, // keel.chats cannot be read: Delete all starts over
    sessions: [], // the last session list
    sessionsError: "", // why the last list could not be read
    renaming: null, // id of the session row being renamed (panel list)
    // The conversation as data, for the page's context column: when it
    // started, and per turn { at, question, trace, proposals, again }.
    model: { createdAt: "", updatedAt: "", turns: [] },
    props: new Map(), // proposal id → { p, status }
    notes: new Map(), // note proposal id → { n, busy, error } (§15)
    // A runbook in use (§16): the latest use, the user's Don't use, the
    // names of earlier uses (their cards say Stopped), the step action in
    // flight, the "Use a different runbook" list, and the runbooks the
    // agent proposed.
    run: null,
    noRunbook: false,
    runNames: new Map(),
    runBusy: false,
    runError: "",
    choosing: false,
    others: null,
    bookProps: new Map(), // runbook proposal id → { b, busy, error }
    // Notebooks (§18): the ids the user attached (sent with each question
    // until removed), and the notebook drafts the agent proposed.
    attached: [],
    nbProps: new Map(), // notebook draft id → { p, busy, error }
  };
  let pollTimer = null;
  let pollLeft = 0;

  if (el.toggleKbd) el.toggleKbd.textContent = deps.isMac ? "⌘I" : "Ctrl+⇧I";
  if (el.toggle) el.toggle.title = deps.isMac ? "Agent (⌘I)" : "Agent (Ctrl+Shift+I)";

  // --- views and listeners ---

  const subs = new Set();
  function emit(type) {
    for (const fn of subs) {
      try {
        fn(type);
      } catch (err) {
        console.error(err);
      }
    }
  }

  // ctl is what a thread view may ask of the conversation.
  const ctl = {
    proposal: (id) => state.props.get(id) || null,
    paneById,
    type: typeProposal,
    again: proposeAgain,
    saveEdit,
    copy: (id) => {
      const rec = state.props.get(id);
      if (rec) deps.copyText(rec.p.command);
    },
    note: (id) => state.notes.get(id) || null,
    saveNote,
    dismissNote: (id) => noteAction(id, "DELETE", ""),
    undoNote: (id) => noteAction(id, "POST", "/undo"),
    // runbooks (§16)
    runbook: () => ({
      run: state.run,
      noRunbook: state.noRunbook,
      names: state.runNames,
      busy: !!state.busy || state.runBusy,
      error: state.runError,
      choosing: state.choosing,
      others: state.others,
    }),
    proposeStep,
    stepAction,
    stopRunbook,
    moveRun,
    useRunbook,
    toggleChoose,
    bookProp: (id) => state.bookProps.get(id) || null,
    saveBookProp,
    bookPropAction,
    openRunbooks: () => deps.openRunbooks?.(),
    nbProp: (id) => state.nbProps.get(id) || null,
    openNbDraft,
    nbPropAction,
    openNotebook: (id) => deps.openNotebook?.(id),
  };

  const views = [];
  function eachView(fn) {
    for (const v of views) fn(v);
  }

  const panelView = createThreadView({
    doc,
    el: el.thread,
    ctl,
    isMac: deps.isMac,
    emptyHTML: panelEmptyHTML,
    typeLabel: () => "Type into pane",
  });
  views.push(panelView);

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
    eachView((v) => v.paintEmpty());
    emit("settings");
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

  function panelEmptyHTML() {
    if (state.keyConfigured === false) return connectHTML();
    const lines = state.settings?.scrollbackLines || 200;
    return `<div class="k-agent-empty">${ICON.ai}
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
      eachView((v) => v.paintEmpty());
      emit("settings");
      el.input?.focus({ preventScroll: true });
    } catch (err) {
      if (errEl) errEl.textContent = err?.message || String(err);
    }
  }

  // --- title and the saved note ---

  function paintTitle() {
    setText(el.title, state.title || NEW_TITLE);
    if (el.sessBtn) el.sessBtn.title = state.title ? `${state.title} · Recent conversations` : "Recent conversations";
    if (el.saved && state.historyDays != null) {
      setText(el.saved, A.savedLabel(state.historyDays, state.saveError));
      el.saved.classList.toggle("k-agent-saved--warn", !!state.saveError && state.historyDays > 0);
      el.saved.title = state.saveError ? `Not saved: ${state.saveError}` : "";
    }
  }

  function setTitle(title) {
    state.title = title || "";
    paintTitle();
    emit("title");
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
    eachView((v) => v.repaint());
    emit("scope");
  }

  function paintChips() {
    el.chips.innerHTML =
      scopePanes()
        .map(
          (p) =>
            `<span class="k-agent-chip" data-session="${escapeHtml(p.sessionId)}">${ICON.at}<span>${escapeHtml(p.label)}</span><button type="button" data-agent-unscope="${escapeHtml(
              p.sessionId,
            )}" aria-label="Stop reading ${escapeHtml(p.label)}">${ICON.x}</button></span>`,
        )
        .join("") + attachedChipsHTML();
  }

  // --- notebooks the user attaches (§18) ---

  function notebooks() {
    return deps.listNotebooks?.() || [];
  }

  function attachedNotebooks() {
    const all = notebooks();
    return state.attached.map((id) => all.find((n) => n.id === id)).filter(Boolean);
  }

  // attachedChipsHTML is one chip per attached notebook, for any composer.
  function attachedChipsHTML() {
    return attachedNotebooks()
      .map(
        (nb) =>
          `<span class="k-agent-chip k-agent-chip--notebook" data-notebook-chip="${escapeHtml(nb.id)}">${ICON.notebook}<span>${escapeHtml(nb.name || "Notebook")}</span><button type="button" data-agent-detach="${escapeHtml(
            nb.id,
          )}" aria-label="Stop sending ${escapeHtml(nb.name || "Notebook")}">${ICON.x}</button></span>`,
      )
      .join("");
  }

  // nbPickerHTML is the Attach notebook menu: every saved notebook, at
  // most MAX_ATTACHED ticked. Nothing is read until the user asks.
  function nbPickerHTML() {
    const all = NB.attachable(notebooks());
    if (!all.length) return `<p class="k-help k-agent-picker-empty">No notebooks yet. Write one first; then attach it here.</p>`;
    const full = state.attached.length >= NB.MAX_ATTACHED;
    const rows = all.map((nb) => {
      const on = state.attached.includes(nb.id);
      const off = !on && full;
      return `<button type="button" class="k-agent-pick" role="menuitemcheckbox" aria-checked="${on}"${off ? ' aria-disabled="true"' : ""} data-agent-attach-pick="${escapeHtml(nb.id)}">
        <span class="k-check" aria-hidden="true"><span class="k-i">${ICON.tick}</span></span>
        <span class="k-agent-pick-main"><span class="k-agent-pick-name">${escapeHtml(nb.name || "Notebook")}</span></span>${
          nb.source === "agent" ? '<span class="k-badge k-badge--outline">From agent</span>' : ""
        }</button>`;
    });
    return `<div class="k-menu-label">Notebooks to send with your question</div>${rows.join("")}<p class="k-help k-agent-picker-note">${escapeHtml(
      `At most ${NB.MAX_ATTACHED}. The agent sees only what you attach, redacted, and cannot open any notebook itself.`,
    )}</p>`;
  }

  function paintNbPicker() {
    if (el.nbPicker && !el.nbPicker.hidden) el.nbPicker.innerHTML = nbPickerHTML();
  }

  function openNbPicker() {
    if (!el.nbPicker) return;
    closePicker();
    closeSessions();
    el.nbPicker.innerHTML = nbPickerHTML();
    el.nbPicker.hidden = false;
    el.attach?.setAttribute("aria-expanded", "true");
    (el.nbPicker.querySelector('[aria-checked]:not([aria-disabled="true"])') || el.nbPicker).focus({ preventScroll: true });
  }

  function closeNbPicker({ refocus = false } = {}) {
    if (!el.nbPicker || el.nbPicker.hidden) return;
    el.nbPicker.hidden = true;
    el.attach?.setAttribute("aria-expanded", "false");
    if (refocus) el.input?.focus({ preventScroll: true });
  }

  function setAttached(ids) {
    state.attached = NB.reconcileAttached(ids, notebooks());
    paintChips();
    paintNbPicker();
    emit("attached");
  }

  function attachNotebook(id) {
    if (!id || state.attached.includes(id)) return;
    if (state.attached.length >= NB.MAX_ATTACHED) {
      deps.toast(`Attach at most ${NB.MAX_ATTACHED} notebooks to a question`, true);
      return;
    }
    setAttached([...state.attached, id]);
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
    closeNbPicker();
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

  // --- asking ---

  // ask sends a question in the current conversation. Every view shows it.
  // Returns false when nothing was sent.
  async function ask(question, opts = {}) {
    const q = String(question || "").trim();
    if (!q || state.busy) return false;
    if (state.keyConfigured === false) {
      eachView((v) => v.paintEmpty());
      if (!state.open) setOpen(true, { focus: false });
      requestAnimationFrame(() => root.querySelector("#agent-key")?.focus());
      return false;
    }
    closePicker();
    closeSessions();
    state.turns++;
    const conv = state.conv;
    const inScope = scopePanes();
    closeNbPicker();
    state.attached = NB.reconcileAttached(state.attached, notebooks());
    const sentNotebooks = state.attached.slice();
    const turns = views.map((v) => [v, v.askStart(q, inScope.length)]);
    const turnModel = { at: new Date().toISOString(), question: q, trace: [], proposals: [], pending: true };
    state.model.turns.push(turnModel);
    if (!state.model.createdAt) state.model.createdAt = turnModel.at;
    const ctlr = new AbortController();
    state.busy = ctlr;
    setBusy(true);
    emit("turn");
    try {
      const res = await deps.api("/api/agent/ask", {
        method: "POST",
        body: JSON.stringify({
          conversationId: conv,
          question: q,
          panes: inScope.map((p) => p.sessionId),
          notebooks: sentNotebooks.length ? sentNotebooks : undefined,
          runbookId: opts.runbookId || undefined,
        }),
        signal: ctlr.signal,
      });
      if (res.conversationId !== state.conv) return true; // a new conversation started meanwhile
      state.saveError = res.saveError || "";
      paintTitle();
      if (res.title) setTitle(res.title);
      turnModel.trace = res.trace || [];
      for (const p of res.proposals || []) registerProposal(p);
      for (const n of res.notes || []) registerNote(n);
      for (const b of res.runbooks || []) registerBookProp(b);
      for (const nb of res.notebooks || []) registerNbProp(nb);
      if (opts.runbookId) state.noRunbook = false;
      if (res.runbook) setRun(res.runbook, { paint: false });
      turnModel.proposals = (res.proposals || []).slice();
      turnModel.notes = (res.notes || []).slice();
      state.model.updatedAt = new Date().toISOString();
      for (const [v, t] of turns) v.askDone(t, res);
      eachView((v) => v.paintRuns());
    } catch (err) {
      if (conv !== state.conv) return true;
      if (err?.name === "AbortError") {
        for (const [v, t] of turns) v.askStopped(t);
      } else {
        for (const [v, t] of turns) v.askFailed(t, err);
        if (err?.code === 412) {
          state.keyConfigured = false;
          state.provider = null;
          loadSettings();
        }
      }
    } finally {
      turnModel.pending = false;
      if (state.busy === ctlr) {
        state.busy = null;
        setBusy(false);
      }
      if (conv === state.conv) emit("turn");
    }
    return true;
  }

  function setBusy(on) {
    if (el.stop) el.stop.hidden = !on;
    if (el.send) el.send.disabled = on;
    eachView((v) => v.paintRuns());
    emit("busy");
  }

  // --- proposals ---

  function registerProposal(p) {
    const status = p.typedAt && !p.expired ? { ok: true, text: `Typed into ${p.host || "the pane"} at ${A.stampLabel(p.typedAt)}.` } : null;
    state.props.set(p.id, { p, status });
  }

  function repaintProposal(id) {
    eachView((v) => v.paintCard(id));
    emit("proposal");
  }

  // --- notes the agent proposes (§15) ---
  //
  // A proposal is only a suggestion: keel serve keeps nothing until Save
  // here (POST …/notes/{id}, with the user's edit if any). Dismiss and Undo
  // change only its state; Undo after Save deletes the note again.

  function registerNote(n) {
    state.notes.set(n.id, { n, busy: false, error: "" });
  }

  function repaintNote(id) {
    eachView((v) => v.paintNote(id));
    emit("note");
  }

  async function saveNote(id, text) {
    const rec = state.notes.get(id);
    if (!rec || rec.busy) return false;
    const conv = state.conv;
    rec.busy = true;
    rec.error = "";
    repaintNote(id);
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/notes/${encodeURIComponent(id)}`, {
        method: "POST",
        body: JSON.stringify({ text: text || "" }),
      });
      if (conv !== state.conv) return false;
      rec.n = res.proposal || { ...rec.n, status: "saved", noteId: res.note?.id };
      if (res.redacted) deps.toast(`Saved. ${res.redacted === 1 ? "1 secret-looking value was" : `${res.redacted} secret-looking values were`} replaced first.`);
      return true;
    } catch (err) {
      rec.error = err?.message || String(err);
      return false;
    } finally {
      rec.busy = false;
      if (conv === state.conv) repaintNote(id);
    }
  }

  async function noteAction(id, method, suffix) {
    const rec = state.notes.get(id);
    if (!rec || rec.busy) return;
    const conv = state.conv;
    rec.busy = true;
    rec.error = "";
    repaintNote(id);
    try {
      const n = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/notes/${encodeURIComponent(id)}${suffix}`, { method });
      if (conv !== state.conv) return;
      rec.n = n;
    } catch (err) {
      rec.error = err?.message || String(err);
    } finally {
      rec.busy = false;
      if (conv === state.conv) repaintNote(id);
    }
  }

  async function typeProposal(id, shift) {
    const rec = state.props.get(id);
    if (!rec) return null;
    const pane = paneById(rec.p.sessionId);
    const gate = A.typeGate(rec.p, pane, shift);
    if (!gate.ok) {
      if (gate.reason === "shift") {
        rec.status = { ok: false, text: `Not typed. ${A.shiftReason(rec.p)}: hold ${shiftWord} and press Type.` };
        repaintProposal(id);
        return { refocus: true };
      }
      return null;
    }
    const res = await deps.typeProposal({ proposal: rec.p, conversationId: state.conv, shift: !!shift });
    if (!res) return null;
    if (res.ok && rec.p.runbook) schedulePoll(true);
    if (res.ok) rec.status = { ok: true, text: `Typed into ${rec.p.host || "the pane"}.` };
    else if (res.cancelled) rec.status = null;
    else rec.status = { ok: false, text: res.message || "Not typed." };
    repaintProposal(id);
    return null;
  }

  // proposeAgain asks for a fresh proposal from a saved one. The answer is
  // dropped when the conversation changed while it was asked (resume, new,
  // delete): it belongs to the old one, not to what the views show now.
  async function proposeAgain(id) {
    const conv = state.conv;
    try {
      const p = await deps.api(
        `/api/agent/conversations/${encodeURIComponent(conv)}/proposals/${encodeURIComponent(id)}/again`,
        { method: "POST" },
      );
      if (conv !== state.conv) return null;
      registerProposal(p);
      state.model.turns.push({ at: new Date().toISOString(), again: true, trace: [], proposals: [p] });
      eachView((v) => v.addProposal(p.id));
      emit("proposal");
      return p.id;
    } catch (err) {
      if (conv === state.conv) deps.toast(err?.message || String(err), true);
      return null;
    }
  }

  async function saveEdit(id, command) {
    const rec = state.props.get(id);
    if (!rec) return false;
    const conv = state.conv;
    try {
      const p = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/proposals/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: JSON.stringify({ command }),
      });
      if (conv !== state.conv) return false;
      rec.p = p;
      rec.status = null;
      for (const t of state.model.turns) t.proposals = (t.proposals || []).map((x) => (x.id === id ? p : x));
      repaintProposal(id);
      return true;
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      return false;
    }
  }

  // --- a runbook in use (§16) ---
  //
  // keel serve owns the run: which step is current, what each step's
  // result was. The panel shows it, asks for the next step (an ordinary
  // ask; keel serve attaches the step's proposal), marks a step done,
  // skipped or to retry, and polls while a typed step waits for the exit
  // status its pane's shell reports (OSC 133; reported, not proof).

  function setRun(run, { paint = true } = {}) {
    state.run = run || null;
    if (run?.id) state.runNames.set(run.id, run.name);
    if (paint) eachView((v) => v.paintRuns());
    schedulePoll();
    emit("runbook");
  }

  function schedulePoll(force = false) {
    if (pollTimer) return;
    if (!force && !R.waitingForShell(state.run)) return;
    if (force || pollLeft <= 0) pollLeft = 400; // about 10 minutes
    const conv = state.conv;
    pollTimer = setTimeout(async () => {
      pollTimer = null;
      if (conv !== state.conv || pollLeft-- <= 0) return;
      try {
        const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/runbook`);
        if (conv !== state.conv) return;
        if (res?.runbook) setRun(res.runbook);
      } catch {
        // a locked vault or a restart: try again on the next change
      }
      if (R.waitingForShell(state.run)) schedulePoll();
    }, 1500);
  }

  function runError(text) {
    state.runError = text || "";
    eachView((v) => v.paintRuns());
  }

  async function proposeStep() {
    const act = R.nextAction(state.run, (pid) => !!state.props.get(pid) && !state.props.get(pid).p.expired);
    if (act.kind !== "propose" && act.kind !== "failed") return;
    if (!scopePanes().some((p) => p.readable !== false)) {
      runError("Put a pane in scope first: a step is proposed only for a pane the agent reads.");
      return;
    }
    runError("");
    await ask(R.continueQuestion(act.step));
  }

  async function stepAction(stepId, action) {
    if (state.runBusy) return;
    const conv = state.conv;
    state.runBusy = true;
    runError("");
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/runbook/steps/${encodeURIComponent(stepId)}`, {
        method: "POST",
        body: JSON.stringify({ action }),
      });
      if (conv !== state.conv) return;
      state.runBusy = false;
      setRun(res.runbook);
      if (action === "retry") await proposeStep();
    } catch (err) {
      if (conv === state.conv) runError(err?.message || String(err));
    } finally {
      state.runBusy = false;
      if (conv === state.conv) eachView((v) => v.paintRuns());
    }
  }

  async function stopRunbook() {
    const conv = state.conv;
    state.runBusy = true;
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/runbook`, { method: "DELETE" });
      if (conv !== state.conv) return;
      state.noRunbook = true;
      state.choosing = false;
      state.runError = "";
      setRun(res.runbook);
    } catch (err) {
      if (conv === state.conv) runError(err?.message || String(err));
    } finally {
      state.runBusy = false;
      if (conv === state.conv) eachView((v) => v.paintRuns());
    }
  }

  // moveRun unpins the run from its host, so the next step may go to a
  // pane on another host the runbook is for (the user's choice only).
  async function moveRun() {
    const conv = state.conv;
    state.runBusy = true;
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/runbook/move`, { method: "POST" });
      if (conv !== state.conv) return;
      state.runError = "";
      setRun(res.runbook);
    } catch (err) {
      if (conv === state.conv) runError(err?.message || String(err));
    } finally {
      state.runBusy = false;
      if (conv === state.conv) eachView((v) => v.paintRuns());
    }
  }

  async function useRunbook(id, name) {
    const label = name || state.others?.find((o) => o.id === id)?.name || (state.run?.runbookId === id ? state.run.name : "");
    state.choosing = false;
    state.runError = "";
    if (!state.open) setOpen(true, { focus: false });
    // The scope comes from the settings: read them first, so a panel
    // opened just now asks with its panes in scope.
    if (!state.settings) await loadSettings();
    refresh();
    return ask(R.startQuestion({ name: label || "this runbook" }), { runbookId: id });
  }

  async function toggleChoose() {
    state.choosing = !state.choosing;
    state.others = null;
    eachView((v) => v.paintRuns());
    if (!state.choosing) return;
    try {
      const res = await deps.api("/api/agent/runbooks");
      const inScope = scopePanes();
      state.others = (res?.runbooks || [])
        .filter((rb) => rb.id !== state.run?.runbookId || state.noRunbook)
        .map((rb) => ({ id: rb.id, name: rb.name, when: rb.when || "", ...R.offerFor(rb, inScope) }));
    } catch (err) {
      state.others = [];
      state.runError = err?.message || String(err);
    }
    eachView((v) => v.paintRuns());
  }

  // --- runbooks the agent proposes (§16) ---

  function registerBookProp(b) {
    state.bookProps.set(b.id, { b, busy: false, error: "" });
  }

  function repaintBookProp(id) {
    eachView((v) => v.paintBookProp(id));
  }

  async function saveBookProp(id) {
    const rec = state.bookProps.get(id);
    if (!rec || rec.busy) return;
    const conv = state.conv;
    rec.busy = true;
    rec.error = "";
    repaintBookProp(id);
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/runbooks/${encodeURIComponent(id)}`, { method: "POST" });
      if (conv !== state.conv) return;
      rec.b = res.proposal || { ...rec.b, status: "saved", runbookId: res.runbook?.id };
      if (res.redacted) deps.toast(`Saved. ${res.redacted === 1 ? "1 secret-looking value was" : `${res.redacted} secret-looking values were`} replaced first.`);
    } catch (err) {
      rec.error = err?.message || String(err);
    } finally {
      rec.busy = false;
      if (conv === state.conv) repaintBookProp(id);
    }
  }

  async function bookPropAction(id, method, suffix) {
    const rec = state.bookProps.get(id);
    if (!rec || rec.busy) return;
    const conv = state.conv;
    rec.busy = true;
    rec.error = "";
    repaintBookProp(id);
    try {
      const b = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/runbooks/${encodeURIComponent(id)}${suffix}`, { method });
      if (conv !== state.conv) return;
      rec.b = b;
    } catch (err) {
      rec.error = err?.message || String(err);
    } finally {
      rec.busy = false;
      if (conv === state.conv) repaintBookProp(id);
    }
  }

  // --- notebook drafts the agent proposes (§18) ---

  function registerNbProp(p) {
    state.nbProps.set(p.id, { p, busy: false, error: "" });
  }

  function repaintNbProp(id) {
    eachView((v) => v.paintNbProp(id));
  }

  // openNbDraft asks keel serve for the draft and its draftId, and opens
  // it in the notebook editor as an unsaved draft. Nothing is saved here:
  // the editor's Save notebook does that, with the draftId.
  async function openNbDraft(id) {
    const rec = state.nbProps.get(id);
    if (!rec || rec.busy) return;
    const conv = state.conv;
    rec.busy = true;
    rec.error = "";
    repaintNbProp(id);
    try {
      const d = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/notebooks/${encodeURIComponent(id)}/draft`, { method: "POST" });
      if (conv !== state.conv) return;
      deps.openNotebookDraft?.(d, {
        onSaved: (res) => {
          const cur = state.nbProps.get(id);
          if (!cur || conv !== state.conv) return;
          cur.p = res?.proposal || { ...cur.p, status: "saved", notebookId: res?.notebook?.id };
          repaintNbProp(id);
        },
      });
    } catch (err) {
      rec.error = err?.code === 410 ? "This draft expired with the conversation; ask the agent again." : err?.message || String(err);
    } finally {
      rec.busy = false;
      if (conv === state.conv) repaintNbProp(id);
    }
  }

  async function nbPropAction(id, method, suffix) {
    const rec = state.nbProps.get(id);
    if (!rec || rec.busy) return;
    const conv = state.conv;
    rec.busy = true;
    rec.error = "";
    repaintNbProp(id);
    try {
      const p = await deps.api(`/api/agent/conversations/${encodeURIComponent(conv)}/notebooks/${encodeURIComponent(id)}${suffix}`, { method });
      if (conv !== state.conv) return;
      rec.p = p;
    } catch (err) {
      rec.error = err?.message || String(err);
    } finally {
      rec.busy = false;
      if (conv === state.conv) repaintNbProp(id);
    }
  }

  function resetRun() {
    clearTimeout(pollTimer);
    pollTimer = null;
    state.run = null;
    state.noRunbook = false;
    state.runNames = new Map();
    state.runBusy = false;
    state.runError = "";
    state.choosing = false;
    state.others = null;
    state.bookProps.clear();
    state.nbProps.clear();
    state.attached = [];
  }

  // --- new conversation ---

  // newConversation starts a fresh thread. The old one stays: it is saved
  // in the vault (with history on) and listed under the title button.
  function newConversation({ focus = true } = {}) {
    state.busy?.abort();
    state.conv = A.conversationId();
    state.turns = 0;
    state.title = "";
    state.props.clear();
    state.notes.clear();
    resetRun();
    state.model = { createdAt: "", updatedAt: "", turns: [] };
    state.scopeAuto = true;
    if (state.settings) state.scope = A.defaultScope(state.settings.defaultScope, panes());
    if (el.input) el.input.placeholder = INPUT_HINT;
    eachView((v) => {
      v.clear();
      v.paintEmpty();
    });
    paintTitle();
    refresh();
    emit("reset");
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
      state.saveError = res?.saveError || "";
      state.unreadable = !!res?.unreadable;
      state.sessionsError = "";
      paintTitle();
    } catch (err) {
      state.sessions = [];
      state.sessionsError = err?.code === 423 ? "Unlock the vault to see saved conversations." : err?.message || String(err);
    }
    paintSessions();
    emit("sessions");
  }

  // sessionsEmptyText says why a list shows no rows, or "" when it has
  // some: the page and the panel say the same.
  function sessionsEmptyText(groups) {
    if (state.sessionsError) return state.sessionsError;
    if (state.unreadable) return "Saved conversations cannot be opened: keel.chats cannot be read. New ones are not saved until Delete all starts a fresh file.";
    if (groups.length) return "";
    if (state.sessions.length) return "No conversation matches.";
    if (state.historyDays === 0) return "History is off: conversations are kept only while the panel is open. Turn it on in Settings · Agent.";
    return "No saved conversations.";
  }

  function paintSessions() {
    if (!sessionsOpen()) return;
    const days = state.historyDays;
    setText(el.sessNote, days == null ? "" : A.retentionNote(days));
    if (el.sessClear) el.sessClear.disabled = !state.sessions.length && !state.unreadable;
    const groups = state.unreadable || state.sessionsError ? [] : A.sessionGroups(state.sessions, el.sessFilter?.value || "");
    const empty = sessionsEmptyText(groups);
    if (empty) {
      el.sessList.innerHTML = `<p class="k-help k-agent-sessions-empty"${state.unreadable ? ' role="alert"' : ""}>${escapeHtml(empty)}</p>`;
      return;
    }
    el.sessList.innerHTML = sessionGroupsHTML(groups, (s) => ({ current: s.id === state.conv, renaming: state.renaming === s.id }));
    if (state.renaming) {
      const input = el.sessList.querySelector("[data-sess-title]");
      input?.focus({ preventScroll: true });
      input?.select();
    }
  }

  // resume opens a saved conversation. Nothing is read or sent: the
  // transcript comes from keel serve, and the next ask reads the panes in
  // scope as they are then.
  async function resume(id, { focus = true } = {}) {
    if (!id) return false;
    if (id === state.conv && state.turns > 0) {
      closeSessions();
      if (focus) el.input?.focus({ preventScroll: true });
      return true;
    }
    let t;
    try {
      t = await deps.api(`/api/agent/conversations/${encodeURIComponent(id)}`);
    } catch (err) {
      deps.toast(err?.code === 404 ? "That conversation is gone." : err?.message || String(err), true);
      loadSessions();
      return false;
    }
    closeSessions();
    state.busy?.abort();
    state.conv = t.id;
    state.title = t.title || "";
    state.props.clear();
    state.notes.clear();
    resetRun();
    state.model = {
      createdAt: t.createdAt || "",
      updatedAt: t.updatedAt || "",
      turns: (t.turns || []).map((turn) => ({ at: turn.at, question: turn.question, again: !!turn.again, trace: turn.trace || [], proposals: turn.proposals || [], notes: turn.notes || [] })),
    };
    for (const turn of t.turns || []) for (const p of turn.proposals || []) registerProposal(p);
    for (const turn of t.turns || []) for (const n of turn.notes || []) registerNote(n);
    for (const turn of t.turns || []) for (const b of turn.runbooks || []) registerBookProp(b);
    for (const turn of t.turns || []) for (const nb of turn.notebooks || []) registerNbProp(nb);
    state.noRunbook = !!t.noRunbook;
    if (t.runbook) setRun(t.runbook, { paint: false });
    eachView((v) => v.renderTranscript(t));
    state.turns = Math.max(1, state.model.turns.filter((x) => !x.again).length);
    if (el.input) el.input.placeholder = "Continue this conversation…";
    paintTitle();
    refresh();
    emit("reset");
    emit("title");
    if (focus) el.input?.focus({ preventScroll: true });
    return true;
  }

  async function renameSession(id, title) {
    const t = String(title || "").trim();
    if (!t) return false;
    try {
      const res = await deps.api(`/api/agent/conversations/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ title: t }) });
      const s = state.sessions.find((x) => x.id === id);
      if (s) s.title = res.title;
      if (id === state.conv) setTitle(res.title);
      emit("sessions");
      return true;
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      return false;
    }
  }

  async function panelRename(id, title) {
    await renameSession(id, title);
    state.renaming = null;
    paintSessions();
    el.sessList.querySelector(`[data-sess-rename="${CSS.escape(id)}"]`)?.focus({ preventScroll: true });
  }

  async function deleteSession(id) {
    try {
      await deps.api(`/api/agent/conversations/${encodeURIComponent(id)}`, { method: "DELETE" });
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      return false;
    }
    state.sessions = state.sessions.filter((s) => s.id !== id);
    if (id === state.conv) newConversation({ focus: false });
    paintSessions();
    emit("sessions");
    deps.toast("Conversation deleted");
    return true;
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
    state.saveError = "";
    state.unreadable = false;
    newConversation({ focus: false });
    paintSessions();
    emit("sessions");
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
  el.sessPage?.addEventListener("click", () => {
    closeSessions();
    deps.openPage?.();
  });
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
    if (del) {
      deleteSession(del).then((ok) => {
        if (ok) (el.sessList.querySelector("[data-sess-pick]") || el.sessFilter)?.focus({ preventScroll: true });
      });
    }
  });
  el.sessList?.addEventListener("submit", (ev) => {
    const form = ev.target.closest("[data-sess-rename-form]");
    if (!form) return;
    ev.preventDefault();
    panelRename(form.dataset.sessRenameForm, form.querySelector("[data-sess-title]")?.value);
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
    const nb = ev.target.closest("[data-agent-detach]")?.dataset.agentDetach;
    if (nb) setAttached(state.attached.filter((x) => x !== nb));
  });

  // Attach notebook (§18): a menu of saved notebooks; the ticked ones go
  // with each question until removed.
  el.attach?.addEventListener("click", () => (el.nbPicker?.hidden ? openNbPicker() : closeNbPicker({ refocus: true })));
  el.nbPicker?.addEventListener("click", (ev) => {
    const pick = ev.target.closest("[data-agent-attach-pick]");
    if (!pick || pick.getAttribute("aria-disabled") === "true") return;
    setAttached(NB.toggleAttached(state.attached, pick.dataset.agentAttachPick));
    el.nbPicker.querySelector(`[data-agent-attach-pick="${CSS.escape(pick.dataset.agentAttachPick)}"]`)?.focus({ preventScroll: true });
  });
  el.nbPicker?.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      closeNbPicker({ refocus: true });
      return;
    }
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      const items = [...el.nbPicker.querySelectorAll("button")];
      const i = items.indexOf(doc.activeElement);
      const next = items[(i + (ev.key === "ArrowDown" ? 1 : -1) + items.length) % items.length];
      next?.focus({ preventScroll: true });
      ev.preventDefault();
    }
  });
  doc.addEventListener("pointerdown", (ev) => {
    if (!el.nbPicker || el.nbPicker.hidden) return;
    if (el.nbPicker.contains(ev.target) || el.attach?.contains(ev.target)) return;
    closeNbPicker();
  });

  el.composer.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const q = el.input.value;
    if (!q.trim()) return;
    el.input.value = "";
    // Nothing sent (busy, or no key yet): give the words back.
    ask(q).then((sent) => {
      if (sent === false && !el.input.value) el.input.value = q;
    });
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
  panelView.paintEmpty();

  // conversation is the window's one conversation, for the Agent tab page:
  // it reads the state, adds its own thread view, and changes the
  // conversation only through these calls, so the panel follows.
  const conversation = {
    subscribe(fn) {
      subs.add(fn);
      return () => subs.delete(fn);
    },
    // addView draws the conversation in another thread element. It starts
    // with what the conversation holds now.
    addView(opts) {
      const v = createThreadView({ doc, ctl, isMac: deps.isMac, ...opts });
      views.push(v);
      v.paintEmpty();
      return v;
    },
    snapshot: () => ({
      id: state.conv,
      title: state.title,
      model: state.model,
      busy: !!state.busy,
      scope: scopePanes(),
      keyConfigured: state.keyConfigured,
      historyDays: state.historyDays,
      saveError: state.saveError,
      unreadable: state.unreadable,
      sessions: state.sessions,
      sessionsError: state.sessionsError,
      settings: state.settings,
    }),
    proposal: ctl.proposal,
    again: proposeAgain,
    // Notebooks attached to the next questions (§18).
    attached: () => attachedNotebooks(),
    attachNotebook,
    detachNotebook: (id) => setAttached(state.attached.filter((x) => x !== id)),
    toggleNotebook: (id) => setAttached(NB.toggleAttached(state.attached, id)),
    attachedChipsHTML,
    nbPickerHTML,
    // openNbDraft is a card's Open draft (also from the page's view).
    openNbDraft,
    sessionsEmptyText,
    ask,
    stop: () => state.busy?.abort(),
    resume: (id) => resume(id, { focus: false }),
    newConversation: () => newConversation({ focus: false }),
    rename: renameSession,
    remove: deleteSession,
    loadSessions,
    loadSettings,
  };

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
    conversation,
    // useRunbook starts a runbook in the window's conversation (the
    // Runbooks page's Use in agent): an ask with its id, never a type.
    useRunbook: (rb) => useRunbook(rb.id, rb.name),
    // attachNotebook opens the panel with a notebook attached (a notebook
    // pane's Ask the agent about this notebook). Nothing is sent until the
    // user asks.
    attachNotebook(id) {
      setOpen(true, { focus: false });
      attachNotebook(id);
      requestAnimationFrame(() => el.input?.focus({ preventScroll: true }));
    },
    // notebooksChanged reconciles the attached ids when the list changes.
    notebooksChanged() {
      const before = state.attached.join(",");
      state.attached = NB.reconcileAttached(state.attached, notebooks());
      if (before !== state.attached.join(",")) emit("attached");
      paintChips();
      paintNbPicker();
    },
    attached: () => state.attached.slice(),
    // test seam for node: the current scope ids
    scope: () => state.scope.slice(),
  };
}
