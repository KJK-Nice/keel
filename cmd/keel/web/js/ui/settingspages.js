// Settings · Keyboard, Sharing, Vault & security, Runtime and About
// (docs/ui-redesign.md §5.9 and the board's section list). The markup is in
// internal/ui/settings_sections.templ; this fills it. Reads only:
//
//   GET  /api/runtime   build, addr, dataDir, pid, sessions, uptimeSec,
//                       canStop, signalURL, signalSource, signalSaved
//   GET  /api/vault     mode, locked, keyringOff, path (no secret)
//   PUT  /api/share/signal   the Signal server field, {url} ("" removes it)
//   POST /api/runtime/stop   through `stopRuntime`, after the alertdialog
//
// Two values are written: the signal server, which keel serve saves, and
// the display name, which stays in this browser (term.js keeps it; see
// setDisplayName there).

import { escapeHtml } from "../lib/html.js";
import { keyCaps, shortcutGroups } from "../lib/shortcutmap.js";
import { signalText, stopMessage, uptimeText, vaultModeText, versionOf } from "../lib/settingspages.js";

export function createSettingsPages({ doc = document, api, toast, confirm, isMac, getName, setName, stopRuntime }) {
  const $ = (id) => doc.getElementById(id);
  if (!$("settings-runtime")) return null;
  const text = (id, v) => {
    const el = $(id);
    if (el) el.textContent = v;
  };

  function paintKeyboard() {
    const host = $("kbd-groups");
    if (!host) return;
    host.innerHTML = shortcutGroups(isMac)
      .map(
        (g) => `<section class="settings-group"><h2 class="settings-group-title">${escapeHtml(g.group)}</h2><div class="settings-group-body">${g.items
          .map((it) => {
            const caps = keyCaps(it.keys)
              .map((c) => `<span class="k-kbd" aria-hidden="true">${escapeHtml(c)}</span>`)
              .join("");
            return `<div class="pref-row" data-shortcut="${escapeHtml(it.id)}"><div class="pref-copy"><div class="pref-title">${escapeHtml(it.title)}</div></div><span class="k-keys" role="img" aria-label="${escapeHtml(it.keys)}" data-search="${escapeHtml(it.keys)}">${caps}</span></div>`;
          })
          .join("")}</div></section>`,
      )
      .join("");
  }

  function paintName() {
    const input = $("share-set-name");
    if (!input || doc.activeElement === input) return;
    input.value = getName().custom;
    input.placeholder = getName().fallback;
  }

  function paintRuntime(r) {
    text("run-set-build", r.build || "unknown");
    text("run-set-addr", r.addr || "");
    text("run-set-data", r.dataDir || "");
    text("run-set-pid", String(r.pid ?? ""));
    text("run-set-sessions", String(r.sessions ?? 0));
    text("run-set-uptime", uptimeText(r.uptimeSec));
    paintSignal(r);
    const stop = $("run-set-stop");
    if (stop) stop.disabled = r.canStop === false;
    const v = versionOf(r.build);
    text("about-version", v || "unknown");
    text("about-build", r.build || "unknown");
    text("settings-about-count", v);
  }

  // paintSignal fills the Signal server field, unless it is being edited.
  function paintSignal(info) {
    const sig = signalText(info);
    text("share-set-signal-help", sig.help);
    const input = $("share-set-signal");
    if (!input || doc.activeElement === input) return;
    input.value = sig.value;
    input.placeholder = sig.placeholder;
    input.dataset.saved = sig.value;
  }

  let signalSaving = false;
  async function commitSignal() {
    const input = $("share-set-signal");
    if (!input || signalSaving) return;
    const url = input.value.trim();
    if (url === (input.dataset.saved || "")) return;
    signalSaving = true;
    try {
      const info = await api("/api/share/signal", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url }),
      });
      input.blur();
      paintSignal(info);
      toast(url ? "Signal server saved" : "Signal server removed");
    } catch (err) {
      toast(err?.message || "Could not save the signal server", true);
    } finally {
      signalSaving = false;
    }
  }

  function paintVault(v) {
    const mode = vaultModeText(v);
    text("vault-set-state", v.needsSetup ? "Not set up" : v.locked ? "Locked" : "Unlocked");
    text("vault-set-mode", mode.label);
    text("vault-set-mode-help", mode.help);
    text("vault-set-path", v.path || "");
  }

  let seq = 0;
  async function refresh() {
    paintName();
    const mine = ++seq;
    const [rt, vault] = await Promise.all([api("/api/runtime").catch(() => null), api("/api/vault").catch(() => null)]);
    if (mine !== seq) return;
    if (rt) paintRuntime(rt);
    if (vault) paintVault(vault);
  }

  async function askStop() {
    const sessions = Number($("run-set-sessions")?.textContent) || 0;
    const ok = await confirm(stopMessage(sessions), { title: "Stop the Keel runtime?", action: "Stop runtime" });
    if (ok) stopRuntime();
  }

  async function copyFrom(id) {
    const value = $(id)?.textContent || "";
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      toast("Copied");
    } catch {
      toast("Could not copy", true);
    }
  }

  paintKeyboard();
  paintName();
  const nameInput = $("share-set-name");
  const commitName = () => {
    setName(nameInput.value);
    paintName();
  };
  nameInput?.addEventListener("change", commitName);
  nameInput?.addEventListener("keydown", (ev) => {
    if (ev.key !== "Enter") return;
    ev.preventDefault();
    commitName();
  });
  const signalInput = $("share-set-signal");
  signalInput?.addEventListener("change", () => commitSignal());
  signalInput?.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape") {
      signalInput.value = signalInput.dataset.saved || "";
      signalInput.blur();
      return;
    }
    if (ev.key !== "Enter") return;
    ev.preventDefault();
    commitSignal();
  });
  $("run-set-stop")?.addEventListener("click", () => askStop());
  $("page-settings")?.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-copy-from]");
    if (btn) copyFrom(btn.dataset.copyFrom);
  });

  return { refresh, paintKeyboard };
}
