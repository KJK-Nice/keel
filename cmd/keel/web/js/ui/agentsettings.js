// Settings · Agent (docs/ui-redesign.md §5.9). The section's markup is in
// internal/ui/pages.templ; this fills it from keel serve and saves each
// change as it is made, through the same routes as the agent panel:
//
//   GET/PUT /api/agent/settings       provider, model, detection, suggestions,
//                                     defaultScope, scrollbackLines, redact,
//                                     historyDays (the response is the saved value)
//   GET     /api/agent/providers      descriptors: warning, links, models
//   PUT/DELETE /api/agent/keys/{id}   gemini, anthropic and jev
//   DELETE  /api/agent/conversations  Delete all history
//
// A key is write-only. It is read from the password field, sent once, and
// the field is emptied when the save succeeds; keel serve answers only with
// `configured`, so there is nothing to show or to keep here, and the key is
// never put in a toast, an error line or the console.

import * as S from "../lib/agentsettings.js";
import { createNotesEditor } from "./notes.js";

export function createAgentSettings({ doc = document, api, toast, confirm, widgets = {} }) {
  const root = doc.getElementById("agent-settings");
  if (!root) return null;
  const $ = (id) => doc.getElementById(id);
  const el = {
    providers: $("agent-set-providers"),
    model: $("agent-set-model"),
    modelRow: $("agent-set-model-row"),
    suggest: $("agent-set-suggest"),
    scrollbackSuggest: $("agent-set-history-suggest"),
    detection: $("agent-set-detection"),
    detectionNote: $("agent-set-detection-note"),
    scope: $("agent-set-scope"),
    lines: $("agent-set-lines"),
    linesOut: $("agent-set-lines-out"),
    ips: $("agent-set-ips"),
    hosts: $("agent-set-hostnames"),
    history: $("agent-set-history"),
    deleteHistory: $("agent-set-delete-history"),
    deleteNotes: $("agent-set-delete-notes"),
  };
  // The user's own notes for the agent (§15).
  const notes = createNotesEditor({ root: $("agent-set-notes"), api, toast, kind: "user" });

  const state = {
    settings: null,
    keys: {},
    providers: [],
    cards: {}, // id → { changing, error, busy }
    loaded: false,
    linesTimer: 0,
  };

  const card = (id) => (state.cards[id] ||= { changing: false, error: "", busy: false });
  const setOn = (btn, on) => btn?.setAttribute("aria-checked", on ? "true" : "false");
  const isOn = (btn) => btn?.getAttribute("aria-checked") === "true";
  const jevOn = () => !!state.keys.jev?.configured;

  // --- paint ---

  function paintProviders() {
    if (!el.providers) return;
    // Keep a half-typed key when only another card changed.
    const typed = {};
    for (const input of el.providers.querySelectorAll("[data-agent-key]")) typed[input.dataset.agentKey] = input.value;
    el.providers.innerHTML = state.providers
      .map((d) => S.providerCardHTML(d, !!state.keys[d.id]?.configured, card(d.id)))
      .join("");
    for (const input of el.providers.querySelectorAll("[data-agent-key]")) {
      if (typed[input.dataset.agentKey]) input.value = typed[input.dataset.agentKey];
    }
  }

  function paintModel() {
    const d = state.providers.find((p) => p.kind === "model" && p.id === state.settings?.provider) || state.providers.find((p) => p.kind === "model");
    if (!d || !el.model) return;
    const items = S.modelItems(state.providers, state.settings);
    widgets.fillSelectItems?.(el.model.id, items.map(({ value, label }) => ({ value, label })), state.settings?.model || d.defaultModel, "Default");
  }

  function paintRadios(group, attr, value) {
    for (const r of group?.querySelectorAll(`[${attr}]`) || []) setOn(r, r.getAttribute(attr) === value);
  }

  function paintSettings() {
    const s = state.settings;
    if (!s) return;
    setOn(el.suggest, s.suggestions !== "off");
    setOn(el.scrollbackSuggest, s.historySuggestions !== "off");
    paintRadios(el.detection, "data-agent-detection", s.detection);
    if (el.detection) el.detection.toggleAttribute("data-disabled", s.suggestions === "off");
    for (const r of el.detection?.querySelectorAll("[data-agent-detection]") || []) {
      r.setAttribute("aria-disabled", s.suggestions === "off" ? "true" : "false");
    }
    const note = S.detectionNote(s, jevOn());
    if (el.detectionNote) {
      el.detectionNote.textContent = note;
      el.detectionNote.hidden = !note;
    }
    paintRadios(el.scope, "data-agent-scope", s.defaultScope);
    const lines = S.clampLines(s.scrollbackLines);
    widgets.setSliderValue?.("agent-set-lines", lines);
    if (el.linesOut) el.linesOut.textContent = String(lines);
    setOn(el.ips, !!s.redact?.ips);
    setOn(el.hosts, !!s.redact?.hostnames);
    widgets.setSelectValue?.("agent-set-history", S.historyValue(s.historyDays));
    paintModel();
  }

  function paintAll() {
    paintProviders();
    paintSettings();
  }

  // --- load and save ---

  async function load() {
    notes?.load();
    try {
      const [res, list] = await Promise.all([api("/api/agent/settings"), state.providers.length ? Promise.resolve(state.providers) : api("/api/agent/providers")]);
      state.settings = res.settings || null;
      state.keys = res.keys || {};
      state.providers = (list || []).filter((d) => d.kind === "model" || d.id === "jev");
      state.loaded = true;
      paintAll();
    } catch (err) {
      if (err?.code === 423) return;
      toast(err?.message || "Could not load the agent settings", true);
    }
  }

  // put saves a patch over the saved settings and takes the saved value
  // back, so the screen always shows what keel serve holds.
  async function put(patch, { quiet = true } = {}) {
    try {
      const res = await api("/api/agent/settings", { method: "PUT", body: JSON.stringify(patch) });
      state.settings = res.settings || state.settings;
      state.keys = res.keys || state.keys;
      paintSettings();
      if (!quiet) toast("Saved");
    } catch (err) {
      toast(err?.message || "Could not save", true);
      await load();
    }
  }

  async function saveKey(form) {
    if (!form) return;
    const id = form.dataset.agentKeyform;
    const input = form.querySelector("[data-agent-key]");
    const key = (input?.value || "").trim();
    const ui = card(id);
    const d = state.providers.find((p) => p.id === id);
    if (!key) {
      ui.error = "Paste a key first.";
      paintProviders();
      el.providers.querySelector(`[data-agent-key="${id}"]`)?.focus();
      return;
    }
    ui.busy = true;
    try {
      await api(`/api/agent/keys/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify({ key }) });
      if (input) input.value = "";
      state.keys[id] = { configured: true };
      state.cards[id] = { changing: false, error: "", busy: false };
      paintAll();
      toast(`${d?.name || id} connected. The key is in the vault.`);
    } catch (err) {
      ui.busy = false;
      ui.error = S.keyError(err);
      paintProviders();
      el.providers.querySelector(`[data-agent-key="${id}"]`)?.focus();
    }
  }

  async function disconnect(id) {
    const d = state.providers.find((p) => p.id === id);
    const name = d?.name || id;
    const msg =
      id === "jev"
        ? `Keel will stop noticing plain language on its own and only notice lines that start with #. The ${name} key is removed from the vault.`
        : `The agent panel and suggestions will stop working until you connect a key again. The ${name} key is removed from the vault.`;
    if (!(await confirm(msg, { title: `Disconnect ${name}?`, action: "Disconnect" }))) return;
    try {
      await api(`/api/agent/keys/${encodeURIComponent(id)}`, { method: "DELETE" });
      state.keys[id] = { configured: false };
      state.cards[id] = { changing: false, error: "", busy: false };
      paintAll();
      toast(`${name} disconnected`);
    } catch (err) {
      toast(err?.message || "Could not disconnect", true);
    }
  }

  async function deleteHistory() {
    if (
      !(await confirm("Every agent conversation is forgotten, including the ones saved in the vault, in every window. The log entries that say what the agent read stay in Logs.", {
        title: "Delete all history?",
        action: "Delete all history",
      }))
    )
      return;
    try {
      await api("/api/agent/conversations", { method: "DELETE" });
      toast("Agent history deleted");
    } catch (err) {
      toast(err?.message || "Could not delete the history", true);
    }
  }

  async function deleteNotes() {
    if (
      !(await confirm("Every host note and every note about you is deleted from the vault. Conversations and the log stay.", {
        title: "Delete all notes?",
        action: "Delete all notes",
      }))
    )
      return;
    try {
      await api("/api/agent/notes", { method: "DELETE" });
      toast("Notes deleted");
      notes?.load();
    } catch (err) {
      toast(err?.message || "Could not delete the notes", true);
    }
  }

  // --- events ---

  root.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter" || ev.isComposing) return;
    const form = ev.target.closest("[data-agent-key]")?.closest("[data-agent-keyform]");
    if (!form) return;
    ev.preventDefault();
    saveKey(form);
  });

  root.addEventListener("click", (ev) => {
    const t = ev.target;
    const save = t.closest("[data-agent-save]");
    if (save) {
      saveKey(save.closest("[data-agent-keyform]"));
      return;
    }
    const change = t.closest("[data-agent-change]");
    if (change) {
      const id = change.dataset.agentChange;
      card(id).changing = !card(id).changing;
      card(id).error = "";
      paintProviders();
      if (card(id).changing) el.providers.querySelector(`[data-agent-key="${id}"]`)?.focus();
      return;
    }
    const cancel = t.closest("[data-agent-cancel]");
    if (cancel) {
      state.cards[cancel.dataset.agentCancel] = { changing: false, error: "", busy: false };
      paintProviders();
      return;
    }
    const dis = t.closest("[data-agent-disconnect]");
    if (dis) {
      disconnect(dis.dataset.agentDisconnect);
      return;
    }
    const sw = t.closest("button.k-switch");
    if (sw && root.contains(sw)) {
      const on = !isOn(sw);
      setOn(sw, on);
      if (sw === el.suggest) put({ suggestions: on ? "on" : "off" });
      else if (sw === el.scrollbackSuggest) put({ historySuggestions: on ? "on" : "off" });
      else if (sw === el.ips) put({ redact: { secrets: true, ips: on, hostnames: isOn(el.hosts) } });
      else if (sw === el.hosts) put({ redact: { secrets: true, ips: isOn(el.ips), hostnames: on } });
      return;
    }
    const det = t.closest("[data-agent-detection]");
    if (det && det.getAttribute("aria-disabled") !== "true") {
      paintRadios(el.detection, "data-agent-detection", det.dataset.agentDetection);
      put({ detection: det.dataset.agentDetection });
      return;
    }
    const sc = t.closest("[data-agent-scope]");
    if (sc) {
      paintRadios(el.scope, "data-agent-scope", sc.dataset.agentScope);
      put({ defaultScope: sc.dataset.agentScope });
      return;
    }
    if (t.closest("#agent-set-delete-history")) deleteHistory();
    if (t.closest("#agent-set-delete-notes")) deleteNotes();
  });

  // Radio groups move with the arrow keys, as radios do.
  root.addEventListener("keydown", (ev) => {
    if (!["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight"].includes(ev.key)) return;
    const radio = ev.target.closest('[role="radio"]');
    const group = radio?.closest('[role="radiogroup"]');
    if (!group) return;
    const all = [...group.querySelectorAll('[role="radio"]:not([aria-disabled="true"])')];
    const i = all.indexOf(radio);
    if (i < 0) return;
    ev.preventDefault();
    const next = all[(i + (ev.key === "ArrowDown" || ev.key === "ArrowRight" ? 1 : -1) + all.length) % all.length];
    next.focus();
    next.click();
  });

  el.model?.addEventListener("select-change", (ev) => {
    const v = ev.detail?.value;
    if (!v || v === state.settings?.model) return;
    // A model names its provider: picking a Claude model switches the
    // agent to Anthropic, and the other way round.
    const item = S.modelItems(state.providers, state.settings).find((it) => it.value === v);
    put(item?.provider && item.provider !== state.settings?.provider ? { provider: item.provider, model: v } : { model: v });
  });
  el.history?.addEventListener("select-change", (ev) => {
    const v = ev.detail?.value;
    if (v != null && v !== S.historyValue(state.settings?.historyDays)) put({ historyDays: Number(v) });
  });
  el.lines?.addEventListener("slider-change", (ev) => {
    const v = ev.detail?.values?.[0];
    if (v == null) return;
    const n = S.clampLines(v);
    if (el.linesOut) el.linesOut.textContent = String(n);
    clearTimeout(state.linesTimer);
    state.linesTimer = setTimeout(() => {
      if (n !== state.settings?.scrollbackLines) put({ scrollbackLines: n });
    }, 300);
  });

  return { load };
}
