import { openJoinPeer } from "/joinpeer.js";

// Loaded as a module, so top-level return isn't allowed: everything runs in main().
main();

function main() {
  const params = new URLSearchParams(location.search);
  const room = params.get("room") || "";
  // The #k secret is never sent to this server; it verifies the host's peer.
  const key = new URLSearchParams(location.hash.replace(/^#/, "")).get("k") || "";
  const name = params.get("name") || localStorage.getItem("keel-join-name") || "guest";
  const statusEl = document.getElementById("status");
  const TerminalCtor = window.Terminal;
  if (typeof TerminalCtor !== "function") {
    statusEl.textContent = "Terminal library failed to load";
    return;
  }

  function loadAddon(term, Ctor, ...args) {
    if (typeof Ctor !== "function" || typeof term.loadAddon !== "function") return null;
    try {
      const addon = new Ctor(...args);
      term.loadAddon(addon);
      return addon;
    } catch {
      return null;
    }
  }

  function openJoinLink(_ev, uri) {
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

  function paintJoinProgress(st) {
    const el = document.getElementById("progress");
    if (!el) return;
    const kind = st?.state ?? 0;
    const value = Math.max(0, Math.min(100, Number(st?.value) || 0));
    const bar = el.querySelector(".join-progress-bar");
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

  const term = new TerminalCtor({
    cursorBlink: true,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    fontSize: 12,
    theme: { background: "#0a0a0a", foreground: "#e4e4e4", cursor: "#82a7ff" },
    disableStdin: true,
  });
  loadAddon(term, window.UnicodeGraphemesAddon?.UnicodeGraphemesAddon || window.UnicodeGraphemesAddon);
  loadAddon(term, window.WebLinksAddon?.WebLinksAddon || window.WebLinksAddon, openJoinLink);
  const progress = loadAddon(term, window.ProgressAddon?.ProgressAddon || window.ProgressAddon);
  progress?.onChange?.(paintJoinProgress);
  term.open(document.getElementById("term"));
  term.focus();

  function setStatus(text, follow) {
    statusEl.textContent = text;
    statusEl.classList.toggle("follow", !!follow);
  }

  if (!room) {
    setStatus("Missing room id");
    return;
  }
  if (!/^[0-9a-f]{32}$/.test(key)) {
    setStatus("This link has no key (#k=…): ask the host for a new Share link");
    return;
  }

  let dc = null;
  let driving = false;

  function attachChannel(channel) {
    dc = channel;
    dc.onopen = () => setStatus("Following — wait for the host to pass control", true);
    dc.onclose = () => setStatus("Detached");
    dc.onmessage = (ev) => {
      if (typeof ev.data !== "string") {
        term.write(new Uint8Array(ev.data));
        return;
      }
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (msg.type === "error") {
        term.writeln(`\r\n\x1b[38;2;248;113;113m${msg.message}\x1b[0m`);
        return;
      }
      if (msg.you) driving = msg.you.role === "driver";
      term.options.disableStdin = !driving;
      if (msg.title) {
        setStatus(driving ? `${msg.title} · driving` : `${msg.title} · following`, !driving);
      }
      if (msg.type === "control" && msg.message) setStatus(msg.message, !driving);
    };
  }

  term.onData((data) => {
    if (!driving || !dc || dc.readyState !== "open") return;
    dc.send(new TextEncoder().encode(data));
  });

  openJoinPeer({
    origin: location.origin,
    room,
    key,
    name,
    on: {
      waiting: () => setStatus("Waiting for the host to let you in…"),
      failed: (text) => {
        setStatus(text);
        term.writeln(`\r\n\x1b[38;2;248;113;113m${text}\x1b[0m`);
      },
      hostLeft: () => setStatus("Host left"),
      signalingError: (text) => setStatus(text),
      signalingClosed: () => setStatus("Signaling closed"),
      connectionLost: () => setStatus("Connection lost"),
      channel: attachChannel,
    },
  });

  window.addEventListener("resize", () => {
    if (!driving || !dc || dc.readyState !== "open") return;
    dc.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
  });
}
