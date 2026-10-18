// Notes for the agent (docs/ui-redesign.md §15): the list with add, edit
// and delete, used twice: a host's notes in Edit host (internal/ui
// dialogs.templ, #host-notes) and the user's own notes in Settings · Agent
// (pages.templ, #agent-set-notes).
//
// Every change is saved at once through keel serve (POST/PUT/DELETE
// /api/agent/notes), which redacts the text for secrets before it keeps
// it and logs agent.note. The text shown after a save is the text kept.
// Nothing here talks to the agent or reads a pane.

import { escapeHtml } from "../lib/html.js";
import { answerHTML } from "../lib/agent.js";
import * as N from "../lib/notes.js";
import { ICON } from "./agentthread.js";

// createNotesEditor wires one notes list. opts:
//   root      the block (data-notes-* parts inside)
//   api, toast
//   kind      "host" or "user"
export function createNotesEditor({ root, api, toast, kind = "host" }) {
  if (!root) return null;
  const q = (sel) => root.querySelector(sel);
  const el = {
    count: q("[data-notes-count]"),
    off: q("[data-notes-off]"),
    list: q("[data-notes-list]"),
    input: q("[data-notes-input]"),
    add: q("[data-notes-add]"),
    addRow: q("[data-notes-add-row]"),
    unsaved: q("[data-notes-unsaved]"),
    error: q("[data-notes-error]"),
  };
  const state = { hostId: "", notes: [], editing: "", busy: false, loaded: false, unreadable: false, gen: 0 };

  function mine(all) {
    return kind === "user" ? N.userNotes(all) : N.hostNotes(all, state.hostId);
  }

  function setError(msg) {
    if (!el.error) return;
    el.error.textContent = msg || "";
    el.error.hidden = !msg;
  }

  function rowHTML(n) {
    if (state.editing === n.id) {
      return `<div class="k-note k-note--edit" role="listitem" data-note-id="${escapeHtml(n.id)}" data-note-form>
        <textarea class="k-textarea" aria-label="Edit note" data-note-text rows="2" maxlength="${N.MAX_NOTE_BYTES}" spellcheck="true">${escapeHtml(n.text)}</textarea>
        <span class="k-note-tools">
          <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-note-cancel>Cancel</button>
          <button type="button" class="k-btn k-btn--primary k-btn--sm" data-note-save>Save</button>
        </span>
      </div>`;
    }
    return `<div class="k-note" role="listitem" data-note-id="${escapeHtml(n.id)}">
      <p class="k-note-text">${answerHTML(n.text)}</p>
      <span class="k-note-src">${escapeHtml(N.sourceLabel(n))}</span>
      <span class="k-note-tools">
        <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-note-edit aria-label="Edit note" title="Edit">${ICON.edit}</button>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-note-delete aria-label="Delete note" title="Delete">${ICON.trash}</button>
      </span>
    </div>`;
  }

  function emptyText() {
    if (state.unreadable) return "Saved notes cannot be read. Delete all notes in Settings · Agent starts a fresh file.";
    if (kind === "user") return "No notes. Add how you like to work, e.g. Prefer journalctl over tail -f.";
    return "No notes. Add what the agent should know about this host.";
  }

  function paint() {
    const noHost = kind === "host" && !state.hostId;
    if (el.unsaved) el.unsaved.hidden = !noHost;
    if (el.addRow) el.addRow.hidden = noHost;
    if (el.list) {
      el.list.hidden = noHost;
      el.list.innerHTML = state.notes.length
        ? state.notes.map(rowHTML).join("")
        : `<p class="k-help k-notes-empty">${escapeHtml(state.loaded ? emptyText() : "Loading notes…")}</p>`;
    }
    if (el.count) {
      el.count.textContent = noHost ? "" : N.countLabel(state.notes);
    }
    if (el.add) el.add.disabled = state.busy;
    const editing = state.editing && el.list?.querySelector("[data-note-text]");
    if (editing) {
      editing.focus({ preventScroll: true });
      editing.setSelectionRange?.(editing.value.length, editing.value.length);
    }
  }

  async function load(hostId = "") {
    state.hostId = hostId || "";
    state.editing = "";
    state.loaded = false;
    state.notes = [];
    setError("");
    if (el.input) el.input.value = "";
    const gen = ++state.gen;
    paint();
    if (kind === "host" && !state.hostId) return;
    try {
      const res = await api("/api/agent/notes");
      if (gen !== state.gen) return;
      state.unreadable = !!res?.unreadable;
      state.notes = mine(res?.notes || []);
    } catch (err) {
      if (gen !== state.gen) return;
      setError(err?.message || String(err));
    }
    state.loaded = true;
    paint();
  }

  function saved(res, verb) {
    const extra = N.redactedToast(res?.redacted);
    if (extra) toast?.(`Note ${verb}. ${extra}`);
  }

  async function add() {
    const { text, ok, tooLong } = N.cleanNote(el.input?.value);
    if (!ok) {
      setError(tooLong ? `A note is at most ${N.MAX_NOTE_BYTES} bytes.` : "");
      el.input?.focus();
      return;
    }
    state.busy = true;
    paint();
    try {
      const res = await api("/api/agent/notes", { method: "POST", body: JSON.stringify({ hostId: kind === "host" ? state.hostId : "", text }) });
      state.notes = [...state.notes, res.note];
      if (el.input) el.input.value = "";
      setError("");
      saved(res, "added");
    } catch (err) {
      setError(err?.message || String(err));
    } finally {
      state.busy = false;
      paint();
      el.input?.focus({ preventScroll: true });
    }
  }

  async function save(id, value) {
    const { text, ok, tooLong } = N.cleanNote(value);
    if (!ok) {
      setError(tooLong ? `A note is at most ${N.MAX_NOTE_BYTES} bytes.` : "A note needs some text. Delete it instead.");
      return;
    }
    try {
      const res = await api(`/api/agent/notes/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify({ text }) });
      state.notes = state.notes.map((n) => (n.id === id ? res.note : n));
      state.editing = "";
      setError("");
      saved(res, "saved");
    } catch (err) {
      setError(err?.message || String(err));
    }
    paint();
    el.list?.querySelector(`[data-note-id="${CSS.escape(id)}"] [data-note-edit]`)?.focus({ preventScroll: true });
  }

  async function remove(id) {
    try {
      await api(`/api/agent/notes/${encodeURIComponent(id)}`, { method: "DELETE" });
      state.notes = state.notes.filter((n) => n.id !== id);
      setError("");
      toast?.("Note deleted.");
    } catch (err) {
      setError(err?.message || String(err));
    }
    paint();
    el.input?.focus({ preventScroll: true });
  }

  function edit(id) {
    state.editing = id;
    paint();
  }

  function cancelEdit() {
    const id = state.editing;
    state.editing = "";
    paint();
    if (id) el.list?.querySelector(`[data-note-id="${CSS.escape(id)}"] [data-note-edit]`)?.focus({ preventScroll: true });
  }

  el.add?.addEventListener("click", (ev) => {
    ev.preventDefault();
    add();
  });
  // ↵ in the field adds the note; it must not submit the host form around it.
  el.input?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.isComposing) {
      ev.preventDefault();
      ev.stopPropagation();
      add();
    }
  });
  el.list?.addEventListener("click", (ev) => {
    const row = ev.target.closest("[data-note-id]");
    const id = row?.dataset.noteId;
    if (!id) return;
    if (ev.target.closest("[data-note-edit]")) edit(id);
    else if (ev.target.closest("[data-note-delete]")) remove(id);
    else if (ev.target.closest("[data-note-cancel]")) cancelEdit();
    else if (ev.target.closest("[data-note-save]")) save(id, row.querySelector("[data-note-text]")?.value);
  });
  el.list?.addEventListener("keydown", (ev) => {
    const row = ev.target.closest("[data-note-form]");
    if (!row) return;
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      cancelEdit();
    } else if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) {
      ev.preventDefault();
      ev.stopPropagation();
      save(row.dataset.noteId, row.querySelector("[data-note-text]")?.value);
    }
  });

  return {
    load,
    // setReadable shows that these notes are not sent while the host's
    // "Agent can read" is off.
    setReadable(on) {
      if (el.off) el.off.hidden = !!on;
    },
    notes: () => state.notes.slice(),
  };
}
