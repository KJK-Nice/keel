// The Runbooks tab page (docs/ui-redesign.md §16, board Runbooks; shell in
// internal/ui/runbookspage.templ).
//
// A runbook is a checklist the user curates: ordered steps, each with a
// title, a description, an optional command and an optional expected
// result. The page lists them, edits one at a time, makes drafts from a
// notebook or a saved conversation, and starts one in the agent panel
// (Use in agent). Every save goes through keel serve (POST/PUT
// /api/agent/runbooks), which redacts the text for secrets before it is
// kept in the vault and logs agent.runbook; the editor then shows what was
// kept. Nothing here talks to the agent's provider, reads a pane or types:
// a step reaches a pane only as a proposal the user types through the
// approve-before-type dialog.

import { escapeHtml } from "../lib/html.js";
import * as R from "../lib/runbooks.js";
import { sessionWhen } from "../lib/agent.js";
import { ICON } from "./agentthread.js";

const UP = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 15C18 15 13.58 9 12 9C10.42 9 6 15 6 15"/></svg>';
const DOWN = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M18 9C18 9 13.58 15 12 15C10.42 15 6 9 6 9"/></svg>';
const ADD = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" aria-hidden="true"><path d="M12 5V19"/><path d="M19 12H5"/></svg>';

// createRunbooksPage wires the page. deps:
//   api(path, opts), toast(msg, isError), confirm(msg, opts) → Promise<bool>
//   isMac
//   notebooks()      → [{id, name}] (the vault's notebooks)
//   useRunbook(rb)   starts it in the agent panel (an ask, never a type)
export function createRunbooksPage(deps) {
  const doc = deps.doc || document;
  const $ = (id) => doc.getElementById(id);
  const root = $("page-runbooks");
  if (!root) return null;
  const el = {
    list: $("runbooks-list"),
    detail: $("runbooks-detail"),
    filter: $("runbooks-filter"),
    count: $("runbooks-count"),
    newBtn: $("runbooks-new"),
    fromNotebook: $("runbooks-from-notebook"),
    fromChat: $("runbooks-from-chat"),
    menu: $("runbooks-source-menu"),
  };
  const state = {
    list: [],
    unreadable: false,
    error: "",
    selected: "", // runbook id, or "" for a new (unsaved) one when ed is set
    ed: null, // the editor's state: { id, name, when, hosts, agentMayPick, steps, source, sourceRef, ... }
    dirty: false,
    busy: false,
    menu: "", // "notebook" | "chat" | ""
    loaded: false,
  };
  const saveKey = deps.isMac ? "⌘S" : "Ctrl+S";

  // --- loading ---

  async function load() {
    try {
      const res = await deps.api("/api/agent/runbooks");
      state.list = res?.runbooks || [];
      state.unreadable = !!res?.unreadable;
      state.error = "";
    } catch (err) {
      state.list = [];
      state.error = err?.code === 423 ? "Unlock the vault to see runbooks." : err?.message || String(err);
    }
    state.loaded = true;
    paintList();
    if (state.selected && !state.dirty) {
      const rb = state.list.find((b) => b.id === state.selected);
      if (rb) setEditor(rb);
      else closeEditor();
    }
  }

  // --- the list ---

  function paintList() {
    const shown = R.filterRunbooks(state.list, el.filter?.value || "");
    if (el.filter) el.filter.placeholder = `Filter ${state.list.length} ${state.list.length === 1 ? "runbook" : "runbooks"}`;
    if (el.count) el.count.textContent = state.unreadable ? "" : `${state.list.length} ${state.list.length === 1 ? "runbook" : "runbooks"} · stored in the vault`;
    if (state.error || state.unreadable) {
      const msg = state.unreadable ? "Runbooks cannot be opened: keel.memory cannot be read. Delete all notes in Settings · Agent starts a fresh file." : state.error;
      el.list.innerHTML = `<p class="k-help k-rb-empty" role="alert">${escapeHtml(msg)}</p>`;
      return;
    }
    if (!state.list.length) {
      el.list.innerHTML = `<div class="k-rb-empty"><p class="k-rb-empty-title">No runbooks yet</p><p class="k-help">Write the steps you take for a known problem. The agent can walk you through them, one proposal at a time.</p></div>`;
      return;
    }
    if (!shown.length) {
      el.list.innerHTML = `<p class="k-help k-rb-empty">No runbook matches.</p>`;
      return;
    }
    el.list.innerHTML = shown
      .map((rb) => {
        const src = R.sourceBadge(rb);
        const chips = (rb.hosts || []).map((h) => `<span class="k-badge k-badge--mono">${escapeHtml(h)}</span>`).join("");
        return `<div class="k-rb-row" aria-selected="${rb.id === state.selected}" data-rb-row="${escapeHtml(rb.id)}">
          <button type="button" class="k-rb-row-pick" data-rb-pick="${escapeHtml(rb.id)}" aria-label="Open ${escapeHtml(rb.name)}">
            <span class="k-tile">${ICON.book}</span>
            <span class="k-rb-row-main">
              <span class="k-rb-row-head"><span class="k-item-name">${escapeHtml(rb.name)}</span><span class="${src.cls} k-rb-src">${escapeHtml(src.label)}</span></span>
              ${rb.when ? `<span class="k-rb-row-when">${escapeHtml(rb.when)}</span>` : ""}
              ${chips ? `<span class="k-rb-chips">${chips}</span>` : ""}
            </span>
          </button>
          <button type="button" class="k-switch" role="switch" aria-checked="${rb.agentMayPick ? "true" : "false"}" aria-label="Agent may pick ${escapeHtml(rb.name)}" data-rb-toggle="${escapeHtml(rb.id)}"></button>
        </div>`;
      })
      .join("");
  }

  // --- the editor ---

  function editorFrom(rb) {
    return {
      id: rb.id || "",
      name: rb.name || "",
      when: rb.when || "",
      hosts: (rb.hosts || []).slice(),
      agentMayPick: !!rb.agentMayPick,
      source: rb.source || "user",
      sourceRef: rb.sourceRef || "",
      fromPane: !!rb.fromPane,
      createdAt: rb.createdAt || "",
      lastUsedAt: rb.lastUsedAt || "",
      lastUsedOn: rb.lastUsedOn || "",
      steps: (rb.steps || []).map((s) => ({ id: s.id || "", title: s.title || "", description: s.description || "", command: s.command || "", expect: s.expect || "", risk: s.risk || "" })),
    };
  }

  function setEditor(rb, { dirty = false } = {}) {
    state.ed = editorFrom(rb);
    state.selected = rb.id || "";
    state.dirty = dirty;
    paintEditor();
    paintList();
  }

  function closeEditor() {
    state.ed = null;
    state.selected = "";
    state.dirty = false;
    el.detail.innerHTML = `<div class="k-detail-empty"><p class="k-help">Select a runbook to edit it, or add one with New.</p></div>`;
    paintList();
  }

  async function leaveEditor() {
    if (!state.dirty) return true;
    return deps.confirm
      ? deps.confirm("Your changes to this runbook are not saved.", { title: "Discard changes?", action: "Discard" })
      : true;
  }

  function stepHTML(st, i, n) {
    const kind = R.stepKind(st);
    const lines = Math.min(8, Math.max(2, String(st.command || "").split("\n").length));
    return `<li class="k-rb-step" data-step="${i}">
      <span class="k-rb-n" aria-hidden="true">${i + 1}</span>
      <div class="k-rb-body">
        <div class="k-rb-step-head">
          <input class="k-input k-rb-step-title" data-field="title" value="${escapeHtml(st.title)}" aria-label="Step ${i + 1} title" placeholder="What this step does" maxlength="200" autocomplete="off" />
          ${st.command || st.risk ? `<span class="${kind.cls}">${escapeHtml(kind.label)}</span>` : ""}
          <span class="k-rb-step-tools">
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-step-up aria-label="Move step ${i + 1} up" title="Move up"${i === 0 ? " disabled" : ""}>${UP}</button>
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-step-down aria-label="Move step ${i + 1} down" title="Move down"${i === n - 1 ? " disabled" : ""}>${DOWN}</button>
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-step-remove aria-label="Remove step ${i + 1}" title="Remove">${ICON.trash}</button>
          </span>
        </div>
        <input class="k-input k-rb-step-desc" data-field="description" value="${escapeHtml(st.description)}" aria-label="Step ${i + 1} description" placeholder="Why, and what to look for" maxlength="1000" autocomplete="off" />
        <textarea class="k-textarea k-textarea--mono k-rb-cmd" data-field="command" rows="${lines}" aria-label="Step ${i + 1} command" placeholder="Command (optional: a step without one is a check you mark done)" spellcheck="false" autocomplete="off">${escapeHtml(st.command)}</textarea>
        <input class="k-input k-rb-step-expect" data-field="expect" value="${escapeHtml(st.expect)}" aria-label="Step ${i + 1} expected result" placeholder="Expected result (optional)" maxlength="300" autocomplete="off" />
      </div>
    </li>`;
  }

  function paintEditor({ focus = "" } = {}) {
    const ed = state.ed;
    if (!ed) return;
    const saved = !!ed.id;
    const review = saved && R.needsReview(ed);
    const hosts = ed.hosts
      .map((h, i) => `<span class="k-badge k-badge--mono k-rb-hostchip">${escapeHtml(h)}<button type="button" data-host-remove="${i}" aria-label="Remove ${escapeHtml(h)}">${ICON.x}</button></span>`)
      .join("");
    const draftNote = !saved && ed.source !== "user" ? `<div class="k-review" role="note">${ICON.info}<span>A draft ${ed.source === "notebook" ? "from the notebook" : "from the conversation"}${ed.sourceRef ? ` “${escapeHtml(ed.sourceRef)}”` : ""}. Nothing is saved until you press Save, and the agent may not pick it until you turn that on.</span></div>` : "";
    el.detail.innerHTML = `<form class="k-editor k-rb-editor" id="runbook-editor" autocomplete="off" aria-label="Runbook">
      <div class="k-editor-head">
        <input class="k-input k-editor-title k-rb-name" id="runbook-name" data-field="name" value="${escapeHtml(ed.name)}" aria-label="Runbook name" placeholder="Name this runbook" maxlength="120" />
        <button type="button" class="k-btn" id="runbook-use"${saved ? "" : " disabled"} title="${saved ? "Start it in the agent panel" : "Save it first"}">${ICON.ai}Use in agent</button>
        <button type="button" class="k-btn k-btn--ghost" id="runbook-delete">${saved ? "Delete" : "Discard"}</button>
        <button type="submit" class="k-btn k-btn--primary" id="runbook-save"${state.busy ? " disabled" : ""}>Save<kbd class="k-kbd" aria-hidden="true">${saveKey}</kbd></button>
      </div>
      ${
        review
          ? `<div class="k-rb-review" role="note">${ICON.alert}<span><b>Not enabled yet. Review before enabling.</b><br />${escapeHtml(R.reviewText(ed))}</span><button type="button" class="k-btn k-btn--sm" id="runbook-enable">Enable</button></div>`
          : ""
      }${draftNote}
      <p class="k-error" id="runbook-error" role="alert" hidden></p>
      <div class="k-rb-grid">
        <div class="k-rb-main">
          <div class="k-field"><label class="k-label" for="runbook-when">Use when</label>
            <input class="k-input" id="runbook-when" data-field="when" value="${escapeHtml(ed.when)}" maxlength="300" placeholder="A Patroni replica is behind or its WAL stream keeps restarting." />
            <span class="k-help">The agent compares this line with the question and the panes in scope to decide whether the runbook fits.</span></div>
          <div class="k-menu-label k-rb-steps-label">Steps</div>
          <ol class="k-rb-steps" id="runbook-steps">${ed.steps.map((st, i) => stepHTML(st, i, ed.steps.length)).join("")}</ol>
          <div><button type="button" class="k-btn k-btn--ghost k-btn--sm" id="runbook-add-step"${ed.steps.length >= R.MAX_STEPS ? " disabled" : ""}>${ADD}Add step</button></div>
        </div>
        <aside class="k-rb-box" aria-label="Runbook settings">
          <div class="k-field k-field--row"><div class="k-field"><span class="k-label" id="runbook-pick-label">Agent may pick this runbook</span><span class="k-help">Off: only you can start it.</span></div>
            <button type="button" class="k-switch" role="switch" id="runbook-pick" aria-checked="${ed.agentMayPick ? "true" : "false"}" aria-labelledby="runbook-pick-label"></button></div>
          <div class="k-field"><span class="k-label" id="runbook-hosts-label">Hosts</span>
            <div class="k-inputgroup k-rb-hosts">${hosts}<input class="k-input" id="runbook-host-input" placeholder="Folder or tag" aria-labelledby="runbook-hosts-label" autocomplete="off" spellcheck="false" /></div>
            <span class="k-help">Offered only when a pane in scope is on one of these (its folder or a tag). Empty: any host.</span></div>
          <dl class="k-rb-meta">
            <dt>Source</dt><dd>${escapeHtml(saved ? R.sourceLine(ed) : ed.source === "user" ? "New, not saved" : "Draft, not saved")}</dd>
            <dt>Last used</dt><dd>${escapeHtml(R.usedLabel(ed))}</dd>
            <dt>Steps</dt><dd>${escapeHtml(R.stepsLine(ed))}</dd>
          </dl>
          <p class="k-help">Steps become proposals. Each one waits for you to press Type, with the usual rating and ⇧ rule on production.</p>
        </aside>
      </div>
    </form>`;
    if (focus) {
      const f = el.detail.querySelector(focus);
      f?.focus({ preventScroll: false });
    }
  }

  function setError(msg) {
    const e = $("runbook-error");
    if (!e) return;
    e.textContent = msg || "";
    e.hidden = !msg;
  }

  function readField(target) {
    const field = target.dataset.field;
    if (!field || !state.ed) return;
    const li = target.closest("[data-step]");
    if (li) state.ed.steps[Number(li.dataset.step)][field] = target.value;
    else state.ed[field] = target.value;
    state.dirty = true;
  }

  async function save() {
    const ed = state.ed;
    if (!ed || state.busy) return;
    const input = R.toInput(ed);
    const problem = R.checkInput(input);
    if (problem) {
      setError(problem);
      return;
    }
    state.busy = true;
    setError("");
    try {
      const res = ed.id
        ? await deps.api(`/api/agent/runbooks/${encodeURIComponent(ed.id)}`, { method: "PUT", body: JSON.stringify(input) })
        : await deps.api("/api/agent/runbooks", { method: "POST", body: JSON.stringify(input) });
      const rb = res.runbook;
      state.list = [...state.list.filter((b) => b.id !== rb.id), rb].sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
      state.busy = false;
      setEditor(rb);
      deps.toast(res.redacted ? `Saved. ${res.redacted === 1 ? "1 secret-looking value was" : `${res.redacted} secret-looking values were`} replaced first.` : "Runbook saved");
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      state.busy = false;
      const btn = $("runbook-save");
      if (btn) btn.disabled = false;
    }
  }

  async function remove() {
    const ed = state.ed;
    if (!ed) return;
    if (!ed.id) {
      closeEditor();
      return;
    }
    const ok = deps.confirm ? await deps.confirm(`“${ed.name}” is deleted from the vault. Conversations that used it keep their copy of its steps.`, { title: "Delete runbook?", action: "Delete" }) : true;
    if (!ok) return;
    try {
      await deps.api(`/api/agent/runbooks/${encodeURIComponent(ed.id)}`, { method: "DELETE" });
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      return;
    }
    state.list = state.list.filter((b) => b.id !== ed.id);
    closeEditor();
    deps.toast("Runbook deleted");
  }

  async function setPick(id, on) {
    try {
      const rb = await deps.api(`/api/agent/runbooks/${encodeURIComponent(id)}/pick`, { method: "PUT", body: JSON.stringify({ agentMayPick: on }) });
      state.list = state.list.map((b) => (b.id === rb.id ? rb : b));
      if (state.ed?.id === rb.id) {
        state.ed.agentMayPick = rb.agentMayPick;
        if (!state.dirty) setEditor(rb);
        else paintEditor();
      }
      paintList();
    } catch (err) {
      deps.toast(err?.message || String(err), true);
      paintList();
    }
  }

  async function newRunbook() {
    if (!(await leaveEditor())) return;
    setEditor({ name: "", steps: [{ title: "" }], source: "user", agentMayPick: true }, { dirty: false });
    paintEditor({ focus: "#runbook-name" });
  }

  // --- drafts from a notebook or a conversation ---

  async function openMenu(kind) {
    if (state.menu === kind) {
      closeMenu();
      return;
    }
    state.menu = kind;
    el.fromNotebook?.setAttribute("aria-expanded", String(kind === "notebook"));
    el.fromChat?.setAttribute("aria-expanded", String(kind === "chat"));
    el.menu.hidden = false;
    el.menu.innerHTML = `<p class="k-help k-rb-menu-empty">Loading…</p>`;
    let items = [];
    let empty = "";
    if (kind === "notebook") {
      items = (deps.notebooks?.() || []).map((nb) => ({ id: nb.id, name: nb.name || "Notebook", sub: "" }));
      empty = "No notebooks. Write one on the Notebooks page.";
    } else {
      try {
        const res = await deps.api("/api/agent/conversations");
        items = (res?.sessions || []).map((s) => ({ id: s.id, name: s.title || "Conversation", sub: `${sessionWhen(s.updatedAt)} · ${s.proposals || 0} proposals` }));
        empty = res?.historyDays === 0 ? "History is off, so no conversation is saved." : "No saved conversations.";
      } catch (err) {
        empty = err?.message || String(err);
      }
    }
    if (state.menu !== kind) return;
    const head = kind === "notebook" ? "Make a draft from a notebook: its headings become steps, its shell blocks their commands." : "Make a draft from a conversation: its proposals become steps.";
    el.menu.innerHTML = `<p class="k-help k-rb-menu-head">${escapeHtml(head)}</p>${
      items.length
        ? items
            .map(
              (it) =>
                `<button type="button" class="k-rb-menu-item" role="menuitem" data-draft-${kind}="${escapeHtml(it.id)}"><span class="k-rb-other-name">${escapeHtml(it.name)}</span>${
                  it.sub ? `<span class="k-rb-other-sub">${escapeHtml(it.sub)}</span>` : ""
                }</button>`,
            )
            .join("")
        : `<p class="k-help k-rb-menu-empty">${escapeHtml(empty)}</p>`
    }`;
    el.menu.querySelector("[role=menuitem]")?.focus({ preventScroll: true });
  }

  function closeMenu({ refocus = false } = {}) {
    const was = state.menu;
    state.menu = "";
    el.menu.hidden = true;
    el.fromNotebook?.setAttribute("aria-expanded", "false");
    el.fromChat?.setAttribute("aria-expanded", "false");
    if (refocus) (was === "chat" ? el.fromChat : el.fromNotebook)?.focus({ preventScroll: true });
  }

  async function draft(body) {
    closeMenu();
    if (!(await leaveEditor())) return;
    try {
      const d = await deps.api("/api/agent/runbooks/draft", { method: "POST", body: JSON.stringify(body) });
      setEditor({ ...d, agentMayPick: false }, { dirty: true });
      paintEditor({ focus: "#runbook-name" });
    } catch (err) {
      deps.toast(err?.code === 422 ? "Nothing to make steps from: no headings or shell blocks, or no proposals." : err?.message || String(err), true);
    }
  }

  // --- events ---

  el.newBtn?.addEventListener("click", () => newRunbook());
  el.fromNotebook?.addEventListener("click", () => openMenu("notebook"));
  el.fromChat?.addEventListener("click", () => openMenu("chat"));
  el.filter?.addEventListener("input", () => paintList());
  el.menu?.addEventListener("click", (ev) => {
    const nb = ev.target.closest("[data-draft-notebook]")?.dataset.draftNotebook;
    if (nb) return void draft({ notebookId: nb });
    const chat = ev.target.closest("[data-draft-chat]")?.dataset.draftChat;
    if (chat) draft({ conversationId: chat });
  });
  el.menu?.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      closeMenu({ refocus: true });
    }
    if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      const items = [...el.menu.querySelectorAll("[role=menuitem]")];
      const i = items.indexOf(doc.activeElement);
      items[(i + (ev.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]?.focus({ preventScroll: true });
      ev.preventDefault();
    }
  });
  el.list.addEventListener("click", async (ev) => {
    const toggle = ev.target.closest("[data-rb-toggle]");
    if (toggle) {
      const on = toggle.getAttribute("aria-checked") !== "true";
      toggle.setAttribute("aria-checked", String(on));
      setPick(toggle.dataset.rbToggle, on);
      return;
    }
    const pick = ev.target.closest("[data-rb-pick]")?.dataset.rbPick;
    if (!pick || pick === state.selected) return;
    if (!(await leaveEditor())) return;
    const rb = state.list.find((b) => b.id === pick);
    if (rb) setEditor(rb);
  });

  el.detail.addEventListener("input", (ev) => readField(ev.target));
  el.detail.addEventListener("submit", (ev) => {
    ev.preventDefault();
    save();
  });
  el.detail.addEventListener("click", (ev) => {
    const t = ev.target;
    const ed = state.ed;
    if (!ed) return;
    const li = t.closest("[data-step]");
    const i = li ? Number(li.dataset.step) : -1;
    if (t.closest("#runbook-delete")) return void remove();
    if (t.closest("#runbook-use")) {
      const rb = state.list.find((b) => b.id === ed.id);
      if (rb) deps.useRunbook?.(rb);
      return;
    }
    if (t.closest("#runbook-enable")) {
      ed.agentMayPick = true;
      if (state.dirty) paintEditor();
      setPick(ed.id, true);
      return;
    }
    if (t.closest("#runbook-pick")) {
      ed.agentMayPick = !ed.agentMayPick;
      state.dirty = true;
      t.closest("#runbook-pick").setAttribute("aria-checked", String(ed.agentMayPick));
      return;
    }
    if (t.closest("#runbook-add-step")) {
      ed.steps.push({ id: "", title: "", description: "", command: "", expect: "" });
      state.dirty = true;
      paintEditor({ focus: `[data-step="${ed.steps.length - 1}"] [data-field=title]` });
      return;
    }
    const hr = t.closest("[data-host-remove]");
    if (hr) {
      ed.hosts.splice(Number(hr.dataset.hostRemove), 1);
      state.dirty = true;
      paintEditor({ focus: "#runbook-host-input" });
      return;
    }
    if (i < 0) return;
    if (t.closest("[data-step-up]")) {
      ed.steps = R.moveStep(ed.steps, i, -1);
      state.dirty = true;
      paintEditor({ focus: `[data-step="${Math.max(0, i - 1)}"] [data-step-up]:not([disabled]), [data-step="${Math.max(0, i - 1)}"] [data-step-down]` });
    } else if (t.closest("[data-step-down]")) {
      ed.steps = R.moveStep(ed.steps, i, 1);
      state.dirty = true;
      paintEditor({ focus: `[data-step="${Math.min(ed.steps.length - 1, i + 1)}"] [data-step-down]:not([disabled]), [data-step="${Math.min(ed.steps.length - 1, i + 1)}"] [data-step-up]` });
    } else if (t.closest("[data-step-remove]")) {
      ed.steps.splice(i, 1);
      state.dirty = true;
      paintEditor({ focus: "#runbook-add-step" });
    }
  });
  el.detail.addEventListener("keydown", (ev) => {
    if (ev.target.id === "runbook-host-input") {
      if (ev.key === "Enter" || ev.key === ",") {
        ev.preventDefault();
        const v = ev.target.value;
        if (state.ed && v.trim()) {
          state.ed.hosts = R.addHost(state.ed.hosts, v);
          state.dirty = true;
          paintEditor({ focus: "#runbook-host-input" });
        }
        return;
      }
      if (ev.key === "Backspace" && !ev.target.value && state.ed?.hosts.length) {
        state.ed.hosts.pop();
        state.dirty = true;
        paintEditor({ focus: "#runbook-host-input" });
        return;
      }
    }
    // ↵ in a one-line field never submits; ⌘S / Ctrl+S saves.
    if (ev.key === "Enter" && ev.target.tagName === "INPUT") ev.preventDefault();
  });
  root.addEventListener("keydown", (ev) => {
    if ((ev.metaKey || ev.ctrlKey) && !ev.shiftKey && !ev.altKey && ev.key.toLowerCase() === "s") {
      ev.preventDefault();
      ev.stopPropagation();
      if (state.ed) save();
    }
  });
  doc.addEventListener("pointerdown", (ev) => {
    if (!state.menu) return;
    if (el.menu.contains(ev.target) || el.fromNotebook?.contains(ev.target) || el.fromChat?.contains(ev.target)) return;
    closeMenu();
  });

  return {
    show: () => load(),
    refresh: () => load(),
    newRunbook,
    save: () => save(),
    // for tests and the palette: open a runbook by id
    open: (id) => {
      const rb = state.list.find((b) => b.id === id);
      if (rb) setEditor(rb);
    },
  };
}
