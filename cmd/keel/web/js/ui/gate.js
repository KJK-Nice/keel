// The window-wide gate (internal/ui/gate.templ): Unlock (§5.14) when the
// vault exists and keel serve could not open it, and First run (§5.15):
// step 1 creates the vault (or shows the one the keychain already made),
// step 2 imports hosts from ~/.ssh/config, step 3 (optional) sets up the
// agent. Step 2 also opens on its own from the command palette.
//
// The gate only calls keel serve's routes: /api/vault (state),
// /api/vault/setup (creates a vault that does not exist yet; refused
// otherwise), /api/vault/unlock, /api/ssh-config/preview and /import, and
// /api/first-run/done. Step 3 adds GET /api/agent/providers (text for the
// key field), PUT /api/agent/keys/gemini and PUT /api/agent/settings, and only
// when the user presses Open Keel; nothing is sent to a provider. The gate
// never decides who may unlock or what is copied.

import * as O from "../lib/onboard.js";
import { keyError, safeURL } from "../lib/agentsettings.js";

export function createGate({ doc = document, api, toast, onUnlocked, onImported }) {
  const $ = (id) => doc.getElementById(id);
  const root = $("gate");
  if (!root) return null;
  const el = {
    unlock: $("unlock"),
    unlockForm: $("unlock-form"),
    unlockLead: $("unlock-lead"),
    unlockBanner: $("unlock-banner"),
    unlockBannerText: $("unlock-banner-text"),
    unlockField: $("unlock-field"),
    unlockPass: $("unlock-pass"),
    unlockShow: $("unlock-show"),
    unlockError: $("unlock-error"),
    unlockRetry: $("unlock-retry"),
    unlockRetryHelp: $("unlock-retry-help"),
    unlockSubmit: $("unlock-submit"),
    sessions: $("gate-sessions"),
    onboard: $("onboard"),
    col: $("onb-col"),
    steps: $("onb-steps"),
    stepVault: $("onb-step-vault"),
    stepHosts: $("onb-step-hosts"),
    stepAgent: $("onb-step-agent"),
    vaultForm: $("onb-vault"),
    modeKeychain: $("onb-mode-keychain"),
    modePass: $("onb-mode-pass"),
    keychainDesc: $("onb-keychain-desc"),
    passDesc: $("onb-pass-desc"),
    passFields: $("onb-pass-fields"),
    pass: $("onb-pass"),
    pass2: $("onb-pass2"),
    passError: $("onb-pass-error"),
    vaultPath: $("onb-vault-path"),
    vaultEnc: $("onb-vault-enc"),
    existing: $("onb-existing"),
    existingBtn: $("onb-existing-btn"),
    dataDir: $("onb-data-dir"),
    vaultNext: $("onb-vault-next"),
    vaultNextLabel: $("onb-vault-next-label"),
    hosts: $("onb-hosts"),
    hostsTitle: $("onb-hosts-title"),
    hostsLead: $("onb-hosts-lead"),
    imp: $("onb-import"),
    back: $("onb-back"),
    skip: $("onb-skip"),
    importBtn: $("onb-import-btn"),
    importLabel: $("onb-import-label"),
    agent: $("onb-agent"),
    agentTitle: $("onb-agent-title"),
    agentGemini: $("onb-agent-gemini"),
    agentNone: $("onb-agent-none"),
    agentKey: $("onb-agent-key"),
    agentWarning: $("onb-agent-warning"),
    agentWarningText: $("onb-agent-warning-text"),
    agentKeyLabel: $("onb-agent-key-label"),
    agentKeyInput: $("onb-agent-key-input"),
    agentKeyError: $("onb-agent-key-error"),
    agentLinks: $("onb-agent-links"),
    agentSuggest: $("onb-agent-suggest"),
    agentBack: $("onb-agent-back"),
    agentLater: $("onb-agent-later"),
    agentOpen: $("onb-agent-open"),
  };
  // The keychain's name as the server's OS calls it ("macOS Keychain").
  const keychain = root.dataset.keychain || "the keychain";

  let vs = null; // GET /api/vault
  let flow = ""; // "unlock" | "first-run" | "import"
  let created = false; // this window created the vault in step 1
  let preview = null;
  let sel = null;
  let busy = false;

  const show = (node, on) => {
    if (node) node.hidden = !on;
  };
  const setText = (node, text) => {
    if (node) node.textContent = text;
  };

  function sessionsText(n) {
    if (!n) return "keel serve · no sessions running";
    return `keel serve · ${n === 1 ? "1 session" : `${n} sessions`} waiting`;
  }

  function open(which) {
    root.hidden = false;
    show(el.unlock, which === "unlock");
    show(el.onboard, which !== "unlock");
    setText(el.sessions, sessionsText(vs?.sessions || 0));
  }

  function hide() {
    root.hidden = true;
    flow = "";
  }

  // --- Unlock (§5.14) ---

  function showUnlock(state) {
    vs = state;
    flow = "unlock";
    open("unlock");
    const n = state?.sessions || 0;
    setText(
      el.unlockLead,
      `Your hosts and keys are encrypted on this machine. ${n ? `${n === 1 ? "1 session is" : `${n} sessions are`} waiting in keel serve.` : "keel serve is running."}`,
    );
    const keychainVault = state?.mode === "keychain";
    el.unlockBanner?.classList.toggle("k-pane-banner--signal", keychainVault);
    el.unlockBanner?.classList.toggle("k-pane-banner--haze", !keychainVault);
    setText(
      el.unlockBannerText,
      keychainVault
        ? `${keychain} didn't answer, and this vault's key is kept there, so a passphrase can't open it. Allow Keel in ${keychain}, then run keel serve stop and open Keel again.`
        : state?.keyringOff
          ? "Keel was started with KEEL_KEYRING=off, so it needs your passphrase."
          : `${keychain} doesn't hold this vault's key, so Keel needs your passphrase.`,
    );
    show(el.unlockBanner, true);
    show(el.unlockField, !keychainVault);
    show(el.unlockRetry, !keychainVault && !state?.keyringOff);
    show(el.unlockRetryHelp, !keychainVault && !state?.keyringOff);
    show(el.unlockSubmit, !keychainVault);
    show(el.unlockError, false);
    if (el.unlockPass) {
      el.unlockPass.value = "";
      el.unlockPass.removeAttribute("aria-invalid");
      if (!keychainVault) el.unlockPass.focus({ preventScroll: true });
    }
  }

  async function submitUnlock(ev) {
    ev.preventDefault();
    if (busy) return;
    const passphrase = el.unlockPass?.value || "";
    if (!passphrase) {
      unlockError("Enter the vault passphrase.");
      return;
    }
    busy = true;
    try {
      await api("/api/vault/unlock", { method: "POST", body: JSON.stringify({ passphrase }) });
      el.unlockPass.value = "";
      hide();
      onUnlocked?.();
    } catch (err) {
      unlockError(err.code === 403 ? "That passphrase doesn't open the vault." : err.message);
    } finally {
      busy = false;
    }
  }

  function unlockError(msg) {
    setText(el.unlockError, msg);
    show(el.unlockError, true);
    el.unlockPass?.setAttribute("aria-invalid", "true");
    el.unlockPass?.select();
  }

  // --- First run, step 1: the vault ---

  function setStep(step) {
    const hostsStep = step === "hosts";
    const agentStep = step === "agent";
    show(el.vaultForm, step === "vault");
    show(el.hosts, hostsStep);
    show(el.agent, agentStep);
    if (el.col) el.col.dataset.step = step;
    el.onboard?.setAttribute("aria-labelledby", agentStep ? "onb-agent-title" : hostsStep ? "onb-hosts-title" : "onb-title");
    if (el.stepVault) el.stepVault.dataset.state = step === "vault" ? "now" : "done";
    if (el.stepHosts) el.stepHosts.dataset.state = hostsStep ? "now" : agentStep ? "done" : "";
    if (el.stepAgent) el.stepAgent.dataset.state = agentStep ? "now" : "";
    const now = agentStep ? el.stepAgent : hostsStep ? el.stepHosts : el.stepVault;
    for (const li of [el.stepVault, el.stepHosts, el.stepAgent]) {
      if (li === now) li?.setAttribute("aria-current", "step");
      else li?.removeAttribute("aria-current");
    }
  }

  function radio(node, checked, disabled) {
    if (!node) return;
    node.setAttribute("aria-checked", checked ? "true" : "false");
    if (disabled) node.setAttribute("aria-disabled", "true");
    else node.removeAttribute("aria-disabled");
  }

  function showVaultStep() {
    const setup = !!vs?.needsSetup;
    const mode = vs?.mode || "";
    flow = "first-run";
    open("onboard");
    show(el.steps, true);
    setStep("vault");
    const usesKeychain = !setup && mode === "keychain";
    radio(el.modeKeychain, usesKeychain, !usesKeychain);
    radio(el.modePass, !usesKeychain, usesKeychain);
    setText(
      el.keychainDesc,
      usesKeychain
        ? "The vault key lives in your login keychain. Keel opens without asking."
        : vs?.keyringOff
          ? "Keel was started with KEEL_KEYRING=off, so it does not use the keychain."
          : setup
            ? `${keychain} didn't answer, so Keel can't keep the vault key there. Use a passphrase, or allow Keel in ${keychain} and open Keel again.`
            : "This vault opens with a passphrase.",
    );
    setText(
      el.passDesc,
      usesKeychain
        ? `Keel already made this vault with ${keychain}. It does not re-key a vault here.`
        : "You type it each time keel serve starts. Pick this on shared machines or when there is no keychain.",
    );
    show(el.passFields, setup);
    show(el.passError, false);
    setText(el.vaultPath, vs?.path || "keel.vault");
    setText(el.dataDir, String(vs?.path || "").replace(/[\\/]keel\.vault$/, "") || "the Keel data folder");
    setText(el.vaultEnc, usesKeychain ? "age, with the wrapping key in your keychain" : "age, with a key made from your passphrase (scrypt)");
    setText(el.vaultNextLabel, setup ? "Create vault" : "Continue");
    show(el.existingBtn, setup);
    if (!setup) show(el.existing, false);
    if (setup) el.pass?.focus({ preventScroll: true });
    else el.vaultNext?.focus({ preventScroll: true });
  }

  async function submitVault(ev) {
    ev.preventDefault();
    if (busy) return;
    if (!vs?.needsSetup) {
      showHostsStep();
      return;
    }
    const a = el.pass?.value || "";
    const b = el.pass2?.value || "";
    if (!a) return passError("Choose a passphrase.", el.pass);
    if (a !== b) return passError("The passphrases don't match.", el.pass2);
    busy = true;
    try {
      await api("/api/vault/setup", { method: "POST", body: JSON.stringify({ passphrase: a }) });
      el.pass.value = "";
      el.pass2.value = "";
      created = true;
      onUnlocked?.();
    } catch (err) {
      passError(err.message, el.pass);
    } finally {
      busy = false;
    }
  }

  function passError(msg, field) {
    setText(el.passError, msg);
    show(el.passError, true);
    field?.focus();
  }

  // --- First run, step 2 (and the palette's Import): hosts ---

  async function showHostsStep() {
    open("onboard");
    show(el.steps, flow === "first-run");
    setStep("hosts");
    show(el.back, flow === "first-run");
    setText(el.skip, flow === "first-run" ? "Skip, add hosts later" : "Cancel");
    setText(el.hostsTitle, flow === "first-run" ? "Bring your hosts" : "Import hosts");
    setText(el.hostsLead, `Reading ${vs?.sshConfig || "~/.ssh/config"}…`);
    if (el.imp) el.imp.innerHTML = "";
    preview = null;
    sel = null;
    syncImportButton();
    el.hostsTitle?.focus({ preventScroll: true });
    try {
      preview = await api("/api/ssh-config/preview");
    } catch (err) {
      setText(el.hostsLead, `Keel could not read ${vs?.sshConfig || "~/.ssh/config"}: ${err.message}`);
      return;
    }
    sel = O.defaultSelection(preview);
    setText(el.hostsLead, O.leadText(preview));
    if (el.imp) el.imp.innerHTML = O.importHTML(preview, sel);
    syncImportButton();
  }

  function syncImportButton() {
    const n = preview && sel ? O.selectedCount(preview, sel) : 0;
    setText(el.importLabel, O.addLabel(n));
    if (el.importBtn) el.importBtn.disabled = n === 0 || busy;
    const sum = el.imp?.querySelector("#imp-summary");
    if (sum && preview && sel) sum.textContent = O.summaryText(preview, sel);
    const all = el.imp?.querySelector("[data-imp-all]");
    if (all && preview && sel) all.setAttribute("aria-checked", O.allState(preview, sel));
  }

  function keyAt(i) {
    return preview?.keys?.[Number(i)];
  }

  function onImportClick(ev) {
    if (!preview || !sel) return;
    const noJump = ev.target.closest("[data-imp-nojump]");
    if (noJump) {
      const alias = noJump.getAttribute("data-imp-nojump");
      if (!sel.noJump) sel.noJump = new Set();
      const on = !sel.noJump.has(alias);
      if (on) sel.noJump.add(alias);
      else sel.noJump.delete(alias);
      noJump.setAttribute("aria-checked", on ? "true" : "false");
      return;
    }
    const hostBox = ev.target.closest("[data-imp-host]");
    if (hostBox) {
      const alias = hostBox.getAttribute("data-imp-host");
      const on = !sel.hosts.has(alias);
      if (on) sel.hosts.add(alias);
      else sel.hosts.delete(alias);
      hostBox.setAttribute("aria-checked", on ? "true" : "false");
      syncImportButton();
      return;
    }
    if (ev.target.closest("[data-imp-all]")) {
      const every = O.allState(preview, sel) === "true";
      for (const h of preview.hosts || []) {
        if (every) sel.hosts.delete(h.alias);
        else sel.hosts.add(h.alias);
      }
      for (const box of el.imp.querySelectorAll("[data-imp-host]")) {
        box.setAttribute("aria-checked", every ? "false" : "true");
      }
      syncImportButton();
      return;
    }
    const sw = ev.target.closest("[data-imp-key]");
    if (sw && !sw.disabled) {
      const i = sw.getAttribute("data-imp-key");
      const k = keyAt(i);
      if (!k) return;
      const st = sel.keys.get(k.path) || { on: false, passphrase: "" };
      st.on = !st.on;
      sel.keys.set(k.path, st);
      sw.setAttribute("aria-checked", st.on ? "true" : "false");
      const field = el.imp.querySelector(`[data-imp-pass-field="${i}"]`);
      if (field) {
        field.hidden = !st.on;
        if (st.on) field.querySelector("input")?.focus();
      }
      keyError(i, "");
    }
  }

  function onImportInput(ev) {
    const input = ev.target.closest("[data-imp-pass]");
    if (!input || !sel) return;
    const k = keyAt(input.getAttribute("data-imp-pass"));
    if (!k) return;
    const st = sel.keys.get(k.path) || { on: true, passphrase: "" };
    st.passphrase = input.value;
    sel.keys.set(k.path, st);
    keyError(input.getAttribute("data-imp-pass"), "");
  }

  function keyError(i, msg) {
    const p = el.imp?.querySelector(`[data-imp-key-error="${i}"]`);
    if (!p) return;
    p.textContent = msg;
    p.hidden = !msg;
    const input = el.imp.querySelector(`[data-imp-pass="${i}"]`);
    if (input) {
      if (msg) input.setAttribute("aria-invalid", "true");
      else input.removeAttribute("aria-invalid");
    }
  }

  async function runImport() {
    if (!preview || !sel || busy) return;
    const missing = O.missingPassphrase(preview, sel);
    if (missing >= 0) {
      keyError(missing, `Enter the passphrase for ${preview.keys[missing].name}, or turn this key off.`);
      el.imp.querySelector(`[data-imp-pass="${missing}"]`)?.focus();
      return;
    }
    const noJump = O.missingJump(preview, sel);
    if (noJump) {
      toast(`Add ${noJump.alias}'s jump host too, or tick Without the jump to add it as a direct host.`, true);
      el.imp.querySelector(`[data-imp-nojump="${CSS.escape(noJump.alias)}"]`)?.focus();
      return;
    }
    const body = O.importRequest(preview, sel);
    if (!body.hosts.length) return;
    busy = true;
    syncImportButton();
    try {
      const res = await api("/api/ssh-config/import", { method: "POST", body: JSON.stringify(body) });
      toast(O.importedText(res), (res?.failed || []).length > 0);
      afterHosts();
      onImported?.();
    } catch (err) {
      const path = err.data?.key;
      const i = path ? (preview.keys || []).findIndex((k) => k.path === path) : -1;
      if (i >= 0) {
        const name = preview.keys[i].name;
        keyError(
          i,
          err.errorKind === "wrongPassphrase"
            ? `That passphrase doesn't open ${name}.`
            : err.errorKind === "passphrase"
              ? `Enter the passphrase for ${name}.`
              : err.message,
        );
        el.imp.querySelector(`[data-imp-pass="${i}"]`)?.focus();
      } else {
        toast(err.message, true);
      }
    } finally {
      busy = false;
      syncImportButton();
    }
  }

  // afterHosts leaves step 2: to the Agent step in the first run, and
  // back to the app when the palette opened the import on its own.
  function afterHosts() {
    if (flow === "first-run") showAgentStep();
    else hide();
  }

  async function finish() {
    if (flow === "first-run") {
      try {
        await api("/api/first-run/done", { method: "POST" });
      } catch (err) {
        toast(err.message, true);
      }
    }
    hide();
  }

  // --- First run, step 3: the agent (optional) ---

  let agentChoice = "none"; // "none" | "gemini"
  let agentDescriptor = null;

  function agentError(msg) {
    setText(el.agentKeyError, msg || "");
    if (msg) el.agentKeyInput?.setAttribute("aria-invalid", "true");
    else el.agentKeyInput?.removeAttribute("aria-invalid");
  }

  function pickAgent(choice) {
    agentChoice = choice;
    radio(el.agentGemini, choice === "gemini", false);
    radio(el.agentNone, choice === "none", false);
    show(el.agentKey, choice === "gemini");
    agentError("");
    if (choice === "gemini") el.agentKeyInput?.focus({ preventScroll: true });
  }

  function paintAgentDescriptor() {
    const d = agentDescriptor;
    if (!d) return;
    setText(el.agentKeyLabel, d.keyLabel || "Gemini API key");
    el.agentKeyInput?.setAttribute("aria-label", d.keyLabel || "Gemini API key");
    setText(el.agentWarningText, d.warning || "");
    show(el.agentWarning, !!d.warning);
    if (el.agentLinks) {
      el.agentLinks.textContent = "";
      const add = (href, text) => {
        const url = safeURL(href);
        if (!url) return;
        if (el.agentLinks.childNodes.length) el.agentLinks.append(" · ");
        const a = doc.createElement("a");
        a.href = url;
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        a.dataset.kExternal = "";
        a.textContent = text;
        el.agentLinks.append(a);
      };
      add(d.keyHelpUrl, "Get a key");
      add(d.termsUrl, "Gemini API terms");
    }
  }

  async function showAgentStep() {
    open("onboard");
    show(el.steps, true);
    setStep("agent");
    agentChoice = "none";
    pickAgent("none");
    if (el.agentKeyInput) el.agentKeyInput.value = "";
    el.agentSuggest?.setAttribute("aria-checked", "true");
    el.agentTitle?.focus({ preventScroll: true });
    if (!agentDescriptor) {
      try {
        const list = await api("/api/agent/providers");
        agentDescriptor = (list || []).find((d) => d.id === "gemini") || null;
      } catch {}
    }
    paintAgentDescriptor();
  }

  // openKeel saves what was chosen and ends the first run. A key that is
  // typed is stored in the vault (not checked against the provider, so
  // nothing is sent); the switch is saved either way. A failed save keeps
  // the step open with the reason.
  async function openKeel(ev) {
    ev?.preventDefault();
    if (busy) return;
    const key = (el.agentKeyInput?.value || "").trim();
    if (agentChoice === "gemini" && !key) {
      agentError("Paste your key, or choose Not now.");
      el.agentKeyInput?.focus();
      return;
    }
    busy = true;
    try {
      if (agentChoice === "gemini") {
        await api("/api/agent/keys/gemini", { method: "PUT", body: JSON.stringify({ key }) });
        if (el.agentKeyInput) el.agentKeyInput.value = "";
      }
      const on = el.agentSuggest?.getAttribute("aria-checked") === "true";
      await api("/api/agent/settings", { method: "PUT", body: JSON.stringify({ suggestions: on ? "on" : "off" }) });
    } catch (err) {
      agentError(keyError(err));
      busy = false;
      return;
    }
    busy = false;
    await finish();
  }

  // --- wiring ---

  el.unlockForm?.addEventListener("submit", submitUnlock);
  el.unlockShow?.addEventListener("click", () => {
    const showing = el.unlockPass.type === "text";
    el.unlockPass.type = showing ? "password" : "text";
    el.unlockShow.setAttribute("aria-pressed", showing ? "false" : "true");
    el.unlockShow.setAttribute("aria-label", showing ? "Show passphrase" : "Hide passphrase");
    el.unlockPass.focus();
  });
  el.vaultForm?.addEventListener("submit", submitVault);
  el.existingBtn?.addEventListener("click", () => {
    const on = el.existing.hidden;
    show(el.existing, on);
    el.existingBtn.setAttribute("aria-expanded", on ? "true" : "false");
  });
  for (const r of [el.modeKeychain, el.modePass]) {
    r?.addEventListener("click", () => {
      if (r.getAttribute("aria-disabled") === "true") return;
      radio(el.modeKeychain, r === el.modeKeychain, el.modeKeychain.getAttribute("aria-disabled") === "true");
      radio(el.modePass, r === el.modePass, el.modePass.getAttribute("aria-disabled") === "true");
    });
  }
  el.imp?.addEventListener("click", onImportClick);
  el.imp?.addEventListener("input", onImportInput);
  el.imp?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && ev.target.closest("[data-imp-pass]")) {
      ev.preventDefault();
      runImport();
    }
  });
  el.importBtn?.addEventListener("click", runImport);
  el.skip?.addEventListener("click", () => afterHosts());
  el.agent?.addEventListener("submit", openKeel);
  el.agentGemini?.addEventListener("click", () => pickAgent("gemini"));
  el.agentNone?.addEventListener("click", () => pickAgent("none"));
  el.agentSuggest?.addEventListener("click", () => {
    el.agentSuggest.setAttribute("aria-checked", el.agentSuggest.getAttribute("aria-checked") === "true" ? "false" : "true");
  });
  el.agentKeyInput?.addEventListener("input", () => agentError(""));
  el.agentBack?.addEventListener("click", () => showHostsStep());
  el.agentLater?.addEventListener("click", () => finish());
  el.back?.addEventListener("click", () => showVaultStep());
  root.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && flow === "import") {
      ev.preventDefault();
      hide();
    }
  });

  return {
    // update decides what the gate shows for a vault state: Unlock, step 1
    // of the first run (step 2 right after this window created the vault),
    // or nothing.
    update(state) {
      vs = state;
      if (state?.locked) {
        if (state.needsSetup) showVaultStep();
        else showUnlock(state);
        return;
      }
      if (state?.firstRun) {
        if (flow === "first-run" && (!el.hosts.hidden || !el.agent.hidden)) return;
        flow = "first-run";
        if (created) showHostsStep();
        else showVaultStep();
        return;
      }
      hide();
    },
    // openImport opens step 2 on its own (command palette).
    async openImport() {
      if (!vs) {
        try {
          vs = await api("/api/vault");
        } catch {}
      }
      flow = "import";
      await showHostsStep();
    },
    isOpen: () => !root.hidden,
  };
}
