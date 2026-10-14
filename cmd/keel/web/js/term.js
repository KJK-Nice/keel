import { escapeHtml } from "./lib/html.js";
import * as G from "./lib/groups.js";
import * as V from "./lib/vaultview.js";
import { createPaletteList } from "./ui/palette.js";
import { parseConnect } from "./lib/sshparse.js";
import {
  actionsHTML,
  cardsHTML,
  closedHTML,
  hostPrefill,
  liveClosed,
  normalizeClosed,
  notesHTML,
  offerErrorHTML,
  offerHTML,
  pushClosed,
  recentCards,
  targetText,
} from "./lib/newtab.js";
import {
  computeRects,
  dragSizes,
  insertBeside,
  layoutNotebookIds,
  layoutRuntimeIds,
  leafCount,
  pruneLayout,
  remapLayout,
  removeLeaf,
  replaceLeaf,
  serializeLayout as serializeLayoutWith,
  swapLeaves,
  walkLeaves,
} from "./lib/layout.js";
import { parseJoinLink } from "./lib/joinlink.js";
import { openJoinPeer } from "./lib/joinpeer.js";
import { dispatchSessionMessage, parseSessionMessage, presenceFields, sessionSocketURL, statusClass } from "./lib/session.js";
import { createJoinPrompts } from "./ui/joinprompt.js";
import { createActionToasts } from "./ui/actiontoast.js";
import * as Share from "./lib/share.js";
import { tabCycleTarget, tabIndexTarget } from "./lib/keys.js";
import { classifyShortcut, isAppMod } from "./lib/shortcuts.js";
import { pinScroll } from "./lib/scrollpin.js";
import { runtimeIsStale } from "./lib/runtimecheck.js";
import { showStaleRuntimeBanner } from "./ui/stalebanner.js";
import { createGate } from "./ui/gate.js";

(() => {
  if (window.__keelTermBound) return;
  window.__keelTermBound = true;
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
  // Tab pages (docs/ui-redesign.md §4): each opens as its own tab, one
  // instance each, with the id page:<name>. The page markup is server
  // rendered into #pages and moved into the tab while it is open.
  const PAGES = {
    keychain: { title: "Keychain", glyph: '<path d="M15.5 14.5C18.8137 14.5 21.5 11.8137 21.5 8.5C21.5 5.18629 18.8137 2.5 15.5 2.5C12.1863 2.5 9.5 5.18629 9.5 8.5C9.5 9.38041 9.68962 10.2165 10.0303 10.9697L2.5 18.5V21.5H5.5V19.5H7.5V17.5H9.5L13.0303 13.9697C13.7835 14.3104 14.6196 14.5 15.5 14.5Z"/><path d="M17.5 6.5L16.5 7.5"/>' },
    forwards: { title: "Port forwarding", glyph: '<path d="M20 17L4 17"/><path d="M17 14C17 14 20 16.2 20 17C20 17.8 17 20 17 20"/><path d="M5 7L20 7"/><path d="M7 4C7 4 4 6.2 4 7C4 7.8 7 10 7 10"/>' },
    snippets: { title: "Snippets", glyph: '<path d="M15 4L9 20"/><path d="M5.99997 16C5.99997 16 2.00001 13.054 2 12C1.99999 10.9459 6 8 6 8"/><path d="M18 8C18 8 22 10.946 22 12C22 13.0541 18 16 18 16"/>' },
    notebooks: { title: "Notebooks", glyph: '<path d="M9.99219 11H13.4922M9.99219 7H16.9922"/><path d="M5.99219 6H2.99219M5.99219 12H2.99219M5.99219 18H2.99219"/><path d="M8.99219 21.9388C7.41097 21.8363 6.39738 21.562 5.66376 20.8284C4.49219 19.6569 4.49219 17.7712 4.49219 14V10C4.49219 6.22877 4.49219 4.34315 5.66376 3.17158C6.83533 2 8.72096 2 12.4922 2H12.9922C16.7634 2 18.649 2 19.8206 3.17157C20.9922 4.34315 20.9922 6.22877 20.9922 10V10.5"/>' },
    logs: { title: "Logs", glyph: '<path d="M2 7C2 7.94281 2 8.41421 2.29289 8.70711C2.58579 9 3.05719 9 4 9H6V5C6 3.89543 5.10457 3 4 3C2.89543 3 2 3.89543 2 5V7Z"/><path d="M10 8H15M10 11H13"/><path d="M19 16V7C19 5.11438 19 4.17157 18.4142 3.58579C17.8284 3 16.8856 3 15 3H4"/><path d="M6 9V18C6 19.6569 7.34315 21 9 21H18C19.6569 21 21 19.6569 21 18V16H11"/>' },
    settings: { title: "Settings", glyph: '<path d="M21.3175 7.14139L20.8239 6.28479C20.4506 5.63696 20.264 5.31305 19.9464 5.18388C19.6288 5.05472 19.2696 5.15664 18.5513 5.36048L17.3311 5.70418C16.8725 5.80994 16.3913 5.74994 15.9726 5.53479L15.6357 5.34042C15.2766 5.11043 15.0004 4.77133 14.8475 4.37274L14.5136 3.37536C14.294 2.71534 14.1842 2.38533 13.9228 2.19657C13.6615 2.00781 13.3143 2.00781 12.6199 2.00781H11.5051C10.8108 2.00781 10.4636 2.00781 10.2022 2.19657C9.94085 2.38533 9.83106 2.71534 9.61149 3.37536L9.27753 4.37274C9.12465 4.77133 8.84845 5.11043 8.48937 5.34042L8.15249 5.53479C7.73374 5.74994 7.25259 5.80994 6.79398 5.70418L5.57375 5.36048C4.85541 5.15664 4.49625 5.05472 4.17867 5.18388C3.86109 5.31305 3.67445 5.63696 3.30115 6.28479L2.80757 7.14139C2.45766 7.74864 2.2827 8.05227 2.31666 8.37549C2.35061 8.69871 2.58483 8.95918 3.05326 9.48012L4.0843 10.6328C4.3363 10.9518 4.51521 11.5078 4.51521 12.0077C4.51521 12.5078 4.33636 13.0636 4.08433 13.3827L3.05326 14.5354C2.58483 15.0564 2.35062 15.3168 2.31666 15.6401C2.2827 15.9633 2.45766 16.2669 2.80757 16.8741L3.30114 17.7307C3.67443 18.3785 3.86109 18.7025 4.17867 18.8316C4.49625 18.9608 4.85542 18.8589 5.57377 18.655L6.79394 18.3113C7.25263 18.2055 7.73387 18.2656 8.15267 18.4808L8.4895 18.6752C8.84851 18.9052 9.12464 19.2442 9.2775 19.6428L9.61149 20.6403C9.83106 21.3003 9.94085 21.6303 10.2022 21.8191C10.4636 22.0078 10.8108 22.0078 11.5051 22.0078H12.6199C13.3143 22.0078 13.6615 22.0078 13.9228 21.8191C14.1842 21.6303 14.294 21.3003 14.5136 20.6403L14.8476 19.6428C15.0004 19.2442 15.2765 18.9052 15.6356 18.6752L15.9724 18.4808C16.3912 18.2656 16.8724 18.2055 17.3311 18.3113L18.5513 18.655C19.2696 18.8589 19.6288 18.9608 19.9464 18.8316C20.264 18.7025 20.4506 18.3785 20.8239 17.7307L21.3175 16.8741C21.6674 16.2669 21.8423 15.9633 21.8084 15.6401C21.7744 15.3168 21.5402 15.0564 21.0718 14.5354L20.0407 13.3827C19.7887 13.0636 19.6098 12.5078 19.6098 12.0077C19.6098 11.5078 19.7888 10.9518 20.0407 10.6328L21.0718 9.48012C21.5402 8.95918 21.7744 8.69871 21.8084 8.37549C21.8423 8.05227 21.6674 7.74864 21.3175 7.14139Z"/><path d="M15.5195 12C15.5195 13.933 13.9525 15.5 12.0195 15.5C10.0865 15.5 8.51953 13.933 8.51953 12C8.51953 10.067 10.0865 8.5 12.0195 8.5C13.9525 8.5 15.5195 10.067 15.5195 12Z"/>' },
  };
  const PAGE_PREFIX = "page:";
  const pageTabId = (name) => PAGE_PREFIX + name;
  const SIDEBAR_SEGS = ["hosts", "snippets", "forwards"];
  const SASH_PX = 1; // 1px line gutter between panes; keel.css .split-sash widens the hit area
  // Pane bar tool buttons: ghost icon buttons from the k-* set. icon-btn and the
  // data-pane-* attributes stay as JS hooks.
  const PANE_BTN = "k-btn k-btn--ghost k-btn--sm k-btn--icon icon-btn";
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
    groups: [],
    selectedHostIds: [],
    snippets: [],
    notebooks: [],
    identities: [],
    tunnels: [],
    logs: [],
    herd: [],
    tabs: new Map(),
    panes: new Map(),
    activeId: "",
    // The session tab last shown, so sidebar and page actions that type or
    // split (snippets, notebooks) know their target while a page tab is up.
    lastSessionTabId: "",
    booted: false,
    settingsSection: "appearance",
    query: "",
    sidebarQuery: { snippets: "", forwards: "" },
    selectedIdentity: "",
    selectedSnippet: "",
    tunnelFilter: "all",
    logKind: "all",
    logFilter: "",
    name: params.get("name") || localStorage.getItem("keel-name") || "",
    pendingSession: params.get("session") || params.get("id") || "",
    herdWs: null,
    openModal: null,
    signalURL: "",
    closedTabs: loadClosedTabs(),
    runtimeStopping: false,
    prefs: loadPrefs(),
    ui: loadUiPrefs(),
    hostKeyRetry: null,
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
    const fallback = { size: 12, smoothing: "antialiased", vaultSidebar: true, sidebarSeg: "hosts" };
    try {
      const raw = JSON.parse(localStorage.getItem("keel-ui") || "{}");
      const size = Math.min(16, Math.max(11, Math.round(Number(raw.size) || 12)));
      const smoothing = raw.smoothing === "subpixel" || raw.smoothing === "none" ? raw.smoothing : fallback.smoothing;
      const sidebarSeg = SIDEBAR_SEGS.includes(raw.sidebarSeg) ? raw.sidebarSeg : "hosts";
      return { size, smoothing, vaultSidebar: raw.vaultSidebar !== false, sidebarSeg };
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
    applySidebarSeg();
    applySidebar();
  }

  // Vault sidebar (left, every tab). Open or closed is a window preference;
  // keyboard focus inside it is what makes it chrome (SurfaceChrome), never
  // the fact that it is visible.
  function sidebarOpen() {
    return state.ui.vaultSidebar !== false;
  }

  function sidebarFocused() {
    const side = document.getElementById("vault-sidebar");
    const ae = document.activeElement;
    return !!(side && ae && side.contains(ae));
  }

  function releaseSidebarFocus() {
    if (!sidebarFocused()) return;
    document.activeElement?.blur?.();
    document.getElementById("vault-sidebar")?.blur?.();
  }

  function restoreTermKeyboard() {
    releaseSidebarFocus();
    const pane = focusedPane();
    if (pane && !isNotebookPane(pane) && !termInputOpen(pane)) pane.term?.focus?.();
  }

  function applySidebar() {
    const open = sidebarOpen();
    document.documentElement.dataset.sidebar = open ? "open" : "closed";
    const btn = document.getElementById("btn-sidebar");
    if (btn) {
      btn.setAttribute("aria-pressed", String(open));
      btn.setAttribute("aria-expanded", String(open));
    }
  }

  function setSidebar(open) {
    const next = !!open;
    if (next === sidebarOpen()) {
      applySidebar();
      return;
    }
    state.ui.vaultSidebar = next;
    saveUiPrefs();
    applySidebar();
    restoreTermKeyboard();
    syncSessionSurface();
    fitVisiblePanes();
  }

  function toggleSidebar() {
    setSidebar(!sidebarOpen());
  }

  function sidebarSeg() {
    return SIDEBAR_SEGS.includes(state.ui.sidebarSeg) ? state.ui.sidebarSeg : "hosts";
  }

  const SIDEBAR_FILTER = {
    hosts: { placeholder: "Filter hosts", label: "Search hosts" },
    snippets: { placeholder: "Filter snippets", label: "Filter snippets" },
    forwards: { placeholder: "Filter forwards", label: "Filter forwards" },
  };

  function setSidebarSeg(next) {
    const seg = SIDEBAR_SEGS.includes(next) ? next : "hosts";
    if (state.ui.sidebarSeg !== seg) {
      state.ui.sidebarSeg = seg;
      saveUiPrefs();
    }
    applySidebarSeg();
  }

  function applySidebarSeg() {
    const current = sidebarSeg();
    document.querySelectorAll("[data-sidebar-seg]").forEach((btn) => {
      btn.setAttribute("aria-pressed", String(btn.getAttribute("data-sidebar-seg") === current));
    });
    document.querySelectorAll("[data-sidebar-panel]").forEach((panel) => {
      panel.hidden = panel.getAttribute("data-sidebar-panel") !== current;
    });
    const input = els.search;
    if (input) {
      const f = SIDEBAR_FILTER[current];
      input.placeholder = f.placeholder;
      input.setAttribute("aria-label", f.label);
      const value = current === "hosts" ? state.query : state.sidebarQuery[current] || "";
      if (input.value !== value) input.value = value;
    }
  }

  // revealVault opens the sidebar on Hosts and puts the caret in its filter
  // (what ⌘1 / "Go to Vault" did while the vault was a tab).
  function revealVault() {
    if (state.openModal) closeModal();
    setSidebar(true);
    setSidebarSeg("hosts");
    els.search?.focus({ preventScroll: true });
    els.search?.select?.();
    syncSessionSurface();
  }

  const SIDE_ICON_TERMINAL = V.glyphSVG("local");
  const SIDE_ICONS = {
    snippet: hugeicon('<path d="M15 4L9 20"/><path d="M5.99997 16C5.99997 16 2.00001 13.054 2 12C1.99999 10.9459 6 8 6 8"/><path d="M18 8C18 8 22 10.946 22 12C22 13.0541 18 16 18 16"/>'),
    notebook: hugeicon(PAGES.notebooks.glyph),
    pin: hugeicon('<path d="M3 21L8 16"/><path d="M13.2585 18.8714C9.51516 18.0215 5.97844 14.4848 5.12853 10.7415C4.99399 10.1489 4.92672 9.85266 5.12161 9.37197C5.3165 8.89129 5.55457 8.74255 6.03071 8.44509C7.10705 7.77265 8.27254 7.55888 9.48209 7.66586C11.1793 7.81598 12.0279 7.89104 12.4512 7.67048C12.8746 7.44991 13.1622 6.93417 13.7376 5.90269L14.4664 4.59604C14.9465 3.73528 15.1866 3.3049 15.7513 3.10202C16.316 2.89913 16.6558 3.02199 17.3355 3.26771C18.9249 3.84236 20.1576 5.07505 20.7323 6.66449C20.978 7.34417 21.1009 7.68401 20.898 8.2487C20.6951 8.8134 20.2647 9.05346 19.4039 9.53358L18.0672 10.2792C17.0376 10.8534 16.5229 11.1406 16.3024 11.568C16.0819 11.9955 16.162 12.8256 16.3221 14.4859C16.4399 15.7068 16.2369 16.88 15.5555 17.9697C15.2577 18.4458 15.1088 18.6839 14.6283 18.8786C14.1477 19.0733 13.8513 19.006 13.2585 18.8714Z"/>'),
  };

  function sideMatches(text, q) {
    const needle = String(q || "").trim().toLowerCase();
    return !needle || String(text || "").toLowerCase().includes(needle);
  }

  // paintSidebar fills the Snippets (snippets, then notebooks) and Forwards
  // panels. Running a snippet goes through paletteRunSnippet, which asks
  // Approve before type; nothing here writes a PTY.
  function paintSidebar() {
    applySidebarSeg();
    const snips = document.getElementById("sidebar-snippets");
    if (snips) {
      const q = state.sidebarQuery.snippets;
      const snRows = (state.snippets || [])
        .filter((sn) => sideMatches(`${sn.name} ${sn.command}`, q))
        .map(
          (sn) => `<button type="button" class="k-side-item" data-snippet-run="${escapeHtml(sn.id)}" title="Type into the focused pane after you approve">
            <span class="k-tile k-tile--sm">${SIDE_ICONS.snippet}</span>
            <span class="k-side-main"><span class="k-side-name">${escapeHtml(sn.name || "Snippet")}</span><span class="k-side-sub">${escapeHtml(String(sn.command || "").trim().split("\n")[0])}</span></span>
          </button>`
        )
        .join("");
      const nbRows = V.sortedNotebooks(state.notebooks)
        .filter((nb) => sideMatches(nb.name, q))
        .map((nb) => {
          const pinLabel = nb.pinned ? "Unpin" : "Pin";
          return `<div class="k-side-row">
            <button type="button" class="k-side-item" data-notebook-open="${escapeHtml(nb.id)}">
              <span class="k-tile k-tile--sm">${SIDE_ICONS.notebook}</span>
              <span class="k-side-main"><span class="k-side-name">${escapeHtml(nb.name || "Notebook")}</span></span>
            </button>
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon k-side-pin" data-notebook-pin="${escapeHtml(nb.id)}" aria-label="${pinLabel} ${escapeHtml(nb.name || "Notebook")}" aria-pressed="${nb.pinned ? "true" : "false"}" title="${pinLabel}">${SIDE_ICONS.pin}</button>
          </div>`;
        })
        .join("");
      snips.innerHTML = `<div class="k-side-section">
          ${snRows || `<p class="k-side-note">${q ? "No snippets match." : "No snippets yet."}</p>`}
          <button type="button" class="k-btn k-btn--ghost k-btn--sm k-side-link" data-open-page="snippets">Manage snippets</button>
        </div>
        <div class="k-side-label">Notebooks</div>
        <div class="k-side-section">
          ${nbRows || `<p class="k-side-note">${q ? "No notebooks match." : "No notebooks yet."}</p>`}
          <div class="k-side-actions">
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-side-link" data-notebook-new>New notebook</button>
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-side-link" data-open-page="notebooks">All notebooks</button>
          </div>
        </div>`;
    }
    const fwd = document.getElementById("sidebar-forwards");
    if (fwd) {
      const q = state.sidebarQuery.forwards;
      const hostName = (id) => state.hosts.find((h) => h.id === id)?.name || "";
      const rows = (state.tunnels || [])
        .filter((t) => sideMatches(`${t.name} ${hostName(t.hostId)} ${t.destHost || ""}`, q))
        .map((t) => {
          const st = V.tunnelState(t);
          return `<div class="k-side-row">
            <button type="button" class="k-side-item" data-open-page="forwards" title="${escapeHtml(st.label)}">
              <span class="k-dot ${st.dot}" aria-hidden="true"></span>
              <span class="k-side-main"><span class="k-side-name">${escapeHtml(t.name)}</span><span class="k-side-sub">${escapeHtml(V.tunnelRoute(t))}</span></span>
            </button>
            <button type="button" class="k-switch" role="switch" aria-checked="${st.on ? "true" : "false"}" aria-label="${escapeHtml(t.name)} on" ${st.on ? `data-tunnel-stop="${escapeHtml(t.id)}"` : `data-tunnel-start="${escapeHtml(t.id)}"`}></button>
          </div>`;
        })
        .join("");
      fwd.innerHTML = `<div class="k-side-section">
          ${rows || `<p class="k-side-note">${q ? "No forwards match." : "No forwards yet."}</p>`}
          <div class="k-side-actions">
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-side-link" data-new-tunnel>New forward</button>
            <button type="button" class="k-btn k-btn--ghost k-btn--sm k-side-link" data-open-page="forwards">Manage forwards</button>
          </div>
        </div>`;
    }
    syncHostLive();
  }

  // syncHostLive puts each host's running session count (and its worst
  // state) beside it in the sidebar tree.
  function syncHostLive() {
    const byHost = new Map();
    for (const p of state.panes.values()) {
      if (!isTermPane(p) || !p.runtimeId || !p.hostId || p.remote) continue;
      const cur = byHost.get(p.hostId) || { n: 0, status: "is-ready" };
      cur.n += 1;
      if (p.status === "is-error" || (p.status === "is-connecting" && cur.status !== "is-error")) cur.status = p.status;
      byHost.set(p.hostId, cur);
    }
    document.querySelectorAll("[data-host-live]").forEach((el) => {
      const live = byHost.get(el.getAttribute("data-host-live"));
      el.hidden = !live;
      if (!live) {
        el.textContent = "";
        return;
      }
      const dot = dotFor(live.status);
      el.innerHTML = `<span class="k-dot ${dot.cls}" aria-hidden="true"></span>${live.n}`;
      el.title = `${live.n} ${live.n === 1 ? "session" : "sessions"} · ${dot.label}`;
    });
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

  // The user's choice (system, light, dark) is data-theme-choice; data-theme
  // is always the resolved dark or light, the only two themes CSS knows.
  function themeChoice() {
    return document.documentElement.dataset.themeChoice || "system";
  }

  function resolvedTheme() {
    const choice = themeChoice();
    if (choice === "light" || choice === "dark") return choice;
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }

  function isNativeWindow() {
    return document.documentElement.dataset.native === "1";
  }

  // The terminal palette is the Keel token set (docs/design/tokens.json):
  // --term-bg, --term-fg, --term-cursor, --term-selection and --ansi-0..15,
  // read from the page at apply time so light/dark and any token edit reach
  // xterm without a second copy of the colours here.
  const ANSI_NAMES = [
    "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
    "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite",
  ];
  // Native windows let the OS vibrancy show through a solo pane, matching
  // the 66% tint keel.css gives the chrome behind it.
  const NATIVE_TERM_ALPHA = 0.66;
  let termThemeCache = { key: "", theme: null };

  // Resolve any CSS colour (var(), hex, color-mix) to #rrggbb[aa] by letting
  // the browser compute it on a probe element; xterm takes hex or rgb().
  function resolveCssColor(probe, value) {
    probe.style.color = "";
    probe.style.color = value;
    const m = getComputedStyle(probe).color.match(/rgba?\(([^)]+)\)/);
    if (!m) return "";
    const parts = m[1].split(/[\s,/]+/).filter(Boolean).map(Number);
    const hex = (n) => Math.max(0, Math.min(255, Math.round(n))).toString(16).padStart(2, "0");
    let out = `#${hex(parts[0])}${hex(parts[1])}${hex(parts[2])}`;
    if (parts.length > 3 && parts[3] < 1) out += hex(parts[3] * 255);
    return out;
  }

  function tokenColor(name) {
    const probe = document.createElement("span");
    probe.hidden = true;
    document.body.appendChild(probe);
    try {
      return resolveCssColor(probe, `var(${name})`).slice(0, 7);
    } finally {
      probe.remove();
    }
  }

  function readTermTheme() {
    const probe = document.createElement("span");
    probe.hidden = true;
    document.body.appendChild(probe);
    try {
      const get = (name) => resolveCssColor(probe, `var(${name})`);
      const theme = {
        background: get("--term-bg"),
        foreground: get("--term-fg"),
        cursor: get("--term-cursor"),
        cursorAccent: get("--term-bg"),
        selectionBackground: get("--term-selection"),
      };
      ANSI_NAMES.forEach((name, i) => {
        theme[name] = get(`--ansi-${i}`);
      });
      return theme;
    } finally {
      probe.remove();
    }
  }

  function tokenTermTheme() {
    const key = `${document.documentElement.dataset.theme || ""}`;
    if (!termThemeCache.theme || termThemeCache.key !== key) {
      termThemeCache = { key, theme: readTermTheme() };
    }
    return { ...termThemeCache.theme };
  }

  function xtermTheme() {
    const theme = tokenTermTheme();
    if (isNativeWindow()) {
      theme.background = `${theme.background.slice(0, 7)}${Math.round(NATIVE_TERM_ALPHA * 255).toString(16).padStart(2, "0")}`;
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

  // xterm measures its cell size when a terminal opens and when the font
  // options change. JetBrains Mono / IBM Plex Mono load asynchronously
  // (font-display: swap), so a terminal opened before they arrive keeps
  // the fallback font's cell size and its grid no longer fits the pane
  // until a window resize happens to re-measure. Re-measure once fonts load.
  function remeasureTerminalFonts() {
    for (const p of state.panes.values()) {
      if (!p.term) continue;
      const family = p.term.options.fontFamily;
      p.term.options.fontFamily = `${family} `;
      p.term.options.fontFamily = family;
    }
    fitVisiblePanes();
  }

  function syncThemeSwitch() {
    setSelectValue("set-theme", themeChoice());
  }

  function applyThemeClass() {
    termThemeCache = { key: "", theme: null };
    if (typeof window.__keelApplyTheme === "function") {
      window.__keelApplyTheme(themeChoice());
      return;
    }
    const dark = resolvedTheme() === "dark";
    document.documentElement.dataset.theme = dark ? "dark" : "light";
    document.documentElement.classList.toggle("dark", dark);
    document.documentElement.style.colorScheme = dark ? "dark" : "light";
    document.body?.classList.remove("dark");
  }

  function setTheme(choice) {
    if (choice !== "light" && choice !== "dark" && choice !== "system") choice = "system";
    document.documentElement.dataset.themeChoice = choice;
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
    notebookList: document.getElementById("notebook-list"),
    identityList: document.getElementById("identity-list"),
    tunnelList: document.getElementById("tunnel-list"),
    logList: document.getElementById("log-list"),
    tabs: document.getElementById("tabs"),
    terminals: document.getElementById("terminals"),
    sidebar: document.getElementById("vault-sidebar"),
    pages: document.getElementById("pages"),
    barStatus: document.getElementById("bar-status"),
    controlWidget: document.getElementById("control-widget"),
    btnShare: document.getElementById("btn-share"),
    sharePop: document.getElementById("share-pop"),
    search: document.getElementById("search"),
    modalScrim: document.getElementById("modal-scrim"),
    hostModal: document.getElementById("host-modal"),
    hostForm: document.getElementById("host-form"),
    groupModal: document.getElementById("group-modal"),
    groupForm: document.getElementById("group-form"),
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
    notebookRenameModal: document.getElementById("notebook-rename-modal"),
    notebookRenameForm: document.getElementById("notebook-rename-form"),
    hostKeyModal: document.getElementById("hostkey-modal"),
    hostKeyForm: document.getElementById("hostkey-form"),
    gate: document.getElementById("gate"),
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
      afterListPaint(id);
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

  function fieldValue(form, name, triggerId) {
    const el = form?.elements?.[name];
    if (el && typeof el.value === "string") return el.value;
    const trigger = document.getElementById(triggerId);
    const input = trigger?.previousElementSibling;
    if (input?.hasAttribute?.("data-tui-select-input") && typeof input.value === "string") return input.value;
    const contentId = trigger?.getAttribute("aria-controls");
    const selected = contentId && document.getElementById(contentId)?.querySelector("[data-selected]");
    return selected?.getAttribute("data-tui-select-value") || "";
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
      err.data = data;
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

  // Group and host helpers over the current vault (lib/groups.js).
  const hostSearchHay = (h) => G.hostSearchHay(state.groups, h);
  const groupById = (id) => G.groupById(state.groups, id);
  const groupDescendantDepth = (id) => G.groupDescendantDepth(state.groups, id);
  const groupDepth = (id) => G.groupDepth(state.groups, id);
  const isGroupUnder = (id, ancestorId) => G.isGroupUnder(state.groups, id, ancestorId);
  const nestGroupTreeJS = (query, parentId) => G.nestGroupTree(state.groups, state.hosts, query, parentId);
  const groupOptionEntries = (excludeId) => G.groupOptionEntries(state.groups, excludeId);
  const { parseHostTags, hostSubtitle } = G;

  const MAX_GROUP_DEPTH = 8;
  const GROUP_COLLAPSED_KEY = "keel-group-collapsed";

  function loadCollapsedGroups() {
    try {
      const ids = JSON.parse(localStorage.getItem(GROUP_COLLAPSED_KEY) || "[]");
      return Array.isArray(ids) ? ids.filter((id) => typeof id === "string") : [];
    } catch {
      return [];
    }
  }

  function saveCollapsedGroups(ids) {
    localStorage.setItem(GROUP_COLLAPSED_KEY, JSON.stringify(ids));
  }

  function syncGroupTree() {
    const collapsed = new Set(loadCollapsedGroups());
    document.querySelectorAll(".group-tree-node[data-group-id]").forEach((node) => {
      const id = node.getAttribute("data-group-id");
      const on = collapsed.has(id);
      node.classList.toggle("is-collapsed", on);
      const btn = node.querySelector(":scope > .group-tree-heading [data-group-toggle]");
      if (btn) btn.setAttribute("aria-expanded", on ? "false" : "true");
    });
  }

  function toggleGroupNode(id) {
    if (!id) return;
    const ids = new Set(loadCollapsedGroups());
    if (ids.has(id)) ids.delete(id);
    else ids.add(id);
    saveCollapsedGroups([...ids]);
    syncGroupTree();
  }

  function fillParentGroupSelect(triggerId, selected, excludeId, emptyLabel) {
    const entries = [{ value: "", label: emptyLabel || "No parent" }, ...groupOptionEntries(excludeId)];
    fillSelectItems(triggerId, entries, selected || "", emptyLabel || "No parent");
  }

  function selectedHostSet() {
    return new Set(state.selectedHostIds || []);
  }

  function setHostSelection(ids) {
    state.selectedHostIds = [...new Set(ids.filter(Boolean))];
    syncHostSelection();
  }

  function toggleHostSelect(id, additive) {
    if (!id) return;
    const next = selectedHostSet();
    if (!additive) {
      setHostSelection(next.has(id) && next.size === 1 ? [] : [id]);
      return;
    }
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setHostSelection([...next]);
  }

  function syncHostSelection() {
    const selected = selectedHostSet();
    document.querySelectorAll("[data-drag-host]").forEach((el) => {
      el.classList.toggle("is-selected", selected.has(el.getAttribute("data-drag-host")));
    });
  }

  const WORKSPACE_KEY = "keel-workspace";
  let restoringWorkspace = false;
  let workspaceReady = false;

  function paneRuntimeId(paneId) {
    return state.panes.get(paneId)?.runtimeId || "";
  }

  // savedLeaf is how a pane is stored in the saved workspace layout.
  function savedLeaf(paneId) {
    const pane = state.panes.get(paneId);
    if (isNotebookPane(pane) && pane.notebookId) {
      return { type: "leaf", kind: "notebook", notebookId: pane.notebookId };
    }
    const runtimeId = pane?.runtimeId || paneRuntimeId(paneId);
    return runtimeId ? { type: "leaf", runtimeId } : null;
  }

  function persistWorkspace() {
    if (restoringWorkspace || !workspaceReady) return;
    const tabs = [];
    const pages = [];
    for (const tab of state.tabs.values()) {
      if (tab.page) {
        pages.push(tab.page);
        continue;
      }
      if (tab.start || tab.remote || !tab.layout) continue;
      const layout = serializeLayoutWith(tab.layout, savedLeaf);
      if (!layout) continue;
      const focused = state.panes.get(tab.focusedPaneId);
      const zoomed = state.panes.get(tab.zoomedPaneId);
      tabs.push({
        title: tab.customTitle || "",
        sync: !!tab.sync,
        focusedRuntimeId: focused?.runtimeId || "",
        zoomedRuntimeId: zoomed?.runtimeId || "",
        focusedNotebookId: isNotebookPane(focused) ? focused.notebookId || "" : "",
        zoomedNotebookId: isNotebookPane(zoomed) ? zoomed.notebookId || "" : "",
        layout,
      });
    }
    // active is a runtime id, "nb:<notebook>", "page:<name>", or "" (a
    // New tab or nothing).
    const activeTab = state.tabs.get(state.activeId);
    const activePane = activeTab && !activeTab.page ? focusedPane() : null;
    const activeTerm = activePane && (activePane.runtimeId ? activePane : termTarget(activePane));
    let active = "";
    if (activeTab?.page) active = activeTab.id;
    else if (activeTerm?.runtimeId) active = activeTerm.runtimeId;
    else if (isNotebookPane(activePane) && activePane.notebookId) active = "nb:" + activePane.notebookId;
    const payload = {
      v: 1,
      active,
      tabs,
      pages,
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
    const notebooks = new Set((state.notebooks || []).map((n) => n.id).filter(Boolean));
    const layout = pruneLayout(saved?.layout, live, notebooks);
    const runtimeIds = layoutRuntimeIds(layout);
    const notebookIds = layoutNotebookIds(layout);
    if (!runtimeIds.length && !notebookIds.length) return;
    const runtimeToPane = new Map();
    const notebookToPane = new Map();
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
    if (!tab && notebookIds.length) {
      const rec = notebookById(notebookIds[0]);
      if (!rec) return;
      const created = createNotebookTab(rec, { activate: false });
      tab = created.tab;
      notebookToPane.set(rec.id, created.pane.id);
    }
    if (!tab) return;
    walkLeaves(layout, (leaf) => {
      if (leaf.kind !== "notebook" || !leaf.notebookId || notebookToPane.has(leaf.notebookId)) return;
      const rec = notebookById(leaf.notebookId);
      if (!rec) return;
      const existing = [...state.panes.values()].find((p) => isNotebookPane(p) && p.notebookId === rec.id);
      if (existing) {
        if (existing.tabId === tab.id) notebookToPane.set(rec.id, existing.id);
        return;
      }
      const sibling = tabPanes(tab).find((p) => isTermPane(p)) || tabPanes(tab)[0];
      const nb = createNotebookPane(tab, sibling, rec);
      notebookToPane.set(rec.id, nb.id);
    });
    const remapped = remapLayout(layout, runtimeToPane, notebookToPane);
    if (remapped) {
      tab.layout = remapped;
      tab.dirty = true;
    }
    if (saved.title) tab.customTitle = saved.title;
    if (saved.sync) tab.sync = true;
    const focusedNb = notebookToPane.get(saved.focusedNotebookId);
    if (focusedNb) tab.focusedPaneId = focusedNb;
    else {
      const focusedId = runtimeToPane.get(saved.focusedRuntimeId);
      if (focusedId) tab.focusedPaneId = focusedId;
    }
    const zoomedNb = notebookToPane.get(saved.zoomedNotebookId);
    if (zoomedNb && leafCount(tab.layout) > 1) tab.zoomedPaneId = zoomedNb;
    else {
      const zoomedId = runtimeToPane.get(saved.zoomedRuntimeId);
      if (zoomedId && leafCount(tab.layout) > 1) tab.zoomedPaneId = zoomedId;
    }
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
      // Tab pages come back after the sessions, in the order they had.
      for (const name of Array.isArray(saved?.pages) ? saved.pages : []) {
        if (PAGES[name]) openPage(name, { activate: false, render: false });
      }
      if (typeof saved?.active === "string" && saved.active.startsWith(PAGE_PREFIX)) {
        if (state.tabs.has(saved.active)) state.activeId = saved.active;
      } else if (typeof saved?.active === "string" && saved.active.startsWith("nb:")) {
        const nbId = saved.active.slice(3);
        const pane = [...state.panes.values()].find((p) => isNotebookPane(p) && p.notebookId === nbId);
        if (pane) {
          state.activeId = pane.tabId;
          const tab = state.tabs.get(pane.tabId);
          if (tab) tab.focusedPaneId = pane.id;
        }
      } else if (saved?.active) {
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

  // Hosts outside any (existing) group sit at the root of the sidebar tree.
  function looseHosts(query) {
    const ids = new Set((state.groups || []).map((g) => g.id));
    return state.hosts.filter((h) => (!h.groupId || !ids.has(h.groupId)) && hostMatches(h, query));
  }

  function renderHosts() {
    els.hostList.innerHTML = V.hostsHTML({ tree: nestGroupTreeJS(state.query, ""), loose: looseHosts(state.query) });
    afterListPaint("host-list");
  }

  function renderSnippets() {
    els.snippetList.innerHTML = V.snippetsHTML(state.snippets);
    afterListPaint("snippet-list");
  }

  function renderNotebooks() {
    if (els.notebookList) els.notebookList.innerHTML = V.notebooksHTML(state.notebooks);
    afterListPaint("notebook-list");
  }

  async function paintVaultNotebooks() {
    if (await paintList("notebook-list", "/ui/notebooks?" + datastarQS())) return;
    renderNotebooks();
  }

  function renderIdentities() {
    els.identityList.innerHTML = V.identitiesHTML(state.identities, state.hosts);
    afterListPaint("identity-list");
  }

  function renderTunnels() {
    els.tunnelList.innerHTML = V.tunnelsHTML(state.tunnels, state.hosts);
    afterListPaint("tunnel-list");
  }

  function renderLogs() {
    els.logList.innerHTML = V.logsHTML(state.logs, state.logFilter, { hosts: state.hosts, tunnels: state.tunnels });
    afterListPaint("log-list");
  }

  // afterListPaint re-applies what lives in the page, not in the list
  // markup: tree collapse, selection, filters and the derived detail.
  function afterListPaint(id) {
    if (id === "host-list") {
      els.hostList?.toggleAttribute("data-filtering", !!String(state.query || "").trim());
      syncGroupTree();
      syncHostSelection();
      syncHostLive();
    } else if (id === "identity-list") {
      syncIdentityPage();
    } else if (id === "snippet-list") {
      syncSnippetPage();
    } else if (id === "tunnel-list") {
      syncTunnelPage();
      renderStatusBar();
    } else if (id === "log-list") {
      syncLogPage();
    } else if (id === "notebook-list") {
      applyListFilter("notebook");
    }
  }

  const listFilters = { identity: "", snippet: "", tunnel: "", notebook: "" };
  const LIST_ROWS = {
    identity: "#identity-list [data-identity-select]",
    snippet: "#snippet-list [data-snippet-select]",
    tunnel: "#tunnel-list tr[data-id]",
    notebook: "#notebook-list [data-filter-text]",
  };

  // applyListFilter hides a page's rows that do not match its filter box
  // (and, for forwards, the Running / Stopped chip).
  function applyListFilter(scope) {
    const q = String(listFilters[scope] || "").trim().toLowerCase();
    document.querySelectorAll(LIST_ROWS[scope]).forEach((row) => {
      let show = !q || String(row.getAttribute("data-filter-text") || "").toLowerCase().includes(q);
      if (scope === "tunnel" && state.tunnelFilter !== "all") show = show && row.getAttribute("data-status") === state.tunnelFilter;
      row.hidden = !show;
    });
  }

  function syncIdentityPage() {
    const ids = state.identities || [];
    if (!ids.some((x) => x.id === state.selectedIdentity)) state.selectedIdentity = ids[0]?.id || "";
    document.querySelectorAll("#identity-list [data-identity-select]").forEach((el) => {
      el.setAttribute("aria-selected", String(el.getAttribute("data-identity-select") === state.selectedIdentity));
    });
    applyListFilter("identity");
    const detail = document.getElementById("identity-detail");
    if (detail) {
      const id = ids.find((x) => x.id === state.selectedIdentity);
      detail.innerHTML = V.identityDetailHTML(id, state.hosts);
    }
  }

  function selectIdentity(id) {
    state.selectedIdentity = id || "";
    syncIdentityPage();
  }

  function syncSnippetPage() {
    const list = state.snippets || [];
    if (!list.some((s) => s.id === state.selectedSnippet)) state.selectedSnippet = list[0]?.id || "";
    document.querySelectorAll("#snippet-list [data-snippet-select]").forEach((el) => {
      el.setAttribute("aria-selected", String(el.getAttribute("data-snippet-select") === state.selectedSnippet));
    });
    applyListFilter("snippet");
    const form = document.getElementById("snippet-editor");
    const empty = document.getElementById("snippet-editor-empty");
    const sn = list.find((s) => s.id === state.selectedSnippet);
    if (!form) return;
    form.hidden = !sn;
    if (empty) empty.hidden = !!sn;
    if (!sn) return;
    if (form.dataset.snippetId !== sn.id || !form.dataset.dirty) {
      form.dataset.snippetId = sn.id;
      delete form.dataset.dirty;
      form.elements.id.value = sn.id;
      form.elements.name.value = sn.name || "";
      form.elements.command.value = String(sn.command || "").replace(/\n$/, "");
    }
    document.getElementById("snippet-editor-run")?.setAttribute("data-run", sn.id);
    document.getElementById("snippet-editor-delete")?.setAttribute("data-delete-snippet", sn.id);
    syncSnippetPreview();
  }

  function selectSnippet(id) {
    const form = document.getElementById("snippet-editor");
    if (form) delete form.dataset.dirty;
    state.selectedSnippet = id || "";
    syncSnippetPage();
  }

  // The preview is exactly what Type into pane sends (after Approve): each
  // line with its Enter.
  function syncSnippetPreview() {
    const form = document.getElementById("snippet-editor");
    const pre = document.getElementById("snippet-editor-preview");
    const count = document.getElementById("snippet-editor-count");
    if (!form || !pre) return;
    const view = V.snippetPreview(form.elements.command.value);
    pre.innerHTML = view.html;
    if (count) count.textContent = view.summary;
  }

  async function saveSnippetEditor(ev) {
    ev?.preventDefault?.();
    const form = document.getElementById("snippet-editor");
    const id = form?.elements.id.value;
    if (!form || !id) return;
    const name = form.elements.name.value.trim();
    if (!name) {
      toast("Give the snippet a name", true);
      form.elements.name.focus();
      return;
    }
    let command = form.elements.command.value;
    if (!command.endsWith("\n")) command += "\n";
    await api(`/api/snippets/${id}`, { method: "PUT", body: JSON.stringify({ id, name, command }) });
    delete form.dataset.dirty;
    await loadAll();
    toast("Snippet saved");
  }

  function syncTunnelPage() {
    const counts = { all: 0, running: 0, stopped: 0 };
    for (const t of state.tunnels || []) {
      counts.all += 1;
      counts[V.tunnelState(t).filter] += 1;
    }
    document.querySelectorAll("[data-tunnel-count]").forEach((el) => {
      el.textContent = String(counts[el.getAttribute("data-tunnel-count")] ?? "");
    });
    document.querySelectorAll("[data-tunnel-filter]").forEach((el) => {
      el.setAttribute("aria-pressed", String(el.getAttribute("data-tunnel-filter") === state.tunnelFilter));
    });
    applyListFilter("tunnel");
  }

  function syncLogPage() {
    document.querySelectorAll("[data-log-filter]").forEach((el) => {
      el.setAttribute("aria-pressed", String(el.getAttribute("data-log-filter") === state.logKind));
    });
    document.querySelectorAll("#log-list tr[data-log-kind]").forEach((row) => {
      row.hidden = state.logKind !== "all" && row.getAttribute("data-log-kind") !== state.logKind;
    });
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

  function paneKind(pane) {
    return pane?.kind || "term";
  }

  function isNotebookPane(pane) {
    return paneKind(pane) === "notebook";
  }

  function isTermPane(pane) {
    return paneKind(pane) === "term";
  }

  function termTarget(pane) {
    if (isTermPane(pane) && pane.runtimeId) return pane;
    const tab = pane && state.tabs.get(pane.tabId);
    if (!tab) return null;
    return tabPanes(tab).find((p) => isTermPane(p) && p.runtimeId) || null;
  }

  function tabPanes(tab) {
    const out = [];
    walkLeaves(tab.layout, ({ paneId }) => {
      const p = state.panes.get(paneId);
      if (p) out.push(p);
    });
    return out;
  }

  function visiblePanes(tab) {
    if (!tab) return [];
    if (tab.zoomedPaneId && state.panes.has(tab.zoomedPaneId)) return [state.panes.get(tab.zoomedPaneId)];
    return tabPanes(tab);
  }

  function rehomeLayout(node, tabId) {
    walkLeaves(node, ({ paneId }) => {
      const p = state.panes.get(paneId);
      if (p) p.tabId = tabId;
    });
  }

  function findPaneByRuntime(runtimeId) {
    return [...state.panes.values()].find((p) => p.runtimeId === runtimeId);
  }

  function tabStatus(tab) {
    const panes = tabPanes(tab).filter(isTermPane);
    if (panes.some((p) => p.status === "is-error")) return "is-error";
    if (panes.some((p) => p.status === "is-connecting")) return "is-connecting";
    if (panes.length && panes.every((p) => p.status === "is-ready")) return "is-ready";
    return "";
  }

  function tabTitle(tab) {
    if (tab.page) return PAGES[tab.page]?.title || tab.page;
    if (tab.customTitle) return tab.customTitle;
    if (tab.start) return tab.title || "New Tab";
    const panes = tabPanes(tab);
    const first = panes.find(isTermPane) || panes[0];
    const name = first?.title || tab.title || "Session";
    const n = panes.length;
    const follow = first && !isDriving(first) && first.you ? " · follow" : "";
    const extra = n > 1 ? ` · ${n}` : "";
    return `${name}${extra}${follow}`;
  }

  // Tab label without the pane count (the count renders as a badge).
  function tabName(tab) {
    if (tab.page) return PAGES[tab.page]?.title || tab.page;
    if (tab.customTitle) return tab.customTitle;
    if (tab.start) return tab.title || "New Tab";
    const panes = tabPanes(tab);
    const first = panes.find(isTermPane) || panes[0];
    const name = first?.title || tab.title || "Session";
    // A tab that joined someone else's share (§5.7) says so, whoever drives.
    if (tab.remote) return `${name} · joined`;
    const follow = first && !isDriving(first) && first.you ? " · follow" : "";
    return `${name}${follow}`;
  }

  // State dot (docs/design: live, connecting, dead, idle) for a pane status.
  const DOT_BY_STATUS = {
    "is-ready": { cls: "k-dot--live", label: "Connected" },
    "is-connecting": { cls: "k-dot--connecting", label: "Connecting" },
    "is-error": { cls: "k-dot--dead", label: "Disconnected" },
  };
  function dotFor(status) {
    return DOT_BY_STATUS[status] || { cls: "k-dot--idle", label: "Idle" };
  }

  // chromeTabs is the tab strip in order: sessions, New tabs and tab
  // pages. ⌘1…8 and ⌘9 index into it (the Vault is no longer a tab).
  function chromeTabs() {
    return [...state.tabs.keys()];
  }

  // A session tab holds panes (terminals or notebooks); New tabs and tab
  // pages do not.
  function isSessionTab(tab) {
    return !!tab && !tab.start && !tab.page && !!tab.layout;
  }

  function activeSessionTab() {
    const tab = state.tabs.get(state.activeId);
    return isSessionTab(tab) ? tab : null;
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
    if (!fromId || !state.tabs.has(fromId)) return false;
    const ids = [...state.tabs.keys()];
    const next = reorderTabIds(ids, fromId, toIndex);
    if (next.length !== ids.length || next.every((id, i) => id === ids[i])) return false;
    const tabs = new Map();
    for (const id of next) tabs.set(id, state.tabs.get(id));
    state.tabs = tabs;
    return true;
  }

  function tabStripInsertIndex(clientX) {
    const buttons = [...els.tabs.querySelectorAll(".tab[data-tab]")];
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
    const buttons = [...els.tabs.querySelectorAll(".tab[data-tab]")];
    if (!buttons.length) return;
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
    // Tab pages keep their names: no Rename.
    const fixed = !!state.tabs.get(id)?.page;
    const ids = [...state.tabs.keys()];
    const idx = ids.indexOf(id);
    const others = ids.filter((tid) => tid !== id).length;
    const right = idx < 0 ? 0 : ids.length - idx - 1;
    setTabMenuItem('[data-tab-action="rename"]', { hidden: fixed, disabled: fixed });
    setTabMenuItem('[data-tab-menu-sep="rename"]', { hidden: fixed });
    setTabMenuItem('[data-tab-action="close"]', { disabled: !state.tabs.has(id) });
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
      const nb = tabPanes(tab).find(isNotebookPane);
      if (nb && next) {
        nb.title = next;
        tab.title = next;
        persistNotebook(nb);
        paintNotebook(nb);
      }
    }
  }

  function fitTabRename(input) {
    input.style.width = "0px";
    input.style.width = `${Math.max(1, Math.ceil(input.scrollWidth))}px`;
  }

  function beginTabRename(id) {
    if (!state.tabs.has(id) || state.tabs.get(id).page || tabRenaming) return;
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
    input.focus({ preventScroll: true });
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
      steering: '<path d="M22 12C22 17.5228 17.5228 22 12 22C6.47715 22 2 17.5228 2 12C2 6.47715 6.47715 2 12 2C17.5228 2 22 6.47715 22 12Z"/><path d="M18.9181 10.4125C17.9491 10.8367 16.4851 11.218 15.1595 10.7168C13.2638 10 12.2893 10 12 10C11.7107 10 10.7362 10 8.84049 10.7168C7.51492 11.218 6.05092 10.8367 5.08186 10.4125M19 13.2548C16.8546 13.6631 13.6079 14.878 13.5238 19M10.4762 19C10.3921 14.878 7.14544 13.6631 5 13.2548"/>',
      eye: '<path d="M21.544 11.045C21.848 11.4713 22 11.6845 22 12C22 12.3155 21.848 12.5287 21.544 12.955C20.1779 14.8706 16.6892 19 12 19C7.31078 19 3.8221 14.8706 2.45604 12.955C2.15201 12.5287 2 12.3155 2 12C2 11.6845 2.15201 11.4713 2.45604 11.045C3.8221 9.12944 7.31078 5 12 5C16.6892 5 20.1779 9.12944 21.544 11.045Z"/><path d="M15 12C15 10.3431 13.6569 9 12 9C10.3431 9 9 10.3431 9 12C9 13.6569 10.3431 15 12 15C13.6569 15 15 13.6569 15 12Z"/>',
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
    const addr = pane.wrap.querySelector(".pane-addr");
    if (addr && isTermPane(pane)) addr.textContent = pane.subtitle || "";
    const dotEl = pane.wrap.querySelector(".pane-dot");
    if (dotEl && isTermPane(pane)) {
      const dot = dotFor(pane.status);
      dotEl.className = `k-dot pane-dot ${dot.cls}`;
      dotEl.title = dot.label;
    }
    syncPaneRole(tab, pane);
    syncPaneFail(pane);
    const syncBtn = pane.wrap.querySelector("[data-pane-sync]");
    if (syncBtn) {
      syncBtn.hidden = !!tab.remote || isNotebookPane(pane);
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
      btn.hidden = !!pane.remote || !(pane.hostId || tab.hostId || localHost()?.id);
    });
    const closeBtn = pane.wrap.querySelector("[data-pane-close]");
    if (closeBtn) closeBtn.title = leafCount(tab.layout) > 1 ? "Close pane" : "Close tab";
  }

  // Role chip on the pane bar, only when the session is shared or joined.
  function syncPaneRole(tab, pane) {
    const chip = pane.wrap.querySelector(".pane-role");
    if (!chip) return;
    const shared = isTermPane(pane) && (pane.remote || tab.remote || (pane.attached?.length || 0) > 1) && pane.you;
    const youFollow = pane.wrap.querySelector(".pane-you-follow");
    chip.hidden = !shared;
    if (youFollow) youFollow.hidden = !shared || isDriving(pane);
    syncFollowBanner(tab, pane);
    if (!shared) return;
    const driving = isDriving(pane);
    // Followers see who drives (§5.7: "<name> is driving" and "You follow").
    const st = Share.followState(pane);
    chip.className = `k-role pane-role ${driving ? "k-role--driver" : "k-role--owner"}`;
    chip.innerHTML = driving
      ? `${paneGlyph("steering")}Driving`
      : `${paneGlyph("steering")}${escapeHtml(st.driver ? `${st.driver} is driving` : "Nobody is driving")}`;
  }

  // Per-pane banner for a local follower (another Keel window on the same
  // session, or the owner after passing control). A joined tab has its own
  // banner over the whole tab instead (syncJoinBanner). Display only: input
  // stays gated by isDriving in wirePaneTerminal.
  function syncFollowBanner(tab, pane) {
    const banner = pane.banner;
    if (!banner) return;
    const show = isTermPane(pane) && !!pane.you && !isDriving(pane) && !pane.remote && !tab.remote;
    banner.hidden = !show;
    if (!show) return;
    const st = Share.followState(pane);
    const key = `${st.driver}|${isOwner(pane)}`;
    if (banner.dataset.key === key) return;
    banner.dataset.key = key;
    const who = st.driver ? `${escapeHtml(st.driver)} is driving` : "Nobody is driving";
    banner.innerHTML = `${Share.glyph("eye")}<span>Following · ${who} · read-only</span>${
      isOwner(pane)
        ? `<button type="button" class="k-btn k-btn--sm" data-take="1">${Share.glyph("steering")}Take control</button>`
        : `<button type="button" class="k-btn k-btn--sm" data-request="1">${Share.glyph("steering")}Ask for control</button>`
    }`;
  }

  // The haze banner over a joined tab (§5.7): whose session, on which host,
  // read-only or driving, Ask for control and Leave.
  function syncJoinBanner(tab) {
    if (!tab?.remote || !tab.root) return;
    const pane = tabPanes(tab).find(isTermPane);
    let el = tab.joinBanner;
    if (!el) {
      el = document.createElement("div");
      el.className = "k-pane-banner k-pane-banner--haze k-join-banner";
      el.setAttribute("role", "status");
      tab.joinBanner = el;
    }
    if (tab.stage && el.parentNode !== tab.root) tab.root.insertBefore(el, tab.stage);
    if (!pane?.you) {
      const html = `${Share.glyph("link")}<span>Joining a shared session · waiting for the host</span><button type="button" class="k-btn k-btn--sm k-btn--ghost" data-join-leave="1">Leave</button>`;
      if (el.dataset.html !== html) el.innerHTML = el.dataset.html = html;
      return;
    }
    const html = Share.followBannerHTML(Share.followState(pane));
    if (el.dataset.html !== html) {
      el.innerHTML = html;
      el.dataset.html = html;
    }
  }

  // Status bar (templ statusBar): runtime and sessions, running forwards,
  // the focused pane's name and size, encoding, input sync.
  function renderStatusBar() {
    const bar = document.getElementById("status-bar");
    if (!bar) return;
    const sessions = [...state.panes.values()].filter((p) => isTermPane(p) && p.runtimeId).length;
    const runtime = document.getElementById("sb-runtime-text");
    if (runtime) runtime.textContent = `keel serve · ${sessions} ${sessions === 1 ? "session" : "sessions"}`;
    const running = (state.tunnels || []).filter((t) => t.status === "ready");
    const fwd = document.getElementById("sb-forwards");
    if (fwd) {
      fwd.hidden = running.length === 0;
      const text = document.getElementById("sb-forwards-text");
      if (text) text.textContent = `${running.length} ${running.length === 1 ? "forward" : "forwards"}`;
    }
    const tab = activeSessionTab();
    const pane = tab ? focusedPane() : null;
    const paneEl = document.getElementById("sb-pane");
    const syncEl = document.getElementById("sb-sync");
    const term = pane && isTermPane(pane) ? pane.term : null;
    if (paneEl) {
      paneEl.hidden = !term;
      if (term) paneEl.textContent = `${pane.title || "session"} · ${term.cols}×${term.rows} · UTF-8`;
    }
    if (syncEl) {
      syncEl.hidden = !term || !!tab?.remote;
      if (term) syncEl.textContent = tab.sync ? "Sync on" : "Sync off";
    }
  }

  function markFocusedPane(pane) {
    const tab = state.tabs.get(pane.tabId);
    if (!tab) return;
    tab.focusedPaneId = pane.id;
    state.activeId = pane.tabId;
    for (const p of tabPanes(tab)) syncPaneChrome(tab, p);
    const statusPane = isNotebookPane(pane) ? termTarget(pane) || pane : pane;
    if (els.barStatus) {
      if (isDriving(statusPane)) els.barStatus.textContent = `${statusPane.subtitle} · driving`;
      else els.barStatus.textContent = `${statusPane.subtitle} · following`;
    }
    renderControl(pane);
    renderStatusBar();
    if (!isNotebookPane(pane) && !termInputOpen(pane)) {
      releaseSidebarFocus();
      pane.term?.focus();
    }
    syncSessionSurface();
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
    const chunks = pane.pending;
    pane.pending = [];
    for (const chunk of chunks) pane.term.write(chunk);
    schedulePaneInvalidate(pane);
  }

  function writePaneBytes(pane, data) {
    if (!isTermPane(pane)) return;
    const chunk = data instanceof Uint8Array ? data : new Uint8Array(data);
    if (!pane.term?.element) {
      if (!pane.pending) pane.pending = [];
      pane.pending.push(chunk);
      return;
    }
    pane.term.write(chunk, () => schedulePaneInvalidate(pane));
    schedulePaneInvalidate(pane);
  }

  // WKWebView often keeps a stale compositor layer for the WebGL canvas after
  // xterm paints. Dummy term.resize(cols±1) kicks Relayout but reflows the
  // whole grid and blinks on every keystroke. Refresh + a subpixel canvas
  // layer nudge republishes the GPU texture without changing PTY size.
  function invalidatePaneRender(pane) {
    if (!isNativeWindow() || !pane?.term?.element) return;
    try {
      const rows = pane.term.rows;
      pane.term.refresh(0, Math.max(0, (rows || 1) - 1));
      kickNativeCanvasCompositor(pane);
    } catch {
      /* ignore */
    }
  }

  function kickNativeCanvasCompositor(pane) {
    const root = pane.term?.element;
    if (!root) return;
    for (const canvas of root.querySelectorAll("canvas")) {
      const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
      if (gl) gl.flush();
      canvas.style.transform = canvas._keelLayerFlip ? "translateZ(0)" : "translateZ(0.01px)";
      canvas._keelLayerFlip = !canvas._keelLayerFlip;
    }
  }

  function schedulePaneInvalidate(pane) {
    if (!isNativeWindow() || !pane || pane._invalidateQueued) return;
    pane._invalidateQueued = true;
    requestAnimationFrame(() => {
      pane._invalidateQueued = false;
      if (pane.wrap?.isConnected) invalidatePaneRender(pane);
    });
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
    if (split) theme.background = tokenTermTheme().background;
    pane.term.options.theme = theme;
  }

  function refreshPaneRender(pane) {
    if (!pane?.term?.element) return;
    syncNativeSplitTheme(pane);
    reflowPane(pane);
    invalidatePaneRender(pane);
  }

  function schedulePaneReflow(pane, tries = 12) {
    refreshPaneRender(pane);
    if (!isNativeWindow() || tries <= 0) return;
    if (paneBodyBox(pane)) return;
    requestAnimationFrame(() => schedulePaneReflow(pane, tries - 1));
  }

  function ensurePaneTerm(pane) {
    if (!isTermPane(pane) || !pane?.term || !pane.wrap?.isConnected) return;
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
    if (isNotebookPane(pane)) pane = termTarget(pane);
    // "Typing is off" belongs to the pane that is focused and following.
    followToasts.dismissWhere("typing\n", pane && !isDriving(pane) ? `typing\n${pane.id}` : "");
    if (!pane || !activeSessionTab()) {
      els.controlWidget.hidden = true;
      els.btnShare.hidden = true;
      closeSharePop();
      return;
    }
    const attached = pane.attached || [];
    els.btnShare.hidden = !!pane.remote;
    if (pane.remote) closeSharePop();
    else if (sharePop.paneId && sharePop.paneId !== pane.id) closeSharePop();
    else renderSharePop();
    // Titlebar badge: how many watch through a share link; opens the
    // Share popover with the people list and Pass control.
    const watching = Share.watchingCount(attached);
    els.controlWidget.hidden = !!pane.remote || watching === 0;
    if (!els.controlWidget.hidden) {
      els.controlWidget.innerHTML = `${Share.glyph("eye")}${watching} watching`;
      els.controlWidget.title = "People in this session";
    }
    if (!attached.length && !pane.you) return;
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

  // openPage opens a tab page (Keychain, Port forwarding, Snippets,
  // Notebooks, Logs, Settings), or selects it when it is already open: one
  // instance each. The page's markup moves from #pages into the tab.
  function openPage(name, opts = {}) {
    if (!PAGES[name]) return null;
    const id = pageTabId(name);
    let tab = state.tabs.get(id);
    if (!tab) {
      tab = {
        id,
        page: name,
        title: PAGES[name].title,
        hostId: "",
        remote: false,
        sync: false,
        layout: null,
        focusedPaneId: "",
        zoomedPaneId: "",
        root: document.createElement("div"),
      };
      tab.root.className = "term-tab page-tab";
      const section = document.getElementById(`page-${name}`);
      if (section) tab.root.appendChild(section);
      els.terminals.appendChild(tab.root);
      state.tabs.set(id, tab);
    }
    if (opts.activate !== false) state.activeId = id;
    if (opts.render !== false) renderTabs();
    return tab;
  }

  // parkPage puts a closing tab page's markup back in #pages, so the next
  // open finds it with its state.
  function parkPage(tab) {
    const section = tab.root.querySelector(`#page-${CSS.escape(tab.page)}`);
    if (section && els.pages) els.pages.appendChild(section);
  }

  function stopLogTimer() {
    if (!state.logTimer) return;
    clearInterval(state.logTimer);
    state.logTimer = null;
  }

  // syncActivePage runs what a tab page needs while it is the selected tab:
  // Logs polls keel serve, Settings reads the current preferences.
  function syncActivePage() {
    const page = state.tabs.get(state.activeId)?.page || "";
    if (page === "logs") {
      if (!state.logTimer) {
        loadLogs().catch(() => {});
        state.logTimer = setInterval(() => loadLogs().catch(() => {}), 3000);
      }
    } else {
      stopLogTimer();
    }
    if (page === state.shownPage) return;
    state.shownPage = page;
    if (page === "settings") {
      syncSettingsForm();
      setSettingsSection(state.settingsSection || "appearance");
    } else if (page === "keychain") syncIdentityPage();
    else if (page === "snippets") syncSnippetPage();
    else if (page === "forwards") syncTunnelPage();
    else if (page === "logs") syncLogPage();
  }

  // newItem is ⌘N: the page's own New on Port forwarding, Keychain,
  // Snippets and Notebooks; New host everywhere else.
  function newItem() {
    const page = state.tabs.get(state.activeId)?.page || "";
    if (page === "forwards") {
      fillTunnelForm(null);
      openModal(els.tunnelModal);
    } else if (page === "keychain") {
      fillIdentityForm(null);
      openModal(els.identityModal);
    } else if (page === "snippets") {
      openNewSnippet();
    } else if (page === "notebooks") {
      newNotebook();
    } else {
      fillHostForm(null);
      openModal(els.hostModal);
    }
  }

  function openNewSnippet() {
    els.snippetForm?.reset();
    if (els.snippetForm?.id) els.snippetForm.id.value = "";
    const title = document.getElementById("snippet-modal-title");
    if (title) title.textContent = "New snippet";
    openModal(els.snippetModal);
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

  // splitBox is the area a split node covers in its tab's stage.
  function splitBox(tab, node) {
    const w = tab.stage?.clientWidth || 0;
    const h = tab.stage?.clientHeight || 0;
    return computeRects(tab.layout, w, h, SASH_PX).splits.get(node)?.box || null;
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
      for (const [paneId, r] of computeRects(tab.layout, w, h, SASH_PX).panes) {
        placePane(tab, paneId, r.x, r.y, r.w, r.h);
        shown.add(paneId);
      }
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
    if (!tab?.sashes || !tab.stage) return;
    const { splits } = computeRects(tab.layout, tab.stage.clientWidth || 0, tab.stage.clientHeight || 0, SASH_PX);
    for (const { el, node } of tab.sashes) {
      const r = splits.get(node)?.sash;
      if (!r) continue;
      el.style.left = `${r.x}px`;
      el.style.top = `${r.y}px`;
      el.style.width = `${r.w}px`;
      el.style.height = `${r.h}px`;
    }
  }

  function bindAbsSash(el, node, tab) {
    el.addEventListener("pointerdown", (ev) => {
      ev.preventDefault();
      el.setPointerCapture(ev.pointerId);
      const move = (e) => {
        const box = splitBox(tab, node);
        if (!box) return;
        const stage = tab.stage.getBoundingClientRect();
        const origin = node.dir === "h" ? stage.left + box.x : stage.top + box.y;
        const sizes = dragSizes(node.dir, box, origin, node.dir === "h" ? e.clientX : e.clientY);
        if (!sizes) return;
        node.sizes = sizes;
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

  function tabGlyph(tab) {
    const panes = tab ? tabPanes(tab) : [];
    const notebookOnly = panes.length > 0 && panes.every(isNotebookPane);
    if (notebookOnly) {
      return `<span class="tab-icon" aria-hidden="true">${hugeicon(`<path d="M9.99219 11H13.4922M9.99219 7H16.9922" /><path d="M5.99219 6H2.99219M5.99219 12H2.99219M5.99219 18H2.99219" /><path d="M8.99219 21.9388C7.41097 21.8363 6.39738 21.562 5.66376 20.8284C4.49219 19.6569 4.49219 17.7712 4.49219 14V10C4.49219 6.22877 4.49219 4.34315 5.66376 3.17158C6.83533 2 8.72096 2 12.4922 2H12.9922C16.7634 2 18.649 2 19.8206 3.17157C20.9922 4.34315 20.9922 6.22877 20.9922 10V10.5" /><path d="M19.856 14.4393L20.5511 15.1317C21.1392 15.7174 21.1392 16.6672 20.5511 17.2529L16.9093 20.9485C16.6228 21.2338 16.2563 21.4262 15.8581 21.5003L13.601 21.9883C13.2446 22.0653 12.9273 21.7502 13.0036 21.395L13.4841 19.1598C13.5584 18.7631 13.7516 18.3981 14.0381 18.1128L17.7262 14.4393C18.3143 13.8536 19.2679 13.8536 19.856 14.4393Z" />`)}</span>`;
    }
    return `<span class="tab-icon" aria-hidden="true">${hugeicon(`<path d="M7.5 7.5L8.72654 8.55719C9.24218 9.00163 9.5 9.22386 9.5 9.5C9.5 9.77614 9.24218 9.99836 8.72654 10.4428L7.5 11.5" /><path d="M11.5 12.5H15.5" /><path d="M12 21C15.7497 21 17.6246 21 18.9389 20.0451C19.3634 19.7367 19.7367 19.3634 20.0451 18.9389C21 17.6246 21 15.7497 21 12C21 8.25027 21 6.3754 20.0451 5.06107C19.7367 4.6366 19.3634 4.26331 18.9389 3.95491C17.6246 3 15.7497 3 12 3C8.25027 3 6.3754 3 5.06107 3.95491C4.6366 4.26331 4.26331 4.6366 3.95491 5.06107C3 6.3754 3 8.25027 3 12C3 15.7497 3 17.6246 3.95491 18.9389C4.26331 19.3634 4.6366 19.7367 5.06107 20.0451C6.3754 21 8.25027 21 12 21Z" />`)}</span>`;
  }

  function closeGlyph() {
    return hugeicon(`<path d="M18 6L6.00081 17.9992M17.9992 18L6 6.00085" />`);
  }

  function pageGlyph(tab) {
    return `<span class="tab-icon" aria-hidden="true">${hugeicon(PAGES[tab.page]?.glyph || "")}</span>`;
  }

  function renderTabs() {
    // With no tab left (startup with nothing to restore, or the last one
    // closed) the window shows a New tab: the Vault is no longer a tab to
    // fall back to.
    if (!state.tabs.size && state.booted) createStartTab({ render: false });
    if (!state.tabs.has(state.activeId)) state.activeId = [...state.tabs.keys()].at(-1) || "";
    const ids = chromeTabs();
    const activeTab = state.tabs.get(state.activeId) || null;
    const sessionOn = isSessionTab(activeTab);
    if (sessionOn) state.lastSessionTabId = activeTab.id;

    const keepScroll = els.tabs.scrollLeft;
    if (!tabRenaming) {
      els.tabs.innerHTML = ids
          .map((id, i) => {
            const tab = state.tabs.get(id);
            const st = tabStatus(tab);
            // ⌘1…8 select the first eight tabs; ⌘9 the last.
            const num = i < 8 ? String(i + 1) : i === ids.length - 1 ? "9" : "";
            const closeHint = id === state.activeId ? kbdHint("W") : "";
            const notebookOnly = tabPanes(tab).length > 0 && tabPanes(tab).every(isNotebookPane);
            const dot = dotFor(st);
            const lead = tab.page
              ? pageGlyph(tab)
              : tab.remote
                ? `<span class="tab-icon tab-icon--joined" title="Joined over a share link" aria-hidden="true">${Share.glyph("link")}</span>`
                : notebookOnly || tab.start
                ? tabGlyph(tab)
                : `<span class="k-dot ${dot.cls}" title="${dot.label}" aria-hidden="true"></span>`;
            const count = tabPanes(tab).length;
            return `<button type="button" class="tab k-tab ${tab.page ? "is-page" : ""} ${id === state.activeId ? "is-active" : ""} ${st} ${tab.sync ? "is-sync" : ""}" data-tab="${id}" title="Drag to reorder">
            ${num ? kbdHint(num) : ""}
            ${lead}
            <span class="tab-label">${escapeHtml(tabName(tab))}</span>
            ${count > 1 ? `<span class="k-badge k-badge--mono tab-count" title="${count} panes">${count}</span>` : ""}
            <span class="close k-tab-close" data-close="${id}" title="Close  ${MOD}W">${closeGlyph()}${closeHint}</span>
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

    els.terminals.hidden = false;
    els.terminals.classList.toggle("is-active", !!activeTab);

    for (const tab of state.tabs.values()) {
      const split = leafCount(tab.layout) > 1;
      tab.root.classList.toggle("is-active", tab.id === state.activeId);
      tab.root.classList.toggle("is-split", split && !tab.zoomedPaneId);
      tab.root.classList.toggle("is-zoomed", !!tab.zoomedPaneId);
      if (tab.dirty) {
        if (tab.start) paintStartPage(tab);
        else if (tab.layout) paintLayout(tab);
        tab.dirty = false;
      } else if (tab.start && tab.id === state.activeId && !tab.connecting) {
        paintStartPage(tab);
      }
      walkLeaves(tab.layout, ({ paneId }) => {
        const p = state.panes.get(paneId);
        if (p) {
          syncPaneChrome(tab, p);
          syncNativeSplitTheme(p);
        }
      });
      syncJoinBanner(tab);
    }
    // A joined tab shows no Vault sidebar (§5.7): the joiner sees only the
    // shared terminal. CSS hides the sidebar and its toggle.
    document.documentElement.toggleAttribute("data-joined", !!activeTab?.remote);

    const pane = sessionOn ? focusedPane() : null;
    const tab = activeTab;
    if (els.barStatus) {
      if (!tab || tab.page) els.barStatus.textContent = "";
      else if (tab?.start) els.barStatus.textContent = tab.connecting ? "Connecting…" : "New tab";
      else if (!pane) els.barStatus.textContent = "No session";
      else if (isNotebookPane(pane)) els.barStatus.textContent = pane.title || "Notebook";
      else if (isDriving(pane)) els.barStatus.textContent = `${pane.subtitle} · driving`;
      else els.barStatus.textContent = `${pane.subtitle} · following`;
    }
    renderControl(pane);
    syncHostLive();
    syncActivePage();
    renderStatusBar();
    if (tabDrag?.id) {
      const dragging = els.tabs.querySelector(`.tab[data-tab="${CSS.escape(tabDrag.id)}"]`);
      dragging?.classList.add("is-dragging");
      dragging?.setAttribute("aria-grabbed", "true");
    }
    if (tab && !tab.page) {
      requestAnimationFrame(() => {
        if (tab?.start) {
          if (sidebarFocused()) return;
          const input = tab.root.querySelector(".start-search");
          if (input && document.activeElement !== input) input.focus({ preventScroll: true });
          return;
        }
        fitVisiblePanes();
        if (!isNotebookPane(pane) && !termInputOpen(pane)) pane?.term?.focus();
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
    const [hosts, snippets, notebooks, herd, identities, tunnels, groups] = await Promise.all([
      api("/api/hosts"),
      api("/api/snippets"),
      api("/api/notebooks"),
      api("/api/sessions"),
      api("/api/identities"),
      api("/api/tunnels"),
      api("/api/groups").catch(() => []),
    ]);
    state.hosts = hosts;
    state.snippets = snippets;
    state.notebooks = notebooks || [];
    state.herd = herd;
    state.identities = identities;
    state.tunnels = tunnels;
    state.groups = Array.isArray(groups) ? groups : [];
    const q = datastarQS();
    const [hostsOk, snOk, nbOk, idOk, tnOk] = await Promise.all([
      paintList("host-list", "/ui/hosts?" + q),
      paintList("snippet-list", "/ui/snippets?" + q),
      paintList("notebook-list", "/ui/notebooks?" + q),
      paintList("identity-list", "/ui/identities?" + q),
      paintList("tunnel-list", "/ui/tunnels?" + q),
    ]);
    if (!hostsOk) renderHosts();
    if (!snOk) renderSnippets();
    if (!nbOk) renderNotebooks();
    if (!idOk) renderIdentities();
    if (!tnOk) renderTunnels();
    refreshStartTabs();
    if (state.tabs.has(pageTabId("logs"))) await loadLogs().catch(() => {});
    paintSidebar();
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

  // Recently closed tabs and panes: host id, title, kind and time, nothing
  // else. Kept in localStorage like the recent hosts, so the New tab page and
  // ⌘⇧T still know them after a reload.
  const CLOSED_KEY = "keel-closed-tabs";

  function loadClosedTabs() {
    try {
      return normalizeClosed(JSON.parse(localStorage.getItem(CLOSED_KEY) || "[]"));
    } catch {
      return [];
    }
  }

  function recordClosed(hostId, title, kind) {
    if (!hostId) return;
    state.closedTabs = pushClosed(state.closedTabs, { hostId, title, kind, at: Date.now() });
    try {
      localStorage.setItem(CLOSED_KEY, JSON.stringify(state.closedTabs));
    } catch {
      /* private mode: the list lives for this window only */
    }
  }

  function closedList() {
    return liveClosed(state.closedTabs, state.hosts.map((h) => h.id));
  }

  // reopenClosed opens entry i of the recently closed list as a new session
  // (a fresh PTY: the old one is gone) and takes it off the list.
  function reopenClosed(i = 0, opts = {}) {
    const entry = closedList()[i];
    const host = entry && state.hosts.find((h) => h.id === entry.hostId);
    if (!host) {
      toast("Nothing to reopen");
      return;
    }
    state.closedTabs = state.closedTabs.filter((e) => e !== entry && !(e.hostId === entry.hostId && e.kind === entry.kind && e.at === entry.at));
    try {
      localStorage.setItem(CLOSED_KEY, JSON.stringify(state.closedTabs));
    } catch {
      /* ignore */
    }
    openSession(host, opts).catch((err) => toast(err.message, true));
  }

  function hostMatches(h, q) {
    const needle = (q || "").trim().toLowerCase();
    if (!needle) return true;
    return hostSearchHay(h).includes(needle);
  }

  // The New tab page (docs/ui-redesign.md §5.3): one input, the recent host
  // cards, the start actions and the recently closed rows. Typing filters
  // the saved hosts; a pasted ssh command, ssh:// URL or user@host:port is
  // parsed (lib/sshparse.js) and offered, never run.
  const ALT = isMac ? "⌥" : "Alt+";

  function liveCount(hostId) {
    let n = 0;
    for (const p of state.panes.values()) if (isTermPane(p) && p.hostId === hostId && !p.closing) n++;
    return n;
  }

  function startCards(tab) {
    return recentCards(state.hosts, loadRecentIds(), tab.query || "", hostMatches, localHost());
  }

  function startOfferHTML(tab) {
    const parsed = parseConnect(tab.query || "");
    if (!parsed) return "";
    if (parsed.ok) return offerHTML(parsed);
    // A half-typed user@host is not an error yet; Enter makes it one.
    if (parsed.source === "target" && !tab.showError) return "";
    return offerErrorHTML(parsed.error);
  }

  function paintStartPage(tab) {
    const prev = tab.root.querySelector(".start-search");
    const keepFocus = document.activeElement === prev;
    const caret = typeof prev?.selectionStart === "number" ? prev.selectionStart : null;
    const cards = startCards(tab);
    tab.cards = cards.map((h) => h.id);
    if (!tab.cards.includes(tab.pick)) tab.pick = tab.cards[0] || "";
    const offer = startOfferHTML(tab);
    const q = (tab.query || "").trim();
    const cardsHtml = cards.length
      ? `<div class="k-start-cards">${cardsHTML(cards, (h) => ({
          sub: hostSubtitle(h),
          live: liveCount(h.id),
          alt: ALT,
          picked: h.id === tab.pick,
          connecting: tab.connecting === h.id,
        }))}</div>`
      : q && !offer
        ? `<p class="k-start-empty">No saved host matches “${escapeHtml(q)}”.</p>`
        : "";
    const recentSection = cardsHtml
      ? `<section class="k-start-section" aria-label="${q ? "Matching hosts" : "Recent hosts"}">
          <h2 class="k-start-section-head"><span>${q ? "Hosts" : "Recent"}</span><span>${ALT}1–${ALT}6 to open</span></h2>
          ${cardsHtml}
        </section>`
      : q
        ? cardsHtml
        : "";
    const closed = closedList();
    const closedSection = closed.length
      ? `<section class="k-start-section" aria-label="Recently closed">
          <h2 class="k-start-section-head"><span>Recently closed</span><span>reopens as a new session</span></h2>
          <div class="k-start-closed-list">${closedHTML(closed, (id) => state.hosts.find((h) => h.id === id)?.name || "", Date.now(), MOD)}</div>
        </section>`
      : "";
    const html = `<div class="start-page"><div class="k-start-col" role="region" aria-label="New tab">
      <div class="k-start-head">
        <h1 class="k-start-title">Where to?</h1>
        <label class="k-start-input"><span class="k-i">${SIDE_ICON_TERMINAL}</span><input class="start-search" type="text" aria-label="Host to connect to" placeholder="saved host, user@host:port, or paste an ssh command" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false" /><span class="k-kbd">↵</span></label>
        <p class="k-help k-start-help">Paste <span class="k-mono">ssh -J bastion deploy@10.0.4.21 -p 2222</span> and Keel reads it, shows what it kept and left out, then offers to save it as a host. Nothing runs.</p>
      </div>
      ${offer}
      ${recentSection}
      <section class="k-start-section" aria-label="Start something else">
        <h2 class="k-start-section-head"><span>Start</span></h2>
        <div class="k-start-actions">${actionsHTML(MOD)}</div>
      </section>
      ${closedSection}
    </div></div>`;
    // renderTabs repaints the active New tab; leave the DOM (scroll, caret)
    // alone when nothing on the page changed.
    if (tab.paintedHTML === html) return;
    tab.paintedHTML = html;
    tab.root.innerHTML = html;
    const input = tab.root.querySelector(".start-search");
    if (input) input.value = tab.query || "";
    if (keepFocus && input) {
      input.focus({ preventScroll: true });
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

  function createStartTab(opts = {}) {
    const tab = {
      id: uid("tab"),
      hostId: "",
      title: "New Tab",
      remote: false,
      start: true,
      sync: false,
      query: "",
      pick: "",
      cards: [],
      showError: false,
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
    if (opts.render === false) return tab;
    renderTabs();
    tab.root.querySelector(".start-search")?.focus({ preventScroll: true });
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
      el.toggleAttribute("data-highlighted", el.dataset.startConnect === hostId);
    });
    tab.root.querySelector(`[data-start-connect="${CSS.escape(hostId)}"]`)?.scrollIntoView({ block: "nearest" });
  }

  // ⌥1…⌥6 on the New tab page: open the nth card.
  function openRecent(n) {
    const tab = state.tabs.get(state.activeId);
    if (!tab?.start) return;
    const host = state.hosts.find((h) => h.id === tab.cards?.[n - 1]);
    if (host) openSession(host, { tab }).catch((err) => toast(err.message, true));
  }

  // A parsed command is offered two ways. Both only fill a dialog: nothing
  // is connected or saved until the user confirms there.
  function setDialogNote(id, html) {
    const el = document.getElementById(id);
    if (!el) return;
    el.innerHTML = html;
    el.hidden = !html;
  }

  function offerSaveHost(parsed) {
    fillHostForm(null);
    const f = hostPrefill(parsed);
    const form = els.hostForm;
    form.name.value = f.name;
    form.address.value = f.address;
    form.port.value = f.port;
    form.username.value = f.username;
    setDialogNote("host-import-note", notesHTML(parsed));
    openModal(els.hostModal);
  }

  function offerConnectOnce(parsed) {
    if (!parsed.user) return;
    openQuick(targetText(parsed), notesHTML(parsed));
  }

  function openQuick(target = "", noteHTML = "") {
    if (els.quickForm?.target) els.quickForm.target.value = target;
    if (els.quickForm?.password) els.quickForm.password.value = "";
    setDialogNote("quick-note", noteHTML);
    openModal(els.quickModal);
  }

  function submitStartInput(tab) {
    const parsed = parseConnect(tab.query || "");
    if (parsed?.ok) {
      offerSaveHost(parsed);
      return;
    }
    if (parsed) {
      tab.showError = true;
      paintStartPage(tab);
      return;
    }
    const host = state.hosts.find((h) => h.id === tab.pick) || state.hosts.find((h) => h.id === tab.cards?.[0]);
    if (host) openSession(host, { tab }).catch((err) => toast(err.message, true));
    else toast("No matching host", true);
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
    return isAppMod(ev, isMac);
  }

  let newTabLockUntil = 0;

  function newTab() {
    const now = Date.now();
    if (now < newTabLockUntil) return;
    newTabLockUntil = now + 300;
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
    reopenClosed(0);
  }

  function showKbdHints(on) {
    document.documentElement.classList.toggle("show-kbd", on);
  }

  let kbdTimer = 0;
  let lastShortcutAt = 0;
  let lastShortcutName = "";
  let ptyInjectWaiter = null;
  let ptyChromeHoldUntil = 0;

  function holdPtyFromChrome(ms = 500) {
    ptyChromeHoldUntil = Math.max(ptyChromeHoldUntil, Date.now() + ms);
  }

  function termInputOpen(pane) {
    if (pane) return !!(isTermPane(pane) && pane.inputEl && !pane.inputEl.hidden);
    for (const p of state.panes.values()) {
      if (isTermPane(p) && p.inputEl && !p.inputEl.hidden) return true;
    }
    return false;
  }

  function ptyWritesBlocked() {
    return !!(sidebarFocused() || state.openModal || ptyInjectWaiter || Date.now() < ptyChromeHoldUntil || isNotebookPane(focusedPane()) || termInputOpen());
  }

  function paletteCatalog() {
    const items = [];
    // Commands are the Actions group; the section they used to be filed under
    // stays as their hint so "vault" or "pane" still finds them.
    const add = (item) => items.push({ ...item, hint: item.hint || item.group, group: "Actions" });
    add({ id: "new-tab", group: "Tabs", title: "New tab", icon: "add", keys: `${MOD}T`, run: () => newTab() });
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
      icon: "reopen",
      keys: `${MOD}⇧T`,
      run: () => reopenLastTab(),
    });
    add({
      id: "prev-tab",
      group: "Tabs",
      title: "Previous tab",
      keys: `${MOD}<`,
      run: () => runShortcut("prev-tab"),
    });
    add({
      id: "next-tab",
      group: "Tabs",
      title: "Next tab",
      keys: `${MOD}>`,
      run: () => runShortcut("next-tab"),
    });
    add({ id: "first-tab", group: "Tabs", title: "First tab", keys: `${MOD}1`, run: () => activateTabIndex(1) });
    add({ id: "last-tab", group: "Tabs", title: "Last tab", keys: `${MOD}9`, run: () => activateTabIndex(9) });
    add({
      id: "toggle-sidebar",
      group: "Vault",
      title: "Toggle vault sidebar",
      icon: "sidebar",
      keys: `${MOD}B`,
      run: () => toggleSidebar(),
    });
    add({
      id: "search-hosts",
      group: "Vault",
      title: "Search hosts",
      run: () => runShortcut("search"),
    });
    add({
      id: "new-host",
      group: "Vault",
      title: "New host",
      icon: "add",
      keys: `${MOD}N`,
      run: () => {
        fillHostForm(null);
        openModal(els.hostModal);
      },
    });
    add({
      id: "quick-connect",
      group: "Vault",
      title: "Quick connect",
      keys: `${MOD}⇧O`,
      run: () => runShortcut("quick-connect"),
    });
    add({ id: "join", group: "Vault", title: "Join session", icon: "link", keys: `${MOD}J`, run: () => runShortcut("join") });
    add({ id: "import-ssh-config", group: "Vault", title: "Import hosts from ~/.ssh/config", run: () => gate?.openImport() });
    add({ id: "vault-keychain", group: "Vault", title: "Keychain", run: () => openPage("keychain") });
    add({ id: "vault-forwards", group: "Vault", title: "Port forwarding", run: () => openPage("forwards") });
    add({ id: "vault-snippets", group: "Vault", title: "Snippets", run: () => openPage("snippets") });
    add({ id: "vault-notebooks", group: "Vault", title: "Notebooks", run: () => openPage("notebooks") });
    add({ id: "vault-logs", group: "Vault", title: "Logs", run: () => openPage("logs") });
    add({ id: "settings", group: "Vault", title: "Settings", keys: `${MOD},`, run: () => runShortcut("settings") });
    add({
      id: "split-right",
      group: "Panes",
      title: "Split right",
      icon: "split",
      keys: `${MOD}⇧D`,
      run: () => splitActive("h"),
    });
    add({
      id: "split-down",
      group: "Panes",
      title: "Split down",
      icon: "split",
      keys: `${MOD}⇧E`,
      run: () => splitActive("v"),
    });
    add({
      id: "split-notebook",
      group: "Panes",
      title: "Split notebook",
      run: () => splitNotebook("h"),
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
    add({ id: "share", group: "Terminal", title: "Share session (join link)", run: () => openSharePop() });
    add({ id: "stop-share", group: "Terminal", title: "Stop sharing this session", run: () => stopShareActive() });
    add({
      id: "stop-runtime",
      group: "Terminal",
      title: "Stop Keel runtime",
      run: () => runShortcut("stop-runtime"),
    });
    // Open now: every tab, and the panes of tabs with more than one. Picking
    // one focuses it; it never starts a second session.
    const ids = chromeTabs();
    ids.forEach((id, i) => {
      const tab = state.tabs.get(id);
      const n = i + 1;
      const keys = i < 8 ? `${MOD}${n}` : i === ids.length - 1 ? `${MOD}9` : "";
      const focusTab = () => {
        state.activeId = id;
        renderTabs();
      };
      if (tab.page) {
        items.push({ id: `tab:${id}`, group: "Open now", title: tabTitle(tab), hint: `tab ${n}`, icon: PALETTE_PAGE_ICONS[tab.page] || "action", keys, run: focusTab });
        return;
      }
      if (tab.start) {
        items.push({ id: `tab:${id}`, group: "Open now", title: "New tab", hint: `tab ${n}`, icon: "add", keys, run: focusTab });
        return;
      }
      const panes = tabPanes(tab);
      const first = panes.find(isTermPane) || panes[0];
      const role = first && isTermPane(first) ? (isDriving(first) ? " · driving" : " · following") : "";
      items.push({
        id: `tab:${id}`,
        group: "Open now",
        title: tabName(tab),
        hint: `tab ${n}${panes.length > 1 ? ` · ${panes.length} panes` : ""}${role}`,
        icon: first && isNotebookPane(first) ? "notebook" : "session",
        state: paletteDot(tabStatus(tab)),
        keys,
        run: focusTab,
      });
      if (panes.length > 1) {
        for (const p of panes) {
          items.push({
            id: `pane:${p.id}`,
            group: "Open now",
            title: p.title || "session",
            hint: `pane in tab ${n}${p.subtitle ? ` · ${p.subtitle}` : ""}`,
            icon: isNotebookPane(p) ? "notebook" : "session",
            state: isTermPane(p) ? paletteDot(p.status) : "idle",
            run: () => {
              focusTab();
              markFocusedPane(p);
            },
          });
        }
      }
    });
    // Hosts open a new session; ⌥↵ opens it in a split of the focused pane.
    for (const h of state.hosts) {
      items.push({
        id: `host:${h.id}`,
        group: "Hosts",
        title: `New session on ${h.name}`,
        hint: hostSubtitle(h),
        icon: h.authType === "local" ? "session" : "host",
        tag: h.tags?.[0] || "",
        run: (opts) => (opts?.split ? openHostInSplit(h) : openSession(h)).catch((err) => toast(err.message, true)),
      });
    }
    // Snippets only ever go through paletteRunSnippet: Approve before type.
    for (const sn of state.snippets) {
      items.push({
        id: `snippet:${sn.id}`,
        group: "Snippets",
        title: `Type snippet: ${sn.name}`,
        hint: String(sn.command || "").trim().split("\n")[0],
        icon: "snippet",
        badge: "Approve first",
        run: () => paletteRunSnippet(sn.id),
      });
    }
    add({
      id: "notebook-new",
      group: "Notebooks",
      title: "New notebook",
      run: () => newNotebook(),
    });
    for (const nb of state.notebooks || []) {
      add({
        id: `notebook:${nb.id}`,
        group: "Notebooks",
        title: `Open notebook ${nb.name || "Notebook"}`,
        hint: nb.pinned ? "Pinned" : "Open without a session",
        run: () => openNotebook(nb.id),
      });
    }
    return items;
  }

  const PALETTE_PAGE_ICONS = { keychain: "key", snippets: "snippet", notebooks: "notebook" };

  // State dot for a tab or pane status class (is-ready ...).
  function paletteDot(status) {
    if (status === "is-ready") return "live";
    if (status === "is-connecting") return "connecting";
    if (status === "is-error") return "dead";
    return "idle";
  }

  // ⌥↵ on a host: open it in a split of the focused pane, or as a new
  // session when no session tab is showing.
  async function openHostInSplit(host) {
    const pane = activeSessionTab() && focusedPane();
    if (!pane) return openSession(host);
    rememberRecent(host.id);
    return splitPane(pane.id, "h", host.id);
  }

  const paletteList = createPaletteList({
    form: els.cmdPaletteForm,
    input: els.cmdPaletteInput,
    list: els.cmdPaletteList,
    catalog: () => paletteCatalog(),
    run: (item, opts) => runPaletteItem(item, opts),
  });

  function isCommandPaletteOpen() {
    return !!(els.cmdPalette && els.cmdPalette.open);
  }

  function closeCommandPalette() {
    if (!isCommandPaletteOpen()) return;
    closeModal(els.cmdPalette);
    syncSessionSurface();
  }

  function openCommandPalette() {
    if (!els.cmdPalette) return;
    if (state.openModal && state.openModal !== els.cmdPalette) closeModal();
    paletteList.reset();
    focusedPane()?.term?.blur?.();
    openModal(els.cmdPalette);
    setSessionSurface("palette");
    requestAnimationFrame(() => els.cmdPaletteInput?.focus({ preventScroll: true }));
  }

  function setSessionSurface(surface) {
    for (const pane of state.panes.values()) {
      if (!isTermPane(pane)) continue;
      sendSessionJSON(pane, { type: "focus", surface });
    }
  }

  function syncSessionSurface() {
    if (isCommandPaletteOpen()) setSessionSurface("palette");
    else if (els.ptyInjectModal?.open || sidebarFocused() || isNotebookPane(focusedPane()) || termInputOpen()) setSessionSurface("chrome");
    else setSessionSurface("term");
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
    focusedPane()?.term?.clearSelection?.();
    return new Promise((resolve) => {
      ptyInjectWaiter = resolve;
      openModal(modal);
      setSessionSurface("chrome");
    });
  }

  function settlePtyInject(ok) {
    const done = ptyInjectWaiter;
    holdPtyFromChrome();
    ptyInjectWaiter = null;
    closeModal(els.ptyInjectModal);
    syncSessionSurface();
    if (done) done(!!ok);
  }

  async function paletteRunSnippet(id) {
    const sn = state.snippets.find((s) => s.id === id);
    if (!sn) return;
    // From a tab page (Snippets), type into the session shown last: select
    // its tab first so the approval names the pane you can see.
    if (!activeSessionTab() && state.tabs.has(state.lastSessionTabId)) {
      state.activeId = state.lastSessionTabId;
      renderTabs();
    }
    const pane = termTarget(focusedPane());
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
    pane.term?.clearSelection?.();
    await sendApprovedInject("\u0015" + command, pane);
  }

  async function runPaletteItem(item, opts = {}) {
    closeCommandPalette();
    if (!item?.run) return;
    try {
      await item.run(opts);
    } catch (err) {
      toast(err?.message || String(err), true);
    }
  }

  function runShortcut(name, repeat) {
    const now = Date.now();
    if (!repeat && name === lastShortcutName && now - lastShortcutAt < 300) return;
    lastShortcutName = name;
    lastShortcutAt = now;
    if (name === "new-tab") newTab();
    else if (name === "close-tab") {
      if (state.openModal) closeModal();
      else closeActivePaneOrTab();
    } else if (name === "reopen-tab") reopenLastTab();
    else if (name === "search") revealVault();
    // ⌘N (and the native New Host item, which sends the same name): the
    // page's own New on a tab page, New host elsewhere.
    else if (name === "new-host") newItem();
    else if (name === "quick-connect") openQuick();
    else if (name === "join") openJoin();
    else if (name === "settings") openPage("settings");
    else if (name === "split-right") splitActive("h");
    else if (name === "split-down") splitActive("v");
    else if (name === "split-notebook") splitNotebook("h");
    else if (name === "focus-pane") {
      const pane = focusedPane();
      if (pane) togglePaneZoom(pane.id);
    }
    else if (name === "find-term") {
      const pane = focusedPane();
      if (pane?.search) openFind(pane);
    } else if (name === "toggle-sidebar") toggleSidebar();
    else if (name === "zoom-in") setFontSize(state.prefs.size + 1);
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
    } else if (name === "prev-tab") activateTabDelta(-1);
    else if (name === "next-tab") activateTabDelta(1);
    else if (name === "palette") {
      if (isCommandPaletteOpen()) closeCommandPalette();
      else openCommandPalette();
    }
  }

  function handleShortcut(ev) {
    if (ev.target?.closest?.(".tab-rename")) return false;
    if (els.gate && !els.gate.hidden) return false;
    const action = classifyShortcut(ev, {
      mac: isMac,
      modalOpen: !!state.openModal,
      findOpen: () => isFindOpen(focusedPane()),
      startPage: !!state.tabs.get(state.activeId)?.start && !state.openModal,
    });
    if (!action) return false;
    ev.preventDefault();
    if (action.name === "find-next" || action.name === "find-prev") {
      runPaneFind(focusedPane(), action.name === "find-prev" ? "prev" : "next", false);
    } else if (action.name === "tab-index") {
      activateTabIndex(action.index);
    } else if (action.name === "recent-host") {
      openRecent(action.index);
    } else {
      runShortcut(action.name, action.repeat);
    }
    return true;
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
      if (!isDriving(pane) || !isTermPane(pane)) return;
      const bytes = new TextEncoder().encode(data);
      const tab = state.tabs.get(pane.tabId);
      if (tab?.sync) {
        for (const p of tabPanes(tab)) {
          if (isDriving(p) && isTermPane(p)) sendSessionBytes(p, bytes);
        }
      } else {
        sendSessionBytes(pane, bytes);
      }
    });
    term.onResize(() => {
      if (focusedPane() === pane) renderStatusBar();
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
    // xterm takes opaque #rrggbb here, so no alpha tokens.
    return {
      matchBackground: tokenColor("--hover"),
      matchOverviewRuler: tokenColor("--brass-ink"),
      activeMatchBackground: tokenColor("--brass"),
      activeMatchColorOverviewRuler: tokenColor("--ink"),
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
    input?.focus({ preventScroll: true });
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

  function wireTermInputEl(inputEl, paneId) {
    inputEl.addEventListener("focusin", () => {
      const pane = state.panes.get(paneId);
      if (pane) {
        markFocusedPane(pane);
        syncSessionSurface();
      }
    });
    inputEl.querySelector("[data-term-input]")?.addEventListener("keydown", (ev) => {
      if (ev.key === "Escape") {
        ev.preventDefault();
        ev.stopPropagation();
        const pane = state.panes.get(paneId);
        if (pane) closeTermInput(pane);
        return;
      }
      if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
        ev.preventDefault();
        ev.stopPropagation();
        const pane = state.panes.get(paneId);
        if (pane) confirmTermInput(pane);
      }
    });
  }

  function makePaneShell(info, paneId) {
    const wrap = document.createElement("div");
    wrap.className = "term-wrap";
    wrap.dataset.paneId = paneId;
    const bar = document.createElement("div");
    bar.className = "pane-bar";
    const startDot = dotFor(info.status === "ready" ? "is-ready" : info.status === "connecting" ? "is-connecting" : info.status === "error" || info.status === "exited" ? "is-error" : "");
    bar.innerHTML = `<span class="k-dot pane-dot ${startDot.cls}" title="${startDot.label}" aria-hidden="true"></span>
      <span class="pane-title" title="Drag to rearrange">${escapeHtml(info.title || "session")}</span>
      <span class="pane-addr">${escapeHtml(info.subtitle || "")}</span>
      <span class="k-role pane-role" hidden></span>
      <div class="pane-actions">
        <span class="k-role k-role--follower pane-you-follow" hidden>${paneGlyph("eye")}You follow</span>
        <button type="button" class="${PANE_BTN} pane-sync" data-pane-sync="${paneId}" aria-pressed="false" title="Sync input">${paneGlyph("sync")}</button>
        <button type="button" class="${PANE_BTN}" data-pane-split="h" data-pane-id="${paneId}" data-kbd="⇧D" title="Split right">${kbdHint("⇧D")}${paneGlyph("splitH")}</button>
        <button type="button" class="${PANE_BTN}" data-pane-split="v" data-pane-id="${paneId}" data-kbd="⇧E" title="Split down">${kbdHint("⇧E")}${paneGlyph("splitV")}</button>
        <button type="button" class="${PANE_BTN}" data-pane-zoom="${paneId}" data-kbd="⇧↵" title="Focus view" disabled>${kbdHint("⇧↵")}<span class="pane-zoom-in">${paneGlyph("zoom")}</span><span class="pane-zoom-out">${paneGlyph("unzoom")}</span></button>
        <button type="button" class="${PANE_BTN} pane-close" data-pane-close="${paneId}" title="Close pane">${closeGlyph()}</button>
      </div>`;
    const progressEl = document.createElement("div");
    progressEl.className = "pane-progress";
    progressEl.hidden = true;
    progressEl.innerHTML = `<div class="pane-progress-bar"></div>`;
    const banner = document.createElement("div");
    banner.className = "k-pane-banner k-pane-banner--haze follow-banner";
    banner.setAttribute("role", "status");
    banner.hidden = true;
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
    failEl.innerHTML = `<div class="k-pane-banner k-pane-banner--flare pane-fail-banner" data-fail-banner role="status">
        <span><b data-fail-kicker>Connection failed</b> <code class="k-mono" data-fail-sub></code></span>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-pane-copy-log="${paneId}">Copy logs</button>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-pane-close="${paneId}">Close</button>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-pane-edit-host="${paneId}">Edit host</button>
        <button type="button" class="k-btn k-btn--primary k-btn--sm pane-fail-start" data-pane-reconnect="${paneId}">Reconnect</button>
      </div>
      <div class="pane-fail-log" data-fail-log></div>`;
    const inputEl = document.createElement("div");
    inputEl.className = "term-input";
    inputEl.hidden = true;
    inputEl.innerHTML = `<span class="term-input-kicker">CLI input</span>
      <textarea class="term-input-body" data-term-input rows="2" spellcheck="false" placeholder="Inserted from the notebook — edit here, then confirm. Nothing is typed until you approve."></textarea>
      <div class="term-input-actions">
        <button type="button" class="tiny" data-term-input-cancel="${paneId}">Cancel</button>
        <button type="button" class="tiny" data-term-input-confirm="${paneId}">Confirm</button>
      </div>`;
    wrap.appendChild(bar);
    wrap.appendChild(progressEl);
    wrap.appendChild(banner);
    wrap.appendChild(body);
    wrap.appendChild(inputEl);
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
    wireTermInputEl(inputEl, paneId);
    return { wrap, banner, body, term, fit, search, findEl, failEl, host, inputEl };
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
      kind: "term",
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
      inputEl: shell.inputEl,
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
    wireFollowerNotice(pane);

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
      if (!tab.hostId && pane.hostId) tab.hostId = pane.hostId;
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
    if (!isTermPane(pane) || !pane?.runtimeId || pane.remote) return;
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

    const ws = new WebSocket(
      sessionSocketURL(location, {
        runtimeId: pane.runtimeId,
        cols: pane.term?.cols,
        rows: pane.term?.rows,
        name: state.name,
      }),
    );
    ws.binaryType = "arraybuffer";
    pane.ws = ws;
    ws.addEventListener("open", () => {
      if (isCommandPaletteOpen()) sendSessionJSON(pane, { type: "focus", surface: "palette" });
      else if (els.ptyInjectModal?.open || sidebarFocused() || isNotebookPane(focusedPane()) || termInputOpen()) sendSessionJSON(pane, { type: "focus", surface: "chrome" });
    });
    let live = !opts.skipSnapshot;

    const applyPresence = (msg) => {
      Object.assign(pane, presenceFields(msg));
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

    const handlers = {
      injectChallenge: (nonce) => settleInjectTicket(pane, nonce),
      joinRequest: (msg) =>
        joinPrompts.request({ sessionId: pane.runtimeId, peerId: msg.peerId, name: msg.message, title: pane.title }),
      joinResolved: (msg) => joinPrompts.resolve({ sessionId: pane.runtimeId, peerId: msg.peerId }),
      joinFailed: (msg) => toast(msg.message || "A viewer could not be connected", true),
      status: (msg) => {
        recordFailStatus(pane, msg);
        pane.status = statusClass(msg.status, pane.status);
        applyPresence(msg);
      },
      presence: (msg) => {
        recordFailStatus(pane, msg);
        pane.status = statusClass(msg.status, "is-ready");
        applyPresence(msg);
        if (msg.type === "ready") live = true;
        if (msg.type === "control" && msg.message) noteControl(pane, msg);
      },
      error: (msg) => {
        pane.status = "is-error";
        pane.failKind = "fail";
        pushFailLog(pane, "fail", msg.message || "Connection failed");
        pane.term.writeln(`\r\n\x1b[31m${msg.message}\x1b[0m`);
        renderTabs();
        if (!promptHostKey(msg, { hostId: pane.hostId || msg.hostId, sessionId: pane.runtimeId || msg.sessionId })) {
          toast(msg.message, true);
        }
      },
    };
    ws.addEventListener("message", (ev) => {
      if (pane.wsGen !== gen) return;
      if (typeof ev.data === "string") {
        dispatchSessionMessage(parseSessionMessage(ev.data), handlers);
        return;
      }
      if (live) writePaneBytes(pane, ev.data);
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
      pane.term.writeln("\r\n\x1b[90mRuntime stopped — this session ended.\x1b[0m");
      renderTabs();
      return;
    }
    pane.failKind = pane.wasReady ? "lost" : "fail";
    pushFailLog(pane, "fail", pane.wasReady ? "Connection lost" : "Connection failed");
    pane.term.writeln("\r\n\x1b[90mConnection lost — click Reconnect to attach.\x1b[0m");
    renderTabs();
  }

  function replaySessionSnapshot(pane) {
    try {
      pane.term?.reset();
    } catch {
      /* ignore */
    }
    pane.pending = [];
  }

  function reconnectPane(pane) {
    if (!isTermPane(pane) || !pane?.runtimeId || pane.remote) return;
    const socketWasOpen = sessionSocketOpen(pane);
    pane.startingOver = true;
    pane.failKind = pane.wasReady ? "lost" : "fail";
    pane.failLog = [];
    seedConnectLog(pane);
    pane.status = "is-connecting";
    renderTabs();
    // Local xterm is often empty after a flap (WebGL remount, overlay, or a
    // fresh attach). Skipping the Subscribe snapshot dropped the server ring
    // and left a blank terminal. Reset and replay those raw bytes instead.
    replaySessionSnapshot(pane);
    connectSessionWS(pane);
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
    if (pane.status === "is-connecting") return "Connecting";
    if (pane.failKind === "ended") return "Session ended";
    if (pane.wasReady || pane.failKind === "lost") return "Connection lost";
    return "Connection failed";
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
    const sub = el.querySelector("[data-fail-sub]");
    const kicker = el.querySelector("[data-fail-kicker]");
    const log = el.querySelector("[data-fail-log]");
    const banner = el.querySelector("[data-fail-banner]");
    const edit = el.querySelector("[data-pane-edit-host]");
    const start = el.querySelector("[data-pane-reconnect]");
    if (sub) sub.textContent = failEndpoint(pane);
    if (kicker) kicker.textContent = failHeadline(pane);
    if (banner) {
      banner.classList.toggle("k-pane-banner--flare", !connecting);
      banner.classList.toggle("k-pane-banner--signal", connecting);
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

  // settleInjectTicket hands the server's nonce to whoever is waiting for
  // it (requestInjectTicket).
  function settleInjectTicket(pane, nonce) {
    const done = pane.injectTicketWaiter;
    pane.injectTicketWaiter = null;
    if (typeof done === "function") done(nonce);
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
    if (!isTermPane(pane) || !pane?.runtimeId) return;
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
    sendSessionJSON(termTarget(focusedPane()) || focusedPane(), { type, ...extra });
  }

  function disposeNotebookEditor(pane) {
    if (!pane) return;
    clearTimeout(pane._saveTimer);
    pane._saveTimer = 0;
    try {
      pane.mdEditor?.destroy?.();
    } catch {
      /* ignore */
    }
    pane.mdEditor = null;
    if (pane._docEl) pane._docEl.replaceChildren();
    pane._docEl = null;
  }

  function disposePane(pane) {
    pane.closing = true;
    disposeNotebookEditor(pane);
    pane.fitObs?.disconnect();
    pane.fitObs = null;
    dropWebgl(pane);
    pane.ws?.close();
    try {
      pane.joinPeer?.close();
    } catch {
      /* ignore */
    }
    try {
      pane.term?.dispose?.();
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
    const tab = state.tabs.get(state.activeId);
    if (!tab) return;
    // The lone New tab is what an empty window shows; closing it would only
    // open another.
    if (tab.start && state.tabs.size === 1) return;
    const panes = tabPanes(tab);
    if (panes.length > 1) {
      const target =
        (tab.zoomedPaneId && state.panes.get(tab.zoomedPaneId)) ||
        (tab.focusedPaneId && state.panes.get(tab.focusedPaneId)) ||
        panes[0];
      closePane(target.id);
      return;
    }
    // The last pane closes its tab, like the tab's ×. When no tab is left
    // the window shows a New tab (renderTabs).
    closeTab(tab.id);
  }

  function closePane(paneId, opts = {}) {
    const pane = state.panes.get(paneId);
    if (!pane) return;
    const tab = state.tabs.get(pane.tabId);
    killRuntimeIfOwned(pane, { fromHerd: opts.fromHerd, remote: opts.remote || tab?.remote });
    // The last pane of a tab is recorded as the tab (closeTab skips panes here).
    if (tab && !opts.fromHerd && !opts.remote && !tab.remote && !pane.remote && isTermPane(pane)) {
      recordClosed(pane.hostId, pane.title, leafCount(tab.layout) > 1 ? "pane" : "tab");
    }
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
    const tab = state.tabs.get(id);
    if (!tab) return;
    if (tabRenaming === id) tabRenaming = null;
    if (tab.page) parkPage(tab);
    const panes = tabPanes(tab);
    // A tab the user closed can be reopened (⌘⇧T, New tab page). Tabs that
    // were joined, closed by the herd, or only moved are not recorded.
    if (!opts.skipPanes && !opts.fromHerd && !tab.remote) {
      const first = panes.find(isTermPane);
      recordClosed(tab.hostId || first?.hostId, first?.title || tab.title, "tab");
    }
    if (!opts.skipPanes) {
      for (const p of panes) {
        killRuntimeIfOwned(p, { fromHerd: opts.fromHerd, remote: tab.remote });
        if (p.runtimeId) joinPrompts.forget(p.runtimeId);
        disposePane(p);
      }
    }
    tab.stageObs?.disconnect();
    tab.stageObs = null;
    tab.root.remove();
    state.tabs.delete(id);
    const remain = [...state.tabs.keys()];
    if (state.activeId === id) state.activeId = remain.at(-1) || "";
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

  async function splitPane(paneId, dir, openHostId = "") {
    const pane = state.panes.get(paneId);
    const tab = pane && state.tabs.get(pane.tabId);
    if (!tab || !pane) {
      toast("Open a session first");
      return;
    }
    if (tab.remote || pane.remote) {
      toast("Split needs a local host session");
      return;
    }
    const hostId = openHostId || pane.hostId || tab.hostId || localHost()?.id;
    if (!hostId) {
      toast("Split needs a local host session");
      return;
    }
    tab.focusedPaneId = pane.id;
    tab.zoomedPaneId = "";
    if (isNativeWindow()) {
      for (const p of tabPanes(tab)) dropWebgl(p);
    }
    try {
      const info = await spawnHost(hostId);
      attachRuntime(info, { tab, splitDir: dir, splitPaneId: pane.id });
      loadAll().catch(() => {});
    } catch (err) {
      toast(err.message, true);
      ensurePaneTerm(pane);
    }
  }

  function makeNotebookShell(paneId, title) {
    const wrap = document.createElement("div");
    wrap.className = "term-wrap is-notebook";
    wrap.dataset.paneId = paneId;
    wrap.dataset.paneKind = "notebook";
    const bar = document.createElement("div");
    bar.className = "pane-bar";
    bar.innerHTML = `<span class="k-dot pane-dot k-dot--idle" aria-hidden="true"></span>
      <span class="pane-title" title="Drag to rearrange">${escapeHtml(title || "Notebook")}</span>
      <span class="pane-addr">Notebook</span>
      <div class="pane-actions">
        <button type="button" class="${PANE_BTN} pane-sync" data-pane-sync="${paneId}" aria-pressed="false" title="Sync input" hidden>${paneGlyph("sync")}</button>
        <button type="button" class="${PANE_BTN}" data-pane-split="h" data-pane-id="${paneId}" data-kbd="⇧D" title="Split right">${kbdHint("⇧D")}${paneGlyph("splitH")}</button>
        <button type="button" class="${PANE_BTN}" data-pane-split="v" data-pane-id="${paneId}" data-kbd="⇧E" title="Split down">${kbdHint("⇧E")}${paneGlyph("splitV")}</button>
        <button type="button" class="${PANE_BTN}" data-pane-zoom="${paneId}" data-kbd="⇧↵" title="Focus view" disabled>${kbdHint("⇧↵")}<span class="pane-zoom-in">${paneGlyph("zoom")}</span><span class="pane-zoom-out">${paneGlyph("unzoom")}</span></button>
        <button type="button" class="${PANE_BTN} pane-close" data-pane-close="${paneId}" title="Close pane">${closeGlyph()}</button>
      </div>`;
    const body = document.createElement("div");
    body.className = "term-body notebook-pane";
    wrap.appendChild(bar);
    wrap.appendChild(body);
    wrap.addEventListener("mousedown", (ev) => {
      if (ev.target.closest?.(".pane-bar button, .pane-actions")) return;
      const pane = state.panes.get(paneId);
      if (pane) markFocusedPane(pane);
    });
    wrap.addEventListener("focusin", () => {
      const pane = state.panes.get(paneId);
      if (pane) {
        markFocusedPane(pane);
        syncSessionSurface();
      }
    });
    return { wrap, body };
  }

  const DEFAULT_NOTEBOOK_MD = `# Notebook

Write a runbook here.

- Bullet lists render here
- Fenced \`\`\`sh or \`\`\`bash blocks can run in a terminal after you confirm

1. Numbered lists render too
`;

  function notebookMarkdown(pane) {
    return (pane?.cells || [])
      .map((c) => String(c.body || ""))
      .join("\n\n")
      .replace(/\n{3,}/g, "\n\n")
      .trim();
  }

  function setNotebookMarkdown(pane, md) {
    if (!isNotebookPane(pane)) return;
    const id = pane.cells?.[0]?.id || uid("cell");
    pane.cells = [{ id, body: String(md ?? "") }];
  }

  function copyNotebookCells(cells) {
    if (!Array.isArray(cells) || !cells.length) return [{ id: uid("cell"), body: DEFAULT_NOTEBOOK_MD }];
    return cells.map((c) => ({ id: c.id || uid("cell"), body: String(c.body || "") }));
  }

  function notebookById(id) {
    return (state.notebooks || []).find((n) => n.id === id);
  }

  function rememberNotebook(rec) {
    if (!rec?.id) return;
    const i = state.notebooks.findIndex((n) => n.id === rec.id);
    if (i >= 0) state.notebooks[i] = rec;
    else state.notebooks.unshift(rec);
    paintSidebar();
    paintVaultNotebooks();
  }

  function createNotebookTab(saved, opts = {}) {
    const tab = {
      id: uid("tab"),
      hostId: "",
      title: saved?.name || "Notebook",
      remote: false,
      sync: false,
      layout: null,
      focusedPaneId: "",
      zoomedPaneId: "",
      root: document.createElement("div"),
    };
    tab.root.className = "term-tab";
    ensureTabStage(tab);
    els.terminals.appendChild(tab.root);
    state.tabs.set(tab.id, tab);
    const pane = createNotebookPane(tab, null, saved);
    tab.layout = { type: "leaf", paneId: pane.id };
    tab.focusedPaneId = pane.id;
    tab.dirty = true;
    if (opts.activate !== false) state.activeId = tab.id;
    return { tab, pane };
  }

  function createNotebookPane(tab, sibling, saved) {
    const paneId = uid("pane");
    const title = saved?.name || "Notebook";
    const shell = makeNotebookShell(paneId, title);
    const pane = {
      id: paneId,
      tabId: tab.id,
      kind: "notebook",
      notebookId: saved?.id || "",
      hostId: sibling?.hostId || tab.hostId || "",
      title,
      subtitle: sibling?.subtitle || "notebook",
      pinned: !!saved?.pinned,
      status: "",
      wrap: shell.wrap,
      body: shell.body,
      cells: copyNotebookCells(saved?.cells),
      remote: !!tab.remote,
    };
    state.panes.set(pane.id, pane);
    paintNotebook(pane);
    return pane;
  }

  async function persistNotebook(pane) {
    if (!isNotebookPane(pane)) return;
    const payload = {
      name: (pane.title || "Notebook").trim() || "Notebook",
      cells: copyNotebookCells(pane.cells).map((c) => ({ id: c.id, body: c.body })),
      pinned: !!pane.pinned,
    };
    try {
      const rec = pane.notebookId
        ? await api(`/api/notebooks/${pane.notebookId}`, { method: "PUT", body: JSON.stringify(payload) })
        : await api("/api/notebooks", { method: "POST", body: JSON.stringify(payload) });
      pane.notebookId = rec.id;
      pane.title = rec.name;
      pane.pinned = !!rec.pinned;
      if (Array.isArray(rec.cells) && rec.cells.length === pane.cells.length) {
        rec.cells.forEach((cell, i) => {
          if (pane.cells[i]) pane.cells[i].id = cell.id || pane.cells[i].id;
        });
      }
      rememberNotebook(rec);
      const tab = state.tabs.get(pane.tabId);
      if (tab) syncPaneChrome(tab, pane);
      persistWorkspace();
      paintSidebar();
      paintVaultNotebooks();
    } catch (err) {
      toast(err.message || "Could not save notebook", true);
    }
  }

  function scheduleNotebookSave(pane) {
    if (!isNotebookPane(pane)) return;
    clearTimeout(pane._saveTimer);
    pane._saveTimer = setTimeout(() => persistNotebook(pane), 400);
  }

  function openNotebook(id) {
    const rec = notebookById(id);
    if (!rec) {
      toast("Notebook not found");
      return;
    }
    const existing = [...state.panes.values()].find((p) => isNotebookPane(p) && p.notebookId === id);
    if (existing) {
      state.activeId = existing.tabId;
      markFocusedPane(existing);
      renderTabs();
      return;
    }
    const tab = state.tabs.get(state.activeId);
    if (tab && !tab.start && tab.layout) {
      const focused = focusedPane();
      const sibling = termTarget(focused) || focused || tabPanes(tab)[0];
      if (sibling) {
        const nb = createNotebookPane(tab, sibling, rec);
        tab.layout = replaceLeaf(tab.layout, sibling.id, {
          type: "split",
          dir: "h",
          sizes: [50, 50],
          a: { type: "leaf", paneId: sibling.id },
          b: { type: "leaf", paneId: nb.id },
        });
        tab.zoomedPaneId = "";
        tab.focusedPaneId = nb.id;
        tab.dirty = true;
        renderTabs();
        markFocusedPane(nb);
        return;
      }
    }
    const created = createNotebookTab(rec);
    renderTabs();
    markFocusedPane(created.pane);
  }

  function newNotebook() {
    const tab = state.tabs.get(state.activeId);
    if (tab && !tab.start && tab.layout) {
      splitNotebook("h");
      return;
    }
    const created = createNotebookTab(null);
    renderTabs();
    markFocusedPane(created.pane);
    persistNotebook(created.pane);
  }

  function renameNotebook(id) {
    const rec = notebookById(id);
    const form = els.notebookRenameForm;
    if (!rec || !form) return;
    form.reset();
    const idInput = form.querySelector('[name="id"]');
    const nameInput = form.querySelector('[name="name"]');
    if (idInput) idInput.value = rec.id;
    if (nameInput) nameInput.value = rec.name || "Notebook";
    openModal(els.notebookRenameModal);
    requestAnimationFrame(() => {
      nameInput?.focus({ preventScroll: true });
      nameInput?.select();
    });
  }

  async function saveNotebookRename(event) {
    event.preventDefault();
    const form = els.notebookRenameForm;
    if (!form) return;
    const id = form.querySelector('[name="id"]')?.value;
    const rec = notebookById(id);
    if (!rec) return;
    const name = (form.querySelector('[name="name"]')?.value || "").trim() || "Notebook";
    try {
      const saved = await api(`/api/notebooks/${id}`, {
        method: "PUT",
        body: JSON.stringify({ name, cells: rec.cells, pinned: !!rec.pinned }),
      });
      rememberNotebook(saved);
      for (const pane of state.panes.values()) {
        if (isNotebookPane(pane) && pane.notebookId === id) {
          pane.title = saved.name;
          const tab = state.tabs.get(pane.tabId);
          if (tab) {
            tab.customTitle = saved.name;
            tab.title = saved.name;
            syncPaneChrome(tab, pane);
          }
          paintNotebook(pane);
        }
      }
      closeModal(els.notebookRenameModal);
      persistWorkspace();
      paintSidebar();
      paintVaultNotebooks();
    } catch (err) {
      toast(err.message || "Could not rename notebook", true);
    }
  }

  async function toggleNotebookPin(id) {
    const rec = notebookById(id);
    if (!rec) return;
    try {
      const saved = await api(`/api/notebooks/${id}`, {
        method: "PUT",
        body: JSON.stringify({ name: rec.name, cells: rec.cells, pinned: !rec.pinned }),
      });
      rememberNotebook(saved);
      for (const pane of state.panes.values()) {
        if (isNotebookPane(pane) && pane.notebookId === id) {
          pane.pinned = !!saved.pinned;
          paintNotebook(pane);
        }
      }
      paintSidebar();
      paintVaultNotebooks();
    } catch (err) {
      toast(err.message || "Could not pin notebook", true);
    }
  }

  async function deleteNotebook(id) {
    const rec = notebookById(id);
    if (!rec) return;
    if (!(await askConfirm(`Delete notebook “${rec.name || "Notebook"}”?`))) return;
    try {
      await api(`/api/notebooks/${id}`, { method: "DELETE" });
      state.notebooks = (state.notebooks || []).filter((n) => n.id !== id);
      const open = [...state.panes.values()].filter((p) => isNotebookPane(p) && p.notebookId === id);
      for (const pane of open) closePane(pane.id);
      paintSidebar();
      paintVaultNotebooks();
    } catch (err) {
      toast(err.message || "Could not delete notebook", true);
    }
  }

  function paintNotebook(pane) {
    const body = pane.body || pane.wrap?.querySelector(".notebook-pane");
    if (!body || !isNotebookPane(pane) || pane.closing) return;
    if (!Array.isArray(pane.cells) || !pane.cells.length) {
      pane.cells = [{ id: uid("cell"), body: DEFAULT_NOTEBOOK_MD }];
    }
    const live = pane.mdEditor && pane._docEl && body.contains(pane._docEl);
    if (live) {
      const titleEl = body.querySelector("[data-notebook-title]");
      if (titleEl && document.activeElement !== titleEl) titleEl.value = pane.title || "Notebook";
      return;
    }
    disposeNotebookEditor(pane);
    body.innerHTML = `<div class="notebook-scroll">
      <input class="notebook-title" data-notebook-title="${pane.id}" value="${escapeHtml(pane.title || "Notebook")}" spellcheck="false" aria-label="Rename notebook" />
      <div class="notebook-doc" data-notebook-doc="${pane.id}"></div>
    </div>`;
    pane._docEl = body.querySelector("[data-notebook-doc]");
    mountNotebookEditor(pane);
  }

  function mountNotebookEditor(pane) {
    const host = pane._docEl;
    if (!host || !isNotebookPane(pane) || pane.closing) return;
    try {
      pane.mdEditor?.destroy?.();
    } catch {
      /* ignore */
    }
    pane.mdEditor = null;
    const KeelMD = window.KeelMarkdownEditor;
    if (!KeelMD || typeof KeelMD.mount !== "function") {
      host.innerHTML = `<textarea class="notebook-source" data-notebook-source="${pane.id}" spellcheck="false" placeholder="Markdown document">${escapeHtml(notebookMarkdown(pane))}</textarea>`;
      return;
    }
    pane.mdEditor = KeelMD.mount(host, {
      markdown: notebookMarkdown(pane),
      buttonLabel: "Run in terminal",
      onChange: (md) => {
        if (pane.closing || !pane.mdEditor) return;
        setNotebookMarkdown(pane, md);
        scheduleNotebookSave(pane);
      },
      onCopyFence: (code) => copyNotebookFence(code),
      onRunFence: (code) => runNotebookFence(pane, code),
    });
  }

  function splitNotebook(dir) {
    const pane = focusedPane();
    const tab = pane && state.tabs.get(pane.tabId);
    if (!tab || tab.start || !tab.layout) {
      const created = createNotebookTab(null);
      renderTabs();
      markFocusedPane(created.pane);
      persistNotebook(created.pane);
      return;
    }
    const sibling = termTarget(pane) || pane;
    tab.focusedPaneId = pane.id;
    tab.zoomedPaneId = "";
    const nb = createNotebookPane(tab, sibling);
    tab.layout = replaceLeaf(tab.layout, pane.id, {
      type: "split",
      dir: dir === "v" ? "v" : "h",
      sizes: [50, 50],
      a: { type: "leaf", paneId: pane.id },
      b: { type: "leaf", paneId: nb.id },
    });
    tab.focusedPaneId = nb.id;
    tab.dirty = true;
    renderTabs();
    markFocusedPane(nb);
    persistNotebook(nb);
  }

  function showTermInput(pane, text) {
    if (!isTermPane(pane) || !pane.inputEl) return;
    const area = pane.inputEl.querySelector("[data-term-input]");
    if (area) area.value = String(text || "");
    pane.inputEl.hidden = false;
    pane.wrap?.classList.add("has-term-input");
    pane.term?.blur?.();
    pane.term?.clearSelection?.();
    area?.focus({ preventScroll: true });
    area?.setSelectionRange?.(area.value.length, area.value.length);
    syncSessionSurface();
    fitVisiblePanes();
  }

  function closeTermInput(pane) {
    if (!isTermPane(pane) || !pane.inputEl) return;
    const area = pane.inputEl.querySelector("[data-term-input]");
    if (area) area.value = "";
    pane.inputEl.hidden = true;
    pane.wrap?.classList.remove("has-term-input");
    syncSessionSurface();
    fitVisiblePanes();
  }

  function closeOpenTermInput() {
    let closed = false;
    for (const pane of state.panes.values()) {
      if (termInputOpen(pane)) {
        closeTermInput(pane);
        closed = true;
      }
    }
    return closed;
  }

  function insertNotebookIntoInput(nbPane, command) {
    if (nbPane && !isNotebookPane(nbPane)) return;
    const line = String(command || "").trim();
    if (!line) {
      toast("Write a command first");
      return;
    }
    const term = termTarget(nbPane) || termTarget(focusedPane());
    if (!term) {
      toast("Split a terminal beside this notebook first");
      return;
    }
    showTermInput(term, line);
    markFocusedPane(term);
  }

  async function runNotebookFence(nbPane, command) {
    insertNotebookIntoInput(nbPane, command);
    const term = termTarget(nbPane) || termTarget(focusedPane());
    if (term && termInputOpen(term)) await confirmTermInput(term);
  }

  function copyNotebookFence(text) {
    const line = String(text || "");
    if (!line) {
      toast("Nothing to copy");
      return;
    }
    const done = () => toast("Copied");
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(line).then(done).catch(() => {
        toast("Could not copy", true);
      });
      return;
    }
    toast("Could not copy", true);
  }

  async function confirmTermInput(pane) {
    if (!isTermPane(pane) || !termInputOpen(pane)) return;
    const area = pane.inputEl.querySelector("[data-term-input]");
    const line = (area?.value || "").trim();
    if (!line) {
      toast("Write a command first");
      return;
    }
    if (!isDriving(pane)) {
      toast("You are following — take or request control to type");
      return;
    }
    const command = line.endsWith("\n") ? line : line + "\n";
    const ok = await approveBeforeType(command, pane.subtitle || pane.title || "session");
    if (!ok) return;
    holdPtyFromChrome();
    await sendApprovedInject(command, pane);
    closeTermInput(pane);
  }

  function toggleTabSync(tabId) {
    const tab = state.tabs.get(tabId || state.activeId);
    if (!isSessionTab(tab) || tab.remote) return;
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

  function requestInjectTicket(pane, command) {
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        if (pane.injectTicketWaiter) {
          pane.injectTicketWaiter = null;
          resolve("");
        }
      }, 2500);
      pane.injectTicketWaiter = (nonce) => {
        clearTimeout(timer);
        pane.injectTicketWaiter = null;
        resolve(nonce || "");
      };
      sendSessionJSON(pane, { type: "inject-challenge", command });
    });
  }

  async function sendApprovedInject(command, targetPane) {
    const pane = termTarget(targetPane || focusedPane());
    if (!pane || !isTermPane(pane)) {
      toast("Open a session first");
      return;
    }
    const tab = state.tabs.get(pane.tabId);
    const targets = tab?.sync ? tabPanes(tab).filter((p) => isTermPane(p) && p.runtimeId) : [pane];
    let sent = false;
    for (const p of targets) {
      const open = p.remote ? p.dc?.readyState === "open" : p.ws?.readyState === WebSocket.OPEN;
      if (!open || !isDriving(p) || !isTermPane(p)) continue;
      const nonce = await requestInjectTicket(p, command);
      if (!nonce) continue;
      sendSessionJSON(p, { type: "inject", command, nonce });
      sent = true;
    }
    if (!sent) {
      toast(isDriving(pane) ? "Session is not connected" : "You are following — take or request control to type");
      return;
    }
    if (!isNotebookPane(focusedPane())) pane.term?.focus?.();
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
    setDialogNote("host-import-note", "");
    form.id.value = host?.id || "";
    form.name.value = host?.name || "";
    fillParentGroupSelect("host-parent-group", host?.groupId || "", "", "No group");
    if (form.tags) form.tags.value = Array.isArray(host?.tags) ? host.tags.join(", ") : "";
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
      groupId: fieldValue(form, "groupId", "host-parent-group"),
      group: "",
      tags: parseHostTags(form.tags?.value),
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

  function openGroupForm(group, parentId) {
    const form = els.groupForm;
    if (!form) return;
    form.reset();
    form.id.value = group?.id || "";
    form.name.value = group?.name || "";
    const exclude = group?.id || "";
    const selected = group ? group.parentId || "" : parentId || "";
    fillParentGroupSelect("group-parent", selected, exclude, "No parent");
    const title = document.getElementById("group-modal-title");
    if (title) title.textContent = group?.id ? "Edit group" : "New group";
    openModal(els.groupModal);
  }

  async function createGroup(parentId) {
    const parent = parentId ? groupById(parentId) : null;
    if (parentId && !parent) {
      toast("Group not found", true);
      return;
    }
    if (parent && groupDepth(parent.id) >= MAX_GROUP_DEPTH) {
      toast("group nest is deeper than 8", true);
      return;
    }
    openGroupForm(null, parentId || "");
  }

  function editGroup(id) {
    const g = groupById(id);
    if (!g) return;
    openGroupForm(g, g.parentId || "");
  }

  async function saveGroup(event) {
    event.preventDefault();
    const form = els.groupForm;
    const name = form.name.value.trim();
    if (!name) {
      toast("Label is required", true);
      return;
    }
    const parentId = fieldValue(form, "parentId", "group-parent");
    const id = form.id.value;
    if (id && (parentId === id || isGroupUnder(parentId, id))) {
      toast("group cannot nest under itself", true);
      return;
    }
    if (parentId && groupDepth(parentId) + 1 + (id ? groupDescendantDepth(id) : 0) > MAX_GROUP_DEPTH) {
      toast("group nest is deeper than 8", true);
      return;
    }
    if (id) {
      const g = groupById(id) || { id };
      await api(`/api/groups/${id}`, { method: "PUT", body: JSON.stringify({ ...g, name, parentId }) });
    } else {
      await api("/api/groups", { method: "POST", body: JSON.stringify({ name, parentId }) });
    }
    closeModal(els.groupModal);
    await loadAll();
    toast("Group saved");
  }

  async function renameGroup(id) {
    const g = groupById(id);
    if (!g) return;
    const title = document.querySelector(`[data-group-title="${CSS.escape(id)}"]`);
    if (!title) {
      openGroupForm(g, g.parentId || "");
      return;
    }
    if (title.querySelector("input")) return;
    const input = document.createElement("input");
    input.type = "text";
    input.value = g.name || "";
    input.className = "group-rename";
    const original = title.textContent;
    title.replaceChildren(input);
    input.focus({ preventScroll: true });
    input.select();
    let settled = false;
    const finish = async (ok) => {
      if (settled) return;
      settled = true;
      const next = input.value.trim();
      if (!ok || !next || next === g.name) {
        title.textContent = original;
        return;
      }
      try {
        await api(`/api/groups/${id}`, { method: "PUT", body: JSON.stringify({ ...g, name: next }) });
        await loadAll();
      } catch (err) {
        title.textContent = original;
        toast(err.message, true);
      }
    };
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") {
        ev.preventDefault();
        finish(true);
      }
      if (ev.key === "Escape") {
        ev.preventDefault();
        finish(false);
      }
    });
    input.addEventListener("blur", () => finish(true));
  }

  async function moveGroup(id) {
    const g = groupById(id);
    if (!g) return;
    openGroupForm(g, g.parentId || "");
  }

  async function deleteGroup(id) {
    const g = groupById(id);
    if (!g) return;
    if (!(await askConfirm(`Remove group ${g.name}? Hosts and subgroups move up one level.`))) return;
    await api(`/api/groups/${id}`, { method: "DELETE" });
    await loadAll();
    toast("Group removed");
  }

  async function assignHostsGroup(ids, groupId) {
    const unique = [...new Set((ids || []).filter(Boolean))];
    if (!unique.length) return;
    await api("/api/hosts/move", { method: "POST", body: JSON.stringify({ ids: unique, groupId: groupId || "" }) });
    setHostSelection([]);
    await loadAll();
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
      openQuick(q);
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

  const joinPrompts = createJoinPrompts({
    answer: (sessionId, peerId, allow) =>
      api(`/api/share/${encodeURIComponent(sessionId)}/admit`, {
        method: "POST",
        body: JSON.stringify({ peerId, allow }),
      }),
    onError: (err) => toast(err.message, true),
  });

  // ---- Share popover (docs/ui-redesign.md §5.6) ----------------------------
  // Opening it shares the focused session, as the Share button always did
  // (POST /api/share returns the live link when there already is one). The
  // popover only shows that state and offers the actions that existed
  // before: copy the link, pass or take control, stop sharing.
  const sharePop = { paneId: "", runtimeId: "", status: "", url: "", error: "", copied: false, seq: 0, html: "" };
  const shareLinks = new Map(); // runtimeId -> join link this window was given

  function sharePopOpen() {
    return !!els.sharePop && !els.sharePop.hidden;
  }

  function openSharePop() {
    const pane = termTarget(focusedPane()) || focusedPane();
    if (!els.sharePop || !pane || pane.remote || !isTermPane(pane) || !pane.runtimeId) return;
    const known = shareLinks.get(pane.runtimeId) || "";
    Object.assign(sharePop, { paneId: pane.id, runtimeId: pane.runtimeId, status: known ? "ready" : "pending", url: known, error: "", copied: false, html: "" });
    els.sharePop.hidden = false;
    els.btnShare?.setAttribute("aria-expanded", "true");
    renderSharePop();
    // Focus moves into the popover (Escape closes it; keys no longer reach
    // the terminal while it is open).
    queueMicrotask(() => els.sharePop.focus({ preventScroll: true }));
    if (!known) startShare(pane);
  }

  function closeSharePop({ refocus = false } = {}) {
    if (!els.sharePop || els.sharePop.hidden) return;
    els.sharePop.hidden = true;
    els.btnShare?.setAttribute("aria-expanded", "false");
    sharePop.paneId = "";
    sharePop.html = "";
    if (refocus) focusedPane()?.term?.focus();
  }

  function positionSharePop() {
    const anchor = els.btnShare && !els.btnShare.hidden ? els.btnShare : els.controlWidget;
    if (!anchor || !els.sharePop) return;
    const r = anchor.getBoundingClientRect();
    els.sharePop.style.top = `${Math.round(r.bottom + 6)}px`;
    els.sharePop.style.right = `${Math.max(8, Math.round(window.innerWidth - r.right))}px`;
  }

  function renderSharePop() {
    if (!sharePopOpen()) return;
    const pane = state.panes.get(sharePop.paneId);
    if (!pane) {
      closeSharePop();
      return;
    }
    const html = Share.sharePopoverHTML({
      title: pane.title || "session",
      status: sharePop.status,
      url: sharePop.url,
      error: sharePop.error,
      copied: sharePop.copied,
      people: Share.sharePeople(pane),
      canGrant: isDriving(pane) || !!isOwner(pane),
      canTake: !!isOwner(pane) && !isDriving(pane),
      canAsk: !isOwner(pane) && !isDriving(pane),
    });
    positionSharePop();
    if (html === sharePop.html) return;
    const focusKey = document.activeElement?.closest?.("#share-pop") ? document.activeElement.getAttribute("data-grant") || (document.activeElement.id ? `#${document.activeElement.id}` : "") : null;
    sharePop.html = html;
    els.sharePop.innerHTML = html;
    if (focusKey != null) {
      const again = focusKey.startsWith("#") ? els.sharePop.querySelector(focusKey) : focusKey ? els.sharePop.querySelector(`[data-grant="${CSS.escape(focusKey)}"]`) : null;
      (again || els.sharePop.querySelector("button"))?.focus({ preventScroll: true });
    }
  }

  async function startShare(pane) {
    const seq = ++sharePop.seq;
    const current = () => sharePop.seq === seq && sharePop.runtimeId === pane.runtimeId && sharePopOpen();
    try {
      const out = await api("/api/share", {
        method: "POST",
        body: JSON.stringify({ sessionId: pane.runtimeId }),
      });
      const url = out.url || "";
      if (url) shareLinks.set(pane.runtimeId, url);
      if (current()) Object.assign(sharePop, { status: url ? "ready" : "error", url, error: url ? "" : "The share started but returned no link." });
      renderSharePop();
      // keel serve also puts the link on the macOS pasteboard; WKWebView
      // often refuses a clipboard write this long after the click.
      if (url && (await copyText(url)) && current()) {
        sharePop.copied = true;
        renderSharePop();
      }
    } catch (err) {
      if (current()) {
        Object.assign(sharePop, { status: "error", error: err.message });
        renderSharePop();
      } else {
        toast(err.message, true);
      }
    }
  }

  function shareActive() {
    if (sharePopOpen()) {
      closeSharePop({ refocus: true });
      return;
    }
    openSharePop();
  }

  async function copyShareLink() {
    if (!sharePop.url) return;
    const ok = await copyText(sharePop.url);
    if (ok) {
      sharePop.copied = true;
      renderSharePop();
    } else {
      els.sharePop?.querySelector("#share-link")?.select();
      toast("Copy failed: the link is selected, press ⌘C", true);
    }
  }

  async function stopShareActive() {
    const pane = sharePopOpen() ? state.panes.get(sharePop.paneId) : focusedPane();
    if (!pane || pane.remote) return;
    try {
      await api(`/api/share/${encodeURIComponent(pane.runtimeId)}`, { method: "DELETE" });
      joinPrompts.forget(pane.runtimeId);
      shareLinks.delete(pane.runtimeId);
      controlToasts.dismissWhere(`${pane.runtimeId}\n`);
      closeSharePop({ refocus: true });
      toast("Sharing stopped. The join link no longer works.");
    } catch (err) {
      toast(err.message, true);
    }
  }

  // ---- Control requests and follower feedback (§5.6, §5.7) -----------------
  const controlToasts = createActionToasts({ className: "join-prompts", label: "Share requests" });
  const followToasts = createActionToasts({ className: "k-toasts--center", label: "Following" });

  // noteControl handles a session "control" event. A request (someone asks
  // for control) becomes a toast for whoever may hand over (the driver or
  // the owner: the runtime's GrantControl rule); Hand over sends the same
  // grant-control message as Pass control. Anything else is the old toast.
  function noteControl(pane, msg) {
    if (Share.isControlRequest(msg) && msg.fromId !== pane.you?.id && (isDriving(pane) || isOwner(pane))) {
      const name = Share.clientName(pane.attached, msg.fromId);
      controlToasts.show({
        key: `${pane.runtimeId}\n${msg.fromId}`,
        tone: "haze",
        role: "alertdialog",
        icon: Share.glyph("steering"),
        title: `${name} asks for control`,
        desc: `Of ${pane.title || "this session"}. You follow until ${isOwner(pane) ? "you take it back" : "it is passed back"}.`,
        actions: [
          { label: "Hand over", primary: true, run: () => sendSessionJSON(pane, { type: "grant-control", targetId: msg.fromId }) },
          { label: "Keep driving" },
        ],
      });
      return;
    }
    if (!Share.isControlRequest(msg)) controlToasts.dismissWhere(`${pane.runtimeId}\n`);
    toast(msg.message);
  }

  // A follower who types gets told why nothing happens. Feedback only: it
  // listens in the capture phase and never stops the event, so xterm and
  // wirePaneTerminal (isDriving, ptyWritesBlocked) still decide, as before.
  function wireFollowerNotice(pane) {
    pane.wrap?.addEventListener(
      "keydown",
      (ev) => {
        if (!Share.isTypingKey(ev)) return;
        if (!isTermPane(pane) || !pane.you || isDriving(pane)) return;
        if (ev.target?.closest?.(".term-find, .term-input, .k-pane-banner, .pane-bar")) return;
        const owner = isOwner(pane);
        followToasts.show({
          key: `typing\n${pane.id}`,
          tone: "haze",
          icon: Share.glyph("lock"),
          title: "Typing is off while you follow",
          timeout: 4000,
          actions: [
            owner
              ? { label: "Take control", run: () => sendSessionJSON(pane, { type: "take-control" }) }
              : { label: "Ask for control", run: () => sendSessionJSON(pane, { type: "request-control" }) },
          ],
        });
      },
      true,
    );
  }

  function openJoin() {
    els.joinForm.reset();
    openModal(els.joinModal);
    queueMicrotask(() => els.joinForm.url.focus({ preventScroll: true }));
  }

  function joinRemote({ origin, room, key }) {
    const runtimeId = `join:${origin}:${room}`;
    let existing = findPaneByRuntime(runtimeId);
    if (existing?.status === "is-error") {
      // Denied, failed verification or lost: pasting the link again retries.
      closeTab(existing.tabId);
      existing = null;
    }
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
      kind: "term",
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
      inputEl: shell.inputEl,
      ws: null,
      joinPeer: null,
      dc: null,
      remote: true,
      you: null,
      attached: [],
      driverId: 0,
      ownerId: 0,
    };
    wirePaneTerminal(pane);
    wireFollowerNotice(pane);
    const tab = createTab(pane);
    tab.remote = true;
    tab.hostId = "";

    const applyPresence = (msg) => {
      Object.assign(pane, presenceFields(msg));
      pane.banner.hidden = isDriving(pane);
      renderTabs();
    };

    const fail = (message, writeLine = true) => {
      pane.status = "is-error";
      if (writeLine) pane.term.writeln(`\r\n\x1b[31m${message}\x1b[0m`);
      toast(message, true);
      renderTabs();
    };
    const peer = openJoinPeer({
      origin,
      room,
      key,
      name: state.name || "guest",
      on: {
        waiting: () => pane.term.writeln("\x1b[90mWaiting for the host to let you in…\x1b[0m"),
        failed: (message) => fail(message),
        hostLeft: () => toast("Host left"),
        signalingError: (message) => toast(message, true),
        signalingClosed: () => {
          if (pane.status !== "is-error") fail("Signaling closed", false);
        },
        connectionLost: () => fail("Follow connection lost", false),
        channel: (dc) => attachFollowChannel(pane, dc, applyPresence),
      },
    });
    pane.joinPeer = peer;
    renderTabs();
  }

  // attachFollowChannel wires a joined share's data channel to its pane:
  // PTY bytes in, presence and control messages to the pane chrome.
  function attachFollowChannel(pane, dc, applyPresence) {
    pane.dc = dc;
    dc.onopen = () => {
      pane.status = "is-ready";
      sendResize(pane);
      renderTabs();
    };
    dc.onclose = () => {
      if (pane.status !== "is-error") pane.status = "";
      pane.term.writeln("\r\n\x1b[90mHost detached.\x1b[0m");
      renderTabs();
    };
    const handlers = {
      injectChallenge: (nonce) => settleInjectTicket(pane, nonce),
      error: (msg) => {
        pane.status = "is-error";
        pane.term.writeln(`\r\n\x1b[31m${msg.message}\x1b[0m`);
        renderTabs();
        toast(msg.message, true);
      },
      status: (msg) => {
        pane.status = statusClass(msg.status, pane.status);
        applyPresence(msg);
      },
      presence: (msg) => {
        pane.status = statusClass(msg.status, pane.status);
        applyPresence(msg);
        if (msg.type === "control" && msg.message) noteControl(pane, msg);
      },
    };
    dc.onmessage = (ev) => {
      if (typeof ev.data === "string") dispatchSessionMessage(parseSessionMessage(ev.data), handlers);
      else writePaneBytes(pane, ev.data);
    };
  }

  async function submitJoin(ev) {
    ev.preventDefault();
    try {
      const parsed = parseJoinLink(els.joinForm.url.value);
      closeModal(els.joinModal);
      joinRemote(parsed);
    } catch (err) {
      toast(err.message, true);
    }
  }

  // The window-wide gate: Unlock, First run, and Import hosts (ui/gate.js).
  const gate = createGate({
    api,
    toast,
    onUnlocked: () => boot(),
    onImported: () => loadAll().catch((err) => toast(err.message, true)),
  });

  function activateTabIndex(n) {
    const id = tabIndexTarget(chromeTabs(), n);
    if (!id) return;
    state.activeId = id;
    renderTabs();
  }

  function activateTabDelta(delta) {
    const id = tabCycleTarget(chromeTabs(), state.activeId, delta);
    if (!id) return;
    state.activeId = id;
    renderTabs();
  }

  async function boot() {
    persistName();
    let vault = null;
    try {
      const health = await api("/api/health");
      state.signalURL = health.signalURL || "";
      if (runtimeIsStale(new URLSearchParams(location.search).get("app_build"), health.build)) {
        showStaleRuntimeBanner({
          stopRuntime: () => {
            state.runtimeStopping = true;
            return api("/api/runtime/stop", { method: "POST" });
          },
        });
      }
      vault = await api("/api/vault").catch(() => null);
      if (health.locked) {
        gate?.update(vault || { locked: true, needsSetup: !!health.needsSetup, sessions: health.sessions });
        return;
      }
      await loadAll();
    } catch (err) {
      if (err.code === 423) {
        gate?.update({ locked: true, needsSetup: false });
        return;
      }
      toast(err.message, true);
      return;
    }
    if (gate) gate.update(vault || {});
    else els.gate.hidden = true;
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
    // Only now may an empty strip turn into a New tab: earlier, restore
    // would have left a stray one beside the sessions it brings back.
    state.booted = true;
    renderTabs();
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
    if (window.__keelChromeBound) return;
    window.__keelChromeBound = true;
    document.fonts?.ready?.then(remeasureTerminalFonts);
    document.fonts?.addEventListener?.("loadingdone", remeasureTerminalFonts);
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
          if (closeOpenTermInput()) {
            ev.preventDefault();
            return;
          }
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
        if (handleShortcut(ev)) ev.stopPropagation();
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
      if (themeChoice() === "system") {
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
      if (!tab || !state.tabs.has(tab.dataset.tab)) return;
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
    on(document.getElementById("btn-sidebar"), "click", () => toggleSidebar());
    on(els.sidebar, "focusin", () => syncSessionSurface());
    on(els.sidebar, "focusout", () => {
      requestAnimationFrame(() => syncSessionSurface());
    });
    on(els.sidebar, "click", (ev) => {
      const seg = ev.target.closest("[data-sidebar-seg]");
      if (seg) {
        ev.preventDefault();
        setSidebarSeg(seg.getAttribute("data-sidebar-seg"));
        return;
      }
      const page = ev.target.closest("[data-open-page]");
      if (page) {
        ev.preventDefault();
        ev.stopPropagation();
        openPage(page.getAttribute("data-open-page"));
        return;
      }
      const openNb = ev.target.closest("[data-notebook-open]");
      if (openNb) {
        ev.preventDefault();
        ev.stopPropagation();
        openNotebook(openNb.dataset.notebookOpen);
        return;
      }
      const pinNb = ev.target.closest("[data-notebook-pin]");
      if (pinNb) {
        ev.preventDefault();
        ev.stopPropagation();
        toggleNotebookPin(pinNb.dataset.notebookPin);
        return;
      }
      const newNb = ev.target.closest("[data-notebook-new]");
      if (newNb) {
        ev.preventDefault();
        ev.stopPropagation();
        newNotebook();
        return;
      }
      const newTn = ev.target.closest("[data-new-tunnel]");
      if (newTn) {
        ev.preventDefault();
        ev.stopPropagation();
        fillTunnelForm(null);
        openModal(els.tunnelModal);
        return;
      }
      const runSn = ev.target.closest("[data-snippet-run]");
      if (runSn) {
        ev.preventDefault();
        ev.stopPropagation();
        paletteRunSnippet(runSn.dataset.snippetRun);
      }
    });
    on(els.terminals, "focusin", () => syncSessionSurface());
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
      const note = ev.target.closest("[data-pane-notebook]");
      if (note) {
        ev.preventDefault();
        const p = state.panes.get(note.dataset.paneNotebook);
        if (p) {
          markFocusedPane(p);
          splitNotebook("h");
        }
        return;
      }
      const copyFence = ev.target.closest("[data-notebook-copy]");
      if (copyFence) {
        ev.preventDefault();
        const code = copyFence.closest(".notebook-fence")?.querySelector("code")?.textContent || "";
        copyNotebookFence(code);
        return;
      }
      const insertFence = ev.target.closest("[data-notebook-fence]");
      if (insertFence) {
        ev.preventDefault();
        const wrap = insertFence.closest(".term-wrap");
        const p = wrap && state.panes.get(wrap.dataset.paneId);
        const code = insertFence.closest(".notebook-fence")?.querySelector("code")?.textContent || "";
        if (p) runNotebookFence(p, code);
        return;
      }
      const confirmInput = ev.target.closest("[data-term-input-confirm]");
      if (confirmInput) {
        ev.preventDefault();
        const p = state.panes.get(confirmInput.dataset.termInputConfirm);
        if (p) confirmTermInput(p);
        return;
      }
      const cancelInput = ev.target.closest("[data-term-input-cancel]");
      if (cancelInput) {
        ev.preventDefault();
        const p = state.panes.get(cancelInput.dataset.termInputCancel);
        if (p) closeTermInput(p);
        return;
      }
      const tab = tabFromRoot(ev.target);
      if (!tab?.start) return;
      const connect = ev.target.closest("[data-start-connect]");
      const quick = ev.target.closest("[data-start-quick]");
      const join = ev.target.closest("[data-start-join]");
      const newHost = ev.target.closest("[data-start-newhost]");
      const reopen = ev.target.closest("[data-start-reopen]");
      const offerSave = ev.target.closest("[data-start-offer-save]");
      const offerOnce = ev.target.closest("[data-start-offer-once]");
      if (connect) {
        const host = state.hosts.find((h) => h.id === connect.dataset.startConnect);
        if (host) openSession(host, { tab }).catch((err) => toast(err.message, true));
      } else if (quick) openQuick();
      else if (join) openJoin();
      else if (newHost) {
        fillHostForm(null);
        openModal(els.hostModal);
      } else if (reopen) reopenClosed(Number(reopen.dataset.startReopen), { tab });
      else if (offerSave || offerOnce) {
        const parsed = parseConnect(tab.query || "");
        if (!parsed?.ok) return;
        if (offerSave) offerSaveHost(parsed);
        else offerConnectOnce(parsed);
      }
    });
    on(els.terminals, "input", (ev) => {
      const title = ev.target.closest?.("[data-notebook-title]");
      if (title) {
        const pane = state.panes.get(title.dataset.notebookTitle);
        if (pane && isNotebookPane(pane)) {
          pane.title = title.value;
          const tab = state.tabs.get(pane.tabId);
          if (tab) {
            tab.customTitle = title.value.trim() || tab.customTitle;
            tab.title = pane.title;
            syncPaneChrome(tab, pane);
          }
          scheduleNotebookSave(pane);
        }
        return;
      }
      const src = ev.target.closest?.("[data-notebook-source]");
      if (src) {
        const pane = state.panes.get(src.dataset.notebookSource);
        if (pane && isNotebookPane(pane)) {
          setNotebookMarkdown(pane, src.value);
          scheduleNotebookSave(pane);
        }
        return;
      }
      const input = ev.target.closest?.(".start-search");
      if (!input) return;
      const tab = tabFromRoot(input);
      if (!tab?.start) return;
      tab.query = input.value;
      tab.pick = "";
      tab.showError = false;
      paintStartPage(tab);
    });
    on(els.terminals, "keydown", (ev) => {
      const input = ev.target.closest?.(".start-search");
      if (!input) return;
      const tab = tabFromRoot(input);
      if (!tab?.start) return;
      if (ev.key === "Enter") {
        ev.preventDefault();
        submitStartInput(tab);
        return;
      }
      const ids = tab.cards || [];
      if (!ids.length) return;
      const idx = Math.max(0, ids.indexOf(tab.pick));
      if (ev.key === "ArrowDown") {
        ev.preventDefault();
        setStartPick(tab, ids[Math.min(ids.length - 1, idx + 1)]);
      } else if (ev.key === "ArrowUp") {
        ev.preventDefault();
        setStartPick(tab, ids[Math.max(0, idx - 1)]);
      }
    });

    // Tab pages: list filters, the Forwards and Logs chips, Keychain and
    // Snippets selection, and the snippet editor. The pages move between
    // #pages and their tabs, so these listeners sit on the pages themselves.
    document.querySelectorAll("[data-list-filter]").forEach((input) => {
      on(input, "input", () => {
        const scope = input.getAttribute("data-list-filter");
        listFilters[scope] = input.value;
        applyListFilter(scope);
      });
    });
    on(document.getElementById("page-forwards"), "click", (ev) => {
      const chip = ev.target.closest("[data-tunnel-filter]");
      if (!chip) return;
      state.tunnelFilter = chip.getAttribute("data-tunnel-filter") || "all";
      syncTunnelPage();
    });
    on(document.getElementById("page-logs"), "click", (ev) => {
      const chip = ev.target.closest("[data-log-filter]");
      if (!chip) return;
      state.logKind = chip.getAttribute("data-log-filter") || "all";
      syncLogPage();
    });
    on(document.getElementById("page-keychain"), "click", (ev) => {
      const item = ev.target.closest("[data-identity-select]");
      if (item) selectIdentity(item.getAttribute("data-identity-select"));
    });
    on(document.getElementById("page-snippets"), "click", (ev) => {
      const item = ev.target.closest("[data-snippet-select]");
      if (item) selectSnippet(item.getAttribute("data-snippet-select"));
    });
    const snippetEditor = document.getElementById("snippet-editor");
    on(snippetEditor, "submit", (ev) => saveSnippetEditor(ev).catch((err) => toast(err.message, true)));
    on(snippetEditor, "input", () => {
      snippetEditor.dataset.dirty = "1";
      syncSnippetPreview();
    });
    on(snippetEditor, "keydown", (ev) => {
      const mod = isMac ? ev.metaKey && !ev.ctrlKey : ev.ctrlKey && !ev.metaKey;
      if (!mod || ev.shiftKey || ev.altKey || String(ev.key).toLowerCase() !== "s") return;
      ev.preventDefault();
      ev.stopPropagation();
      snippetEditor.requestSubmit();
    });
    on(document.getElementById("page-settings"), "click", (ev) => {
      const btn = ev.target.closest("[data-settings-section]");
      if (!btn) return;
      setSettingsSection(btn.dataset.settingsSection);
    });
    on(document.getElementById("settings-search"), "input", (ev) => filterSettings(ev.target.value));
    on(document.getElementById("btn-welcome-local"), "click", () => newLocalTab());
    on(els.btnShare, "click", shareActive);
    on(els.controlWidget, "click", shareActive);
    on(els.sharePop, "click", (ev) => {
      if (ev.target.closest("[data-share-copy]")) copyShareLink();
      else if (ev.target.closest("[data-share-stop]")) stopShareActive();
      // data-grant / data-take / data-request are the body handler's, as
      // they were on the old titlebar people chips.
    });
    on(els.sharePop, "keydown", (ev) => {
      if (ev.key !== "Escape") return;
      ev.preventDefault();
      ev.stopPropagation();
      closeSharePop({ refocus: true });
    });
    document.addEventListener(
      "pointerdown",
      (ev) => {
        if (!sharePopOpen() || !(ev.target instanceof Element)) return;
        if (ev.target.closest("#share-pop, #btn-share, #control-widget")) return;
        closeSharePop();
      },
      true,
    );
    window.addEventListener("resize", () => positionSharePop());
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
  }

  function bindLists() {
    document.body.addEventListener("click", async (ev) => {
      if (ev.target.closest("#btn-quick")) {
        openQuick();
        return;
      }
      if (ev.target.closest("#btn-join")) {
        openJoin();
        return;
      }
      const groupToggle = ev.target.closest("[data-group-toggle]");
      if (groupToggle) {
        ev.preventDefault();
        toggleGroupNode(groupToggle.getAttribute("data-group-toggle") || groupToggle.closest("[data-group-id]")?.getAttribute("data-group-id"));
        return;
      }
      const hostTile = ev.target.closest("[data-drag-host]");
      if (hostTile && (ev.metaKey || ev.ctrlKey || ev.shiftKey)) {
        ev.preventDefault();
        toggleHostSelect(hostTile.getAttribute("data-drag-host"), ev.metaKey || ev.ctrlKey || ev.shiftKey);
        return;
      }
      const groupNew = ev.target.closest("[data-group-new]");
      if (groupNew) {
        ev.preventDefault();
        createGroup(groupNew.getAttribute("data-group-new") || "").catch((err) => toast(err.message, true));
        return;
      }
      const groupEdit = ev.target.closest("[data-group-edit]");
      if (groupEdit) {
        ev.preventDefault();
        editGroup(groupEdit.getAttribute("data-group-edit"));
        return;
      }
      const groupRename = ev.target.closest("[data-group-rename]");
      if (groupRename) {
        ev.preventDefault();
        renameGroup(groupRename.getAttribute("data-group-rename")).catch((err) => toast(err.message, true));
        return;
      }
      const groupMove = ev.target.closest("[data-group-move]");
      if (groupMove) {
        ev.preventDefault();
        moveGroup(groupMove.getAttribute("data-group-move")).catch((err) => toast(err.message, true));
        return;
      }
      const groupDelete = ev.target.closest("[data-group-delete]");
      if (groupDelete) {
        ev.preventDefault();
        deleteGroup(groupDelete.getAttribute("data-group-delete")).catch((err) => toast(err.message, true));
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
      const dup = ev.target.closest("[data-duplicate]");
      if (dup) {
        const host = state.hosts.find((h) => h.id === dup.dataset.duplicate);
        if (!host) return;
        if (host.authType === "local") {
          toast("Local Shell cannot be duplicated");
          return;
        }
        const saved = await api(`/api/hosts/${host.id}/duplicate`, { method: "POST" });
        await loadAll();
        toast(`Duplicated as ${saved.name || host.name + " (copy)"}`);
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
      if (run) {
        ev.preventDefault();
        await paletteRunSnippet(run.dataset.run);
        return;
      }
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
      const openNb = ev.target.closest("[data-notebook-open]");
      if (openNb) {
        ev.preventDefault();
        openNotebook(openNb.dataset.notebookOpen);
        return;
      }
      const renameNb = ev.target.closest("[data-notebook-rename]");
      if (renameNb) {
        ev.preventDefault();
        renameNotebook(renameNb.dataset.notebookRename);
        return;
      }
      const pinNb = ev.target.closest("[data-notebook-pin]");
      if (pinNb) {
        ev.preventDefault();
        toggleNotebookPin(pinNb.dataset.notebookPin);
        return;
      }
      const delNb = ev.target.closest("[data-delete-notebook]");
      if (delNb) {
        ev.preventDefault();
        deleteNotebook(delNb.dataset.deleteNotebook);
        return;
      }
      const grant = ev.target.closest("[data-grant]");
      if (grant) sendControl("grant-control", { targetId: Number(grant.dataset.grant) });
      if (ev.target.closest("[data-request]")) sendControl("request-control");
      if (ev.target.closest("[data-take]")) sendControl("take-control");
      // Leave a joined tab: the same as closing it (the peer connection
      // closes; nothing is killed on the host).
      const leave = ev.target.closest("[data-join-leave]");
      if (leave) {
        const tabId = leave.closest(".term-tab") && [...state.tabs.values()].find((t) => t.root === leave.closest(".term-tab"))?.id;
        if (tabId && state.tabs.get(tabId)?.remote) closeTab(tabId);
        return;
      }
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

    let nestDrag = null;
    const clearDropTargets = () => {
      document.querySelectorAll(".group-tree-node.is-drop-target").forEach((el) => el.classList.remove("is-drop-target"));
    };
    document.body.addEventListener("dragstart", (ev) => {
      if (ev.target.closest("[data-group-menu], [data-group-toggle], input, textarea, button")) {
        if (!ev.target.closest("[data-drag-host], [data-drag-group]")) {
          ev.preventDefault();
          return;
        }
      }
      if (ev.target.closest("[data-group-menu], [data-group-toggle]")) {
        ev.preventDefault();
        return;
      }
      const host = ev.target.closest("[data-drag-host]");
      const folder = ev.target.closest("[data-drag-group]");
      if (host) {
        const id = host.getAttribute("data-drag-host");
        const selected = selectedHostSet();
        const ids = selected.has(id) && selected.size ? [...selected] : [id];
        nestDrag = { kind: "hosts", ids };
      } else if (folder) {
        nestDrag = { kind: "group", id: folder.getAttribute("data-drag-group") };
      } else {
        nestDrag = null;
        return;
      }
      ev.dataTransfer.effectAllowed = "move";
      ev.dataTransfer.setData("text/plain", nestDrag.id);
    });
    document.body.addEventListener("dragover", (ev) => {
      if (!nestDrag) return;
      const target = ev.target.closest("[data-group-drop]");
      clearDropTargets();
      if (!target) return;
      ev.preventDefault();
      target.classList.add("is-drop-target");
      ev.dataTransfer.dropEffect = "move";
    });
    document.body.addEventListener("dragleave", (ev) => {
      const node = ev.target.closest?.(".group-tree-node");
      if (node && !node.contains(ev.relatedTarget)) node.classList.remove("is-drop-target");
    });
    document.body.addEventListener("drop", (ev) => {
      const target = ev.target.closest("[data-group-drop]");
      clearDropTargets();
      if (!nestDrag || !target) return;
      ev.preventDefault();
      const dest = target.getAttribute("data-group-drop") || "";
      const drag = nestDrag;
      nestDrag = null;
      if (drag.kind === "hosts") {
        if (drag.ids?.length && dest !== undefined) {
          assignHostsGroup(drag.ids, dest).catch((err) => toast(err.message, true));
        }
        return;
      }
      if (drag.kind === "group") {
        if (!drag.id || drag.id === dest || isGroupUnder(dest, drag.id)) {
          toast("group cannot nest under itself", true);
          return;
        }
        const g = groupById(drag.id);
        if (!g) return;
        if (dest && groupDepth(dest) + 1 + groupDescendantDepth(drag.id) > MAX_GROUP_DEPTH) {
          toast("group nest is deeper than 8", true);
          return;
        }
        api(`/api/groups/${drag.id}`, { method: "PUT", body: JSON.stringify({ ...g, parentId: dest }) })
          .then(loadAll)
          .catch((err) => toast(err.message, true));
      }
    });
    document.body.addEventListener("dragend", () => {
      nestDrag = null;
      clearDropTargets();
    });

    // One filter box for the sidebar's segment: Hosts asks keel serve for
    // the tree, Snippets and Forwards filter what is already loaded.
    let hostFilterSeq = 0;
    on(els.search, "input", () => {
      const seg = sidebarSeg();
      if (seg !== "hosts") {
        state.sidebarQuery[seg] = els.search.value;
        paintSidebar();
        return;
      }
      state.query = els.search.value;
      const seq = ++hostFilterSeq;
      paintList("host-list", "/ui/hosts?" + datastarQS()).then((ok) => {
        if (!ok && seq === hostFilterSeq) renderHosts();
      });
    });
    on(els.search, "keydown", (ev) => {
      if (ev.key === "Escape" && els.search.value) {
        ev.preventDefault();
        ev.stopPropagation();
        els.search.value = "";
        els.search.dispatchEvent(new Event("input", { bubbles: true }));
        return;
      }
      if (ev.key !== "Enter" || sidebarSeg() !== "hosts") return;
      ev.preventDefault();
      connectFromSearch();
    });
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
    on(document.getElementById("btn-quick"), "click", () => openQuick());
    on(document.getElementById("btn-new-snippet"), "click", () => {
      els.snippetForm?.reset();
      const title = document.getElementById("snippet-modal-title");
      if (title) title.textContent = "New snippet";
      openModal(els.snippetModal);
    });
    on(document.getElementById("btn-new-notebook"), "click", () => newNotebook());
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
    on(document.getElementById("group-cancel"), "click", () => closeModal(els.groupModal));
    on(document.getElementById("quick-cancel"), "click", () => closeModal(els.quickModal));
    on(document.getElementById("snippet-cancel"), "click", () => closeModal(els.snippetModal));
    on(document.getElementById("notebook-rename-cancel"), "click", () => closeModal(els.notebookRenameModal));
    on(document.getElementById("identity-cancel"), "click", () => closeModal(els.identityModal));
    on(document.getElementById("tunnel-cancel"), "click", () => closeModal(els.tunnelModal));
    on(document.getElementById("hostkey-cancel"), "click", () => closeModal(els.hostKeyModal));
    on(els.modalScrim, "click", () => closeModal());
    [
      els.hostModal,
      els.groupModal,
      els.quickModal,
      els.snippetModal,
      els.notebookRenameModal,
      els.joinModal,
      els.identityModal,
      els.tunnelModal,
      els.hostKeyModal,
      els.cmdPalette,
    ].forEach((modal) => on(modal, "close", () => {
      if (state.openModal === modal) state.openModal = null;
      els.modalScrim?.classList.remove("is-open");
      fitVisiblePanes();
      if (modal === els.cmdPalette) syncSessionSurface();
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
      syncSessionSurface();
    });
    on(els.ptyInjectForm, "submit", (ev) => {
      ev.preventDefault();
      settlePtyInject(true);
    });
    on(document.getElementById("pty-inject-ok"), "click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      settlePtyInject(true);
    });
    on(document.getElementById("pty-inject-ok"), "pointerdown", (ev) => {
      ev.stopPropagation();
    });
    on(document.getElementById("pty-inject-cancel"), "click", () => settlePtyInject(false));
    on(els.hostForm?.authType, "change", toggleAuthFields);
    on(els.hostForm?.keepAlive, "change", toggleKeepAliveFields);
    on(els.identityForm?.kind, "change", toggleIdentFields);
    on(els.tunnelForm?.kind, "change", toggleTunnelDest);
    on(els.hostForm, "submit", (ev) => saveHost(ev).catch((err) => toast(err.message, true)));
    on(els.groupForm, "submit", (ev) => saveGroup(ev).catch((err) => toast(err.message, true)));
    on(els.identityForm, "submit", (ev) => saveIdentity(ev).catch((err) => toast(err.message, true)));
    on(els.tunnelForm, "submit", (ev) => saveTunnel(ev).catch((err) => toast(err.message, true)));
    on(els.quickForm, "submit", (ev) => saveQuick(ev).catch((err) => toast(err.message, true)));
    on(els.snippetForm, "submit", (ev) => saveSnippet(ev).catch((err) => toast(err.message, true)));
    on(els.notebookRenameForm, "submit", (ev) => saveNotebookRename(ev).catch((err) => toast(err.message, true)));
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
  document.getElementById("btn-new-tab")?.setAttribute("title", `New tab  ${MOD}T`);
  document.getElementById("btn-new-tab")?.blur();
  document.getElementById("btn-new-host")?.setAttribute("title", `New host  ${MOD}N`);
  // Page shortcut hints are written as macOS glyphs in the markup.
  if (!isMac) {
    document.querySelectorAll(".k-page .k-kbd").forEach((el) => {
      el.textContent = el.textContent.replace("⌘", MOD);
    });
  }
  toggleAuthFields();
  toggleIdentFields();
  toggleTunnelDest();

  if (!state.name) state.name = "guest";
  pinScroll(document, [document.scrollingElement, document.body, document.querySelector(".app"), document.querySelector(".main")]);
  boot();
})();
