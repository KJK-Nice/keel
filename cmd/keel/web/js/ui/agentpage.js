// The Agent tab page (docs/ui-redesign.md §5.4.2, board AgentPage; shell in
// internal/ui/agentpage.templ): saved conversations on the left, the
// conversation in the middle, what it may read, has read and proposed on
// the right.
//
// The page has no conversation of its own. It shows the window's one
// conversation (agentpanel.js `conversation`) in a second thread view, and
// every change (resume, ask, rename, delete, Type, Propose again) goes
// through that conversation, so the agent panel shows the same thing. Like
// the panel it never writes to a pane: Type into <pane> goes through the
// approve-before-type dialog that term.js opens. Every server string is
// escaped (escapeHtml) or set as text.

import { escapeHtml } from "../lib/html.js";
import * as A from "../lib/agent.js";
import { setText } from "../lib/dom.js";
import { ICON, sessionGroupsHTML } from "./agentthread.js";

const NEW_TITLE = "New conversation";

// createAgentPage wires the page. deps:
//   conversation     the panel's conversation API
//   isMac
//   isShown()        the page is the selected tab
//   openPanel()      Continue in panel
//   openSettings()   Retention settings (Settings · Agent)
//   doc
export function createAgentPage(deps) {
  const doc = deps.doc || document;
  const $ = (id) => doc.getElementById(id);
  const root = $("page-agent");
  const conv = deps.conversation;
  if (!root || !conv) return null;
  const el = {
    newBtn: $("agentpage-new"),
    filter: $("agentpage-filter"),
    hosts: $("agentpage-hosts"),
    list: $("agentpage-list"),
    retention: $("agentpage-retention"),
    retentionSettings: $("agentpage-retention-settings"),
    title: $("agentpage-title"),
    renameForm: $("agentpage-rename-form"),
    renameInput: $("agentpage-rename-input"),
    meta: $("agentpage-meta"),
    panel: $("agentpage-panel"),
    panelKbd: $("agentpage-panel-kbd"),
    rename: $("agentpage-rename"),
    remove: $("agentpage-delete"),
    thread: $("agentpage-thread"),
    composer: $("agentpage-composer"),
    input: $("agentpage-input"),
    chips: $("agentpage-chips"),
    attach: $("agentpage-attach"),
    nbPicker: $("agentpage-nb-picker"),
    send: $("agentpage-send"),
    stop: $("agentpage-stop"),
    scope: $("agentpage-scope"),
    scopeCount: $("agentpage-scope-count"),
    reads: $("agentpage-reads"),
    props: $("agentpage-props"),
    propsCount: $("agentpage-props-count"),
  };
  const state = { host: "", renaming: false };
  if (el.panelKbd) el.panelKbd.textContent = deps.isMac ? "⌘I" : "Ctrl+⇧I";

  function emptyHTML() {
    const s = conv.snapshot();
    if (s.keyConfigured === false) {
      return `<div class="k-agent-empty">${ICON.ai}
        <p class="k-agent-empty-title">Connect Gemini first</p>
        <p class="k-help">Add a key in the agent panel or in Settings · Agent. Nothing is sent until then.</p>
        <button type="button" class="k-btn k-btn--sm k-agentpage-empty-action" data-agentpage-open-panel>Open the agent panel</button>
      </div>`;
    }
    const lines = s.settings?.scrollbackLines || 200;
    return `<div class="k-agent-empty">${ICON.ai}
      <p class="k-agent-empty-title">Ask about your sessions</p>
      <p class="k-help">Pick a conversation on the left, or ask a new question. The agent reads the last ${lines} lines of each pane in scope, redacts secrets, and answers. Every read is written to the log.</p>
    </div>`;
  }

  const view = conv.addView({
    el: el.thread,
    emptyHTML,
    // The page is away from the panes, so the button names the pane it
    // types into.
    typeLabel: (p, pane) => `Type into ${pane ? A.paneTarget(pane.label, pane.sub) : p.host || "pane"}`,
  });

  // --- left column: the saved conversations ---

  function paintHosts(sessions) {
    const hosts = A.hostFilters(sessions);
    if (state.host && !hosts.includes(state.host)) state.host = "";
    if (!hosts.length) {
      el.hosts.innerHTML = "";
      el.hosts.hidden = true;
      return;
    }
    el.hosts.hidden = false;
    el.hosts.innerHTML = ["", ...hosts]
      .map(
        (h) =>
          `<button type="button" class="k-chip" data-agentpage-host="${escapeHtml(h)}" aria-pressed="${state.host === h}">${escapeHtml(h || "All")}</button>`,
      )
      .join("");
  }

  function paintList() {
    const s = conv.snapshot();
    paintHosts(s.unreadable || s.sessionsError ? [] : s.sessions);
    const groups = s.unreadable || s.sessionsError ? [] : A.sessionGroups(s.sessions, el.filter?.value || "", new Date(), state.host);
    let empty = conv.sessionsEmptyText(groups);
    if (empty && state.host && s.sessions.length && !s.sessionsError && !s.unreadable) empty = "No conversations on this host.";
    if (empty) {
      el.list.innerHTML = `<p class="k-help k-agent-sessions-empty"${s.unreadable ? ' role="alert"' : ""}>${escapeHtml(empty)}</p>`;
    } else {
      el.list.innerHTML = sessionGroupsHTML(groups, (row) => ({ current: row.id === s.id, actions: false }));
    }
    paintRetention(s);
  }

  function paintRetention(s = conv.snapshot()) {
    const days = s.historyDays;
    let text = "";
    if (days != null) {
      if (days <= 0) text = "Not saved · kept while open";
      else if (s.saveError) text = `Not saved: ${s.saveError}`;
      else text = `Saved encrypted in the vault · ${A.retentionNote(days).toLowerCase()}`;
    }
    setText(el.retention, text);
  }

  // --- middle: the conversation's header and composer ---

  function paintHead() {
    const s = conv.snapshot();
    const turns = s.model.turns;
    const questions = turns.filter((t) => !t.again).length;
    if (!state.renaming) {
      setText(el.title, s.title || NEW_TITLE);
      el.title.hidden = false;
      el.renameForm.hidden = true;
    }
    const hosts = A.conversationHosts(turns)
      .slice(0, 4)
      .map((h) => `<span class="k-badge k-badge--mono">${escapeHtml(h)}</span>`)
      .join("");
    const when = questions ? A.startedLabel(s.model.createdAt || turns[0]?.at, questions) : "No questions yet";
    el.meta.innerHTML = `${hosts}<span>${escapeHtml(when)}</span>`;
    // Rename and Delete need a conversation keel serve knows: one that was
    // asked or resumed.
    const known = questions > 0 || turns.length > 0;
    el.rename.disabled = !known;
    el.remove.disabled = !known;
    if (el.input) el.input.placeholder = questions ? "Continue this conversation…" : "Ask about your sessions…";
  }

  function syncComposer() {
    const busy = conv.snapshot().busy;
    if (el.stop) el.stop.hidden = !busy;
    if (el.send) el.send.disabled = busy;
  }

  function startRename() {
    const s = conv.snapshot();
    if (el.rename.disabled) return;
    state.renaming = true;
    el.title.hidden = true;
    el.renameForm.hidden = false;
    el.renameInput.value = s.title || "";
    el.renameInput.focus({ preventScroll: true });
    el.renameInput.select();
  }

  function endRename({ refocus = true } = {}) {
    state.renaming = false;
    paintHead();
    if (refocus) el.rename.focus({ preventScroll: true });
  }

  // --- right column: scope, reads, proposals ---

  function ctxRow(name, extra) {
    return `<div class="k-agentpage-ctx-row"><span class="k-agentpage-ctx-name">${escapeHtml(name)}</span>${extra}</div>`;
  }

  function paintContext() {
    const s = conv.snapshot();
    setText(el.scopeCount, String(s.scope.length));
    el.scope.innerHTML = s.scope.length
      ? s.scope
          .map(
            (p) =>
              `<div class="k-agentpage-ctx-row"><span class="k-dot k-dot--live" aria-hidden="true"></span><span class="k-agentpage-ctx-name">${escapeHtml(A.paneTarget(p.label, p.sub))}</span>${
                p.production ? '<span class="k-badge k-badge--signal">Production</span>' : ""
              }<span class="k-badge k-badge--haze">Agent reads</span></div>`,
          )
          .join("")
      : `<p class="k-help k-agentpage-ctx-empty">No pane in scope. Choose panes in the agent panel.</p>`;

    const reads = A.readSummary(s.model.turns);
    el.reads.innerHTML = reads.length
      ? reads.map((r) => ctxRow(r.host, `<span>${escapeHtml(r.linesLabel)}</span><span class="k-agentpage-ctx-faint">${escapeHtml(r.redactedLabel)}</span>`)).join("")
      : `<p class="k-help k-agentpage-ctx-empty">Nothing read yet.</p>`;

    const seen = new Set();
    const props = [];
    for (const t of s.model.turns) {
      for (const p of t.proposals || []) {
        if (seen.has(p.id)) continue;
        seen.add(p.id);
        props.push(conv.proposal(p.id) || { p, status: null });
      }
    }
    setText(el.propsCount, String(props.length));
    el.props.innerHTML = props.length
      ? props
          .map(({ p, status }) => {
            const st = A.proposalState(p, !!status?.ok);
            const cmd = String(p.command || "").replace(/\s+/g, " ").trim();
            return ctxRow(cmd, `<span class="k-badge ${st.cls}">${escapeHtml(st.label)}</span>`);
          })
          .join("")
      : `<p class="k-help k-agentpage-ctx-empty">No proposals yet.</p>`;
  }

  function paintAll() {
    paintList();
    paintHead();
    paintContext();
    syncComposer();
  }

  // show runs when the page becomes the selected tab: the list and the
  // settings may have changed meanwhile.
  function show() {
    conv.loadSettings();
    conv.loadSessions();
    paintAll();
  }

  // --- notebooks attached to the question (§18), the panel's ---

  function paintAttached() {
    if (el.chips) el.chips.innerHTML = conv.attachedChipsHTML?.() || "";
    if (el.nbPicker && !el.nbPicker.hidden) el.nbPicker.innerHTML = conv.nbPickerHTML?.() || "";
  }

  function openNbPicker() {
    if (!el.nbPicker) return;
    el.nbPicker.innerHTML = conv.nbPickerHTML?.() || "";
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

  conv.subscribe((type) => {
    switch (type) {
      case "attached":
        paintAttached();
        break;
      case "sessions":
      case "settings":
        paintList();
        if (type === "settings") paintHead();
        break;
      case "reset":
        state.renaming = false;
        paintAll();
        paintAttached();
        break;
      case "title":
        paintHead();
        break;
      case "turn":
        paintHead();
        paintContext();
        syncComposer();
        // A finished ask saved the conversation: list it.
        if (!conv.snapshot().busy) {
          if (deps.isShown?.()) conv.loadSessions();
        }
        break;
      case "busy":
        syncComposer();
        break;
      case "scope":
      case "proposal":
        paintContext();
        break;
    }
  });

  // --- events ---

  el.newBtn?.addEventListener("click", () => {
    conv.newConversation();
    el.input?.focus({ preventScroll: true });
  });
  el.filter?.addEventListener("input", () => paintList());
  el.hosts?.addEventListener("click", (ev) => {
    const b = ev.target.closest("[data-agentpage-host]");
    if (!b) return;
    state.host = b.dataset.agentpageHost || "";
    paintList();
  });
  el.list?.addEventListener("click", async (ev) => {
    const id = ev.target.closest("[data-sess-pick]")?.dataset.sessPick;
    if (!id) return;
    if (await conv.resume(id)) el.list.querySelector(`[data-sess-pick="${CSS.escape(id)}"]`)?.focus({ preventScroll: true });
  });
  // ↑↓ move between the search field and the rows.
  root.querySelector(".k-agentpage-list-col")?.addEventListener("keydown", (ev) => {
    if (ev.key !== "ArrowDown" && ev.key !== "ArrowUp") return;
    if (ev.target !== el.filter && !ev.target.closest?.("[data-sess-pick]")) return;
    const items = [el.filter, ...el.list.querySelectorAll("[data-sess-pick]")].filter(Boolean);
    const i = items.indexOf(doc.activeElement);
    const next = items[(i + (ev.key === "ArrowDown" ? 1 : -1) + items.length) % items.length];
    next?.focus({ preventScroll: true });
    ev.preventDefault();
  });
  el.retentionSettings?.addEventListener("click", () => deps.openSettings?.());

  el.panel?.addEventListener("click", () => deps.openPanel?.());
  el.rename?.addEventListener("click", () => startRename());
  el.renameForm?.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const s = conv.snapshot();
    const title = el.renameInput.value.trim();
    if (title && title !== s.title) await conv.rename(s.id, title);
    endRename();
  });
  el.renameInput?.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    ev.preventDefault();
    ev.stopPropagation();
    endRename();
  });
  el.remove?.addEventListener("click", async () => {
    const s = conv.snapshot();
    if (await conv.remove(s.id)) el.filter?.focus({ preventScroll: true });
  });

  el.composer?.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const q = el.input.value;
    if (!q.trim()) return;
    el.input.value = "";
    // Nothing sent (busy, or no key yet): give the words back.
    conv.ask(q).then((sent) => {
      if (sent === false && !el.input.value) el.input.value = q;
    });
  });
  el.input?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing && !ev.altKey && !ev.metaKey && !ev.ctrlKey) {
      ev.preventDefault();
      el.composer.requestSubmit();
    }
  });
  el.stop?.addEventListener("click", () => conv.stop());
  el.attach?.addEventListener("click", () => (el.nbPicker?.hidden ? openNbPicker() : closeNbPicker({ refocus: true })));
  el.nbPicker?.addEventListener("click", (ev) => {
    const pick = ev.target.closest("[data-agent-attach-pick]");
    if (!pick || pick.getAttribute("aria-disabled") === "true") return;
    conv.toggleNotebook?.(pick.dataset.agentAttachPick);
    el.nbPicker.querySelector(`[data-agent-attach-pick="${CSS.escape(pick.dataset.agentAttachPick)}"]`)?.focus({ preventScroll: true });
  });
  el.nbPicker?.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      closeNbPicker({ refocus: true });
    }
  });
  doc.addEventListener("pointerdown", (ev) => {
    if (!el.nbPicker || el.nbPicker.hidden) return;
    if (el.nbPicker.contains(ev.target) || el.attach?.contains(ev.target)) return;
    closeNbPicker();
  });
  el.chips?.addEventListener("click", (ev) => {
    const id = ev.target.closest("[data-agent-detach]")?.dataset.agentDetach;
    if (id) conv.detachNotebook?.(id);
  });
  el.thread?.addEventListener("click", (ev) => {
    if (ev.target.closest("[data-agentpage-open-panel]")) deps.openPanel?.();
  });

  paintAll();
  paintAttached();

  return {
    show,
    // newConversation is ⌘N while the page is the selected tab.
    newConversation() {
      conv.newConversation();
      el.input?.focus({ preventScroll: true });
    },
    view,
  };
}
