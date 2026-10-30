// Pure helpers for Settings · Sharing, Vault & security, Runtime and About
// (ui/settingspages.js fills the markup). No DOM.

// uptimeText: "45 s", "12 min", "3 h 05 min", "2 d 4 h".
export function uptimeText(sec) {
  const s = Math.max(0, Math.floor(Number(sec) || 0));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} h ${String(m % 60).padStart(2, "0")} min`;
  return `${Math.floor(h / 24)} d ${h % 24} h`;
}

// versionOf: the revision part of a build id "<revision>-<web hash>",
// shortened to 7 characters ("dev" when the binary has no VCS info).
export function versionOf(build) {
  const rev = String(build || "").split("-")[0];
  if (!rev) return "";
  const dirty = rev.endsWith("+dirty");
  const base = dirty ? rev.slice(0, -"+dirty".length) : rev;
  return base.slice(0, 7) + (dirty ? "+dirty" : "");
}

// displayName: the custom name when set, else the default, else "guest".
export function displayName(custom, fallback) {
  return String(custom || "").trim() || String(fallback || "").trim() || "guest";
}

// vaultModeText: how the vault is unlocked, from GET /api/vault.
export function vaultModeText(v) {
  if (!v) return { label: "", help: "" };
  if (v.mode === "keychain") return { label: "OS keychain", help: "The key that opens the vault is kept in your operating system's keychain. No passphrase to type." };
  if (v.mode === "passphrase") {
    return {
      label: "Passphrase",
      help: v.keyringOff ? "The OS keychain is off for this run (KEEL_KEYRING=off), so Keel asks for the passphrase." : "Keel asks for the passphrase when it starts.",
    };
  }
  return { label: "Not set up", help: "The vault is created on first run." };
}

// signalText: the Signal server field from /api/runtime or PUT
// /api/share/signal ({signalURL, signalSource, signalSaved}): the input's
// value (the saved server), its placeholder and the help line.
export function signalText(info) {
  const url = String(info?.signalURL || "").trim();
  const saved = String(info?.signalSaved || "").trim();
  const placeholder = "https://signal.example.com";
  const relay = "Join links go through this server. It never sees the terminal or the key in the link's #fragment.";
  switch (info?.signalSource) {
    case "env":
      return { value: saved, placeholder, help: `KEEL_SIGNAL is set for this run, so Keel uses ${url}. The server saved here is used when it is not set.` };
    case "settings":
      return { value: saved, placeholder, help: relay };
    case "build":
      return { value: saved, placeholder: url, help: `This copy of Keel was built with ${url}. Enter another server to use it instead. ${relay}` };
  }
  return { value: saved, placeholder, help: "Share is off until you enter a keel-signal server: one you run yourself, or one someone runs for you." };
}

// stopMessage: the alertdialog text for Stop runtime.
export function stopMessage(sessions) {
  const n = Number(sessions) || 0;
  const what = n === 1 ? "the 1 running session" : n > 1 ? `all ${n} running sessions` : "every running session";
  return `This ends ${what} and every port forward, then closes keel serve. Reopen Keel to start it again.`;
}
