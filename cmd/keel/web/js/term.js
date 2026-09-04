(() => {
  function createTerminal(opts) {
    const Ctor = window.Terminal;
    if (typeof Ctor !== "function") {
      throw new Error("Terminal library failed to load");
    }
    return new Ctor(opts);
  }
  const FitAddonCtor = window.FitAddon?.FitAddon || window.FitAddon;
  const SearchAddonCtor = window.SearchAddon?.SearchAddon || window.SearchAddon;
  const WebLinksAddonCtor = window.WebLinksAddon?.WebLinksAddon || window.WebLinksAddon;
  const UnicodeGraphemesCtor = window.UnicodeGraphemesAddon?.UnicodeGraphemesAddon || window.UnicodeGraphemesAddon;
  const ProgressAddonCtor = window.ProgressAddon?.ProgressAddon || window.ProgressAddon;
  const params = new URLSearchParams(location.search);
  const VAULT = "vault";
  const SASH_PX = 6;
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform) || navigator.userAgent.includes("Mac");
  const MOD = isMac ? "⌘" : "Ctrl+";
  const FONT_STACKS = {
    jetbrains: "JetBrains Mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    ibmplex: "IBM Plex Mono, ui-monospace, Menlo, Consolas, monospace",
    menlo: "Menlo, SFMono-Regular, ui-monospace, monospace",
    consolas: "Consolas, ui-monospace, monospace",
    system: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
  };

  const state = {
    hosts: [],
    snippets: [],
    identities: [],
    tunnels: [],
    logs: [],
    herd: [],
    tabs: new Map(),
    panes: new Map(),
    activeId: VAULT,
    vaultSection: "hosts",
    settingsSection: "appearance",
    query: "",
    logFilter: "",
    name: params.get("name") || localStorage.getItem("keel-name") || "",
    pendingSession: params.get("session") || params.get("id") || "",
    herdWs: null,
    openModal: null,
    signalURL: "",
    lastClosedHostId: null,
    runtimeStopping: false,
    prefs: loadPrefs(),
    ui: loadUiPrefs(),
    hostKeyRetry: null,
  };

  const TERM_THEMES = {
    dark: {
      background: "#0a0a0a",
      foreground: "#e4e4e4",
      cursor: "#82a7ff",
      cursorAccent: "#0a0a0a",
      selectionBackground: "#2a3344",
      black: "#1a1a1a",
      red: "#f87171",
      green: "#4ade80",
      yellow: "#e8b849",
      blue: "#82a7ff",
      magenta: "#c084fc",
      cyan: "#67e8f9",
      white: "#ececec",
      brightBlack: "#737373",
    },
    light: {
      background: "#fbfbfa",
      foreground: "#1a1a1a",
      cursor: "#3b6cff",
      cursorAccent: "#fbfbfa",
      selectionBackground: "#dbe4ff",
      black: "#1a1a1a",
      red: "#dc2626",
      green: "#15803d",
      yellow: "#b45309",
      blue: "#3b6cff",
      magenta: "#7c3aed",
      cyan: "#0e7490",
      white: "#f5f5f4",
      brightBlack: "#737373",
    },
  };

  function loadPrefs() {
    const fallback = { font: "jetbrains", size: 12, copyOnSelect: true, rightClickPaste: true };
    try {
      const raw = JSON.parse(localStorage.getItem("keel-term") || "{}");
      return {
        font: FONT_STACKS[raw.font] ? raw.font : fallback.font,
        size: Math.min(22, Math.max(10, Number(raw.size) || 12)),
        copyOnSelect: raw.copyOnSelect !== false,
        rightClickPaste: raw.rightClickPaste !== false,
      };
    } catch {
      return fallback;
    }
  }

  function savePrefs() {
    localStorage.setItem("keel-term", JSON.stringify(state.prefs));
  }

  const SIZE_MIN = 10;
  const SIZE_MAX = 22;
  const SIZE_DEFAULT = 12;
  const UI_SIZE_MIN = 11;
  const UI_SIZE_MAX = 16;
  const UI_SIZE_DEFAULT = 12;

  function loadUiPrefs() {
    const fallback = { size: 12, smoothing: "antialiased", view: "card" };
    try {
      const raw = JSON.parse(localStorage.getItem("keel-ui") || "{}");
      const size = Math.min(16, Math.max(11, Math.round(Number(raw.size) || 12)));
      const smoothing = raw.smoothing === "subpixel" || raw.smoothing === "none" ? raw.smoothing : fallback.smoothing;
      const view = raw.view === "list" ? "list" : "card";
      return { size, smoothing, view };
    } catch {
      return fallback;
    }
  }

  function saveUiPrefs() {
    localStorage.setItem("keel-ui", JSON.stringify(state.ui));
  }

  function applyUiPrefs() {
    document.documentElement.style.setProperty("--ui-size", `${state.ui.size}px`);
    document.documentElement.dataset.smooth = state.ui.smoothing;
    applyVaultView(state.ui.view);
  }

  function applyVaultView(next) {
    const view = next === "list" ? "list" : "card";
    document.documentElement.dataset.vaultView = view;
    document.querySelectorAll("[data-vault-view-btn]").forEach((btn) => {
      btn.setAttribute("aria-pressed", String(btn.getAttribute("data-vault-view-btn") === view));
    });
  }

  function setVaultView(next) {
    const view = next === "list" ? "list" : "card";
    if (view === state.ui.view) return;
    state.ui.view = view;
    saveUiPrefs();
    applyUiPrefs();
  }

  function setUiSize(next) {
    const size = Math.min(UI_SIZE_MAX, Math.max(UI_SIZE_MIN, Math.round(Number(next) || UI_SIZE_DEFAULT)));
    if (size === state.ui.size) return;
    state.ui.size = size;
    saveUiPrefs();
    applyUiPrefs();
    syncSettingsForm();
  }

  function setSmoothing(next) {
    const smoothing = next === "subpixel" || next === "none" ? next : "antialiased";
    if (smoothing === state.ui.smoothing) return;
    state.ui.smoothing = smoothing;
    saveUiPrefs();
    applyUiPrefs();
    syncSettingsForm();
  }

  function setFontSize(next) {
    const size = Math.min(SIZE_MAX, Math.max(SIZE_MIN, Math.round(Number(next) || SIZE_DEFAULT)));
    if (size === state.prefs.size) return;
    state.prefs.size = size;
    savePrefs();
    syncSettingsForm();
    applyTerminalPrefs();
  }

  function fontFamily() {
    return FONT_STACKS[state.prefs.font] || FONT_STACKS.jetbrains;
  }

  function uid(prefix) {
    return `${prefix}${Date.now().toString(36)}${Math.random().toString(36).slice(2, 7)}`;
  }

  function resolvedTheme() {
    const choice = document.documentElement.dataset.theme || "system";
    if (choice === "light" || choice === "dark") return choice;
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }

  function isNativeWindow() {
    return document.documentElement.dataset.native === "1";
  }

  function xtermTheme() {
    const theme = { ...TERM_THEMES[resolvedTheme()] };
    if (isNativeWindow()) {
      theme.background = resolvedTheme() === "light" ? "#fbfbfaa8" : "#0a0a0aa8";
    }
    return theme;
  }

  function termOpts() {
    return {
      cursorBlink: true,
      fontFamily: fontFamily(),
      fontSize: state.prefs.size,
      lineHeight: 1,
      letterSpacing: 0,
      customGlyphs: true,
      allowTransparency: false,
      overviewRuler: { width: 0 },
      theme: xtermTheme(),
    };
  }

  function applyTerminalPrefs() {
    const theme = xtermTheme();
    const family = fontFamily();
    const size = state.prefs.size;
    const glass = false;
    for (const p of state.panes.values()) {
      if (!p.term) continue;
      p.term.options.allowTransparency = glass;
      p.term.options.theme = theme;
      p.term.options.fontFamily = family;
      p.term.options.fontSize = size;
      p.term.options.lineHeight = 1;
      p.term.options.letterSpacing = 0;
      p.term.options.customGlyphs = true;
      if (isFindOpen(p)) runPaneFind(p, "next", true);
    }
    fitVisiblePanes();
  }

  function syncThemeSwitch() {
    const choice = document.documentElement.dataset.theme || "system";
    setSelectValue("set-theme", choice);
  }

  function applyThemeClass() {
    if (typeof window.__keelApplyTheme === "function") {
      window.__keelApplyTheme(document.documentElement.dataset.theme || "system");
      return;
    }
    const choice = document.documentElement.dataset.theme || "system";
    const dark =
      choice === "dark" ||
      (choice === "system" && !window.matchMedia("(prefers-color-scheme: light)").matches);
    document.documentElement.classList.toggle("dark", dark);
    document.documentElement.style.colorScheme = dark ? "dark" : "light";
    document.body?.classList.remove("dark");
  }

  function setTheme(choice) {
    if (choice !== "light" && choice !== "dark" && choice !== "system") choice = "system";
    document.documentElement.dataset.theme = choice;
    localStorage.setItem("keel-theme", choice);
    applyThemeClass();
    syncThemeSwitch();
    applyTerminalPrefs();
  }

  function syncTermPreview() {
    const preview = document.getElementById("term-preview");
    if (!preview) return;
    preview.style.fontFamily = fontFamily();
    preview.style.fontSize = `${state.prefs.size}px`;
  }

  function syncSettingsForm() {
    const out = document.getElementById("set-size-out");
    const copy = document.getElementById("set-copy");
    const paste = document.getElementById("set-paste");
    const uiOut = document.getElementById("set-ui-size-out");
    setSelectValue("set-font", state.prefs.font);
    if (out) out.textContent = String(state.prefs.size);
    setInputChecked(copy, state.prefs.copyOnSelect);
    setInputChecked(paste, state.prefs.rightClickPaste);
    if (uiOut) uiOut.textContent = String(state.ui.size);
    setSelectValue("set-smooth", state.ui.smoothing);
    setSliderValue("set-ui-size", state.ui.size);
    setSliderValue("set-size", state.prefs.size);
    syncThemeSwitch();
    syncTermPreview();
  }

  const els = {
    hostList: document.getElementById("host-list"),
    snippetList: document.getElementById("snippet-list"),
    identityList: document.getElementById("identity-list"),
    tunnelList: document.getElementById("tunnel-list"),
    logList: document.getElementById("log-list"),
    tabs: document.getElementById("tabs"),
    terminals: document.getElementById("terminals"),
    vault: document.getElementById("vault"),
    barStatus: document.getElementById("bar-status"),
    controlWidget: document.getElementById("control-widget"),
    btnShare: document.getElementById("btn-share"),
    search: document.getElementById("search"),
    modalScrim: document.getElementById("modal-scrim"),
    hostModal: document.getElementById("host-modal"),
    hostForm: document.getElementById("host-form"),
    identityModal: document.getElementById("identity-modal"),
    identityForm: document.getElementById("identity-form"),
    tunnelModal: document.getElementById("tunnel-modal"),
    tunnelForm: document.getElementById("tunnel-form"),
    quickModal: document.getElementById("quick-modal"),
    quickForm: document.getElementById("quick-form"),
    joinModal: document.getElementById("join-modal"),
    joinForm: document.getElementById("join-form"),
    snippetModal: document.getElementById("snippet-modal"),
    snippetForm: document.getElementById("snippet-form"),
    hostKeyModal: document.getElementById("hostkey-modal"),
    hostKeyForm: document.getElementById("hostkey-form"),
    gate: document.getElementById("gate"),
    gateForm: document.getElementById("gate-form"),
    cmdPalette: document.getElementById("cmd-palette"),
    cmdPaletteForm: document.getElementById("cmd-palette-form"),
    cmdPaletteInput: document.getElementById("cmd-palette-input"),
    cmdPaletteList: document.getElementById("cmd-palette-list"),
    ptyInjectModal: document.getElementById("pty-inject-modal"),
    ptyInjectForm: document.getElementById("pty-inject-form"),
  };

  function on(el, type, fn, opts) {
    if (!el) return;
    el.addEventListener(type, fn, opts);
  }

  function datastarQS() {
    return new URLSearchParams({
      datastar: JSON.stringify({
        query: state.query || "",
        logFilter: state.logFilter || "",
      }),
    }).toString();
  }

  async function paintList(id, url) {
    try {
      const res = await fetch(url, { headers: { Accept: "text/html" } });
      if (!res.ok) return false;
      const html = await res.text();
      const current = document.getElementById(id);
      if (!current) return false;
      const tpl = document.createElement("template");
      tpl.innerHTML = html.trim();
      const next = tpl.content.querySelector("#" + CSS.escape(id));
      if (!next) return false;
      current.innerHTML = next.innerHTML;
      return true;
    } catch {
      return false;
    }
  }

  function setSelectValue(triggerId, value) {
    const trigger = document.getElementById(triggerId);
    if (!trigger) return;
    if (trigger.tagName === "SELECT") {
      trigger.value = value;
      return;
    }
    const contentId = trigger.getAttribute("aria-controls");
    const content = contentId ? document.getElementById(contentId) : null;
    if (!content) return;
    const item = [...content.querySelectorAll("[data-tui-select-item]")].find(
      (el) => el.getAttribute("data-tui-select-value") === value
    );
    if (!item) return;
    content.querySelectorAll("[data-tui-select-item]").forEach((el) => {
      const on = el === item;
      el.toggleAttribute("data-selected", on);
      el.setAttribute("aria-selected", on ? "true" : "false");
    });
    const label =
      item.getAttribute("data-tui-select-label") ||
      (item.querySelector("[data-tui-select-item-text]") || item).textContent.trim();
    const span = trigger.querySelector("[data-tui-select-value]");
    if (span) span.textContent = label;
    trigger.removeAttribute("data-placeholder");
    const input = trigger.previousElementSibling;
    if (input?.hasAttribute?.("data-tui-select-input")) input.value = value;
  }

  function fillSelectItems(triggerId, entries, selected, emptyLabel) {
    const trigger = document.getElementById(triggerId);
    if (!trigger) return;
    const contentId = trigger.getAttribute("aria-controls");
    const content = contentId ? document.getElementById(contentId) : null;
    if (!content) return;
    const group =
      content.querySelector("[data-slot='select-group']") ||
      content.querySelector("[data-tui-select-viewport]") ||
      content;
    const proto = group.querySelector("[data-tui-select-item]");
    if (!proto) return;
    const list = entries.length ? entries : [{ value: "", label: emptyLabel || "—" }];
    group.replaceChildren(
      ...list.map((entry) => {
        const node = proto.cloneNode(true);
        node.setAttribute("data-tui-select-value", entry.value);
        node.setAttribute("data-tui-select-label", entry.label);
        node.setAttribute("aria-selected", "false");
        node.removeAttribute("data-selected");
        const text = node.querySelector("[data-tui-select-item-text]");
        if (text) text.textContent = entry.label;
        return node;
      })
    );
    const pick = selected && list.some((entry) => entry.value === selected) ? selected : list[0].value;
    setSelectValue(triggerId, pick);
  }

  function setSliderValue(id, value) {
    const root = document.getElementById(id);
    if (!root) return;
    const thumb = root.querySelector("[data-tui-slider-thumb]");
    if (!thumb) return;
    const min = parseFloat(root.getAttribute("data-min") || "0");
    const max = parseFloat(root.getAttribute("data-max") || "100");
    const v = Math.min(max, Math.max(min, Number(value)));
    const f = max === min ? 0 : (v - min) / (max - min);
    const thumbPx = 12;
    thumb.setAttribute("aria-valuenow", String(v));
    thumb.style.left = `calc(${(f * 100).toFixed(4)}% - ${(f * thumbPx).toFixed(2)}px)`;
    const range = root.querySelector("[data-tui-slider-range]");
    if (range) {
      range.style.left = "0";
      range.style.width = `calc(${(f * 100).toFixed(4)}% + ${(thumbPx / 2 - f * thumbPx).toFixed(2)}px)`;
    }
    const input = root.querySelector("[data-tui-slider-input]");
    if (input) input.value = String(v);
  }

  function setInputChecked(el, checked) {
    if (!el) return;
    el.checked = !!checked;
    el.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function askConfirm(message) {
    const trigger = document.getElementById("confirm-dialog-trigger");
    const ok = document.getElementById("confirm-dialog-ok");
    const desc = document.getElementById("confirm-dialog-desc");
    if (!trigger || !ok) return Promise.resolve(window.confirm(message));
    return new Promise((resolve) => {
      if (desc) desc.textContent = message;
      let settled = false;
      const popup = document.getElementById("confirm-dialog");
      const finish = (value) => {
        if (settled) return;
        settled = true;
        ok.removeEventListener("click", onOk);
        popup?.removeEventListener("dialog-close", onClose);
        resolve(value);
      };
      const onOk = () => finish(true);
      const onClose = () => finish(false);
      ok.addEventListener("click", onOk);
      popup?.addEventListener("dialog-close", onClose);
      trigger.click();
    });
  }

  function authHeaders() {
    return { "Content-Type": "application/json" };
  }

  async function api(path, opts = {}) {
    const res = await fetch(path, {
      headers: { ...authHeaders(), ...(opts.headers || {}) },
      ...opts,
    });
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (res.status === 401 || res.status === 423) {
      const err = new Error(data.error || "vault locked");
      err.code = res.status;
      throw err;
    }
    if (!res.ok) {
      const err = new Error(data.error || res.statusText);
      err.code = res.status;
      err.errorKind = data.errorKind;
      err.hostname = data.hostname;
      err.fingerprint = data.fingerprint;
      err.expected = data.expected;
      throw err;
    }
    return data;
  }

  const pendingToasts = [];

  function flushToasts() {
    const add = window.tui?.toast?.add;
    if (!add) return false;
    while (pendingToasts.length) add(pendingToasts.shift());
    return true;
  }

  function toast(message, isError = false) {
    const payload = { title: message };
    if (isError) payload.type = "error";
    if (window.tui?.toast?.add) {
      window.tui.toast.add(payload);
      return;
    }
    pendingToasts.push(payload);
    if (pendingToasts.armed) return;
    pendingToasts.armed = true;
    let tries = 0;
    const retry = () => {
      if (flushToasts()) {
        pendingToasts.armed = false;
        return;
      }
      if (++tries > 40) {
        pendingToasts.armed = false;
        pendingToasts.length = 0;
        return;
      }
      window.setTimeout(retry, 50);
    };
    document.addEventListener("DOMContentLoaded", retry, { once: true });
    window.addEventListener("load", retry, { once: true });
    retry();
  }

  function persistName() {
    if (state.name) localStorage.setItem("keel-name", state.name);
  }

  function escapeHtml(value) {
    return String(value ?? "")
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;")
      .replaceAll('"', "&quot;");
  }

  function groupHosts(hosts) {
    const q = state.query.trim().toLowerCase();
    const filtered = hosts.filter((h) => {
      const hay = `${h.name} ${h.group} ${h.address} ${h.username}`.toLowerCase();
      return !q || hay.includes(q);
    });
    const groups = new Map();
    for (const h of filtered) {
      const key = h.group || "Ungrouped";
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(h);
    }
    return groups;
  }

  function hostSubtitle(h) {
    if (h.authType === "local") return h.subtitle || "this machine";
    const port = h.port && h.port !== 22 ? `:${h.port}` : "";
    const ident = h.identityId ? " · identity" : "";
    return `${h.username}@${h.address}${port}${ident}`;
  }

  function statusColor(status) {
    if (status === "ready") return "#5ee0b5";
    if (status === "connecting") return "#f5c15d";
    if (status === "stopped") return "#8a8a8a";
    return "#ff6b7a";
  }

  const WORKSPACE_KEY = "keel-workspace";
  let restoringWorkspace = false;
  let workspaceReady = false;

  function paneRuntimeId(paneId) {
    return state.panes.get(paneId)?.runtimeId || "";
  }

  function serializeLayout(node) {
    if (!node) return null;
    if (node.type === "leaf") {
      const runtimeId = paneRuntimeId(node.paneId);
      return runtimeId ? { type: "leaf", runtimeId } : null;
    }
    const a = serializeLayout(node.a);
    const b = serializeLayout(node.b);
    if (!a) return b;
    if (!b) return a;
    const sizes = Array.isArray(node.sizes) && node.sizes.length === 2
      ? [Number(node.sizes[0]) || 50, Number(node.sizes[1]) || 50]
      : [50, 50];
    return { type: "split", dir: node.dir === "v" ? "v" : "h", sizes, a, b };
  }

  function layoutRuntimeIds(node, out = []) {
    if (!node) return out;
    if (node.type === "leaf") {
      if (node.runtimeId && !out.includes(node.runtimeId)) out.push(node.runtimeId);
      return out;
    }
    layoutRuntimeIds(node.a, out);
    layoutRuntimeIds(node.b, out);
    return out;
  }

  function pruneLayout(node, live) {
    if (!node) return null;
    if (node.type === "leaf") return live.has(node.runtimeId) ? node : null;
    const a = pruneLayout(node.a, live);
    const b = pruneLayout(node.b, live);
    if (!a) return b;
    if (!b) return a;
    return {
      type: "split",
      dir: node.dir === "v" ? "v" : "h",
      sizes: Array.isArray(node.sizes) && node.sizes.length === 2 ? node.sizes : [50, 50],
      a,
      b,
    };
  }

  function remapLayout(node, runtimeToPane) {
    if (!node) return null;
    if (node.type === "leaf") {
      const paneId = runtimeToPane.get(node.runtimeId);
      return paneId ? { type: "leaf", paneId } : null;
    }
    const a = remapLayout(node.a, runtimeToPane);
    const b = remapLayout(node.b, runtimeToPane);
    if (!a) return b;
    if (!b) return a;
    return {
      type: "split",
      dir: node.dir === "v" ? "v" : "h",
      sizes: Array.isArray(node.sizes) && node.sizes.length === 2 ? [...node.sizes] : [50, 50],
      a,
      b,
    };
  }

  function persistWorkspace() {
    if (restoringWorkspace || !workspaceReady) return;
    const tabs = [];
    for (const tab of state.tabs.values()) {
      if (tab.start || tab.remote || !tab.layout) continue;
      const layout = serializeLayout(tab.layout);
      if (!layout) continue;
      tabs.push({
        title: tab.customTitle || "",
        sync: !!tab.sync,
        focusedRuntimeId: paneRuntimeId(tab.focusedPaneId),
        zoomedRuntimeId: paneRuntimeId(tab.zoomedPaneId),
        layout,
      });
    }
    const activePane = state.activeId !== VAULT ? focusedPane() : null;
    const payload = {
      v: 1,
      active: state.activeId === VAULT ? VAULT : activePane?.runtimeId || "",
      tabs,
    };
    try {
      localStorage.setItem(WORKSPACE_KEY, JSON.stringify(payload));
    } catch {
      /* ignore quota */
    }
  }

  function loadWorkspace() {
    try {
      const raw = JSON.parse(localStorage.getItem(WORKSPACE_KEY) || "null");
      if (!raw || raw.v !== 1 || !Array.isArray(raw.tabs)) return null;
      return raw;
    } catch {
      return null;
    }
  }

  function restoreSavedTab(saved, live) {
    const layout = pruneLayout(saved?.layout, live);
    const runtimeIds = layoutRuntimeIds(layout);
    if (!runtimeIds.length) return;
    const runtimeToPane = new Map();
    let tab = null;
    for (const rid of runtimeIds) {
      const existing = findPaneByRuntime(rid);
      if (existing) {
        runtimeToPane.set(rid, existing.id);
        tab = state.tabs.get(existing.tabId) || tab;
        continue;
      }
      const info = state.herd.find((s) => s.id === rid);
      if (!info) continue;
      const pane = tab
        ? attachRuntime(info, { tab, splitDir: "h", splitPaneId: tab.focusedPaneId })
        : attachRuntime(info, { freshTab: true });
      if (!pane) continue;
      tab = state.tabs.get(pane.tabId);
      runtimeToPane.set(rid, pane.id);
    }
    if (!tab) return;
    const remapped = remapLayout(layout, runtimeToPane);
    if (remapped) {
      tab.layout = remapped;
      tab.dirty = true;
    }
    if (saved.title) tab.customTitle = saved.title;
    if (saved.sync) tab.sync = true;
    const focusedId = runtimeToPane.get(saved.focusedRuntimeId);
    if (focusedId) tab.focusedPaneId = focusedId;
    const zoomedId = runtimeToPane.get(saved.zoomedRuntimeId);
    if (zoomedId && leafCount(tab.layout) > 1) tab.zoomedPaneId = zoomedId;
  }

  function restoreOpenSessions() {
    restoringWorkspace = true;
    try {
      const saved = loadWorkspace();
      const live = new Set((state.herd || []).map((s) => s.id).filter(Boolean));
      if (saved?.tabs?.length) {
        for (const tab of saved.tabs) restoreSavedTab(tab, live);
      }
      for (const info of state.herd) {
        if (info?.id && !findPaneByRuntime(info.id)) attachRuntime(info);
      }
      if (saved?.active === VAULT) state.activeId = VAULT;
      else if (saved?.active) {
        const pane = findPaneByRuntime(saved.active);
        if (pane) {
          state.activeId = pane.tabId;
          const tab = state.tabs.get(pane.tabId);
          if (tab) tab.focusedPaneId = pane.id;
        }
      }
      renderTabs();
    } finally {
      restoringWorkspace = false;
      workspaceReady = true;
      persistWorkspace();
    }
  }

  function glyphSVG(kind) {
    if (kind === "folder") {
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M8 7H16.75C18.8567 7 19.91 7 20.6667 7.50559C20.9943 7.72447 21.2755 8.00572 21.4944 8.33329C22 9.08996 22 10.1433 22 12.25C22 15.7612 22 17.5167 21.1573 18.7779C20.7926 19.3238 20.3238 19.7926 19.7779 20.1573C18.5167 21 16.7612 21 13.25 21H12C7.28595 21 4.92893 21 3.46447 19.5355C2 18.0711 2 15.714 2 11V7.94427C2 6.1278 2 5.21956 2.38032 4.53806C2.65142 4.05227 3.05227 3.65142 3.53806 3.38032C4.21956 3 5.1278 3 6.94427 3C8.10802 3 8.6899 3 9.19926 3.19101C10.3622 3.62712 10.8418 4.68358 11.3666 5.73313L12 7"/></svg>`;
    }
    if (kind === "local") {
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M7.5 7.5L8.72654 8.55719C9.24218 9.00163 9.5 9.22386 9.5 9.5C9.5 9.77614 9.24218 9.99836 8.72654 10.4428L7.5 11.5"/><path d="M11.5 12.5H15.5"/><path d="M12 21C15.7497 21 17.6246 21 18.9389 20.0451C19.3634 19.7367 19.7367 19.3634 20.0451 18.9389C21 17.6246 21 15.7497 21 12C21 8.25027 21 6.3754 20.0451 5.06107C19.7367 4.6366 19.3634 4.26331 18.9389 3.95491C17.6246 3 15.7497 3 12 3C8.25027 3 6.3754 3 5.06107 3.95491C4.6366 4.26331 4.26331 4.6366 3.95491 5.06107C3 6.3754 3 8.25027 3 12C3 15.7497 3 17.6246 3.95491 18.9389C4.26331 19.3634 4.6366 19.7367 5.06107 20.0451C6.3754 21 8.25027 21 12 21Z"/></svg>`;
    }
    if (kind === "password") {
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M16 9H19"/><path d="M16 13H19"/><path d="M4.99806 16.6018C5.28942 15.7247 5.83071 14.9651 6.5472 14.4277C7.26369 13.8904 8.12005 13.6018 8.99806 13.6018C9.87606 13.6018 10.7324 13.8904 11.4489 14.4277C12.1654 14.9651 12.7067 15.7247 12.9981 16.6018"/><path d="M8.94922 11.3984C10.0538 11.3984 10.9492 10.503 10.9492 9.39844C10.9492 8.29387 10.0538 7.39844 8.94922 7.39844C7.84465 7.39844 6.94922 8.29387 6.94922 9.39844C6.94922 10.503 7.84465 11.3984 8.94922 11.3984Z"/><path d="M15 4H9C5.70017 4 4.05025 4 3.02513 5.02513C2 6.05025 2 7.70017 2 11V13C2 16.2998 2 17.9497 3.02513 18.9749C4.05025 20 5.70017 20 9 20H15C18.2998 20 19.9497 20 20.9749 18.9749C22 17.9497 22 16.2998 22 13V11C22 7.70017 22 6.05025 20.9749 5.02513C19.9497 4 18.2998 4 15 4Z"/></svg>`;
    }
    if (kind === "key") {
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M15.5 14.5C18.8137 14.5 21.5 11.8137 21.5 8.5C21.5 5.18629 18.8137 2.5 15.5 2.5C12.1863 2.5 9.5 5.18629 9.5 8.5C9.5 9.38041 9.68962 10.2165 10.0303 10.9697L2.5 18.5V21.5H5.5V19.5H7.5V17.5H9.5L13.0303 13.9697C13.7835 14.3104 14.6196 14.5 15.5 14.5Z"/><path d="M17.5 6.5L16.5 7.5"/></svg>`;
    }
    if (kind === "snippet") {
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M15 4L9 20"/><path d="M5.99997 16C5.99997 16 2.00001 13.054 2 12C1.99999 10.9459 6 8 6 8"/><path d="M18 8C18 8 22 10.946 22 12C22 13.0541 18 16 18 16"/></svg>`;
    }
    if (kind === "log") {
      return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M2 7C2 7.94281 2 8.41421 2.29289 8.70711C2.58579 9 3.05719 9 4 9H6V5C6 3.89543 5.10457 3 4 3C2.89543 3 2 3.89543 2 5V7Z"/><path d="M10 8H15M10 11H13"/><path d="M19 16V7C19 5.11438 19 4.17157 18.4142 3.58579C17.8284 3 16.8856 3 15 3H4"/></svg>`;
    }
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="size-4"><path d="M19 4H5C4.06812 4 3.60218 4 3.23463 4.15224C2.74458 4.35523 2.35523 4.74458 2.15224 5.23463C2 5.60218 2 6.06812 2 7C2 7.93188 2 8.39782 2.15224 8.76537C2.35523 9.25542 2.74458 9.64477 3.23463 9.84776C3.60218 10 4.06812 10 5 10H19C19.9319 10 20.3978 10 20.7654 9.84776C21.2554 9.64477 21.6448 9.25542 21.8478 8.76537C22 8.39782 22 7.93188 22 7C22 6.06812 22 5.60218 21.8478 5.23463C21.6448 4.74458 21.2554 4.35523 20.7654 4.15224C20.3978 4 19.9319 4 19 4Z"/><path d="M19 14H5C4.06812 14 3.60218 14 3.23463 14.1522C2.74458 14.3552 2.35523 14.7446 2.15224 15.2346C2 15.6022 2 16.0681 2 17C2 17.9319 2 18.3978 2.15224 18.7654C2.35523 19.2554 2.74458 19.6448 3.23463 19.8478C3.60218 20 4.06812 20 5 20H19C19.9319 20 20.3978 20 20.7654 19.8478C21.2554 19.6448 21.6448 19.2554 21.8478 18.7654C22 18.3978 22 17.9319 22 17C22 16.0681 22 15.6022 21.8478 15.2346C21.6448 14.7446 21.2554 14.3552 20.7654 14.1522C20.3978 14 19.9319 14 19 14Z"/></svg>`;
  }

  function tileGlyph(color, kind) {
    if (kind === "folder") {
      return `<span class="bg-primary/15 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg">${glyphSVG("folder")}</span>`;
    }
    const c = color || "#5ee0b5";
    const inner = kind === "L" || kind === "R" || kind === "D"
      ? `<span class="text-xs font-semibold">${kind}</span>`
      : glyphSVG(kind);
    return `<span class="flex size-9 shrink-0 items-center justify-center rounded-lg" style="--host-color:${c};background:color-mix(in srgb, var(--host-color) 22%, transparent);color:var(--host-color)">${inner}</span>`;
  }

  function emptyHTML(title, desc) {
    return `<div data-slot="empty" class="gap-4 rounded-xl border-dashed p-6 flex w-full flex-col items-center justify-center text-center"><div data-slot="empty-title" class="text-sm font-medium tracking-tight">${escapeHtml(title)}</div><div data-slot="empty-description" class="text-muted-foreground text-sm">${escapeHtml(desc)}</div></div>`;
  }

  function tileSection(title, cards) {
    return `<section class="flex flex-col gap-2"><h2 class="text-muted-foreground text-xs font-medium tracking-wider uppercase">${escapeHtml(title)}</h2><div class="tile-grid">${cards}</div></section>`;
  }

  function renderHosts() {
    const groups = groupHosts(state.hosts);
    if (!groups.size) {
      els.hostList.innerHTML = `<div data-slot="empty" class="gap-4 rounded-xl border-dashed p-6 flex w-full flex-col items-center justify-center text-center"><div data-slot="empty-title" class="text-sm font-medium tracking-tight">No hosts match</div><div data-slot="empty-description" class="text-muted-foreground text-sm">Add a server or clear the search.</div></div>`;
      return;
    }
    const folders = [...groups.entries()].filter(([name]) => name && name !== "Hosts" && name !== "Ungrouped");
    const hosts = [...groups.values()].flat();
    const html = [];
    if (folders.length) {
      html.push(`<section class="flex flex-col gap-2"><h2 class="text-muted-foreground text-xs font-medium tracking-wider uppercase">Groups</h2><div class="tile-grid">`);
      for (const [name, list] of folders) {
        html.push(`
          <div class="ring-foreground/10 bg-card text-card-foreground host-tile cursor-pointer overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" data-group-filter="${escapeHtml(name)}" role="button" tabindex="0">
            <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
              ${tileGlyph("", "folder")}
              <div class="min-w-0 flex-1">
                <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(name)}</div>
                <p data-slot="card-description" class="text-muted-foreground text-sm">${list.length === 1 ? "1 Host" : `${list.length} Hosts`}</p>
              </div>
            </div>
          </div>`);
      }
      html.push(`</div></section>`);
    }
    html.push(`<section class="flex flex-col gap-2"><h2 class="text-muted-foreground text-xs font-medium tracking-wider uppercase">Hosts</h2><div class="tile-grid">`);
    for (const h of hosts) {
      const kind = h.authType === "local" ? "local" : "ssh";
      const groupBadge = h.group && h.group !== "Hosts" ? `<span data-slot="badge" class="bg-secondary text-secondary-foreground h-5 rounded-4xl px-2 text-xs">${escapeHtml(h.group)}</span>` : "";
      html.push(`
        <div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" data-id="${h.id}">
          <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
            ${tileGlyph(h.color, kind)}
            <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-connect="${h.id}">
              <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(h.name)}</div>
              <p data-slot="card-description" class="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
                <span data-slot="badge" class="border-border h-5 rounded-4xl border px-2 text-xs">${kind}</span>
                ${groupBadge}
                <span class="truncate">${escapeHtml(hostSubtitle(h))}</span>
              </p>
            </div>
            <div class="flex items-center gap-1">
              <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-edit="${h.id}" title="Edit">Edit</button>
              ${h.authType === "local" ? "" : `<button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-delete="${h.id}" title="Remove">✕</button>`}
            </div>
          </div>
        </div>`);
    }
    html.push(`</div></section>`);
    els.hostList.innerHTML = html.join("");
  }

  function renderSnippets() {
    if (!state.snippets.length) {
      els.snippetList.innerHTML = emptyHTML("No snippets yet", "Save commands you type all the time. Click one to send it into the focused pane.");
    } else {
      const cards = state.snippets
        .map(
          (s) => `
        <div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" data-id="${s.id}">
          <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
            ${tileGlyph("#6ea8ff", "snippet")}
            <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-run="${s.id}">
              <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(s.name)}</div>
              <p data-slot="card-description" class="text-muted-foreground snippet-cmd truncate text-sm">${escapeHtml(s.command.trim())}</p>
            </div>
            <div class="flex items-center gap-1">
              <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-edit-snippet="${s.id}" title="Edit">Edit</button>
              <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-delete-snippet="${s.id}" title="Remove">✕</button>
            </div>
          </div>
        </div>`
        )
        .join("");
      els.snippetList.innerHTML = tileSection("Snippets", cards);
    }
  }

  function renderIdentities() {
    if (!state.identities.length) {
      els.identityList.innerHTML = emptyHTML("No identities yet", "Add a password or key and attach it to hosts.");
      return;
    }
    const cards = state.identities
      .map((id) => {
        const kind = id.kind === "key" ? "key" : "password";
        const label = kind === "key" ? "Private key" : "Auth password";
        const color = kind === "key" ? "#c084fc" : "#6ea8ff";
        return `
        <div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" data-id="${id.id}">
          <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
            ${tileGlyph(color, kind)}
            <div class="min-w-0 flex-1 cursor-pointer" role="button" tabindex="0" data-edit-identity="${id.id}">
              <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(id.name)}</div>
              <p data-slot="card-description" class="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
                <span data-slot="badge" class="border-border h-5 rounded-4xl border px-2 text-xs">${escapeHtml(label)}</span>
                ${id.username ? `<span class="truncate">${escapeHtml(id.username)}</span>` : ""}
              </p>
            </div>
            <div class="flex items-center gap-1">
              <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-edit-identity="${id.id}" title="Edit">Edit</button>
              <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-delete-identity="${id.id}" title="Remove">✕</button>
            </div>
          </div>
        </div>`;
      })
      .join("");
    els.identityList.innerHTML = tileSection("Identities", cards);
  }

  function renderTunnels() {
    if (!state.tunnels.length) {
      els.tunnelList.innerHTML = emptyHTML("No forwards yet", "Local (−L), remote (−R), or dynamic SOCKS5 (−D).");
      return;
    }
    const cards = state.tunnels
      .map((t) => {
        const host = state.hosts.find((h) => h.id === t.hostId);
        const dest = t.kind === "dynamic" ? "SOCKS5" : `${t.destHost}:${t.destPort}`;
        const listen = t.listenAddr || `${t.bindAddr}:${t.bindPort || "auto"}`;
        const st = t.status || "stopped";
        const letter = t.kind === "remote" ? "R" : t.kind === "dynamic" ? "D" : "L";
        const kindLabel = t.kind === "remote" ? "Remote" : t.kind === "dynamic" ? "Dynamic" : "Local";
        return `
          <div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm" data-id="${t.id}">
            <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
              ${tileGlyph(statusColor(st), letter)}
              <div class="min-w-0 flex-1">
                <div data-slot="card-title" class="truncate text-sm font-medium">${escapeHtml(t.name)}</div>
                <p data-slot="card-description" class="text-muted-foreground flex flex-wrap items-center gap-1 text-sm">
                  <span data-slot="badge" class="border-border h-5 rounded-4xl border px-2 text-xs">${kindLabel}</span>
                  <span data-slot="badge" class="bg-secondary text-secondary-foreground h-5 rounded-4xl px-2 text-xs">${escapeHtml(st)}</span>
                  <span class="truncate">${escapeHtml(host?.name || t.hostId)} · ${escapeHtml(listen)} → ${escapeHtml(dest)}</span>
                  ${t.error ? `<span class="text-destructive truncate">${escapeHtml(t.error)}</span>` : ""}
                </p>
              </div>
              <div class="flex items-center gap-1">
                <button type="button" class="hover:bg-muted hover:text-foreground inline-flex h-6 items-center rounded-md px-2 text-xs" data-tunnel-start="${t.id}">Start</button>
                <button type="button" class="hover:bg-muted hover:text-foreground inline-flex h-6 items-center rounded-md px-2 text-xs" data-tunnel-stop="${t.id}">Stop</button>
                <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-edit-tunnel="${t.id}" title="Edit">Edit</button>
                <button type="button" class="hover:bg-muted hover:text-foreground inline-flex size-6 items-center justify-center rounded-md text-xs" data-delete-tunnel="${t.id}" title="Remove">✕</button>
              </div>
            </div>
          </div>`;
      })
      .join("");
    els.tunnelList.innerHTML = tileSection("Port forwarding", cards);
  }

  function logColor(kind) {
    const k = String(kind || "").toLowerCase();
    if (k === "error" || k === "fail" || k === "denied") return "#ff6b7a";
    if (k === "session") return "#5ee0b5";
    if (k === "tunnel") return "#6ea8ff";
    return "#8a8a8a";
  }

  function logParts(iso) {
    if (!iso) return { date: "", time: "" };
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return { date: "", time: "" };
    return {
      date: d.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" }),
      time: d.toLocaleTimeString(undefined, { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" }),
    };
  }

  function renderLogs() {
    const q = state.logFilter.trim().toLowerCase();
    const rows = state.logs.filter((e) => {
      if (!q) return true;
      return `${e.kind} ${e.message}`.toLowerCase().includes(q);
    });
    if (!rows.length) {
      els.logList.innerHTML = emptyHTML("No log entries", "Connection and vault events show up here.");
      return;
    }
    const cards = rows
      .map((e) => {
        const { date, time } = logParts(e.time);
        return `<div class="ring-foreground/10 bg-card text-card-foreground host-tile overflow-hidden rounded-xl py-0 text-sm ring-1 hover:bg-muted/50" data-slot="card" data-size="sm">
          <div data-slot="card-content" class="flex flex-row items-center gap-3 px-3 py-3">
            ${tileGlyph(logColor(e.kind), "log")}
            <div class="log-when">
              <div data-slot="card-title" class="text-sm font-medium">${escapeHtml(date)}</div>
              <p data-slot="card-description" class="text-muted-foreground text-sm">${escapeHtml(time)}</p>
            </div>
            <span data-slot="badge" class="kind bg-secondary text-secondary-foreground h-5 shrink-0 rounded-4xl px-2 text-xs">${escapeHtml(e.kind)}</span>
            <div class="text-muted-foreground min-w-0 flex-1 truncate text-sm">${escapeHtml(e.message)}</div>
          </div>
        </div>`;
      })
      .join("");
    els.logList.innerHTML = `<section class="flex flex-col gap-2"><h2 class="text-muted-foreground text-xs font-medium tracking-wider uppercase">Logs</h2><div class="flex flex-col gap-2">${cards}</div></section>`;
  }

  function isDriving(pane) {
    return pane?.you?.role === "driver";
  }

  function isOwner(pane) {
    return pane?.you?.id && pane.you.id === pane.ownerId;
  }

  function focusedPane() {
    const tab = state.tabs.get(state.activeId);
    if (!tab) return null;
    return state.panes.get(tab.focusedPaneId) || tabPanes(tab)[0] || null;
  }

  function tabPanes(tab) {
    const out = [];
    walkLeaves(tab.layout, (id) => {
      const p = state.panes.get(id);
      if (p) out.push(p);
    });
    return out;
  }

  function visiblePanes(tab) {
    if (!tab) return [];
    if (tab.zoomedPaneId && state.panes.has(tab.zoomedPaneId)) return [state.panes.get(tab.zoomedPaneId)];
    return tabPanes(tab);
  }

  function walkLeaves(node, fn) {
    if (!node) return;
    if (node.type === "leaf") {
      fn(node.paneId);
      return;
    }
    walkLeaves(node.a, fn);
    walkLeaves(node.b, fn);
  }

  function leafCount(node) {
    if (!node) return 0;
    if (node.type === "leaf") return 1;
    return leafCount(node.a) + leafCount(node.b);
  }

  function replaceLeaf(node, paneId, next) {
    if (!node) return node;
    if (node.type === "leaf") return node.paneId === paneId ? next : node;
    return { ...node, a: replaceLeaf(node.a, paneId, next), b: replaceLeaf(node.b, paneId, next) };
  }

  function removeLeaf(node, paneId) {
    if (!node) return null;
    if (node.type === "leaf") return node.paneId === paneId ? null : node;
    const a = removeLeaf(node.a, paneId);
    const b = removeLeaf(node.b, paneId);
    if (!a) return b;
    if (!b) return a;
    return { ...node, a, b };
  }

  function swapLeaves(node, aId, bId) {
    if (!node || aId === bId) return node;
    if (node.type === "leaf") {
      if (node.paneId === aId) return { ...node, paneId: bId };
      if (node.paneId === bId) return { ...node, paneId: aId };
      return node;
    }
    return { ...node, a: swapLeaves(node.a, aId, bId), b: swapLeaves(node.b, aId, bId) };
  }

  function insertBeside(layout, targetId, incoming, edge) {
    if (!layout || !incoming || !edge || edge === "center") return layout;
    const dir = edge === "left" || edge === "right" ? "h" : "v";
    const incomingFirst = edge === "left" || edge === "top";
    const targetLeaf = { type: "leaf", paneId: targetId };
    return replaceLeaf(layout, targetId, {
      type: "split",
      dir,
      sizes: [50, 50],
      a: incomingFirst ? incoming : targetLeaf,
      b: incomingFirst ? targetLeaf : incoming,
    });
  }

  function rehomeLayout(node, tabId) {
    walkLeaves(node, (id) => {
      const p = state.panes.get(id);
      if (p) p.tabId = tabId;
    });
  }

  function findPaneByRuntime(runtimeId) {
    return [...state.panes.values()].find((p) => p.runtimeId === runtimeId);
  }

  function tabStatus(tab) {
    const panes = tabPanes(tab);
    if (panes.some((p) => p.status === "is-error")) return "is-error";
    if (panes.some((p) => p.status === "is-connecting")) return "is-connecting";
    if (panes.length && panes.every((p) => p.status === "is-ready")) return "is-ready";
    return "";
  }

  function tabTitle(tab) {
    if (tab.customTitle) return tab.customTitle;
    if (tab.start) return tab.title || "New Tab";
    const panes = tabPanes(tab);
    const first = panes[0];
    const name = first?.title || tab.title || "Session";
    const n = panes.length;
    const follow = first && !isDriving(first) && first.you ? " · follow" : "";
    const extra = n > 1 ? ` · ${n}` : "";
    return `${name}${extra}${follow}`;
  }

  function chromeTabs() {
    return [VAULT, ...state.tabs.keys()];
  }

  let tabDrag = null;
  let paneDrag = null;
  let ignoreTabClick = false;
  let tabRenaming = null;
  let tabMenuTarget = "";
  let dropOverlay = null;

  function reorderTabIds(ids, fromId, toIndex) {
    const from = ids.indexOf(fromId);
    if (from < 0) return ids;
    if (toIndex === from || toIndex === from + 1) return ids;
    const next = ids.slice();
    next.splice(from, 1);
    const at = Math.max(0, Math.min(toIndex > from ? toIndex - 1 : toIndex, next.length));
    next.splice(at, 0, fromId);
    return next;
  }

  function moveTabToIndex(fromId, toIndex) {
    if (!fromId || fromId === VAULT || !state.tabs.has(fromId)) return false;
    const ids = [...state.tabs.keys()];
    const next = reorderTabIds(ids, fromId, toIndex);
    if (next.length !== ids.length || next.every((id, i) => id === ids[i])) return false;
    const tabs = new Map();
    for (const id of next) tabs.set(id, state.tabs.get(id));
    state.tabs = tabs;
    return true;
  }

  function tabStripInsertIndex(clientX) {
    const buttons = [...els.tabs.querySelectorAll(".tab[data-tab]")].filter((el) => el.dataset.tab !== VAULT);
    for (let i = 0; i < buttons.length; i++) {
      const r = buttons[i].getBoundingClientRect();
      if (clientX < r.left + r.width / 2) return i;
    }
    return buttons.length;
  }

  function clearTabDropMarks() {
    for (const el of els.tabs.querySelectorAll(".is-drop-before, .is-drop-after, .is-dragging")) {
      el.classList.remove("is-drop-before", "is-drop-after", "is-dragging");
    }
  }

  function paintTabDrop(toIndex) {
    clearTabDropMarks();
    if (!tabDrag) return;
    const src = els.tabs.querySelector(`.tab[data-tab="${CSS.escape(tabDrag.id)}"]`);
    src?.classList.add("is-dragging");
    src?.setAttribute("aria-grabbed", "true");
    const buttons = [...els.tabs.querySelectorAll(".tab[data-tab]")].filter((el) => el.dataset.tab !== VAULT);
    const vault = els.tabs.querySelector(".tab.is-vault");
    if (!buttons.length) {
      vault?.classList.add("is-drop-after");
      return;
    }
    if (toIndex <= 0) {
      vault?.classList.add("is-drop-after");
      return;
    }
    if (toIndex >= buttons.length) {
      buttons[buttons.length - 1]?.classList.add("is-drop-after");
      return;
    }
    buttons[toIndex]?.classList.add("is-drop-before");
  }

  function autoScrollTabs(clientX) {
    const r = els.tabs.getBoundingClientRect();
    const edge = 36;
    if (clientX < r.left + edge) els.tabs.scrollLeft -= 14;
    else if (clientX > r.right - edge) els.tabs.scrollLeft += 14;
    syncTabOverflow();
  }

  function syncTabOverflow() {
    const el = els.tabs;
    const wrap = el?.closest(".tabs-scroller");
    if (!el || !wrap) return;
    const max = Math.max(0, el.scrollWidth - el.clientWidth);
    wrap.toggleAttribute("data-overflow-left", el.scrollLeft > 1);
    wrap.toggleAttribute("data-overflow-right", max - el.scrollLeft > 1);
  }

  function setTabMenuItem(sel, { hidden = false, disabled = false } = {}) {
    const el = document.querySelector(sel);
    if (!el) return;
    el.hidden = hidden;
    if ("disabled" in el) el.disabled = disabled;
    if (disabled) el.setAttribute("aria-disabled", "true");
    else el.removeAttribute("aria-disabled");
  }

  function syncTabContextMenu(id) {
    const vault = id === VAULT;
    const ids = [...state.tabs.keys()];
    const idx = vault ? -1 : ids.indexOf(id);
    const others = vault ? ids.length : ids.filter((tid) => tid !== id).length;
    const right = vault ? ids.length : idx < 0 ? 0 : ids.length - idx - 1;
    setTabMenuItem('[data-tab-action="rename"]', { hidden: vault, disabled: vault });
    setTabMenuItem('[data-tab-menu-sep="rename"]', { hidden: vault });
    setTabMenuItem('[data-tab-action="close"]', { hidden: vault, disabled: vault });
    setTabMenuItem('[data-tab-action="close-others"]', { disabled: others === 0 });
    setTabMenuItem('[data-tab-action="close-right"]', { disabled: right === 0 });
    setTabMenuItem('[data-tab-action="close-all"]', { disabled: ids.length === 0 });
  }

  function closeTabs(ids) {
    for (const id of ids) closeTab(id, { silent: true });
    renderTabs();
  }

  function applyTabRename(id, raw, ok) {
    const tab = state.tabs.get(id);
    tabRenaming = null;
    if (ok && tab) {
      const next = String(raw || "").trim();
      if (next) tab.customTitle = next;
      else delete tab.customTitle;
    }
  }

  function fitTabRename(input) {
    input.style.width = "0px";
    input.style.width = `${Math.max(1, Math.ceil(input.scrollWidth))}px`;
  }

  function beginTabRename(id) {
    if (id === VAULT || !state.tabs.has(id) || tabRenaming) return;
    const tab = state.tabs.get(id);
    const btn = els.tabs.querySelector(`.tab[data-tab="${CSS.escape(id)}"]`);
    const label = btn?.querySelector(".tab-label");
    if (!label) return;
    tabRenaming = id;
    const input = document.createElement("input");
    input.type = "text";
    input.className = "tab-rename";
    input.size = 1;
    input.value = tabTitle(tab);
    input.setAttribute("aria-label", "Rename tab");
    label.replaceWith(input);
    input.focus();
    input.select();
    fitTabRename(input);
    let done = false;
    const finish = (ok) => {
      if (done) return;
      done = true;
      applyTabRename(id, input.value, ok);
      renderTabs();
    };
    btn.addEventListener("click", (ev) => {
      if (ev.target === btn) {
        ev.preventDefault();
        ev.stopPropagation();
      }
    }, true);
    input.addEventListener("pointerdown", (ev) => ev.stopPropagation());
    input.addEventListener("keyup", (ev) => {
      ev.stopPropagation();
      if (ev.key === " " || ev.code === "Space") ev.preventDefault();
    });
    input.addEventListener("keypress", (ev) => ev.stopPropagation());
    input.addEventListener("input", () => fitTabRename(input));
    input.addEventListener("keydown", (ev) => {
      ev.stopPropagation();
      if (ev.key === " " || ev.code === "Space") {
        ev.preventDefault();
        const start = input.selectionStart ?? input.value.length;
        const end = input.selectionEnd ?? start;
        input.setRangeText(" ", start, end, "end");
        fitTabRename(input);
        return;
      }
      if (ev.key === "Enter") {
        ev.preventDefault();
        input.blur();
      } else if (ev.key === "Escape") {
        ev.preventDefault();
        finish(false);
      }
    });
    input.addEventListener("blur", () => finish(true));
  }

  function runTabContextAction(action, id) {
    if (action === "rename") {
      requestAnimationFrame(() => beginTabRename(id));
      return;
    }
    if (action === "close") {
      closeTab(id);
      return;
    }
    const ids = [...state.tabs.keys()];
    if (action === "close-others") {
      closeTabs(ids.filter((tid) => tid !== id));
      return;
    }
    if (action === "close-right") {
      const idx = ids.indexOf(id);
      if (idx >= 0) closeTabs(ids.slice(idx + 1));
      else if (id === VAULT) closeTabs(ids);
      return;
    }
    if (action === "close-all") closeTabs(ids);
  }

  function endTabDrag() {
    if (!tabDrag) return;
    const src = els.tabs.querySelector(`.tab[data-tab="${CSS.escape(tabDrag.id)}"]`);
    src?.removeAttribute("aria-grabbed");
    tabDrag = null;
    document.body.classList.remove("is-tab-dragging");
    clearTabDropMarks();
    hidePaneDrop();
  }

  function endPaneDrag() {
    if (!paneDrag) return;
    const pane = state.panes.get(paneDrag.paneId);
    pane?.wrap?.classList.remove("is-dragging");
    paneDrag = null;
    document.body.classList.remove("is-pane-dragging");
    hidePaneDrop();
    clearTabDropMarks();
  }

  function paneGlyph(name) {
    const icons = {
      splitH: '<path d="M11.9922 21L11.9922 20L11.9922 4L11.9922 3" /><path d="M7.99219 19.9995C6.69208 19.8699 5.81694 19.5772 5.16376 18.9157C3.99219 17.7291 3.99219 15.8193 3.99219 11.9997C3.99219 8.18016 3.99219 6.27038 5.16376 5.08379C5.81694 4.42224 6.69207 4.12952 7.99219 4" /><path d="M15.9922 19.9998C17.2923 19.8703 18.1674 19.5776 18.8206 18.916C19.9922 17.7294 19.9922 15.8196 19.9922 12.0001C19.9922 8.1805 19.9922 6.27072 18.8206 5.08413C18.192 4.44741 17.3577 4.15236 16.1368 4.01562" />',
      splitV: '<path d="M20.9922 11.9993L2.99219 11.9993" /><path d="M19.9917 15.9993C19.8621 17.2994 19.5694 18.1745 18.9079 18.8277C17.7213 19.9993 15.8115 19.9993 11.9919 19.9993C8.17235 19.9993 6.26257 19.9993 5.07598 18.8277C4.41443 18.1745 4.12171 17.2994 3.99219 15.9993" /><path d="M19.992 7.99927C19.8625 6.69915 19.5697 5.82402 18.9082 5.17084C17.7216 3.99927 15.8118 3.99927 11.9923 3.99927C8.17269 3.99927 6.26291 3.99927 5.07632 5.17084C4.4396 5.7995 4.14454 6.63376 4.00781 7.85464" />',
      sync: '<circle cx="12" cy="12" r="2" /><path d="M7.5 8C6.5 9 6 10.5 6 12C6 13.5 6.5 15 7.5 16" /><path d="M4.5 6C3 7.5 2 9.5 2 12C2 14.5 3 16.5 4.5 18" /><path d="M16.5 16C17.5 15 18 13.5 18 12C18 10.5 17.5 9 16.5 8" /><path d="M19.5 18C21 16.5 22 14.5 22 12C22 9.5 21 7.5 19.5 6" />',
      zoom: '<path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7"/>',
      unzoom: '<path d="M4 14h6v6M20 10h-6V4M14 10 21 3M3 21l7-7"/>',
      unplug: '<path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/><path d="M8 6h8"/><path d="M10 6v4M14 6v4"/><path d="M9 10h6v2a3 3 0 0 1-6 0z"/><path d="M12 15v3"/><path d="M5 5l14 14"/>',
      prompt: '<rect x="4" y="5" width="16" height="14" rx="2"/><path d="M8 10l3 2.5L8 15"/><path d="M13 15h3"/>',
      person: '<path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>',
      gear: '<circle cx="12" cy="12" r="3"/><path d="M12 3v2M12 19v2M4.2 6.2l1.4 1.4M18.4 16.4l1.4 1.4M3 12h2M19 12h2M4.2 17.8l1.4-1.4M18.4 7.6l1.4-1.4"/>',
      frown: '<circle cx="12" cy="12" r="9"/><path d="M8 15s1.5-2 4-2 4 2 4 2"/><path d="M9 9h.01M15 9h.01"/>',
    };
    return hugeicon(icons[name] || "");
  }

  function ensurePaneDrop() {
    if (dropOverlay) return dropOverlay;
    dropOverlay = document.createElement("div");
    dropOverlay.className = "pane-drop";
    dropOverlay.hidden = true;
    dropOverlay.innerHTML = `<div class="pane-drop-region"></div>`;
    document.body.appendChild(dropOverlay);
    return dropOverlay;
  }

  function hidePaneDrop() {
    if (dropOverlay) dropOverlay.hidden = true;
  }

  function paintPaneDrop(target) {
    const overlay = ensurePaneDrop();
    if (!target?.paneId || !target.edge) {
      overlay.hidden = true;
      return;
    }
    const pane = state.panes.get(target.paneId);
    const wrap = pane?.wrap;
    if (!wrap?.isConnected) {
      overlay.hidden = true;
      return;
    }
    const r = wrap.getBoundingClientRect();
    overlay.hidden = false;
    overlay.style.left = `${r.left}px`;
    overlay.style.top = `${r.top}px`;
    overlay.style.width = `${r.width}px`;
    overlay.style.height = `${r.height}px`;
    overlay.dataset.edge = target.edge;
  }

  function dropEdgeForRect(rect, x, y) {
    const relX = (x - rect.left) / Math.max(1, rect.width);
    const relY = (y - rect.top) / Math.max(1, rect.height);
    const dist = { left: relX, right: 1 - relX, top: relY, bottom: 1 - relY };
    const edge = Object.keys(dist).reduce((best, key) => (dist[key] < dist[best] ? key : best), "left");
    if (dist[edge] > 0.28) return "center";
    return edge;
  }

  function hitTabStrip(x, y) {
    const strip = els.tabs?.closest(".tab-strip") || els.tabs;
    if (!strip) return false;
    const r = strip.getBoundingClientRect();
    return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom + 10;
  }

  function paneDropAt(x, y, opts = {}) {
    const excludePaneId = opts.excludePaneId || "";
    const fromTabId = opts.fromTabId || "";
    const tab = state.tabs.get(state.activeId);
    if (!tab || tab.start || !tab.layout) return null;
    for (const pane of tabPanes(tab)) {
      if (!pane.wrap?.isConnected) continue;
      const r = pane.wrap.getBoundingClientRect();
      if (x < r.left || x > r.right || y < r.top || y > r.bottom) continue;
      if (pane.id === excludePaneId) return { paneId: pane.id, edge: null, self: true };
      if (fromTabId && pane.tabId === fromTabId) return { paneId: pane.id, edge: null, self: true };
      return { paneId: pane.id, edge: dropEdgeForRect(r, x, y), self: false };
    }
    return null;
  }

  function syncPaneChrome(tab, pane) {
    if (!tab || !pane?.wrap) return;
    const focused = tab.focusedPaneId === pane.id;
    const zoomed = tab.zoomedPaneId === pane.id;
    pane.wrap.classList.toggle("is-focused", focused);
    pane.wrap.classList.toggle("is-sync", !!tab.sync);
    pane.wrap.classList.toggle("is-zoomed", zoomed);
    const title = pane.wrap.querySelector(".pane-title");
    if (title) title.textContent = pane.title || "session";
    syncPaneFail(pane);
    const syncBtn = pane.wrap.querySelector("[data-pane-sync]");
    if (syncBtn) {
      syncBtn.hidden = !!tab.remote;
      syncBtn.classList.toggle("is-on", !!tab.sync);
      syncBtn.setAttribute("aria-pressed", String(!!tab.sync));
      syncBtn.title = tab.sync ? "Stop syncing input" : "Sync input";
    }
    const zoomBtn = pane.wrap.querySelector("[data-pane-zoom]");
    if (zoomBtn) {
      const canZoom = leafCount(tab.layout) > 1 || !!tab.zoomedPaneId;
      zoomBtn.disabled = !canZoom;
      zoomBtn.classList.toggle("is-on", zoomed);
      zoomBtn.title = zoomed ? "Exit focus view" : "Focus view";
    }
    pane.wrap.querySelectorAll("[data-pane-split]").forEach((btn) => {
      btn.hidden = !!pane.remote || !pane.hostId;
    });
    const closeBtn = pane.wrap.querySelector("[data-pane-close]");
    if (closeBtn) closeBtn.title = leafCount(tab.layout) > 1 ? "Close pane" : "Close tab";
  }

  function markFocusedPane(pane) {
    const tab = state.tabs.get(pane.tabId);
    if (!tab) return;
    tab.focusedPaneId = pane.id;
    state.activeId = pane.tabId;
    for (const p of tabPanes(tab)) syncPaneChrome(tab, p);
    if (els.barStatus) {
      if (isDriving(pane)) els.barStatus.textContent = `${pane.subtitle} · driving`;
      else els.barStatus.textContent = `${pane.subtitle} · following`;
    }
    renderControl(pane);
    pane.term?.focus();
  }

  function paneBodyBox(pane) {
    const body = pane.wrap?.querySelector(".term-body");
    if (!body) return null;
    const r = body.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) return null;
    return r;
  }

  function flushPanePending(pane) {
    if (!pane?.pending?.length || !pane.term?.element) return;
    for (const chunk of pane.pending) pane.term.write(chunk);
    pane.pending = [];
  }

  function writePaneBytes(pane, data) {
    const chunk = data instanceof Uint8Array ? data : new Uint8Array(data);
    if (!pane.term?.element) {
      if (!pane.pending) pane.pending = [];
      pane.pending.push(chunk);
      return;
    }
    pane.term.write(chunk);
  }

  function reflowPane(pane) {
    if (!pane?.wrap?.isConnected || !pane.term || !pane.fit) return;
    try {
      if (!pane.term.element) return;
      let cell = pane.term._core?._renderService?.dimensions?.css?.cell;
      if (!cell?.width || !cell?.height) {
        pane.term.resize(80, 24);
        cell = pane.term._core?._renderService?.dimensions?.css?.cell;
      }
      const box = paneBodyBox(pane);
      if (box && cell?.width && cell?.height) {
        const ruler = pane.term.options.overviewRuler?.width || 0;
        const cols = Math.max(2, Math.floor((box.width - ruler) / cell.width));
        const rows = Math.max(1, Math.floor(box.height / cell.height));
        if (cols !== pane.term.cols || rows !== pane.term.rows) pane.term.resize(cols, rows);
      } else {
        pane.fit.fit();
      }
      pane.term.refresh(0, Math.max(0, (pane.term.rows || 1) - 1));
      sendResize(pane);
      flushPanePending(pane);
    } catch {
      /* ignore */
    }
  }

  function syncNativeSplitTheme(pane) {
    if (!isNativeWindow() || !pane?.term) return;
    const tab = state.tabs.get(pane.tabId);
    const split = tab && leafCount(tab.layout) > 1 && !tab.zoomedPaneId;
    const theme = { ...xtermTheme() };
    if (split) theme.background = resolvedTheme() === "light" ? "#fbfbfa" : "#0a0a0a";
    pane.term.options.theme = theme;
  }

  function refreshPaneRender(pane) {
    if (!pane?.term?.element) return;
    syncNativeSplitTheme(pane);
    reflowPane(pane);
    if (!isNativeWindow()) return;
    const cols = pane.term.cols;
    const rows = pane.term.rows;
    if (cols > 2 && rows > 1) {
      pane.term.resize(cols - 1, rows);
      pane.term.resize(cols, rows);
    }
    pane.term.refresh(0, Math.max(0, rows - 1));
  }

  function schedulePaneReflow(pane, tries = 12) {
    refreshPaneRender(pane);
    if (!isNativeWindow() || tries <= 0) return;
    if (paneBodyBox(pane)) return;
    requestAnimationFrame(() => schedulePaneReflow(pane, tries - 1));
  }

  function ensurePaneTerm(pane) {
    if (!pane?.term || !pane.wrap?.isConnected) return;
    if (!pane.term.element) {
      if (!pane.host) return;
      pane.term.open(pane.host);
      observePaneFit(pane);
    }
    if (paneWantsWebgl(pane)) attachWebgl(pane);
    else dropWebgl(pane);
    schedulePaneReflow(pane);
  }

  function observePaneFit(pane) {
    if (typeof ResizeObserver === "undefined" || pane.fitObs) return;
    pane.fitObs = new ResizeObserver(() => refreshPaneRender(pane));
    pane.fitObs.observe(pane.wrap);
  }

  function renderControl(pane) {
    if (!els.controlWidget) return;
    if (!pane || state.activeId === VAULT) {
      els.controlWidget.hidden = true;
      els.btnShare.hidden = true;
      return;
    }
    const attached = pane.attached || [];
    els.btnShare.hidden = !!pane.remote;
    if (!attached.length && !pane.you) {
      els.controlWidget.hidden = true;
      return;
    }
    els.controlWidget.hidden = false;
    const canGrant = isDriving(pane) || isOwner(pane);
    const people = attached
      .map((c) => {
        const driving = c.role === "driver" || c.id === pane.driverId;
        const grant = canGrant && !driving ? `data-grant="${c.id}"` : "";
        return `<button type="button" class="chip-person ${driving ? "is-driver" : ""} ${grant ? "is-grantable" : ""}" ${grant} title="${driving ? "Driving" : canGrant ? "Pass control" : ""}">${escapeHtml(c.name)}${driving ? " *" : ""}</button>`;
      })
      .join("");
    let action = "";
    if (!isDriving(pane) && isOwner(pane)) {
      action = `<button class="tiny" data-take="1">Take control</button>`;
    } else if (!isDriving(pane)) {
      action = `<button class="tiny" data-request="1">Request control</button>`;
    }
    els.controlWidget.innerHTML = `<div class="people">${people}</div>${action}`;
    pane.wrap?.classList.toggle("is-follow", !isDriving(pane));
    if (pane.term) pane.term.options.disableStdin = !isDriving(pane);
  }

  function setSettingsSection(id) {
    state.settingsSection = id;
    document.querySelectorAll("[data-settings-section]").forEach((btn) => {
      const on = btn.dataset.settingsSection === id;
      btn.classList.toggle("is-active", on);
      btn.setAttribute("aria-selected", on ? "true" : "false");
    });
    document.querySelectorAll("[data-settings-panel]").forEach((panel) => {
      const on = panel.dataset.settingsPanel === id;
      panel.classList.toggle("is-active", on);
      panel.hidden = !on;
    });
  }

  function filterSettings(query) {
    const q = query.trim().toLowerCase();
    document.querySelectorAll(".settings-panel .pref-row:not(.pref-row-preview)").forEach((row) => {
      row.hidden = q !== "" && !row.textContent.toLowerCase().includes(q);
    });
    document.querySelectorAll(".settings-group").forEach((group) => {
      const rows = group.querySelectorAll(".pref-row:not(.pref-row-preview)");
      group.hidden = q !== "" && ![...rows].some((row) => !row.hidden);
    });
  }

  function setVaultSection(name) {
    state.vaultSection = name;
    document.querySelectorAll(".vault-nav").forEach((b) => b.classList.toggle("is-active", b.dataset.vault === name));
    document.querySelectorAll(".vault-page").forEach((p) => p.classList.toggle("is-active", p.dataset.page === name));
    els.vault?.classList.toggle("is-settings", name === "settings");
    if (name === "logs") {
      loadLogs().catch(() => {});
      if (!state.logTimer) state.logTimer = setInterval(() => loadLogs().catch(() => {}), 3000);
    } else if (state.logTimer) {
      clearInterval(state.logTimer);
      state.logTimer = null;
    }
    if (name === "settings") {
      syncSettingsForm();
      setSettingsSection(state.settingsSection || "appearance");
    }
  }

  function showVault() {
    state.activeId = VAULT;
    renderTabs();
  }

  function ensureTabStage(tab) {
    if (!tab?.root) return;
    if (tab.stage?.isConnected) return;
    tab.stage = document.createElement("div");
    tab.stage.className = "term-stage";
    tab.root.replaceChildren(tab.stage);
    if (typeof ResizeObserver !== "undefined" && !tab.stageObs) {
      tab.stageObs = new ResizeObserver(() => {
        if (tab.start || !tab.layout) return;
        layoutTabPanes(tab);
        syncSashElements(tab);
        if (tab.id !== state.activeId) return;
        for (const p of visiblePanes(tab)) refreshPaneRender(p);
      });
      tab.stageObs.observe(tab.stage);
    }
  }

  function splitRect(tab, target) {
    const w = tab.stage?.clientWidth || 0;
    const h = tab.stage?.clientHeight || 0;
    let found = null;
    const walk = (node, x, y, rw, rh) => {
      if (!node || found) return;
      if (node === target) {
        found = { x, y, w: rw, h: rh };
        return;
      }
      if (node.type === "leaf") return;
      const total = node.sizes[0] + node.sizes[1] || 1;
      const aFrac = node.sizes[0] / total;
      if (node.dir === "h") {
        const aW = Math.max(0, Math.floor((rw - SASH_PX) * aFrac));
        walk(node.a, x, y, aW, rh);
        walk(node.b, x + aW + SASH_PX, y, Math.max(0, rw - SASH_PX - aW), rh);
      } else {
        const aH = Math.max(0, Math.floor((rh - SASH_PX) * aFrac));
        walk(node.a, x, y, rw, aH);
        walk(node.b, x, y + aH + SASH_PX, rw, Math.max(0, rh - SASH_PX - aH));
      }
    };
    if (tab.layout) walk(tab.layout, 0, 0, w, h);
    return found;
  }

  function placePane(tab, paneId, x, y, w, h) {
    const pane = state.panes.get(paneId);
    if (!pane?.wrap) return;
    if (pane.wrap.parentNode !== tab.stage) tab.stage.appendChild(pane.wrap);
    pane.wrap.classList.remove("is-parked");
    pane.wrap.style.position = "absolute";
    pane.wrap.style.left = `${x}px`;
    pane.wrap.style.top = `${y}px`;
    pane.wrap.style.width = `${Math.max(0, w)}px`;
    pane.wrap.style.height = `${Math.max(0, h)}px`;
  }

  function layoutSplit(tab, node, x, y, w, h, shown) {
    if (!node) return;
    if (node.type === "leaf") {
      placePane(tab, node.paneId, x, y, w, h);
      shown.add(node.paneId);
      return;
    }
    const total = node.sizes[0] + node.sizes[1] || 1;
    const aFrac = node.sizes[0] / total;
    if (node.dir === "h") {
      const aW = Math.max(0, Math.floor((w - SASH_PX) * aFrac));
      const bW = Math.max(0, w - SASH_PX - aW);
      layoutSplit(tab, node.a, x, y, aW, h, shown);
      layoutSplit(tab, node.b, x + aW + SASH_PX, y, bW, h, shown);
    } else {
      const aH = Math.max(0, Math.floor((h - SASH_PX) * aFrac));
      const bH = Math.max(0, h - SASH_PX - aH);
      layoutSplit(tab, node.a, x, y, w, aH, shown);
      layoutSplit(tab, node.b, x, y + aH + SASH_PX, w, bH, shown);
    }
  }

  function layoutTabPanes(tab) {
    if (!tab?.stage) return;
    const w = tab.stage.clientWidth || tab.root.clientWidth;
    const h = tab.stage.clientHeight || tab.root.clientHeight;
    if (w < 2 || h < 2) return;
    const shown = new Set();
    if (tab.zoomedPaneId && state.panes.has(tab.zoomedPaneId)) {
      placePane(tab, tab.zoomedPaneId, 0, 0, w, h);
      shown.add(tab.zoomedPaneId);
    } else if (tab.layout) {
      layoutSplit(tab, tab.layout, 0, 0, w, h, shown);
    }
    for (const pane of tabPanes(tab)) {
      if (shown.has(pane.id) || !pane.wrap) continue;
      if (pane.wrap.parentNode !== tab.stage) tab.stage.appendChild(pane.wrap);
      pane.wrap.classList.add("is-parked");
      pane.wrap.style.position = "absolute";
      pane.wrap.style.left = "-10000px";
      pane.wrap.style.top = "0px";
    }
  }

  function syncSashElements(tab) {
    if (!tab?.sashes) return;
    for (const { el, node } of tab.sashes) {
      const box = splitRect(tab, node);
      if (!box) continue;
      const total = node.sizes[0] + node.sizes[1] || 1;
      const aFrac = node.sizes[0] / total;
      if (node.dir === "h") {
        const aW = Math.max(0, Math.floor((box.w - SASH_PX) * aFrac));
        el.style.left = `${box.x + aW}px`;
        el.style.top = `${box.y}px`;
        el.style.width = `${SASH_PX}px`;
        el.style.height = `${box.h}px`;
      } else {
        const aH = Math.max(0, Math.floor((box.h - SASH_PX) * aFrac));
        el.style.left = `${box.x}px`;
        el.style.top = `${box.y + aH}px`;
        el.style.width = `${box.w}px`;
        el.style.height = `${SASH_PX}px`;
      }
    }
  }

  function bindAbsSash(el, node, tab) {
    el.addEventListener("pointerdown", (ev) => {
      ev.preventDefault();
      el.setPointerCapture(ev.pointerId);
      const move = (e) => {
        const box = splitRect(tab, node);
        if (!box) return;
        const stage = tab.stage.getBoundingClientRect();
        const total = node.dir === "h" ? box.w : box.h;
        if (total < 8) return;
        const origin = node.dir === "h" ? stage.left + box.x : stage.top + box.y;
        const pos = node.dir === "h" ? e.clientX : e.clientY;
        let pct = ((pos - origin) / total) * 100;
        pct = Math.min(80, Math.max(20, pct));
        node.sizes = [pct, 100 - pct];
        layoutTabPanes(tab);
        syncSashElements(tab);
        for (const p of visiblePanes(tab)) refreshPaneRender(p);
      };
      const up = () => {
        el.removeEventListener("pointermove", move);
        el.removeEventListener("pointerup", up);
        fitVisiblePanes();
        persistWorkspace();
      };
      el.addEventListener("pointermove", move);
      el.addEventListener("pointerup", up);
    });
  }

  function paintSashes(tab) {
    if (!tab?.stage) return;
    tab.stage.querySelectorAll(":scope > .split-sash").forEach((el) => el.remove());
    tab.sashes = [];
    if (tab.zoomedPaneId || !tab.layout || tab.layout.type === "leaf") return;
    const walk = (node) => {
      if (!node || node.type === "leaf") return;
      const el = document.createElement("div");
      el.className = `split-sash ${node.dir === "h" ? "is-vert" : "is-horiz"}`;
      tab.stage.appendChild(el);
      tab.sashes.push({ el, node });
      bindAbsSash(el, node, tab);
      walk(node.a);
      walk(node.b);
    };
    walk(tab.layout);
    syncSashElements(tab);
  }

  function paintLayout(tab) {
    ensureTabStage(tab);
    if (isNativeWindow() && visiblePanes(tab).length > 1) {
      for (const pane of tabPanes(tab)) dropWebgl(pane);
    }
    for (const pane of tabPanes(tab)) {
      syncPaneChrome(tab, pane);
      if (pane.wrap && pane.wrap.parentNode !== tab.stage) tab.stage.appendChild(pane.wrap);
    }
    layoutTabPanes(tab);
    paintSashes(tab);
    for (const pane of visiblePanes(tab)) ensurePaneTerm(pane);
  }

  function kbdHint(key) {
    return `<kbd class="kbd-hint" aria-hidden="true">${escapeHtml(key)}</kbd>`;
  }

  function hugeicon(paths) {
    return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
  }

  function tabGlyph() {
    return `<span class="tab-icon" aria-hidden="true">${hugeicon(`<path d="M7.5 7.5L8.72654 8.55719C9.24218 9.00163 9.5 9.22386 9.5 9.5C9.5 9.77614 9.24218 9.99836 8.72654 10.4428L7.5 11.5" /><path d="M11.5 12.5H15.5" /><path d="M12 21C15.7497 21 17.6246 21 18.9389 20.0451C19.3634 19.7367 19.7367 19.3634 20.0451 18.9389C21 17.6246 21 15.7497 21 12C21 8.25027 21 6.3754 20.0451 5.06107C19.7367 4.6366 19.3634 4.26331 18.9389 3.95491C17.6246 3 15.7497 3 12 3C8.25027 3 6.3754 3 5.06107 3.95491C4.6366 4.26331 4.26331 4.6366 3.95491 5.06107C3 6.3754 3 8.25027 3 12C3 15.7497 3 17.6246 3.95491 18.9389C4.26331 19.3634 4.6366 19.7367 5.06107 20.0451C6.3754 21 8.25027 21 12 21Z" />`)}</span>`;
  }

  function closeGlyph() {
    return hugeicon(`<path d="M18 6L6.00081 17.9992M17.9992 18L6 6.00085" />`);
  }

  function vaultGlyph() {
    return `<span class="tab-icon" aria-hidden="true">${hugeicon(`<path d="M13 2H11C6.75736 2 4.63604 2 3.31802 3.31802C2 4.63604 2 6.75736 2 11C2 15.2426 2 17.364 3.31802 18.682C4.63604 20 6.75736 20 11 20H13C17.2426 20 19.364 20 20.682 18.682C22 17.364 22 15.2426 22 11C22 6.75736 22 4.63604 20.682 3.31802C19.364 2 17.2426 2 13 2Z" /><path d="M17 15C18 14.4692 18 13.6148 18 11.9062V10.0938C18 8.38516 18 7.53082 17 7" /><path d="M18 22V20" /><path d="M6 22V20" /><path d="M9.49983 13C10.6158 13 11.5205 12.1046 11.5205 11C11.5205 9.89543 10.6158 9 9.49983 9M9.49983 13C8.38384 13 7.47914 12.1046 7.47914 11C7.47914 9.89543 8.38384 9 9.49983 9M9.49983 13V15M9.49983 9V7M7.74987 12L5.9999 13M12.9998 9L11.2498 10M11.2499 12L12.9999 13M6 9L7.74997 10" />`)}</span>`;
  }

  function renderTabs() {
    const ids = [...state.tabs.keys()];
    const vaultOn = state.activeId === VAULT || !state.tabs.has(state.activeId);
    if (vaultOn) state.activeId = VAULT;
    const chrome = chromeTabs();
    const activeTab = vaultOn ? null : state.tabs.get(state.activeId);
    const sessionOn = !vaultOn && activeTab && !activeTab.start;

    const keepScroll = els.tabs.scrollLeft;
    if (!tabRenaming) {
      els.tabs.innerHTML =
        `<button type="button" class="tab is-vault ${vaultOn ? "is-active" : ""}" data-tab="${VAULT}">${kbdHint("1")}${vaultGlyph()}Vault</button>` +
        ids
          .map((id, i) => {
            const tab = state.tabs.get(id);
            const st = tabStatus(tab);
            const chromeIdx = i + 1;
            const num = chromeIdx < 8 ? String(chromeIdx + 1) : chromeIdx === chrome.length - 1 ? "9" : "";
            const closeHint = id === state.activeId ? kbdHint("W") : "";
            return `<button type="button" class="tab ${id === state.activeId ? "is-active" : ""} ${st} ${tab.sync ? "is-sync" : ""}" data-tab="${id}" title="Drag to reorder">
            ${num ? kbdHint(num) : ""}
            ${tabGlyph()}
            <span class="tab-label">${escapeHtml(tabTitle(tab))}</span>
            <span class="close" data-close="${id}" title="Close  ${MOD}W">${closeGlyph()}${closeHint}</span>
          </button>`;
          })
          .join("");
      els.tabs.scrollLeft = keepScroll;
      const activeBtn = els.tabs.querySelector(".tab.is-active");
      if (activeBtn) {
        const left = activeBtn.offsetLeft;
        const right = left + activeBtn.offsetWidth;
        const viewLeft = els.tabs.scrollLeft;
        const viewRight = viewLeft + els.tabs.clientWidth;
        if (left < viewLeft) els.tabs.scrollLeft = left;
        else if (right > viewRight) els.tabs.scrollLeft = right - els.tabs.clientWidth;
      }
    }
    syncTabOverflow();

    els.vault.classList.toggle("is-active", vaultOn);
    els.terminals.hidden = false;
    els.terminals.classList.toggle("is-active", !vaultOn);

    for (const tab of state.tabs.values()) {
      const split = leafCount(tab.layout) > 1;
      tab.root.classList.toggle("is-active", tab.id === state.activeId);
      tab.root.classList.toggle("is-split", split && !tab.zoomedPaneId);
      tab.root.classList.toggle("is-zoomed", !!tab.zoomedPaneId);
      if (tab.dirty) {
        if (tab.start) paintStartPage(tab);
        else if (tab.layout) paintLayout(tab);
        tab.dirty = false;
      }
      walkLeaves(tab.layout, (pid) => {
        const p = state.panes.get(pid);
        if (p) {
          syncPaneChrome(tab, p);
          syncNativeSplitTheme(p);
        }
      });
    }

    const pane = sessionOn ? focusedPane() : null;
    const tab = activeTab;
    if (els.barStatus) {
      if (vaultOn) els.barStatus.textContent = "Vault";
      else if (tab?.start) els.barStatus.textContent = tab.connecting ? "Connecting…" : "New tab";
      else if (!pane) els.barStatus.textContent = "No session";
      else if (isDriving(pane)) els.barStatus.textContent = `${pane.subtitle} · driving`;
      else els.barStatus.textContent = `${pane.subtitle} · following`;
    }
    renderControl(pane);
    if (tabDrag?.id) {
      const dragging = els.tabs.querySelector(`.tab[data-tab="${CSS.escape(tabDrag.id)}"]`);
      dragging?.classList.add("is-dragging");
      dragging?.setAttribute("aria-grabbed", "true");
    }
    if (!vaultOn) {
      requestAnimationFrame(() => {
        if (tab?.start) {
          const input = tab.root.querySelector(".start-search");
          if (input && document.activeElement !== input) input.focus();
          return;
        }
        fitVisiblePanes();
        pane?.term?.focus();
      });
    }
    persistWorkspace();
  }

  function fitVisiblePanes() {
    const tab = state.tabs.get(state.activeId);
    if (!tab) return;
    const run = () => {
      layoutTabPanes(tab);
      syncSashElements(tab);
      for (const p of visiblePanes(tab)) ensurePaneTerm(p);
    };
    requestAnimationFrame(() => requestAnimationFrame(run));
  }

  function applyHerdEvent(ev) {
    if (!ev || !ev.type) return;
    if (ev.type === "snapshot" && Array.isArray(ev.sessions)) {
      state.herd = ev.sessions;
      return;
    }
    if (ev.type === "spawn" && ev.session) {
      if (!state.herd.some((s) => s.id === ev.session.id)) state.herd.push(ev.session);
      else state.herd = state.herd.map((s) => (s.id === ev.session.id ? ev.session : s));
      return;
    }
    if (ev.type === "kill") {
      const id = ev.id || ev.session?.id;
      state.herd = state.herd.filter((s) => s.id !== id);
      const pane = findPaneByRuntime(id);
      if (pane) closePane(pane.id, { fromHerd: true });
      return;
    }
    if ((ev.type === "update" || ev.type === "presence" || ev.type === "control") && ev.session) {
      const i = state.herd.findIndex((s) => s.id === ev.session.id);
      if (i >= 0) state.herd[i] = { ...state.herd[i], ...ev.session };
      else state.herd.push(ev.session);
    }
  }

  function connectHerd() {
    if (state.herdWs) {
      try {
        state.herdWs.close();
      } catch {
        /* ignore */
      }
    }
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws/herd`);
    state.herdWs = ws;
    ws.addEventListener("message", (ev) => {
      try {
        applyHerdEvent(JSON.parse(ev.data));
      } catch {
        /* ignore */
      }
    });
    ws.addEventListener("close", () => {
      if (state.herdWs === ws) {
        state.herdWs = null;
        setTimeout(connectHerd, 2000);
      }
    });
  }

  async function loadLogs() {
    state.logs = await api("/api/logs?limit=200");
    if (!(await paintList("log-list", "/ui/logs?" + datastarQS()))) renderLogs();
  }

  async function loadAll() {
    const [hosts, snippets, herd, identities, tunnels] = await Promise.all([
      api("/api/hosts"),
      api("/api/snippets"),
      api("/api/sessions"),
      api("/api/identities"),
      api("/api/tunnels"),
    ]);
    state.hosts = hosts;
    state.snippets = snippets;
    state.herd = herd;
    state.identities = identities;
    state.tunnels = tunnels;
    const q = datastarQS();
    const [hostsOk, snOk, idOk, tnOk] = await Promise.all([
      paintList("host-list", "/ui/hosts?" + q),
      paintList("snippet-list", "/ui/snippets?" + q),
      paintList("identity-list", "/ui/identities?" + q),
      paintList("tunnel-list", "/ui/tunnels?" + q),
    ]);
    if (!hostsOk) renderHosts();
    if (!snOk) renderSnippets();
    if (!idOk) renderIdentities();
    if (!tnOk) renderTunnels();
    refreshStartTabs();
    if (state.vaultSection === "logs") await loadLogs().catch(() => {});
  }

  function localHost() {
    return state.hosts.find((h) => h.authType === "local");
  }

  const RECENT_KEY = "keel-recent-hosts";
  const RECENT_MAX = 12;

  function loadRecentIds() {
    try {
      const ids = JSON.parse(localStorage.getItem(RECENT_KEY) || "[]");
      return Array.isArray(ids) ? ids.filter((id) => typeof id === "string") : [];
    } catch {
      return [];
    }
  }

  function rememberRecent(hostId) {
    if (!hostId) return;
    const ids = [hostId, ...loadRecentIds().filter((id) => id !== hostId)].slice(0, RECENT_MAX);
    localStorage.setItem(RECENT_KEY, JSON.stringify(ids));
  }

  function hostMatches(h, q) {
    const needle = (q || "").trim().toLowerCase();
    if (!needle) return true;
    return `${h.name} ${h.group} ${h.address} ${h.username}`.toLowerCase().includes(needle);
  }

  function startHostList(tab) {
    const q = tab.query || "";
    const byId = new Map(state.hosts.map((h) => [h.id, h]));
    const recent = [];
    for (const id of loadRecentIds()) {
      const h = byId.get(id);
      if (h && hostMatches(h, q)) recent.push(h);
    }
    const local = localHost();
    if (local && hostMatches(local, q) && !recent.some((h) => h.id === local.id)) recent.unshift(local);
    const seen = new Set(recent.map((h) => h.id));
    const rest = state.hosts.filter((h) => !seen.has(h.id) && hostMatches(h, q));
    return { recent, rest };
  }

  function visibleStartHosts(tab) {
    const { recent, rest } = startHostList(tab);
    return [...recent, ...rest];
  }

  function startHostButton(h, tab) {
    const picked = tab.pick === h.id;
    const connecting = tab.connecting === h.id;
    return `<button type="button" class="host ${picked ? "is-open" : ""}" data-start-connect="${h.id}">
      <span class="dot" style="--dot:${h.color};background:${h.color}"></span>
      <span class="meta">
        <b>${escapeHtml(h.name)}</b>
        <small>${connecting ? "Connecting…" : escapeHtml(hostSubtitle(h))}</small>
      </span>
    </button>`;
  }

  function paintStartPage(tab) {
    const prev = tab.root.querySelector(".start-search");
    const keepFocus = document.activeElement === prev;
    const caret = typeof prev?.selectionStart === "number" ? prev.selectionStart : null;
    const hosts = visibleStartHosts(tab);
    if (!hosts.some((h) => h.id === tab.pick)) tab.pick = hosts[0]?.id || "";
    const { recent, rest } = startHostList(tab);
    const recentHtml = recent.length
      ? `<h2 class="group-label">Recent</h2><div class="host-list">${recent.map((h) => startHostButton(h, tab)).join("")}</div>`
      : "";
    const restHtml = (() => {
      if (!rest.length) return "";
      const groups = new Map();
      for (const h of rest) {
        const key = h.group || "Ungrouped";
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key).push(h);
      }
      const parts = [`<h2 class="group-label">${recent.length ? "All" : "Hosts"}</h2>`];
      for (const [group, hosts] of groups) {
        parts.push(`<div class="group-label">${escapeHtml(group)}</div>`);
        parts.push(`<div class="host-list">${hosts.map((h) => startHostButton(h, tab)).join("")}</div>`);
      }
      return parts.join("");
    })();
    const empty = !recent.length && !rest.length ? `<p class="empty">No hosts match.</p>` : "";
    tab.root.innerHTML = `<div class="start-page"><div class="start-inner">
      <p class="eyebrow">Connect</p>
      <h1>New tab</h1>
      <div class="search"><input type="search" class="start-search" placeholder="Search hosts" autocomplete="off" /></div>
      ${recentHtml}${restHtml}${empty}
      <div class="page-actions">
        <button type="button" class="btn btn-ghost" data-start-quick>Quick connect</button>
        <button type="button" class="btn btn-ghost" data-start-join>Join session</button>
      </div>
    </div></div>`;
    const input = tab.root.querySelector(".start-search");
    if (input) input.value = tab.query || "";
    if (keepFocus && input) {
      input.focus();
      if (caret != null) input.setSelectionRange(caret, caret);
    }
    if (tab.pick) {
      tab.root.querySelector(`[data-start-connect="${CSS.escape(tab.pick)}"]`)?.scrollIntoView({ block: "nearest" });
    }
  }

  function refreshStartTabs() {
    for (const tab of state.tabs.values()) {
      if (tab.start) paintStartPage(tab);
    }
  }

  function createStartTab() {
    const tab = {
      id: uid("tab"),
      hostId: "",
      title: "New Tab",
      remote: false,
      start: true,
      sync: false,
      query: "",
      pick: "",
      connecting: "",
      layout: null,
      focusedPaneId: "",
      zoomedPaneId: "",
      root: document.createElement("div"),
    };
    tab.root.className = "term-tab start-tab";
    els.terminals.appendChild(tab.root);
    state.tabs.set(tab.id, tab);
    state.activeId = tab.id;
    paintStartPage(tab);
    renderTabs();
    tab.root.querySelector(".start-search")?.focus();
    return tab;
  }

  function tabFromRoot(el) {
    const root = el?.closest?.(".term-tab");
    if (!root) return null;
    return [...state.tabs.values()].find((t) => t.root === root) || null;
  }

  function fillableStartTab(explicit) {
    if (explicit?.start) return explicit;
    const active = state.tabs.get(state.activeId);
    return active?.start ? active : null;
  }

  function setStartPick(tab, hostId) {
    tab.pick = hostId;
    tab.root.querySelectorAll("[data-start-connect]").forEach((el) => {
      el.classList.toggle("is-open", el.dataset.startConnect === hostId);
    });
    tab.root.querySelector(`[data-start-connect="${CSS.escape(hostId)}"]`)?.scrollIntoView({ block: "nearest" });
  }

  function probeSize() {
    const termProbe = document.createElement("div");
    termProbe.style.cssText = "position:absolute;visibility:hidden;width:800px;height:400px";
    document.body.appendChild(termProbe);
    const probe = createTerminal(termOpts());
    probe.open(termProbe);
    const cols = probe.cols || 80;
    const rows = probe.rows || 24;
    probe.dispose();
    termProbe.remove();
    return { cols, rows };
  }

  async function spawnHost(hostId) {
    const { cols, rows } = probeSize();
    return api("/api/sessions", {
      method: "POST",
      body: JSON.stringify({ hostId, cols, rows }),
    });
  }

  async function openSession(host, opts = {}) {
    rememberRecent(host.id);
    const tab = fillableStartTab(opts.tab);
    if (tab?.start) {
      if (tab.connecting) return;
      tab.connecting = host.id;
      tab.pick = host.id;
      paintStartPage(tab);
    }
    try {
      const info = await spawnHost(host.id);
      attachRuntime(info, tab ? { tab } : {});
    } catch (err) {
      if (tab?.start) {
        tab.connecting = "";
        paintStartPage(tab);
      }
      throw err;
    }
    loadAll().catch(() => {});
  }

  function isHoldModKey(ev) {
    return isMac ? ev.key === "Meta" : ev.key === "Control";
  }

  function hasAppMod(ev) {
    return isMac ? ev.metaKey && !ev.ctrlKey : ev.ctrlKey && !ev.metaKey;
  }

  function newTab() {
    createStartTab();
  }

  function newLocalTab() {
    const host = localHost();
    if (!host) {
      toast("Local shell is not available", true);
      return;
    }
    openSession(host).catch((err) => toast(err.message, true));
  }

  function reopenLastTab() {
    const id = state.lastClosedHostId;
    if (!id) return;
    const host = state.hosts.find((h) => h.id === id);
    if (host) openSession(host).catch((err) => toast(err.message, true));
  }

  function showKbdHints(on) {
    document.documentElement.classList.toggle("show-kbd", on);
  }

  let kbdTimer = 0;
  let lastShortcutAt = 0;
  let lastShortcutName = "";
  let paletteCursor = 0;
  let paletteVisible = [];
  let ptyInjectWaiter = null;
  let ptyChromeHoldUntil = 0;

  function holdPtyFromChrome(ms = 500) {
    ptyChromeHoldUntil = Math.max(ptyChromeHoldUntil, Date.now() + ms);
  }

  function ptyWritesBlocked() {
    return !!(state.openModal || ptyInjectWaiter || Date.now() < ptyChromeHoldUntil);
  }

  function paletteQuery() {
    return (els.cmdPaletteInput?.value || "").trim().toLowerCase();
  }

  function paletteCatalog() {
    const items = [];
    const add = (item) => items.push(item);
    add({ id: "new-tab", group: "Tabs", title: "New tab", keys: `${MOD}T`, run: () => newTab() });
    add({ id: "new-local", group: "Tabs", title: "New Local Shell", run: () => newLocalTab() });
    add({
      id: "close-tab",
      group: "Tabs",
      title: "Close pane or tab",
      keys: `${MOD}W`,
      run: () => closeActivePaneOrTab(),
    });
    add({
      id: "reopen-tab",
      group: "Tabs",
      title: "Reopen last tab",
      keys: `${MOD}⇧T`,
      run: () => reopenLastTab(),
    });
    add({ id: "vault", group: "Vault", title: "Go to Vault", keys: `${MOD}1`, run: () => showVault() });
    add({
      id: "search-hosts",
      group: "Vault",
      title: "Search hosts in Vault",
      run: () => runShortcut("search"),
    });
    add({
      id: "new-host",
      group: "Vault",
      title: "New host",
      keys: `${MOD}N`,
      run: () => runShortcut("new-host"),
    });
    add({
      id: "quick-connect",
      group: "Vault",
      title: "Quick connect",
      keys: `${MOD}⇧O`,
      run: () => runShortcut("quick-connect"),
    });
    add({ id: "join", group: "Vault", title: "Join session", keys: `${MOD}J`, run: () => runShortcut("join") });
    add({
      id: "vault-hosts",
      group: "Vault",
      title: "Hosts",
      run: () => {
        showVault();
        setVaultSection("hosts");
      },
    });
    add({
      id: "vault-keychain",
      group: "Vault",
      title: "Keychain",
      run: () => {
        showVault();
        setVaultSection("keychain");
      },
    });
    add({
      id: "vault-forwards",
      group: "Vault",
      title: "Port forwarding",
      run: () => {
        showVault();
        setVaultSection("forwards");
      },
    });
    add({
      id: "vault-snippets",
      group: "Vault",
      title: "Snippets",
      run: () => {
        showVault();
        setVaultSection("snippets");
      },
    });
    add({
      id: "vault-logs",
      group: "Vault",
      title: "Logs",
      run: () => {
        showVault();
        setVaultSection("logs");
      },
    });
    add({ id: "settings", group: "Vault", title: "Settings", keys: `${MOD},`, run: () => runShortcut("settings") });
    add({
      id: "split-right",
      group: "Panes",
      title: "Split right",
      keys: `${MOD}⇧D`,
      run: () => splitActive("h"),
    });
    add({
      id: "split-down",
      group: "Panes",
      title: "Split down",
      keys: `${MOD}⇧E`,
      run: () => splitActive("v"),
    });
    add({
      id: "focus-pane-zoom",
      group: "Panes",
      title: "Focus view (zoom pane)",
      keys: `${MOD}⇧↵`,
      run: () => runShortcut("focus-pane"),
    });
    add({ id: "find-term", group: "Panes", title: "Find in terminal", keys: `${MOD}F`, run: () => runShortcut("find-term") });
    add({
      id: "sync-panes",
      group: "Panes",
      title: "Toggle sync input",
      run: () => toggleTabSync(state.activeId),
    });
    add({ id: "font-up", group: "Terminal", title: "Larger text", keys: `${MOD}+`, run: () => runShortcut("zoom-in") });
    add({ id: "font-down", group: "Terminal", title: "Smaller text", keys: `${MOD}-`, run: () => runShortcut("zoom-out") });
    add({ id: "font-reset", group: "Terminal", title: "Reset text size", keys: `${MOD}0`, run: () => runShortcut("zoom-reset") });
    add({
      id: "stop-runtime",
      group: "Terminal",
      title: "Stop Keel runtime",
      run: () => runShortcut("stop-runtime"),
    });
    for (const h of state.hosts) {
      add({
        id: `host:${h.id}`,
        group: "Connect",
        title: `Connect ${h.name}`,
        hint: hostSubtitle(h),
        run: () => openSession(h).catch((err) => toast(err.message, true)),
      });
    }
    for (const tab of state.tabs.values()) {
      add({
        id: `tab:${tab.id}`,
        group: "Tabs",
        title: `Focus tab ${tabTitle(tab)}`,
        run: () => {
          state.activeId = tab.id;
          renderTabs();
        },
      });
    }
    const active = state.tabs.get(state.activeId);
    if (active) {
      for (const p of tabPanes(active)) {
        add({
          id: `pane:${p.id}`,
          group: "Panes",
          title: `Focus pane ${p.title || "session"}`,
          hint: p.subtitle || "",
          run: () => markFocusedPane(p),
        });
      }
    }
    for (const sn of state.snippets) {
      add({
        id: `snippet:${sn.id}`,
        group: "Snippets",
        title: `Type snippet: ${sn.name}`,
        hint: "Confirm before sending to the terminal",
        run: () => paletteRunSnippet(sn.id),
      });
    }
    return items;
  }

  function paletteMatches(item, q) {
    if (!q) return true;
    const hay = `${item.title} ${item.hint || ""} ${item.group} ${item.keys || ""}`.toLowerCase();
    return q.split(/\s+/).filter(Boolean).every((part) => hay.includes(part));
  }

  function paintCommandPalette() {
    const list = els.cmdPaletteList;
    if (!list) return;
    const q = paletteQuery();
    paletteVisible = paletteCatalog().filter((item) => paletteMatches(item, q));
    if (paletteCursor >= paletteVisible.length) paletteCursor = Math.max(0, paletteVisible.length - 1);
    if (!paletteVisible.length) {
      list.innerHTML = `<p class="cmd-palette-empty">No matching commands</p>`;
      els.cmdPaletteInput?.removeAttribute("aria-activedescendant");
      return;
    }
    const parts = [];
    let lastGroup = "";
    paletteVisible.forEach((item, i) => {
      if (item.group !== lastGroup) {
        lastGroup = item.group;
        parts.push(`<div class="cmd-palette-group">${escapeHtml(item.group)}</div>`);
      }
      const selected = i === paletteCursor;
      const active = selected ? " is-active" : "";
      const hint = item.hint ? `<span class="cmd-palette-item-hint">${escapeHtml(item.hint)}</span>` : "";
      const keys = item.keys ? `<kbd class="cmd-palette-item-keys">${escapeHtml(item.keys)}</kbd>` : "";
      parts.push(
        `<button type="button" class="cmd-palette-item${active}" role="option" data-palette-index="${i}" id="cmd-palette-opt-${i}" aria-selected="${selected ? "true" : "false"}">
          <span class="cmd-palette-item-text"><span class="cmd-palette-item-title">${escapeHtml(item.title)}</span>${hint}</span>
          ${keys}
        </button>`
      );
    });
    list.innerHTML = parts.join("");
    if (els.cmdPaletteInput) {
      if (paletteVisible.length) {
        els.cmdPaletteInput.setAttribute("aria-activedescendant", `cmd-palette-opt-${paletteCursor}`);
      } else {
        els.cmdPaletteInput.removeAttribute("aria-activedescendant");
      }
    }
    list.querySelector(".cmd-palette-item.is-active")?.scrollIntoView({ block: "nearest" });
  }

  function isCommandPaletteOpen() {
    return !!(els.cmdPalette && els.cmdPalette.open);
  }

  function closeCommandPalette() {
    if (!isCommandPaletteOpen()) return;
    closeModal(els.cmdPalette);
  }

  function openCommandPalette() {
    if (!els.cmdPalette) return;
    if (state.openModal && state.openModal !== els.cmdPalette) closeModal();
    if (els.cmdPaletteInput) els.cmdPaletteInput.value = "";
    paletteCursor = 0;
    paintCommandPalette();
    focusedPane()?.term?.blur?.();
    openModal(els.cmdPalette);
    requestAnimationFrame(() => els.cmdPaletteInput?.focus());
  }

  // approve-before-type: palette (and any chrome) must resolve true before PTY writes.
  function approveBeforeType(command, sessionLabel) {
    const modal = els.ptyInjectModal;
    const preview = document.getElementById("pty-inject-preview");
    const sess = document.getElementById("pty-inject-session");
    if (!modal || typeof modal.showModal !== "function") {
      return Promise.resolve(window.confirm(`Type into ${sessionLabel || "the terminal"}?\n\n${command}`));
    }
    if (sess) sess.textContent = sessionLabel || "the focused session";
    if (preview) preview.textContent = command;
    focusedPane()?.term?.blur?.();
    return new Promise((resolve) => {
      ptyInjectWaiter = resolve;
      openModal(modal);
    });
  }

  function settlePtyInject(ok) {
    const done = ptyInjectWaiter;
    holdPtyFromChrome();
    ptyInjectWaiter = null;
    closeModal(els.ptyInjectModal);
    if (done) done(!!ok);
  }

  async function paletteRunSnippet(id) {
    const sn = state.snippets.find((s) => s.id === id);
    if (!sn) return;
    const pane = focusedPane();
    if (!pane) {
      toast("Open a session first");
      return;
    }
    if (!isDriving(pane)) {
      toast("You are following — take or request control to type");
      return;
    }
    const command = sn.command.endsWith("\n") ? sn.command : sn.command + "\n";
    const ok = await approveBeforeType(command, pane.subtitle || pane.title || "session");
    if (!ok) return;
    holdPtyFromChrome();
    sendSnippet(id);
  }

  async function runPaletteItem(item) {
    closeCommandPalette();
    if (!item?.run) return;
    try {
      await item.run();
    } catch (err) {
      toast(err?.message || String(err), true);
    }
  }

  function movePaletteCursor(delta) {
    if (!paletteVisible.length) return;
    paletteCursor = (paletteCursor + delta + paletteVisible.length) % paletteVisible.length;
    paintCommandPalette();
  }

  function runShortcut(name) {
    const now = Date.now();
    if (name === lastShortcutName && now - lastShortcutAt < 80) return;
    lastShortcutName = name;
    lastShortcutAt = now;
    if (name === "new-tab") newTab();
    else if (name === "close-tab") {
      if (state.openModal) closeModal();
      else closeActivePaneOrTab();
    } else if (name === "reopen-tab") reopenLastTab();
    else if (name === "search") {
      if (state.openModal) closeModal();
      if (state.activeId !== VAULT) {
        state.activeId = VAULT;
        setVaultSection("hosts");
        renderTabs();
      } else {
        setVaultSection("hosts");
      }
      els.search.focus();
      els.search.select?.();
    } else if (name === "new-host") {
      fillHostForm(null);
      openModal(els.hostModal);
    } else if (name === "quick-connect") openModal(els.quickModal);
    else if (name === "join") openJoin();
    else if (name === "settings") {
      state.activeId = VAULT;
      setVaultSection("settings");
      setSettingsSection(state.settingsSection || "appearance");
      renderTabs();
    } else if (name === "split-right") splitActive("h");
    else if (name === "split-down") splitActive("v");
    else if (name === "focus-pane") {
      const pane = focusedPane();
      if (pane) togglePaneZoom(pane.id);
    }
    else if (name === "find-term") {
      const pane = focusedPane();
      if (pane?.search) openFind(pane);
    } else if (name === "zoom-in") setFontSize(state.prefs.size + 1);
    else if (name === "zoom-out") setFontSize(state.prefs.size - 1);
    else if (name === "zoom-reset") setFontSize(SIZE_DEFAULT);
    else if (name === "stop-runtime") {
      state.runtimeStopping = true;
      api("/api/runtime/stop", { method: "POST" })
        .then(() => toast("Keel runtime stopped"))
        .catch((err) => {
          state.runtimeStopping = false;
          toast(err.message, true);
        });
    } else if (name === "palette") {
      if (isCommandPaletteOpen()) closeCommandPalette();
      else openCommandPalette();
    }
  }

  function handleShortcut(ev) {
    if (ev.target?.closest?.(".tab-rename")) return false;
    if (!hasAppMod(ev) || ev.altKey) return false;
    if (els.gate && !els.gate.hidden) return false;
    const raw = ev.key || "";
    const key = raw.length === 1 ? raw.toLowerCase() : raw;
    if (key === "w") {
      ev.preventDefault();
      runShortcut("close-tab");
      return true;
    }
    if (key === "t" && ev.shiftKey) {
      ev.preventDefault();
      runShortcut("reopen-tab");
      return true;
    }
    if (key === "t") {
      if (state.openModal) return false;
      ev.preventDefault();
      runShortcut("new-tab");
      return true;
    }
    if (key === "d" && ev.shiftKey) {
      ev.preventDefault();
      runShortcut("split-right");
      return true;
    }
    if (key === "e" && ev.shiftKey) {
      ev.preventDefault();
      runShortcut("split-down");
      return true;
    }
    if ((key === "enter" || ev.key === "Enter") && ev.shiftKey) {
      ev.preventDefault();
      runShortcut("focus-pane");
      return true;
    }
    if (key === "=" || key === "+" || ev.code === "Equal" || ev.code === "NumpadAdd") {
      ev.preventDefault();
      runShortcut("zoom-in");
      return true;
    }
    if (key === "-" || key === "_" || ev.code === "Minus" || ev.code === "NumpadSubtract") {
      ev.preventDefault();
      runShortcut("zoom-out");
      return true;
    }
    if ((key === "0" || ev.code === "Digit0" || ev.code === "Numpad0") && !ev.shiftKey) {
      ev.preventDefault();
      runShortcut("zoom-reset");
      return true;
    }
    if (key === "k" && !ev.shiftKey) {
      ev.preventDefault();
      runShortcut("palette");
      return true;
    }
    if (key === "p" && ev.shiftKey) {
      ev.preventDefault();
      runShortcut("palette");
      return true;
    }
    if (state.openModal) return false;
    if (key === "f" && !ev.shiftKey) {
      ev.preventDefault();
      runShortcut("find-term");
      return true;
    }
    if (key === "g") {
      const pane = focusedPane();
      if (isFindOpen(pane)) {
        ev.preventDefault();
        runPaneFind(pane, ev.shiftKey ? "prev" : "next", false);
        return true;
      }
    }
    if (key === "n" && !ev.shiftKey) {
      ev.preventDefault();
      runShortcut("new-host");
      return true;
    }
    if (key === "o" && ev.shiftKey) {
      ev.preventDefault();
      runShortcut("quick-connect");
      return true;
    }
    if (key === "j" && !ev.shiftKey) {
      ev.preventDefault();
      runShortcut("join");
      return true;
    }
    if (key === "," && !ev.shiftKey) {
      ev.preventDefault();
      runShortcut("settings");
      return true;
    }
    if (!ev.shiftKey && /^[1-9]$/.test(ev.key)) {
      ev.preventDefault();
      activateTabIndex(Number(ev.key));
      return true;
    }
    return false;
  }

  window.__keelShortcut = (name) => runShortcut(name);

  function termKeyHandler(ev) {
    if (ev.type !== "keydown") return true;
    // Unmodified keys (including hold-to-repeat in vim) must reach xterm.
    if (!hasAppMod(ev) || ev.altKey) return true;
    return !handleShortcut(ev);
  }

  function wirePaneTerminal(pane) {
    const term = pane.term;
    term.attachCustomKeyEventHandler(termKeyHandler);
    term.onData((data) => {
      if (ptyWritesBlocked()) return;
      if (!isDriving(pane)) return;
      const bytes = new TextEncoder().encode(data);
      const tab = state.tabs.get(pane.tabId);
      if (tab?.sync) {
        for (const p of tabPanes(tab)) {
          if (isDriving(p)) sendSessionBytes(p, bytes);
        }
      } else {
        sendSessionBytes(pane, bytes);
      }
    });
    term.onSelectionChange(() => {
      if (!state.prefs.copyOnSelect) return;
      const sel = term.getSelection();
      if (sel) copyText(sel);
    });
    pane.wrap.addEventListener("contextmenu", async (ev) => {
      if (!state.prefs.rightClickPaste) return;
      ev.preventDefault();
      if (!isDriving(pane)) {
        toast("You are following — take or request control to paste");
        return;
      }
      try {
        const text = await navigator.clipboard.readText();
        if (text) term.paste(text);
      } catch {
        toast("Clipboard paste blocked", true);
      }
    });
    pane.wrap.addEventListener("mousedown", (ev) => {
      if (ev.target.closest?.(".pane-bar button, .pane-actions")) return;
      markFocusedPane(pane);
    });
  }

  function paneWantsWebgl(pane) {
    if (!pane?.term) return false;
    if (!isNativeWindow()) return true;
    const tab = state.tabs.get(pane.tabId);
    return visiblePanes(tab).length <= 1;
  }

  function attachWebgl(pane) {
    const term = pane?.term;
    if (!term || term._keelWebgl || !paneWantsWebgl(pane)) return;
    const Ctor = window.WebglAddon?.WebglAddon || window.WebglAddon;
    if (typeof Ctor !== "function" || typeof term.loadAddon !== "function") return;
    try {
      const addon = new Ctor();
      addon.onContextLoss?.(() => {
        try {
          addon.dispose();
        } catch {
          /* DOM renderer */
        }
        term._keelWebgl = null;
        if (paneWantsWebgl(pane)) requestAnimationFrame(() => attachWebgl(pane));
      });
      term.loadAddon(addon);
      term._keelWebgl = addon;
    } catch {
      /* xterm 6 default DOM renderer */
    }
  }

  function dropWebgl(pane) {
    if (!pane?.term?._keelWebgl) return;
    try {
      pane.term._keelWebgl.dispose();
    } catch {
      /* ignore */
    }
    pane.term._keelWebgl = null;
  }

  function loadTermAddon(term, Ctor, ...args) {
    if (typeof Ctor !== "function" || typeof term.loadAddon !== "function") return null;
    try {
      const addon = new Ctor(...args);
      term.loadAddon(addon);
      return addon;
    } catch {
      return null;
    }
  }

  function openTermLink(_ev, uri) {
    let url;
    try {
      url = new URL(uri);
    } catch {
      return;
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") return;
    const a = document.createElement("a");
    a.href = url.href;
    a.target = "_blank";
    a.rel = "noopener noreferrer";
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  function searchDecorations() {
    const light = resolvedTheme() === "light";
    return {
      matchBackground: light ? "#c5d4ff" : "#3d4f73",
      matchOverviewRuler: light ? "#3b6cff" : "#82a7ff",
      activeMatchBackground: light ? "#3b6cff" : "#82a7ff",
      activeMatchColorOverviewRuler: light ? "#171717" : "#f4f4f4",
    };
  }

  function paintPaneProgress(el, st) {
    if (!el) return;
    const kind = st?.state ?? 0;
    const value = Math.max(0, Math.min(100, Number(st?.value) || 0));
    const bar = el.querySelector(".pane-progress-bar");
    if (!kind) {
      el.hidden = true;
      el.classList.remove("is-error", "is-pause", "is-indeterminate");
      if (bar) bar.style.width = "0%";
      return;
    }
    el.hidden = false;
    el.classList.toggle("is-error", kind === 2);
    el.classList.toggle("is-pause", kind === 4);
    el.classList.toggle("is-indeterminate", kind === 3);
    if (bar) bar.style.width = kind === 3 ? "" : `${value}%`;
  }

  function isFindOpen(pane) {
    return !!(pane?.findEl && !pane.findEl.hidden);
  }

  function paintFindCount(el, ev) {
    if (!el) return;
    if (!ev || ev.resultCount == null) {
      el.textContent = "";
      return;
    }
    if (!ev.resultCount) {
      el.textContent = "0";
      return;
    }
    if (ev.resultIndex < 0) {
      el.textContent = `${ev.resultCount}+`;
      return;
    }
    el.textContent = `${ev.resultIndex + 1}/${ev.resultCount}`;
  }

  function runPaneFind(pane, dir, incremental) {
    if (!pane?.search || !pane.findEl) return;
    const input = pane.findEl.querySelector(".term-find-input");
    const count = pane.findEl.querySelector(".term-find-count");
    const term = input?.value ?? "";
    if (!term) {
      try {
        pane.search.clearDecorations();
      } catch {
        /* ignore */
      }
      if (count) count.textContent = "";
      return;
    }
    const opts = {
      caseSensitive: pane.findEl.querySelector(".term-find-case")?.getAttribute("aria-pressed") === "true",
      incremental: !!incremental,
      decorations: searchDecorations(),
    };
    let found = false;
    try {
      found = dir === "prev" ? pane.search.findPrevious(term, opts) : pane.search.findNext(term, opts);
    } catch {
      found = false;
    }
    if (count && !count.textContent) count.textContent = found ? "" : "0";
  }

  function openFind(pane) {
    if (!pane?.findEl || !pane.search) return;
    for (const other of state.panes.values()) {
      if (other !== pane && isFindOpen(other)) closeFind(other, false);
    }
    pane.findEl.hidden = false;
    const input = pane.findEl.querySelector(".term-find-input");
    input?.focus();
    input?.select();
    if (input?.value) runPaneFind(pane, "next", true);
  }

  function closeFind(pane, refocus) {
    if (!pane?.findEl) return;
    pane.findEl.hidden = true;
    try {
      pane.search?.clearDecorations();
    } catch {
      /* ignore */
    }
    const count = pane.findEl.querySelector(".term-find-count");
    if (count) count.textContent = "";
    if (refocus) {
      try {
        pane.term?.focus();
      } catch {
        /* ignore */
      }
    }
  }

  function closeOpenFind() {
    const focused = focusedPane();
    if (isFindOpen(focused)) {
      closeFind(focused, true);
      return true;
    }
    for (const pane of state.panes.values()) {
      if (isFindOpen(pane)) {
        closeFind(pane, false);
        return true;
      }
    }
    return false;
  }

  function wireFindEl(findEl, paneId) {
    findEl.addEventListener("keydown", (ev) => {
      if (ev.key !== "Enter") return;
      ev.preventDefault();
      ev.stopPropagation();
      const pane = state.panes.get(paneId);
      if (pane) runPaneFind(pane, ev.shiftKey ? "prev" : "next", false);
    });
    findEl.querySelector(".term-find-input")?.addEventListener("input", () => {
      const pane = state.panes.get(paneId);
      if (pane) runPaneFind(pane, "next", true);
    });
    findEl.querySelector(".term-find-prev")?.addEventListener("click", () => {
      const pane = state.panes.get(paneId);
      if (pane) runPaneFind(pane, "prev", false);
    });
    findEl.querySelector(".term-find-next")?.addEventListener("click", () => {
      const pane = state.panes.get(paneId);
      if (pane) runPaneFind(pane, "next", false);
    });
    findEl.querySelector(".term-find-case")?.addEventListener("click", (ev) => {
      const btn = ev.currentTarget;
      const on = btn.getAttribute("aria-pressed") !== "true";
      btn.setAttribute("aria-pressed", on ? "true" : "false");
      btn.classList.toggle("is-on", on);
      const pane = state.panes.get(paneId);
      if (pane && isFindOpen(pane)) runPaneFind(pane, "next", true);
    });
    findEl.querySelector(".term-find-close")?.addEventListener("click", () => {
      const pane = state.panes.get(paneId);
      if (pane) closeFind(pane, true);
    });
  }

  function makePaneShell(info, paneId) {
    const wrap = document.createElement("div");
    wrap.className = "term-wrap";
    wrap.dataset.paneId = paneId;
    const bar = document.createElement("div");
    bar.className = "pane-bar";
    bar.innerHTML = `<span class="pane-title" title="Drag to rearrange">${escapeHtml(info.title || "session")}</span>
      <div class="pane-actions">
        <button type="button" class="icon-btn pane-sync" data-pane-sync="${paneId}" aria-pressed="false" title="Sync input">${paneGlyph("sync")}</button>
        <button type="button" class="icon-btn" data-pane-split="h" data-pane-id="${paneId}" data-kbd="⇧D" title="Split right">${kbdHint("⇧D")}${paneGlyph("splitH")}</button>
        <button type="button" class="icon-btn" data-pane-split="v" data-pane-id="${paneId}" data-kbd="⇧E" title="Split down">${kbdHint("⇧E")}${paneGlyph("splitV")}</button>
        <button type="button" class="icon-btn" data-pane-zoom="${paneId}" data-kbd="⇧↵" title="Focus view" disabled>${kbdHint("⇧↵")}<span class="pane-zoom-in">${paneGlyph("zoom")}</span><span class="pane-zoom-out">${paneGlyph("unzoom")}</span></button>
        <button type="button" class="icon-btn pane-close" data-pane-close="${paneId}" title="Close pane">${closeGlyph()}</button>
      </div>`;
    const progressEl = document.createElement("div");
    progressEl.className = "pane-progress";
    progressEl.hidden = true;
    progressEl.innerHTML = `<div class="pane-progress-bar"></div>`;
    const banner = document.createElement("div");
    banner.className = "follow-banner";
    banner.hidden = true;
    banner.textContent = "Following — request control to type";
    const body = document.createElement("div");
    body.className = "term-body";
    const host = document.createElement("div");
    host.className = "term-host";
    body.appendChild(host);
    const findEl = document.createElement("div");
    findEl.className = "term-find";
    findEl.hidden = true;
    findEl.setAttribute("role", "search");
    findEl.innerHTML = `<input type="text" class="term-find-input" placeholder="Find" autocomplete="off" spellcheck="false" aria-label="Find in terminal" />
      <span class="term-find-count" aria-live="polite"></span>
      <button type="button" class="icon-btn term-find-prev" title="Previous match">↑</button>
      <button type="button" class="icon-btn term-find-next" title="Next match">↓</button>
      <button type="button" class="icon-btn term-find-case" title="Match case" aria-pressed="false">Aa</button>
      <button type="button" class="icon-btn term-find-close" title="Close">×</button>`;
    const failEl = document.createElement("div");
    failEl.className = "pane-fail";
    failEl.hidden = true;
    failEl.innerHTML = `<div class="pane-fail-card">
      <div class="pane-fail-head">
        <span class="pane-fail-dot" data-fail-dot></span>
        <div class="pane-fail-meta">
          <b data-fail-title>session</b>
          <small data-fail-sub></small>
        </div>
        <button type="button" class="tiny pane-fail-copy" data-pane-copy-log="${paneId}">Copy logs</button>
      </div>
      <div class="pane-fail-link" data-fail-link>
        <span class="pane-fail-node">${paneGlyph("unplug")}</span>
        <span class="pane-fail-wire"></span>
        <span class="pane-fail-node">${paneGlyph("prompt")}</span>
      </div>
      <p class="pane-fail-kicker" data-fail-kicker>Connection failed with connection log:</p>
      <div class="pane-fail-log" data-fail-log></div>
      <div class="pane-fail-actions">
        <button type="button" class="btn" data-pane-close="${paneId}">Close</button>
        <button type="button" class="btn" data-pane-edit-host="${paneId}">Edit host</button>
        <button type="button" class="btn pane-fail-start" data-pane-reconnect="${paneId}">Start over</button>
      </div>
    </div>`;
    wrap.appendChild(bar);
    wrap.appendChild(progressEl);
    wrap.appendChild(banner);
    wrap.appendChild(body);
    const term = createTerminal(termOpts());
    const Addon = FitAddonCtor?.FitAddon || FitAddonCtor;
    const fit = typeof Addon === "function" ? new Addon() : { fit() {} };
    if (typeof term.loadAddon === "function" && typeof Addon === "function") term.loadAddon(fit);
    loadTermAddon(term, UnicodeGraphemesCtor);
    const search = loadTermAddon(term, SearchAddonCtor);
    loadTermAddon(term, WebLinksAddonCtor, openTermLink);
    const progress = loadTermAddon(term, ProgressAddonCtor);
    progress?.onChange?.((st) => paintPaneProgress(progressEl, st));
    search?.onDidChangeResults?.((ev) => paintFindCount(findEl.querySelector(".term-find-count"), ev));
    body.appendChild(findEl);
    body.appendChild(failEl);
    wireFindEl(findEl, paneId);
    return { wrap, banner, body, term, fit, search, findEl, failEl, host };
  }

  function createTab(paneSeed) {
    const tab = {
      id: uid("tab"),
      hostId: paneSeed.hostId || "",
      title: paneSeed.title || "Session",
      remote: !!paneSeed.remote,
      sync: false,
      layout: { type: "leaf", paneId: paneSeed.id },
      focusedPaneId: paneSeed.id,
      zoomedPaneId: "",
      root: document.createElement("div"),
    };
    tab.root.className = "term-tab";
    ensureTabStage(tab);
    els.terminals.appendChild(tab.root);
    paneSeed.tabId = tab.id;
    state.tabs.set(tab.id, tab);
    state.panes.set(paneSeed.id, paneSeed);
    state.activeId = tab.id;
    tab.dirty = true;
    return tab;
  }

  function attachRuntime(info, opts = {}) {
    const existing = findPaneByRuntime(info.id);
    if (existing && !opts.splitDir) {
      const leftover = opts.tab?.start && opts.tab.id !== existing.tabId ? opts.tab : null;
      state.activeId = existing.tabId;
      const tab = state.tabs.get(existing.tabId);
      if (tab) tab.focusedPaneId = existing.id;
      if (leftover) closeTab(leftover.id);
      else renderTabs();
      if (!sessionSocketOpen(existing)) reconnectPane(existing);
      existing.term?.focus();
      return existing;
    }

    const paneId = uid("pane");
    const shell = makePaneShell(info, paneId);
    const pane = {
      id: paneId,
      tabId: opts.tab?.id,
      runtimeId: info.id,
      hostId: info.hostId,
      title: info.title,
      subtitle: info.subtitle,
      status: info.status === "ready" ? "is-ready" : info.status === "connecting" ? "is-connecting" : info.status === "error" || info.status === "exited" ? "is-error" : "",
      term: shell.term,
      fit: shell.fit,
      host: shell.host,
      wrap: shell.wrap,
      banner: shell.banner,
      search: shell.search,
      findEl: shell.findEl,
      failEl: shell.failEl,
      failLog: [],
      failKind: info.status === "exited" ? "ended" : info.status === "error" ? "fail" : "",
      wasReady: info.status === "ready",
      color: info.color || "",
      ws: null,
      you: null,
      attached: info.attached || [],
      driverId: info.driverId,
      ownerId: info.ownerId,
      remote: false,
    };
    wirePaneTerminal(pane);

    let tab = opts.tab;
    if (!tab && !opts.splitDir && !opts.freshTab) tab = fillableStartTab();
    if (!tab) {
      tab = createTab(pane);
    } else if (tab.start) {
      pane.tabId = tab.id;
      state.panes.set(pane.id, pane);
      tab.layout = { type: "leaf", paneId: pane.id };
      tab.focusedPaneId = pane.id;
      tab.hostId = pane.hostId || "";
      tab.title = pane.title || tab.title;
      tab.start = false;
      tab.connecting = "";
      tab.root.classList.remove("start-tab");
      tab.dirty = true;
      rememberRecent(pane.hostId);
    } else {
      pane.tabId = tab.id;
      state.panes.set(pane.id, pane);
      const focused = opts.splitPaneId || tab.focusedPaneId;
      tab.zoomedPaneId = "";
      tab.layout = replaceLeaf(tab.layout, focused, {
        type: "split",
        dir: opts.splitDir || "h",
        sizes: [50, 50],
        a: { type: "leaf", paneId: focused },
        b: { type: "leaf", paneId: pane.id },
      });
      tab.focusedPaneId = focused;
      tab.dirty = true;
    }

    if (pane.status !== "is-ready") seedConnectLog(pane);
    if (pane.status === "is-error" && info.error) pushFailLog(pane, "fail", info.error);
    renderTabs();
    requestAnimationFrame(() => requestAnimationFrame(() => connectSessionWS(pane)));
    return pane;
  }

  function sessionSocketOpen(pane) {
    return pane?.ws?.readyState === WebSocket.OPEN;
  }

  function connectSessionWS(pane, opts = {}) {
    if (!pane?.runtimeId || pane.remote) return;
    const gen = (pane.wsGen || 0) + 1;
    pane.wsGen = gen;
    const prev = pane.ws;
    pane.ws = null;
    if (prev && prev.readyState < WebSocket.CLOSING) {
      try {
        prev.close();
      } catch {
        /* ignore */
      }
    }

    const proto = location.protocol === "https:" ? "wss" : "ws";
    const q = new URLSearchParams({
      id: pane.runtimeId,
      cols: String(pane.term?.cols || 80),
      rows: String(pane.term?.rows || 24),
      name: state.name || "guest",
      kind: "web",
    });
    const ws = new WebSocket(`${proto}://${location.host}/ws/session?${q}`);
    ws.binaryType = "arraybuffer";
    pane.ws = ws;
    let live = !opts.skipSnapshot;

    const applyPresence = (msg) => {
      if (msg.you) pane.you = msg.you;
      if (msg.attached) pane.attached = msg.attached;
      if (msg.driverId != null) pane.driverId = msg.driverId;
      if (msg.ownerId != null) pane.ownerId = msg.ownerId;
      if (msg.title) pane.title = msg.title;
      pane.banner.hidden = isDriving(pane);
      const herd = state.herd.find((h) => h.id === pane.runtimeId);
      if (herd) {
        herd.attached = pane.attached;
        herd.driverId = pane.driverId;
        herd.ownerId = pane.ownerId;
        herd.clients = pane.attached.length;
        if (msg.status) herd.status = msg.status;
      }
      renderTabs();
    };

    ws.addEventListener("message", (ev) => {
      if (pane.wsGen !== gen) return;
      if (typeof ev.data === "string") {
        const msg = JSON.parse(ev.data);
        if (msg.type === "status") {
          recordFailStatus(pane, msg);
          if (msg.status === "ready") pane.status = "is-ready";
          else if (msg.status === "connecting") pane.status = "is-connecting";
          else if (msg.status === "error" || msg.status === "exited") pane.status = "is-error";
          applyPresence(msg);
        } else if (msg.type === "ready" || msg.type === "presence" || msg.type === "control") {
          recordFailStatus(pane, msg);
          if (msg.status === "connecting") pane.status = "is-connecting";
          else if (msg.status === "error" || msg.status === "exited") pane.status = "is-error";
          else pane.status = "is-ready";
          applyPresence(msg);
          if (msg.type === "ready") live = true;
          if (msg.type === "control" && msg.message) toast(msg.message);
        } else if (msg.type === "error") {
          pane.status = "is-error";
          pane.failKind = "fail";
          pushFailLog(pane, "fail", msg.message || "Connection failed");
          pane.term.writeln(`\r\n\x1b[38;2;248;113;113m${msg.message}\x1b[0m`);
          renderTabs();
          if (!promptHostKey(msg, { hostId: pane.hostId || msg.hostId, sessionId: pane.runtimeId || msg.sessionId })) {
            toast(msg.message, true);
          }
        }
        return;
      }
      if (!live) return;
      writePaneBytes(pane, ev.data);
    });
    ws.addEventListener("close", () => {
      if (pane.wsGen !== gen || pane.closing || !state.panes.has(pane.id)) return;
      noteSessionClosed(pane, gen);
    });
    ws.addEventListener("error", () => {
      if (pane.wsGen !== gen || pane.closing) return;
      pane.status = "is-error";
      pane.failKind = "lost";
      pushFailLog(pane, "fail", "WebSocket error");
      renderTabs();
    });
  }

  async function sessionEnded(runtimeId) {
    if (state.runtimeStopping) return true;
    if (runtimeId && !state.herd.some((s) => s.id === runtimeId)) return true;
    try {
      const sessions = await api("/api/sessions");
      if (!Array.isArray(sessions)) return true;
      return runtimeId ? !sessions.some((s) => s.id === runtimeId) : false;
    } catch {
      return true;
    }
  }

  async function noteSessionClosed(pane, gen) {
    if (pane.closing || (gen != null && pane.wsGen !== gen) || !state.panes.has(pane.id)) return;
    pane.status = "is-error";
    renderTabs();
    const ended = await sessionEnded(pane.runtimeId);
    if (pane.closing || (gen != null && pane.wsGen !== gen) || !state.panes.has(pane.id)) return;
    if (ended) {
      pane.status = "is-error";
      pane.failKind = "ended";
      pushFailLog(pane, "fail", "Runtime stopped — this session ended.");
      pane.term.writeln("\r\n\x1b[38;2;139;149;168mRuntime stopped — this session ended.\x1b[0m");
      renderTabs();
      return;
    }
    pane.failKind = pane.wasReady ? "lost" : "fail";
    pushFailLog(pane, "fail", pane.wasReady ? "Connection lost" : "Connection failed");
    pane.term.writeln("\r\n\x1b[38;2;139;149;168mConnection lost — click Start over to attach.\x1b[0m");
    renderTabs();
  }

  function reconnectPane(pane) {
    if (!pane?.runtimeId || pane.remote) return;
    const socketWasOpen = sessionSocketOpen(pane);
    pane.startingOver = true;
    pane.failKind = pane.wasReady ? "lost" : "fail";
    pane.failLog = [];
    seedConnectLog(pane);
    pane.status = "is-connecting";
    renderTabs();
    connectSessionWS(pane, { skipSnapshot: true });
    const herd = state.herd.find((s) => s.id === pane.runtimeId);
    if (socketWasOpen || !herd || herd.status !== "ready") {
      api(`/api/sessions/${pane.runtimeId}/reconnect`, { method: "POST" }).catch((err) => {
        pane.status = "is-error";
        pane.failKind = "fail";
        pushFailLog(pane, "fail", err.message || "Reconnect failed");
        renderTabs();
        toast(err.message, true);
      });
    }
  }

  function failHost(pane) {
    return state.hosts.find((h) => h.id === pane.hostId) || null;
  }

  function failEndpoint(pane) {
    const host = failHost(pane);
    if (!host || host.authType === "local") return pane.subtitle || "Local shell";
    const port = host.port || 22;
    return `SSH ${host.address}:${port}`;
  }

  function failHeadline(pane) {
    if (pane.status === "is-connecting") return "Connecting with connection log:";
    if (pane.failKind === "ended") return "Session ended with connection log:";
    if (pane.wasReady || pane.failKind === "lost") return "Connection lost with connection log:";
    return "Connection failed with connection log:";
  }

  function failNoise(message) {
    const m = String(message || "").trim().toLowerCase();
    return !m || /^(attached|attaching…|attaching\.\.\.|joined|left|connected|control passed|took control)$/.test(m);
  }

  function pushFailLog(pane, kind, text) {
    if (!pane || !text) return;
    const line = String(text).replace(/\s+/g, " ").trim();
    if (!line) return;
    pane.failLog = pane.failLog || [];
    const last = pane.failLog[pane.failLog.length - 1];
    if (last && last.text === line) return;
    pane.failLog.push({ kind: kind || "work", text: line });
    if (pane.failLog.length > 40) pane.failLog.shift();
  }

  function seedConnectLog(pane) {
    const host = failHost(pane);
    if (!host || host.authType === "local") {
      pushFailLog(pane, "user", "Starting a local shell");
      return;
    }
    const addr = host.address || "";
    const port = host.port || 22;
    pushFailLog(pane, "user", `Starting a new connection to: "${addr}" port "${port}"`);
    pushFailLog(pane, "work", `Starting address resolution of "${addr}"`);
    pushFailLog(pane, "work", "Address resolution finished");
    pushFailLog(pane, "work", `Connecting to "${addr}" port "${port}"`);
  }

  function recordFailStatus(pane, msg) {
    if (!msg) return;
    if (msg.status === "ready") {
      pane.wasReady = true;
      pane.failKind = "";
      pane.failLog = [];
      pane.startingOver = false;
      return;
    }
    if (failNoise(msg.message)) return;
    if (msg.status === "connecting") {
      if (/reconnect/i.test(msg.message || "") && !pane.startingOver && pane.wasReady) pane.failKind = "lost";
      const fail = /fail|error|refused|timed out|timeout/i.test(msg.message);
      pushFailLog(pane, fail ? "fail" : "work", msg.message);
      return;
    }
    if (msg.status === "exited") {
      pane.failKind = "ended";
      pane.startingOver = false;
      pushFailLog(pane, "fail", msg.message || "Session ended");
      return;
    }
    if (msg.status === "error") {
      pane.failKind = pane.wasReady ? "lost" : "fail";
      pane.startingOver = false;
      pushFailLog(pane, "fail", msg.message || "Connection failed");
    }
  }

  function syncPaneFail(pane) {
    const el = pane?.failEl;
    if (!el) return;
    const host = failHost(pane);
    const connecting = pane.status === "is-connecting" && host?.authType !== "local";
    const show = !pane.remote && (pane.status === "is-error" || connecting);
    pane.wrap.classList.toggle("is-fail", show && !connecting);
    pane.wrap.classList.toggle("is-connecting-fail", show && connecting);
    el.hidden = !show;
    if (!show) {
      if (pane.status === "is-ready") reflowPane(pane);
      return;
    }
    const title = el.querySelector("[data-fail-title]");
    const sub = el.querySelector("[data-fail-sub]");
    const kicker = el.querySelector("[data-fail-kicker]");
    const log = el.querySelector("[data-fail-log]");
    const dot = el.querySelector("[data-fail-dot]");
    const edit = el.querySelector("[data-pane-edit-host]");
    const start = el.querySelector("[data-pane-reconnect]");
    if (title) title.textContent = pane.title || host?.name || "session";
    if (sub) sub.textContent = failEndpoint(pane);
    if (kicker) kicker.textContent = failHeadline(pane);
    if (dot) {
      const color = pane.color || host?.color || "#6ea8ff";
      dot.style.setProperty("--dot", color);
      dot.style.background = color;
    }
    if (edit) edit.hidden = !pane.hostId || !!pane.remote;
    if (start) start.hidden = connecting;
    if (log) {
      const rows = pane.failLog?.length
        ? pane.failLog
        : [{ kind: connecting ? "work" : "fail", text: connecting ? "Connecting…" : "No connection log yet." }];
      log.innerHTML = rows
        .map((row) => {
          const icon = row.kind === "user" ? "person" : row.kind === "fail" ? "frown" : "gear";
          return `<div class="pane-fail-row ${row.kind === "fail" ? "is-fail" : ""}">${paneGlyph(icon)}<span>${escapeHtml(row.text)}</span></div>`;
        })
        .join("");
      log.scrollTop = log.scrollHeight;
    }
  }

  async function copyPaneFailLog(pane) {
    const lines = (pane?.failLog || []).map((row) => row.text).filter(Boolean);
    const text = lines.length ? lines.join("\n") : "No connection log yet.";
    const ok = await copyText(text);
    toast(ok ? "Copied connection log" : "Could not copy log", !ok);
  }

  function editPaneHost(pane) {
    if (!pane?.hostId) return;
    const host = failHost(pane);
    if (!host) {
      toast("Host not found", true);
      return;
    }
    fillHostForm(host);
    openModal(els.hostModal);
  }

  function sendSessionJSON(pane, obj) {
    const raw = JSON.stringify(obj);
    if (pane?.remote) {
      if (pane.dc?.readyState === "open") pane.dc.send(raw);
      return;
    }
    if (pane?.ws?.readyState === WebSocket.OPEN) pane.ws.send(raw);
  }

  function sendSessionBytes(pane, bytes) {
    if (pane?.remote) {
      if (pane.dc?.readyState === "open") pane.dc.send(bytes);
      return;
    }
    if (pane?.ws?.readyState === WebSocket.OPEN) pane.ws.send(bytes);
  }

  function sendResize(pane) {
    if (!pane || !isDriving(pane) || !pane.term) return;
    sendSessionJSON(pane, { type: "resize", cols: pane.term.cols, rows: pane.term.rows });
  }

  function sendControl(type, extra = {}) {
    sendSessionJSON(focusedPane(), { type, ...extra });
  }

  function disposePane(pane) {
    pane.closing = true;
    pane.fitObs?.disconnect();
    pane.fitObs = null;
    dropWebgl(pane);
    pane.ws?.close();
    pane.sigWs?.close();
    try {
      pane.pc?.close();
    } catch {
      /* ignore */
    }
    try {
      pane.term.dispose();
    } catch {
      /* ignore */
    }
    pane.wrap.remove();
    state.panes.delete(pane.id);
  }

  function killRuntimeIfOwned(pane, opts = {}) {
    if (opts.fromHerd || opts.remote || pane.remote || !pane.runtimeId) return;
    pane.closing = true;
    api(`/api/sessions/${pane.runtimeId}`, { method: "DELETE" }).catch(() => {});
  }

  function closeActivePaneOrTab() {
    if (!state.activeId || state.activeId === VAULT) return;
    const tab = state.tabs.get(state.activeId);
    if (!tab) return;
    const panes = tabPanes(tab);
    if (panes.length > 1) {
      const target =
        (tab.zoomedPaneId && state.panes.get(tab.zoomedPaneId)) ||
        (tab.focusedPaneId && state.panes.get(tab.focusedPaneId)) ||
        panes[0];
      closePane(target.id);
      return;
    }
    // Keep the last remaining session tab (one live pane). Tab × / pane × can still close it.
    if (state.tabs.size <= 1 && panes.length === 1) return;
    closeTab(tab.id);
  }

  function closePane(paneId, opts = {}) {
    const pane = state.panes.get(paneId);
    if (!pane) return;
    const tab = state.tabs.get(pane.tabId);
    killRuntimeIfOwned(pane, { fromHerd: opts.fromHerd, remote: opts.remote || tab?.remote });
    disposePane(pane);
    if (!tab) return;
    const next = removeLeaf(tab.layout, paneId);
    if (!next) {
      closeTab(tab.id, { skipPanes: true });
      return;
    }
    tab.layout = next;
    if (tab.zoomedPaneId === paneId) tab.zoomedPaneId = "";
    if (tab.focusedPaneId === paneId) {
      const left = tabPanes(tab)[0];
      tab.focusedPaneId = left?.id || "";
    }
    tab.dirty = true;
    renderTabs();
  }

  function closeTab(id, opts = {}) {
    if (id === VAULT) return;
    const tab = state.tabs.get(id);
    if (!tab) return;
    if (tabRenaming === id) tabRenaming = null;
    const panes = tabPanes(tab);
    if (tab.hostId) state.lastClosedHostId = tab.hostId;
    else if (panes[0]?.hostId) state.lastClosedHostId = panes[0].hostId;
    if (!opts.skipPanes) {
      for (const p of panes) {
        killRuntimeIfOwned(p, { fromHerd: opts.fromHerd, remote: tab.remote });
        disposePane(p);
      }
    }
    tab.stageObs?.disconnect();
    tab.stageObs = null;
    tab.root.remove();
    state.tabs.delete(id);
    const remain = [...state.tabs.keys()];
    if (state.activeId === id) state.activeId = remain.at(-1) || VAULT;
    if (!opts.silent) {
      renderTabs();
    }
  }

  async function splitActive(dir) {
    const pane = focusedPane();
    if (!pane) {
      toast("Open a session first");
      return;
    }
    return splitPane(pane.id, dir);
  }

  async function splitPane(paneId, dir) {
    const pane = state.panes.get(paneId);
    const tab = pane && state.tabs.get(pane.tabId);
    if (!tab || !pane) {
      toast("Open a session first");
      return;
    }
    if (tab.remote || pane.remote || !pane.hostId) {
      toast("Split needs a local host session");
      return;
    }
    tab.focusedPaneId = pane.id;
    tab.zoomedPaneId = "";
    if (isNativeWindow()) {
      for (const p of tabPanes(tab)) dropWebgl(p);
    }
    try {
      const info = await spawnHost(pane.hostId);
      attachRuntime(info, { tab, splitDir: dir, splitPaneId: pane.id });
      loadAll().catch(() => {});
    } catch (err) {
      toast(err.message, true);
      ensurePaneTerm(pane);
    }
  }

  function toggleTabSync(tabId) {
    const tab = state.tabs.get(tabId || state.activeId);
    if (!tab || tab.remote || tab.start) return;
    tab.sync = !tab.sync;
    renderTabs();
  }

  function togglePaneZoom(paneId) {
    const pane = state.panes.get(paneId);
    const tab = pane && state.tabs.get(pane.tabId);
    if (!tab) return;
    if (leafCount(tab.layout) < 2 && tab.zoomedPaneId !== paneId) return;
    tab.zoomedPaneId = tab.zoomedPaneId === paneId ? "" : paneId;
    tab.focusedPaneId = paneId;
    tab.dirty = true;
    state.activeId = tab.id;
    renderTabs();
  }

  function movePaneTo(fromPaneId, targetPaneId, edge) {
    if (!fromPaneId || !targetPaneId || fromPaneId === targetPaneId || !edge) return false;
    const from = state.panes.get(fromPaneId);
    const to = state.panes.get(targetPaneId);
    if (!from || !to || from.tabId !== to.tabId) return false;
    const tab = state.tabs.get(from.tabId);
    if (!tab?.layout) return false;
    if (edge === "center") {
      tab.layout = swapLeaves(tab.layout, fromPaneId, targetPaneId);
    } else {
      const next = removeLeaf(tab.layout, fromPaneId);
      if (!next) return false;
      tab.layout = insertBeside(next, targetPaneId, { type: "leaf", paneId: fromPaneId }, edge);
    }
    tab.zoomedPaneId = "";
    tab.focusedPaneId = fromPaneId;
    tab.dirty = true;
    renderTabs();
    return true;
  }

  function dockTabIntoPane(srcTabId, targetPaneId, edge) {
    const src = state.tabs.get(srcTabId);
    const target = state.panes.get(targetPaneId);
    const dest = target && state.tabs.get(target.tabId);
    if (!src || !dest || src.id === dest.id || src.start || dest.start || !src.layout || !dest.layout) return false;
    const dockEdge = edge === "center" || !edge ? "right" : edge;
    dest.zoomedPaneId = "";
    src.zoomedPaneId = "";
    rehomeLayout(src.layout, dest.id);
    dest.layout = insertBeside(dest.layout, targetPaneId, src.layout, dockEdge);
    dest.focusedPaneId = src.focusedPaneId || targetPaneId;
    dest.sync = dest.sync || src.sync;
    dest.dirty = true;
    closeTab(src.id, { skipPanes: true, silent: true });
    state.activeId = dest.id;
    dest.dirty = true;
    renderTabs();
    return true;
  }

  function extractPaneToTab(paneId, toIndex) {
    const pane = state.panes.get(paneId);
    const src = pane && state.tabs.get(pane.tabId);
    if (!src || leafCount(src.layout) < 2) return false;
    const next = removeLeaf(src.layout, paneId);
    if (!next) return false;
    src.layout = next;
    src.zoomedPaneId = "";
    if (src.focusedPaneId === paneId) src.focusedPaneId = tabPanes(src)[0]?.id || "";
    src.dirty = true;
    const tab = createTab(pane);
    if (typeof toIndex === "number" && toIndex >= 0) moveTabToIndex(tab.id, toIndex);
    renderTabs();
    return true;
  }

  function sendSnippet(id) {
    const sn = state.snippets.find((s) => s.id === id);
    if (!sn) return;
    const pane = focusedPane();
    if (!pane) {
      toast("Open a session first");
      return;
    }
    const command = sn.command.endsWith("\n") ? sn.command : sn.command + "\n";
    const bytes = new TextEncoder().encode(command);
    const tab = state.tabs.get(pane.tabId);
    const targets = tab?.sync ? tabPanes(tab) : [pane];
    let sent = false;
    for (const p of targets) {
      const open = p.remote ? p.dc?.readyState === "open" : p.ws?.readyState === WebSocket.OPEN;
      if (!open || !isDriving(p)) continue;
      sendSessionBytes(p, bytes);
      sent = true;
    }
    if (!sent) {
      toast(isDriving(pane) ? "Session is not connected" : "You are following — take or request control to type");
      return;
    }
    pane.term.focus();
  }

  function fillIdentitySelect(selected) {
    fillSelectItems(
      "host-identity",
      state.identities.map((id) => ({ value: id.id, label: id.name })),
      selected,
      "No identities — add in Keychain"
    );
  }

  function fillHostForm(host) {
    const form = els.hostForm;
    if (!form) return;
    form.reset();
    form.id.value = host?.id || "";
    form.name.value = host?.name || "";
    form.group.value = host?.group || "";
    form.address.value = host?.address && host.address !== "localhost" ? host.address : host?.address || "";
    form.port.value = host?.port || 22;
    form.username.value = host?.username || "";
    form.password.value = "";
    form.privateKey.value = "";
    form.passphrase.value = "";
    setSelectValue("host-auth-type", host?.authType || "password");
    fillIdentitySelect(host?.identityId || "");
    const color = host?.color || "#5ee0b5";
    const radio = form.querySelector(`input[name="color"][value="${color}"]`);
    if (radio) {
      radio.checked = true;
      radio.dispatchEvent(new Event("change", { bubbles: true }));
    }
    const keepOn = !host || host.keepAliveSec !== -1;
    if (form.keepAlive) setInputChecked(form.keepAlive, keepOn);
    if (form.keepAliveSec) form.keepAliveSec.value = host?.keepAliveSec > 0 ? String(host.keepAliveSec) : "30";
    if (form.autoReconnect) setInputChecked(form.autoReconnect, !host?.noAutoReconnect);
    const title = document.getElementById("host-modal-title");
    if (title) title.textContent = host?.id ? "Edit host" : "New host";
    toggleAuthFields();
  }

  function toggleAuthFields() {
    const form = els.hostForm;
    if (!form?.authType) return;
    const auth = form.authType.value;
    form.querySelectorAll("[data-auth]").forEach((node) => {
      node.style.display = node.dataset.auth === auth ? "grid" : "none";
    });
    const ssh = auth !== "local";
    form.querySelectorAll("[data-ssh-only]").forEach((node) => {
      node.style.display = ssh ? (node.classList.contains("grid-2") ? "grid" : "") : "none";
    });
    toggleKeepAliveFields();
  }

  function toggleKeepAliveFields() {
    const form = els.hostForm;
    if (!form?.keepAliveSec) return;
    form.keepAliveSec.disabled = !form.keepAlive?.checked;
  }

  function toggleIdentFields() {
    const form = els.identityForm;
    if (!form?.kind) return;
    const kind = form.kind.value;
    form.querySelectorAll("[data-ident]").forEach((node) => {
      node.style.display = node.dataset.ident === kind ? "grid" : "none";
    });
  }

  function toggleTunnelDest() {
    const form = els.tunnelForm;
    if (!form?.kind) return;
    const kind = form.kind.value;
    form.querySelectorAll("[data-tunnel-dest]").forEach((node) => {
      node.style.display = kind === "dynamic" ? "none" : "grid";
    });
  }

  function secretPayload(value) {
    if (!value || value === "__saved__") return "";
    return value;
  }

  async function saveHost(event) {
    event.preventDefault();
    const form = els.hostForm;
    const payload = {
      name: form.name.value.trim(),
      group: form.group.value.trim(),
      address: form.address.value.trim(),
      port: Number(form.port.value || 22),
      username: form.username.value.trim(),
      authType: form.authType.value,
      identityId: form.authType.value === "identity" ? form.identityId.value : "",
      password: secretPayload(form.password.value),
      privateKey: secretPayload(form.privateKey.value),
      passphrase: secretPayload(form.passphrase.value),
      color: form.color.value,
      keepAliveSec: form.authType.value === "local" ? 0 : form.keepAlive?.checked ? Number(form.keepAliveSec.value || 30) : -1,
      noAutoReconnect: form.authType.value !== "local" && !form.autoReconnect?.checked,
    };
    if (form.id.value) {
      await api(`/api/hosts/${form.id.value}`, { method: "PUT", body: JSON.stringify({ ...payload, id: form.id.value }) });
    } else {
      await api("/api/hosts", { method: "POST", body: JSON.stringify(payload) });
    }
    closeModal(els.hostModal);
    await loadAll();
    toast("Host saved");
  }

  function fillIdentityForm(id) {
    const form = els.identityForm;
    if (!form) return;
    form.reset();
    form.id.value = id?.id || "";
    form.name.value = id?.name || "";
    form.username.value = id?.username || "";
    setSelectValue("ident-kind", id?.kind || "password");
    const title = document.getElementById("identity-modal-title");
    if (title) title.textContent = id?.id ? "Edit identity" : "New identity";
    toggleIdentFields();
  }

  async function saveIdentity(event) {
    event.preventDefault();
    const form = els.identityForm;
    const payload = {
      name: form.name.value.trim(),
      username: form.username.value.trim(),
      kind: form.kind.value,
      password: secretPayload(form.password.value),
      privateKey: secretPayload(form.privateKey.value),
      passphrase: secretPayload(form.passphrase.value),
    };
    if (form.id.value) {
      await api(`/api/identities/${form.id.value}`, { method: "PUT", body: JSON.stringify({ ...payload, id: form.id.value }) });
    } else {
      await api("/api/identities", { method: "POST", body: JSON.stringify(payload) });
    }
    closeModal(els.identityModal);
    await loadAll();
    toast("Identity saved");
  }

  function sshHosts() {
    return state.hosts.filter((h) => h.authType !== "local");
  }

  function fillTunnelHosts(selected) {
    fillSelectItems(
      "tunnel-host",
      sshHosts().map((h) => ({ value: h.id, label: h.name })),
      selected,
      "Add an SSH host first"
    );
  }

  function fillTunnelForm(t) {
    const form = els.tunnelForm;
    if (!form) return;
    form.reset();
    form.id.value = t?.id || "";
    form.name.value = t?.name || "";
    setSelectValue("tunnel-kind", t?.kind || "local");
    form.bindAddr.value = t?.bindAddr || "127.0.0.1";
    form.bindPort.value = t?.bindPort ?? 0;
    form.destHost.value = t?.destHost || "127.0.0.1";
    form.destPort.value = t?.destPort || 5432;
    fillTunnelHosts(t?.hostId);
    const title = document.getElementById("tunnel-modal-title");
    if (title) title.textContent = t?.id ? "Edit forward" : "New forward";
    toggleTunnelDest();
  }

  async function saveTunnel(event) {
    event.preventDefault();
    const form = els.tunnelForm;
    if (!form.hostId.value) {
      toast("Pick an SSH host first", true);
      return;
    }
    const payload = {
      name: form.name.value.trim(),
      hostId: form.hostId.value,
      kind: form.kind.value,
      bindAddr: form.bindAddr.value.trim() || "127.0.0.1",
      bindPort: Number(form.bindPort.value || 0),
      destHost: form.destHost.value.trim(),
      destPort: Number(form.destPort.value || 0),
    };
    if (form.id.value) {
      await api(`/api/tunnels/${form.id.value}`, { method: "PUT", body: JSON.stringify({ ...payload, id: form.id.value }) });
    } else {
      await api("/api/tunnels", { method: "POST", body: JSON.stringify(payload) });
    }
    closeModal(els.tunnelModal);
    await loadAll();
    toast("Forward saved");
  }

  async function saveQuick(event) {
    event.preventDefault();
    const target = els.quickForm.target.value.trim();
    const password = els.quickForm.password.value;
    const parsed = parseTarget(target);
    if (!parsed) {
      toast("Could not parse user@host", true);
      return;
    }
    const host = await api("/api/hosts", {
      method: "POST",
      body: JSON.stringify({
        name: `${parsed.user}@${parsed.host}`,
        group: "Quick connect",
        address: parsed.host,
        port: parsed.port,
        username: parsed.user,
        authType: "password",
        password,
        color: "#6ea8ff",
      }),
    });
    closeModal(els.quickModal);
    await loadAll();
    openSession(host);
  }

  function parseTarget(value) {
    const match = value.match(/^(?:ssh\s+)?([^@\s]+)@(\[?[A-Za-z0-9._:-]+\]?)(?::(\d+))?$/);
    if (!match) return null;
    return { user: match[1], host: match[2].replace(/^\[|\]$/g, ""), port: Number(match[3] || 22) };
  }

  function connectFromSearch() {
    const q = (els.search?.value || "").trim();
    const parsed = parseTarget(q);
    if (parsed) {
      if (els.quickForm?.target) els.quickForm.target.value = q;
      openModal(els.quickModal);
      return;
    }
    const first = document.querySelector("#host-list [data-connect]");
    if (first) {
      first.click();
      return;
    }
    toast("No matching host", true);
  }

  async function saveSnippet(event) {
    event.preventDefault();
    const form = els.snippetForm;
    let command = form.command.value;
    if (!command.endsWith("\n")) command += "\n";
    const payload = { name: form.name.value.trim(), command };
    if (form.id.value) {
      await api(`/api/snippets/${form.id.value}`, { method: "PUT", body: JSON.stringify({ ...payload, id: form.id.value }) });
    } else {
      await api("/api/snippets", { method: "POST", body: JSON.stringify(payload) });
    }
    closeModal(els.snippetModal);
    await loadAll();
    toast("Snippet saved");
  }

  function openModal(el) {
    if (!el) return;
    if (state.openModal && state.openModal !== el) closeModal(state.openModal);
    try {
      if (typeof el.showModal === "function") {
        if (!el.open) el.showModal();
      } else if (typeof el.show === "function" && !el.open) {
        el.show();
      }
    } catch {
      /* already open */
    }
    els.modalScrim?.classList.add("is-open");
    state.openModal = el;
  }

  function closeModal(el = state.openModal) {
    if (!el) {
      state.openModal = null;
      els.modalScrim?.classList.remove("is-open");
      return;
    }
    holdPtyFromChrome();
    try {
      if (typeof el.close === "function" && el.open) el.close();
    } catch {
      /* ignore */
    }
    els.modalScrim?.classList.remove("is-open");
    if (state.openModal === el) state.openModal = null;
  }

  function promptHostKey(src, retry = {}) {
    const kind = src?.errorKind;
    if (kind !== "unknown-host" && kind !== "host-key-mismatch") return false;
    const hostname = src.hostname || src.hostKeyHost || "";
    const fingerprint = src.fingerprint || src.hostKeyFingerprint || "";
    if (!hostname || !fingerprint) return false;
    state.hostKeyRetry = {
      hostId: retry.hostId || src.hostId || "",
      sessionId: retry.sessionId || src.sessionId || src.id || "",
      tunnelId: retry.tunnelId || "",
      replace: kind === "host-key-mismatch",
    };
    const title = document.getElementById("hostkey-title");
    const hint = document.getElementById("hostkey-hint");
    const hostEl = document.getElementById("hostkey-host");
    const fpEl = document.getElementById("hostkey-fp");
    const expectedEl = document.getElementById("hostkey-expected");
    if (title) title.textContent = kind === "host-key-mismatch" ? "Host key changed" : "Unknown host key";
    if (hint) {
      hint.textContent =
        kind === "host-key-mismatch"
          ? "This machine presented a different key than the one saved in Keel. Only replace it if you reinstalled the server."
          : "Trust this key once to connect. Keel will reject a different key later.";
    }
    if (hostEl) hostEl.textContent = hostname;
    if (fpEl) fpEl.textContent = fingerprint;
    if (expectedEl) {
      expectedEl.hidden = kind !== "host-key-mismatch" || !src.expected;
      expectedEl.textContent = src.expected ? `saved: ${src.expected}` : "";
    }
    els.hostKeyForm.dataset.hostname = hostname;
    els.hostKeyForm.dataset.fingerprint = fingerprint;
    openModal(els.hostKeyModal);
    return true;
  }

  async function submitHostKey(ev) {
    ev.preventDefault();
    const hostname = els.hostKeyForm.dataset.hostname;
    const fingerprint = els.hostKeyForm.dataset.fingerprint;
    const retry = state.hostKeyRetry || {};
    try {
      await api("/api/known-hosts", {
        method: "POST",
        body: JSON.stringify({ hostname, fingerprint, replace: !!retry.replace }),
      });
      closeModal(els.hostKeyModal);
      if (retry.sessionId) {
        await api(`/api/sessions/${retry.sessionId}`, { method: "DELETE" }).catch(() => {});
      }
      if (retry.hostId) {
        const host = state.hosts.find((h) => h.id === retry.hostId);
        if (host) await openSession(host);
      }
      if (retry.tunnelId) {
        await api(`/api/tunnels/${retry.tunnelId}/start`, { method: "POST" });
        await loadAll();
      }
      state.hostKeyRetry = null;
      toast("Host key saved");
    } catch (err) {
      toast(err.message, true);
    }
  }

  async function copyText(text) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.cssText = "position:fixed;top:0;left:0;width:1px;height:1px;opacity:0";
      document.body.appendChild(ta);
      ta.select();
      let ok = false;
      try {
        ok = document.execCommand("copy");
      } catch {
        /* WKWebView often blocks this after await fetch */
      }
      ta.remove();
      return ok;
    }
  }

  async function shareActive() {
    const pane = focusedPane();
    if (!pane || pane.remote) return;
    try {
      toast("Sharing…");
      const out = await api("/api/share", {
        method: "POST",
        body: JSON.stringify({ sessionId: pane.runtimeId }),
      });
      const url = out.url || "";
      if (!url) {
        toast("Share ready");
        return;
      }
      const copied = await copyText(url);
      toast(copied ? `Copied ${url}` : url);
      if (!copied) window.prompt("Join link", url);
    } catch (err) {
      toast(err.message, true);
    }
  }

  function openJoin() {
    els.joinForm.reset();
    openModal(els.joinModal);
    queueMicrotask(() => els.joinForm.url.focus());
  }

  function parseJoinLink(input, fallbackOrigin) {
    const raw = String(input || "").trim();
    if (!raw) throw new Error("paste a Share link");
    try {
      const u = new URL(raw);
      const room = u.searchParams.get("room");
      if (!room) throw new Error("link has no room");
      return { origin: u.origin, room };
    } catch (err) {
      if (err.message === "link has no room") throw err;
      if (/^[0-9a-f]{8,}$/i.test(raw) && fallbackOrigin) {
        return { origin: String(fallbackOrigin).replace(/\/$/, ""), room: raw };
      }
      throw new Error("paste the full Share link");
    }
  }

  function joinRemote({ origin, room }) {
    const runtimeId = `join:${origin}:${room}`;
    const existing = findPaneByRuntime(runtimeId);
    if (existing) {
      state.activeId = existing.tabId;
      renderTabs();
      existing.term.focus();
      return;
    }
    const info = {
      id: runtimeId,
      hostId: "",
      title: "Follow",
      subtitle: origin.replace(/^https?:\/\//, ""),
      status: "",
    };
    const paneId = uid("pane");
    const shell = makePaneShell(info, paneId);
    const pane = {
      id: paneId,
      runtimeId,
      hostId: "",
      title: "Follow",
      subtitle: info.subtitle,
      status: "",
      term: shell.term,
      fit: shell.fit,
      host: shell.host,
      wrap: shell.wrap,
      banner: shell.banner,
      search: shell.search,
      findEl: shell.findEl,
      ws: null,
      sigWs: null,
      pc: null,
      dc: null,
      remote: true,
      you: null,
      attached: [],
      driverId: 0,
      ownerId: 0,
    };
    wirePaneTerminal(pane);
    const tab = createTab(pane);
    tab.remote = true;
    tab.hostId = "";

    const applyPresence = (msg) => {
      if (msg.you) pane.you = msg.you;
      if (msg.attached) pane.attached = msg.attached;
      if (msg.driverId != null) pane.driverId = msg.driverId;
      if (msg.ownerId != null) pane.ownerId = msg.ownerId;
      if (msg.title) pane.title = msg.title;
      pane.banner.hidden = isDriving(pane);
      renderTabs();
    };

    const proto = origin.startsWith("https:") ? "wss" : "ws";
    const host = origin.replace(/^https?:\/\//, "");
    const q = new URLSearchParams({
      role: "join",
      room,
      name: state.name || "guest",
    });
    const sigWs = new WebSocket(`${proto}://${host}/ws?${q}`);
    pane.sigWs = sigWs;
    const pc = new RTCPeerConnection({
      iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
    });
    pane.pc = pc;

    const sendSignal = (msg) => {
      if (sigWs.readyState === WebSocket.OPEN) {
        sigWs.send(JSON.stringify({ type: "signal", ...msg }));
      }
    };

    pc.onicecandidate = (ev) => {
      if (!ev.candidate) return;
      sendSignal({
        kind: "ice",
        candidate: ev.candidate.candidate,
        sdpMid: ev.candidate.sdpMid,
        sdpMLineIndex: ev.candidate.sdpMLineIndex,
      });
    };
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === "failed" || pc.connectionState === "disconnected") {
        pane.status = "is-error";
        toast("Follow connection lost", true);
        renderTabs();
      }
    };
    pc.ondatachannel = (ev) => {
      const dc = ev.channel;
      dc.binaryType = "arraybuffer";
      pane.dc = dc;
      dc.onopen = () => {
        pane.status = "is-ready";
        sendResize(pane);
        renderTabs();
      };
      dc.onclose = () => {
        if (pane.status !== "is-error") pane.status = "";
        pane.term.writeln("\r\n\x1b[38;2;139;149;168mHost detached.\x1b[0m");
        renderTabs();
      };
      dc.onmessage = (ev2) => {
        if (typeof ev2.data === "string") {
          let msg;
          try {
            msg = JSON.parse(ev2.data);
          } catch {
            return;
          }
          if (msg.type === "error") {
            pane.status = "is-error";
            pane.term.writeln(`\r\n\x1b[38;2;248;113;113m${msg.message}\x1b[0m`);
            renderTabs();
            toast(msg.message, true);
            return;
          }
          if (msg.type === "ready" || msg.type === "presence" || msg.type === "control" || msg.type === "status") {
            if (msg.status === "ready") pane.status = "is-ready";
            if (msg.status === "connecting") pane.status = "is-connecting";
            if (msg.status === "error" || msg.status === "exited") pane.status = "is-error";
            applyPresence(msg);
            if (msg.type === "control" && msg.message) toast(msg.message);
          }
          return;
        }
        pane.term.write(new Uint8Array(ev2.data));
      };
    };

    sigWs.addEventListener("message", async (ev) => {
      const msg = JSON.parse(ev.data);
      if (msg.type === "error") {
        toast(msg.name || "signaling error", true);
        return;
      }
      if (msg.type === "leave") {
        toast("Host left");
        pc.close();
        return;
      }
      if (msg.type !== "signal") return;
      if (msg.kind === "offer" && msg.sdp) {
        await pc.setRemoteDescription({ type: "offer", sdp: msg.sdp });
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        sendSignal({ kind: "answer", sdp: answer.sdp });
      }
      if (msg.kind === "ice" && msg.candidate) {
        try {
          await pc.addIceCandidate({
            candidate: msg.candidate,
            sdpMid: msg.sdpMid,
            sdpMLineIndex: msg.sdpMLineIndex,
          });
        } catch {
          /* ignore */
        }
      }
    });
    sigWs.addEventListener("error", () => toast("Signaling failed", true));
    sigWs.addEventListener("close", () => {
      if (pc.connectionState !== "connected" && pane.status !== "is-error") {
        pane.status = "is-error";
        toast("Signaling closed", true);
        renderTabs();
      }
    });
    renderTabs();
  }

  async function submitJoin(ev) {
    ev.preventDefault();
    try {
      const parsed = parseJoinLink(els.joinForm.url.value, state.signalURL);
      closeModal(els.joinModal);
      joinRemote(parsed);
    } catch (err) {
      toast(err.message, true);
    }
  }

  function showGate(needsSetup) {
    els.gate.hidden = false;
    const title = document.getElementById("gate-title");
    const hint = document.getElementById("gate-hint");
    const submit = document.getElementById("gate-submit");
    if (needsSetup) {
      if (title) title.textContent = "Create vault";
      if (hint) hint.textContent = "Choose a passphrase to encrypt host passwords and keys on this machine.";
      if (submit) submit.textContent = "Create vault";
    } else {
      if (title) title.textContent = "Unlock vault";
      if (hint) hint.textContent = "Host passwords and keys are encrypted. Enter your vault passphrase.";
      if (submit) submit.textContent = "Unlock";
    }
    if (els.gateForm?.passphrase) {
      els.gateForm.passphrase.value = "";
      els.gateForm.passphrase.focus();
    }
    if (els.gateForm) els.gateForm.dataset.setup = needsSetup ? "1" : "0";
  }

  function activateTabIndex(n) {
    const list = chromeTabs();
    let idx = n - 1;
    if (n === 9) idx = list.length - 1;
    const id = list[idx];
    if (!id) return;
    state.activeId = id;
    renderTabs();
  }

  async function boot() {
    persistName();
    try {
      const health = await api("/api/health");
      state.signalURL = health.signalURL || "";
      if (health.locked) {
        showGate(!!health.needsSetup);
        return;
      }
      await loadAll();
    } catch (err) {
      if (err.code === 423) {
        showGate(false);
        return;
      }
      toast(err.message, true);
      return;
    }
    els.gate.hidden = true;
    connectHerd();
    renderTabs();
    const chromeFocus = document.activeElement;
    if (chromeFocus && chromeFocus.closest?.(".workspace-bar")) chromeFocus.blur();
    syncSettingsForm();
    restoreOpenSessions();
    if (state.pendingSession) {
      const pane = findPaneByRuntime(state.pendingSession);
      if (pane) {
        state.activeId = pane.tabId;
        const tab = state.tabs.get(pane.tabId);
        if (tab) tab.focusedPaneId = pane.id;
        renderTabs();
      } else {
        const info = state.herd.find((s) => s.id === state.pendingSession);
        if (info) attachRuntime(info);
      }
      state.pendingSession = "";
    }
  }

  function finishTabPointer(ev) {
    if (!tabDrag || ev.pointerId !== tabDrag.pointerId) return;
    const drag = tabDrag;
    const overStrip = hitTabStrip(ev.clientX, ev.clientY);
    const dock = drag.moved && !overStrip ? paneDropAt(ev.clientX, ev.clientY, { fromTabId: drag.id }) : null;
    const toIndex = drag.moved && overStrip ? tabStripInsertIndex(ev.clientX) : -1;
    endTabDrag();
    if (!drag.moved) return;
    ignoreTabClick = true;
    window.setTimeout(() => {
      ignoreTabClick = false;
    }, 50);
    if (dock && !dock.self && dock.edge) {
      dockTabIntoPane(drag.id, dock.paneId, dock.edge);
      return;
    }
    if (overStrip && moveTabToIndex(drag.id, toIndex)) renderTabs();
  }

  function finishPanePointer(ev) {
    if (!paneDrag || ev.pointerId !== paneDrag.pointerId) return;
    const drag = paneDrag;
    const overStrip = hitTabStrip(ev.clientX, ev.clientY);
    const dock = drag.moved && !overStrip ? paneDropAt(ev.clientX, ev.clientY, { excludePaneId: drag.paneId }) : null;
    const toIndex = drag.moved && overStrip ? tabStripInsertIndex(ev.clientX) : -1;
    endPaneDrag();
    if (!drag.moved) return;
    if (overStrip) {
      extractPaneToTab(drag.paneId, toIndex);
      return;
    }
    if (dock && !dock.self && dock.edge) movePaneTo(drag.paneId, dock.paneId, dock.edge);
  }

  function bindChrome() {
    window.addEventListener(
      "keydown",
      (ev) => {
        if (isHoldModKey(ev) && !ev.repeat) {
          clearTimeout(kbdTimer);
          kbdTimer = window.setTimeout(() => showKbdHints(true), 280);
        } else if (!isHoldModKey(ev)) {
          clearTimeout(kbdTimer);
        }
        if (ev.key === "Escape" && tabDrag) {
          ev.preventDefault();
          ignoreTabClick = tabDrag.moved;
          endTabDrag();
          return;
        }
        if (ev.key === "Escape" && paneDrag) {
          ev.preventDefault();
          endPaneDrag();
          return;
        }
        if (ev.key === "Escape") {
          if (closeOpenFind()) {
            ev.preventDefault();
            return;
          }
          const tab = state.tabs.get(state.activeId);
          if (tab?.zoomedPaneId) {
            ev.preventDefault();
            togglePaneZoom(tab.zoomedPaneId);
            return;
          }
        }
        if (ev.key === "Escape" && state.openModal) {
          ev.preventDefault();
          closeModal();
          return;
        }
        handleShortcut(ev);
      },
      true
    );
    window.addEventListener("keyup", (ev) => {
      if (isHoldModKey(ev)) {
        clearTimeout(kbdTimer);
        showKbdHints(false);
      }
    });
    window.addEventListener("blur", () => {
      clearTimeout(kbdTimer);
      showKbdHints(false);
      if (tabDrag) {
        ignoreTabClick = tabDrag.moved;
        endTabDrag();
      }
      if (paneDrag) endPaneDrag();
    });
    window.addEventListener("resize", () => {
      fitVisiblePanes();
      syncTabOverflow();
    });
    on(els.tabs, "scroll", syncTabOverflow, { passive: true });
    on(els.tabs, "wheel", (ev) => {
      if (Math.abs(ev.deltaX) >= Math.abs(ev.deltaY)) return;
      if (els.tabs.scrollWidth <= els.tabs.clientWidth) return;
      els.tabs.scrollLeft += ev.deltaY;
      ev.preventDefault();
    }, { passive: false });
    if (typeof ResizeObserver !== "undefined" && els.tabs) {
      new ResizeObserver(syncTabOverflow).observe(els.tabs);
    }
    window.matchMedia("(prefers-color-scheme: light)").addEventListener("change", () => {
      if ((document.documentElement.dataset.theme || "system") === "system") {
        applyThemeClass();
        applyTerminalPrefs();
      }
    });

    document.addEventListener(
      "contextmenu",
      (ev) => {
        if (!(ev.target instanceof Element)) return;
        const trigger = ev.target.closest("#tab-ctx-trigger");
        if (!trigger) return;
        const tab = ev.target.closest(".tab[data-tab]");
        if (!tab) {
          ev.preventDefault();
          ev.stopPropagation();
          return;
        }
        tabMenuTarget = tab.dataset.tab;
        syncTabContextMenu(tabMenuTarget);
      },
      true
    );
    document.addEventListener(
      "pointerup",
      (ev) => {
        if (ev.button !== 2) return;
        if (!(ev.target instanceof Element)) return;
        if (!ev.target.closest("#tab-ctx-trigger")) return;
        const tab = ev.target.closest(".tab[data-tab]");
        if (!tab) return;
        tabMenuTarget = tab.dataset.tab;
        syncTabContextMenu(tabMenuTarget);
        const x = ev.clientX;
        const y = ev.clientY;
        requestAnimationFrame(() => {
          const menu = document.getElementById("tab-ctx");
          if (menu && menu.hasAttribute("data-open")) return;
          tab.dispatchEvent(
            new MouseEvent("contextmenu", {
              bubbles: true,
              cancelable: true,
              clientX: x,
              clientY: y,
              view: window,
              button: 2,
            })
          );
        });
      },
      true
    );
    document.addEventListener("click", (ev) => {
      if (!(ev.target instanceof Element)) return;
      const item = ev.target.closest("[data-tab-action]");
      if (!item || item.disabled || item.getAttribute("aria-disabled") === "true") return;
      runTabContextAction(item.dataset.tabAction, tabMenuTarget);
    });
    on(els.tabs, "click", (ev) => {
      if (ev.target.closest(".tab-rename")) return;
      if (tabRenaming) {
        if (ev.target.closest(`.tab[data-tab="${CSS.escape(tabRenaming)}"]`)) return;
        const input = els.tabs.querySelector(".tab-rename");
        applyTabRename(tabRenaming, input?.value, true);
      }
      if (ignoreTabClick) {
        ev.preventDefault();
        ev.stopPropagation();
        return;
      }
      const close = ev.target.closest("[data-close]");
      if (close) {
        ev.preventDefault();
        closeTab(close.dataset.close);
        return;
      }
      const tab = ev.target.closest("[data-tab]");
      if (tab) {
        state.activeId = tab.dataset.tab;
        renderTabs();
      }
    });
    on(els.tabs, "pointerdown", (ev) => {
      if (ev.button !== 0) return;
      if (ev.metaKey || ev.ctrlKey || ev.altKey || ev.shiftKey) return;
      if (ev.target.closest("[data-close], .tab-rename")) return;
      const tab = ev.target.closest(".tab[data-tab]");
      if (!tab || tab.dataset.tab === VAULT) return;
      if (!state.tabs.has(tab.dataset.tab)) return;
      tabDrag = { id: tab.dataset.tab, startX: ev.clientX, startY: ev.clientY, pointerId: ev.pointerId, moved: false };
    });
    on(els.tabs, "dragstart", (ev) => ev.preventDefault());
    on(els.terminals, "pointerdown", (ev) => {
      if (ev.button !== 0) return;
      if (ev.metaKey || ev.ctrlKey || ev.altKey || ev.shiftKey) return;
      if (ev.target.closest("button")) return;
      const bar = ev.target.closest(".pane-bar");
      if (!bar) return;
      const wrap = bar.closest(".term-wrap");
      const paneId = wrap?.dataset.paneId;
      if (!paneId || !state.panes.has(paneId)) return;
      paneDrag = { paneId, startX: ev.clientX, startY: ev.clientY, pointerId: ev.pointerId, moved: false };
      try {
        bar.setPointerCapture(ev.pointerId);
      } catch {
        /* ignore */
      }
    });
    document.addEventListener(
      "pointermove",
      (ev) => {
        if (tabDrag && ev.pointerId === tabDrag.pointerId) {
          const dy = ev.clientY - (tabDrag.startY ?? tabDrag.startX);
          if (!tabDrag.moved && Math.hypot(ev.clientX - tabDrag.startX, dy) < 6) return;
          if (!tabDrag.moved) {
            tabDrag.moved = true;
            document.body.classList.add("is-tab-dragging");
          }
          ev.preventDefault();
          autoScrollTabs(ev.clientX);
          if (hitTabStrip(ev.clientX, ev.clientY)) {
            hidePaneDrop();
            paintTabDrop(tabStripInsertIndex(ev.clientX));
          } else {
            clearTabDropMarks();
            const dragging = els.tabs.querySelector(`.tab[data-tab="${CSS.escape(tabDrag.id)}"]`);
            dragging?.classList.add("is-dragging");
            paintPaneDrop(paneDropAt(ev.clientX, ev.clientY, { fromTabId: tabDrag.id }));
          }
          return;
        }
        if (!paneDrag || ev.pointerId !== paneDrag.pointerId) return;
        if (!paneDrag.moved && Math.hypot(ev.clientX - paneDrag.startX, ev.clientY - paneDrag.startY) < 6) return;
        if (!paneDrag.moved) {
          paneDrag.moved = true;
          document.body.classList.add("is-pane-dragging");
          const pane = state.panes.get(paneDrag.paneId);
          pane?.wrap?.classList.add("is-dragging");
          const tab = pane && state.tabs.get(pane.tabId);
          if (tab?.zoomedPaneId) {
            tab.zoomedPaneId = "";
            tab.dirty = true;
            renderTabs();
          }
        }
        ev.preventDefault();
        if (hitTabStrip(ev.clientX, ev.clientY)) {
          hidePaneDrop();
          const src = state.panes.get(paneDrag.paneId);
          const tab = src && state.tabs.get(src.tabId);
          if (tab && leafCount(tab.layout) > 1) paintTabDrop(tabStripInsertIndex(ev.clientX));
          else clearTabDropMarks();
        } else {
          clearTabDropMarks();
          paintPaneDrop(paneDropAt(ev.clientX, ev.clientY, { excludePaneId: paneDrag.paneId }));
        }
      },
      { capture: true, passive: false }
    );
    document.addEventListener("pointerup", finishTabPointer, true);
    document.addEventListener("pointerup", finishPanePointer, true);
    document.addEventListener(
      "pointercancel",
      (ev) => {
        if (tabDrag && ev.pointerId === tabDrag.pointerId) {
          ignoreTabClick = tabDrag.moved;
          endTabDrag();
        }
        if (paneDrag && ev.pointerId === paneDrag.pointerId) endPaneDrag();
      },
      true
    );

    on(document.getElementById("btn-new-tab"), "click", () => newTab());
    on(els.terminals, "click", (ev) => {
      const split = ev.target.closest("[data-pane-split]");
      if (split) {
        ev.preventDefault();
        splitPane(split.dataset.paneId, split.dataset.paneSplit);
        return;
      }
      const sync = ev.target.closest("[data-pane-sync]");
      if (sync) {
        ev.preventDefault();
        const pane = state.panes.get(sync.dataset.paneSync);
        if (pane) toggleTabSync(pane.tabId);
        return;
      }
      const zoom = ev.target.closest("[data-pane-zoom]");
      if (zoom && !zoom.disabled) {
        ev.preventDefault();
        togglePaneZoom(zoom.dataset.paneZoom);
        return;
      }
      const tab = tabFromRoot(ev.target);
      if (!tab?.start) return;
      const connect = ev.target.closest("[data-start-connect]");
      const quick = ev.target.closest("[data-start-quick]");
      const join = ev.target.closest("[data-start-join]");
      if (connect) {
        const host = state.hosts.find((h) => h.id === connect.dataset.startConnect);
        if (host) openSession(host, { tab }).catch((err) => toast(err.message, true));
      } else if (quick) openModal(els.quickModal);
      else if (join) openJoin();
    });
    on(els.terminals, "input", (ev) => {
      const input = ev.target.closest?.(".start-search");
      if (!input) return;
      const tab = tabFromRoot(input);
      if (!tab?.start) return;
      tab.query = input.value;
      tab.pick = "";
      paintStartPage(tab);
    });
    on(els.terminals, "keydown", (ev) => {
      const input = ev.target.closest?.(".start-search");
      if (!input) return;
      const tab = tabFromRoot(input);
      if (!tab?.start) return;
      const hosts = visibleStartHosts(tab);
      if (!hosts.length) return;
      const idx = Math.max(0, hosts.findIndex((h) => h.id === tab.pick));
      if (ev.key === "ArrowDown") {
        ev.preventDefault();
        setStartPick(tab, hosts[Math.min(hosts.length - 1, idx + 1)].id);
      } else if (ev.key === "ArrowUp") {
        ev.preventDefault();
        setStartPick(tab, hosts[Math.max(0, idx - 1)].id);
      } else if (ev.key === "Enter") {
        ev.preventDefault();
        const host = hosts.find((h) => h.id === tab.pick) || hosts[0];
        if (host) openSession(host, { tab }).catch((err) => toast(err.message, true));
      }
    });

    on(document.getElementById("vault-rail"), "click", (ev) => {
      const btn = ev.target.closest("[data-vault]");
      if (!btn) return;
      if (state.activeId !== VAULT) {
        state.activeId = VAULT;
        renderTabs();
      }
      setVaultSection(btn.dataset.vault);
    });
    on(document.querySelector("[data-settings-back]"), "click", () => {
      const search = document.getElementById("settings-search");
      if (search) search.value = "";
      filterSettings("");
      setVaultSection("hosts");
    });
    on(document.getElementById("page-settings"), "click", (ev) => {
      const btn = ev.target.closest("[data-settings-section]");
      if (!btn) return;
      setSettingsSection(btn.dataset.settingsSection);
    });
    on(document.getElementById("settings-search"), "input", (ev) => filterSettings(ev.target.value));
    on(document.getElementById("btn-welcome-local"), "click", () => newLocalTab());
    on(els.btnShare, "click", shareActive);
    on(document.getElementById("set-theme"), "select-change", (ev) => setTheme(ev.detail?.value));
    on(document.getElementById("settings-form"), "submit", (ev) => ev.preventDefault());
    on(document.getElementById("set-ui-size"), "slider-change", (ev) => {
      const v = ev.detail?.values?.[0];
      if (v != null) setUiSize(v);
    });
    on(document.getElementById("set-smooth"), "select-change", (ev) => setSmoothing(ev.detail?.value));
    on(document.getElementById("set-font"), "select-change", (ev) => {
      if (!ev.detail?.value) return;
      state.prefs.font = ev.detail.value;
      savePrefs();
      applyTerminalPrefs();
      syncTermPreview();
    });
    on(document.getElementById("set-size"), "slider-change", (ev) => {
      const v = ev.detail?.values?.[0];
      if (v != null) setFontSize(v);
    });
    on(document.getElementById("set-copy"), "change", (ev) => {
      state.prefs.copyOnSelect = ev.target.checked;
      savePrefs();
    });
    on(document.getElementById("set-paste"), "change", (ev) => {
      state.prefs.rightClickPaste = ev.target.checked;
      savePrefs();
    });
    on(els.gateForm, "submit", (ev) => {
      ev.preventDefault();
      const passphrase = els.gateForm.passphrase?.value;
      const setup = els.gateForm.dataset.setup === "1";
      const path = setup ? "/api/vault/setup" : "/api/vault/unlock";
      api(path, { method: "POST", body: JSON.stringify({ passphrase }) })
        .then(() => boot())
        .catch((err) => toast(err.message, true));
    });
  }

  function bindLists() {
    document.body.addEventListener("click", async (ev) => {
      const viewBtn = ev.target.closest("[data-vault-view-btn]");
      if (viewBtn) {
        setVaultView(viewBtn.getAttribute("data-vault-view-btn"));
        return;
      }
      if (ev.target.closest("#btn-quick")) {
        openModal(els.quickModal);
        return;
      }
      if (ev.target.closest("#btn-join")) {
        openJoin();
        return;
      }
      const groupFilter = ev.target.closest("[data-group-filter]");
      if (groupFilter && els.search) {
        const next = groupFilter.getAttribute("data-group-filter") || "";
        els.search.value = next;
        state.query = next;
        els.search.dispatchEvent(new Event("input", { bubbles: true }));
        return;
      }
      const connect = ev.target.closest("[data-connect]");
      if (connect) {
        const host = state.hosts.find((h) => h.id === connect.dataset.connect);
        if (host) openSession(host).catch((err) => toast(err.message, true));
        return;
      }
      const edit = ev.target.closest("[data-edit]");
      if (edit) {
        const host = state.hosts.find((h) => h.id === edit.dataset.edit);
        if (host) {
          fillHostForm(host);
          openModal(els.hostModal);
        }
        return;
      }
      const del = ev.target.closest("[data-delete]");
      if (del) {
        const host = state.hosts.find((h) => h.id === del.dataset.delete);
        if (!host) return;
        if (host.authType === "local") {
          toast("Local Shell is part of the workspace and cannot be removed");
          return;
        }
        if (!(await askConfirm(`Remove ${host.name}?`))) return;
        await api(`/api/hosts/${host.id}`, { method: "DELETE" });
        await loadAll();
        return;
      }
      const run = ev.target.closest("[data-run]");
      if (run) sendSnippet(run.dataset.run);
      const editSn = ev.target.closest("[data-edit-snippet]");
      if (editSn) {
        const sn = state.snippets.find((s) => s.id === editSn.dataset.editSnippet);
        if (!sn || !els.snippetForm) return;
        els.snippetForm.reset();
        els.snippetForm.id.value = sn.id;
        els.snippetForm.name.value = sn.name;
        els.snippetForm.command.value = sn.command;
        const title = document.getElementById("snippet-modal-title");
        if (title) title.textContent = "Edit snippet";
        openModal(els.snippetModal);
      }
      const delSn = ev.target.closest("[data-delete-snippet]");
      if (delSn) {
        const sn = state.snippets.find((s) => s.id === delSn.dataset.deleteSnippet);
        if (!sn || !(await askConfirm(`Delete snippet “${sn.name}”?`))) return;
        api(`/api/snippets/${sn.id}`, { method: "DELETE" }).then(loadAll);
      }
      const grant = ev.target.closest("[data-grant]");
      if (grant) sendControl("grant-control", { targetId: Number(grant.dataset.grant) });
      if (ev.target.closest("[data-request]")) sendControl("request-control");
      if (ev.target.closest("[data-take]")) sendControl("take-control");
      const paneClose = ev.target.closest("[data-pane-close]");
      if (paneClose) closePane(paneClose.dataset.paneClose);
      const paneRec = ev.target.closest("[data-pane-reconnect]");
      if (paneRec) {
        const pane = state.panes.get(paneRec.dataset.paneReconnect);
        reconnectPane(pane);
      }
      const paneCopy = ev.target.closest("[data-pane-copy-log]");
      if (paneCopy) copyPaneFailLog(state.panes.get(paneCopy.dataset.paneCopyLog));
      const paneEdit = ev.target.closest("[data-pane-edit-host]");
      if (paneEdit) editPaneHost(state.panes.get(paneEdit.dataset.paneEditHost));

      const editId = ev.target.closest("[data-edit-identity]");
      if (editId) {
        const id = state.identities.find((x) => x.id === editId.dataset.editIdentity);
        fillIdentityForm(id);
        openModal(els.identityModal);
      }
      const delId = ev.target.closest("[data-delete-identity]");
      if (delId) {
        const id = state.identities.find((x) => x.id === delId.dataset.deleteIdentity);
        if (!id || !(await askConfirm(`Delete identity “${id.name}”?`))) return;
        api(`/api/identities/${id.id}`, { method: "DELETE" }).then(loadAll);
      }
      const startTn = ev.target.closest("[data-tunnel-start]");
      if (startTn) {
        api(`/api/tunnels/${startTn.dataset.tunnelStart}/start`, { method: "POST" })
          .then(loadAll)
          .catch((err) => {
            if (!promptHostKey(err, { tunnelId: startTn.dataset.tunnelStart })) toast(err.message, true);
          });
      }
      const stopTn = ev.target.closest("[data-tunnel-stop]");
      if (stopTn) {
        api(`/api/tunnels/${stopTn.dataset.tunnelStop}/stop`, { method: "POST" })
          .then(loadAll)
          .catch((err) => toast(err.message, true));
      }
      const editTn = ev.target.closest("[data-edit-tunnel]");
      if (editTn) {
        const t = state.tunnels.find((x) => x.id === editTn.dataset.editTunnel);
        fillTunnelForm(t);
        openModal(els.tunnelModal);
      }
      const delTn = ev.target.closest("[data-delete-tunnel]");
      if (delTn) {
        const t = state.tunnels.find((x) => x.id === delTn.dataset.deleteTunnel);
        if (!t || !(await askConfirm(`Delete forward “${t.name}”?`))) return;
        api(`/api/tunnels/${t.id}`, { method: "DELETE" }).then(loadAll);
      }
    });

    on(els.search, "input", () => {
      state.query = els.search.value;
      paintList("host-list", "/ui/hosts?" + datastarQS());
    });
    on(els.search, "keydown", (ev) => {
      if (ev.key !== "Enter") return;
      ev.preventDefault();
      connectFromSearch();
    });
    on(document.getElementById("btn-host-connect"), "click", () => connectFromSearch());
    on(document.getElementById("log-filter"), "input", (ev) => {
      state.logFilter = ev.target.value;
      paintList("log-list", "/ui/logs?" + datastarQS());
    });
    on(document.getElementById("btn-refresh-logs"), "click", () => loadLogs().catch((err) => toast(err.message, true)));
  }

  function bindModals() {
    on(document.getElementById("btn-new-host"), "click", () => {
      fillHostForm(null);
      openModal(els.hostModal);
    });
    on(document.getElementById("btn-quick"), "click", () => openModal(els.quickModal));
    on(document.getElementById("btn-new-snippet"), "click", () => {
      els.snippetForm?.reset();
      const title = document.getElementById("snippet-modal-title");
      if (title) title.textContent = "New snippet";
      openModal(els.snippetModal);
    });
    on(document.getElementById("btn-new-identity"), "click", () => {
      fillIdentityForm(null);
      openModal(els.identityModal);
    });
    on(document.getElementById("btn-new-tunnel"), "click", () => {
      fillTunnelForm(null);
      openModal(els.tunnelModal);
    });
    on(document.getElementById("btn-join"), "click", openJoin);
    on(document.getElementById("join-cancel"), "click", () => closeModal(els.joinModal));
    on(document.getElementById("host-cancel"), "click", () => closeModal(els.hostModal));
    on(document.getElementById("quick-cancel"), "click", () => closeModal(els.quickModal));
    on(document.getElementById("snippet-cancel"), "click", () => closeModal(els.snippetModal));
    on(document.getElementById("identity-cancel"), "click", () => closeModal(els.identityModal));
    on(document.getElementById("tunnel-cancel"), "click", () => closeModal(els.tunnelModal));
    on(document.getElementById("hostkey-cancel"), "click", () => closeModal(els.hostKeyModal));
    on(els.modalScrim, "click", () => closeModal());
    [
      els.hostModal,
      els.quickModal,
      els.snippetModal,
      els.joinModal,
      els.identityModal,
      els.tunnelModal,
      els.hostKeyModal,
      els.cmdPalette,
    ].forEach((modal) => on(modal, "close", () => {
      if (state.openModal === modal) state.openModal = null;
      els.modalScrim?.classList.remove("is-open");
      fitVisiblePanes();
    }));
    on(els.ptyInjectModal, "close", () => {
      if (ptyInjectWaiter) {
        const done = ptyInjectWaiter;
        ptyInjectWaiter = null;
        done(false);
      }
      if (state.openModal === els.ptyInjectModal) state.openModal = null;
      els.modalScrim?.classList.remove("is-open");
      fitVisiblePanes();
    });
    on(els.cmdPaletteForm, "submit", (ev) => {
      ev.preventDefault();
      runPaletteItem(paletteVisible[paletteCursor]);
    });
    on(els.cmdPaletteInput, "input", () => {
      paletteCursor = 0;
      paintCommandPalette();
    });
    on(els.cmdPaletteInput, "keydown", (ev) => {
      if (ev.key === "ArrowDown") {
        ev.preventDefault();
        movePaletteCursor(1);
      } else if (ev.key === "ArrowUp") {
        ev.preventDefault();
        movePaletteCursor(-1);
      }
    });
    on(els.cmdPaletteList, "pointerdown", (ev) => {
      const btn = ev.target.closest?.("[data-palette-index]");
      if (!btn) return;
      ev.preventDefault();
      const i = Number(btn.dataset.paletteIndex);
      if (!Number.isFinite(i)) return;
      paletteCursor = i;
      runPaletteItem(paletteVisible[i]);
    });
    on(els.ptyInjectForm, "submit", (ev) => {
      ev.preventDefault();
      settlePtyInject(true);
    });
    on(document.getElementById("pty-inject-cancel"), "click", () => settlePtyInject(false));
    on(els.hostForm?.authType, "change", toggleAuthFields);
    on(els.hostForm?.keepAlive, "change", toggleKeepAliveFields);
    on(els.identityForm?.kind, "change", toggleIdentFields);
    on(els.tunnelForm?.kind, "change", toggleTunnelDest);
    on(els.hostForm, "submit", (ev) => saveHost(ev).catch((err) => toast(err.message, true)));
    on(els.identityForm, "submit", (ev) => saveIdentity(ev).catch((err) => toast(err.message, true)));
    on(els.tunnelForm, "submit", (ev) => saveTunnel(ev).catch((err) => toast(err.message, true)));
    on(els.quickForm, "submit", (ev) => saveQuick(ev).catch((err) => toast(err.message, true)));
    on(els.snippetForm, "submit", (ev) => saveSnippet(ev).catch((err) => toast(err.message, true)));
    on(els.joinForm, "submit", (ev) => submitJoin(ev));
    on(els.hostKeyForm, "submit", (ev) => submitHostKey(ev).catch((err) => toast(err.message, true)));
  }

  bindChrome();
  bindLists();
  bindModals();
  window.addEventListener("pagehide", persistWorkspace);
  window.addEventListener("beforeunload", persistWorkspace);
  applyThemeClass();
  syncThemeSwitch();
  applyUiPrefs();
  syncSettingsForm();
  if (els.search) els.search.placeholder = "Find a host or ssh user@hostname...";
  const searchKbd = document.getElementById("search-kbd");
  if (searchKbd) searchKbd.textContent = `${MOD}K`;
  document.getElementById("btn-new-tab")?.setAttribute("title", `New tab  ${MOD}T`);
  document.getElementById("btn-new-tab")?.blur();
  toggleAuthFields();
  toggleIdentFields();
  toggleTunnelDest();

  if (!state.name) state.name = "guest";
  boot();
})();
