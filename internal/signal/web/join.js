import { answerOffer } from "/sharemac.js";

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

  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/ws?role=join&room=${encodeURIComponent(room)}&name=${encodeURIComponent(name)}`);
  const pc = new RTCPeerConnection({ iceServers: [{ urls: "stun:stun.l.google.com:19302" }] });
  let dc = null;
  let driving = false;

  function sendSignal(msg) {
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "signal", ...msg }));
  }

  pc.onicecandidate = (ev) => {
    if (ev.candidate) {
      sendSignal({
        kind: "ice",
        candidate: ev.candidate.candidate,
        sdpMid: ev.candidate.sdpMid,
        sdpMLineIndex: ev.candidate.sdpMLineIndex,
      });
    }
  };

  pc.onconnectionstatechange = () => {
    if (pc.connectionState === "failed" || pc.connectionState === "disconnected") {
      setStatus("Connection lost");
    }
  };

  pc.ondatachannel = (ev) => {
    dc = ev.channel;
    dc.binaryType = "arraybuffer";
    dc.onopen = () => setStatus("Following — wait for the host to pass control", true);
    dc.onclose = () => setStatus("Detached");
    dc.onmessage = (ev) => {
      if (typeof ev.data === "string") {
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
        return;
      }
      term.write(new Uint8Array(ev.data));
    };
  };

  term.onData((data) => {
    if (!driving || !dc || dc.readyState !== "open") return;
    dc.send(new TextEncoder().encode(data));
  });

  function fail(text) {
    setStatus(text);
    term.writeln(`\r\n\x1b[38;2;248;113;113m${text}\x1b[0m`);
    pc.close();
    ws.close();
  }

  // One message at a time, so ICE candidates can't race ahead of the offer.
  let chain = Promise.resolve();
  ws.onmessage = (ev) => {
    chain = chain.then(() => onSignal(ev)).catch(() => {});
  };
  async function onSignal(ev) {
    const msg = JSON.parse(ev.data);
    if (msg.type === "error") {
      setStatus(msg.name || "error");
      return;
    }
    if (msg.type === "leave") {
      setStatus("Host left");
      pc.close();
      return;
    }
    if (msg.type !== "signal") return;
    if (msg.kind === "pending") {
      setStatus("Waiting for the host to let you in…");
      return;
    }
    if (msg.kind === "denied") {
      fail("The host declined your request to join.");
      return;
    }
    if (msg.kind === "offer" && msg.sdp) {
      try {
        sendSignal(await answerOffer(pc, key, msg));
      } catch (err) {
        fail(err.message);
        return;
      }
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
  }
  ws.onerror = () => setStatus("Signaling failed");
  ws.onclose = () => {
    if (pc.connectionState !== "connected") setStatus("Signaling closed");
  };

  window.addEventListener("resize", () => {
    if (!driving || !dc || dc.readyState !== "open") return;
    dc.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
  });
}
