(() => {
  const params = new URLSearchParams(location.search);
  const room = params.get("room") || "";
  const name = params.get("name") || localStorage.getItem("keel-join-name") || "guest";
  const statusEl = document.getElementById("status");
  const TerminalCtor = window.Terminal;
  if (typeof TerminalCtor !== "function") {
    statusEl.textContent = "Terminal library failed to load";
    return;
  }
  const term = new TerminalCtor({
    cursorBlink: true,
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    fontSize: 12,
    theme: { background: "#0a0a0a", foreground: "#e4e4e4", cursor: "#82a7ff" },
    disableStdin: true,
  });
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

  ws.onmessage = async (ev) => {
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
  };
  ws.onerror = () => setStatus("Signaling failed");
  ws.onclose = () => {
    if (pc.connectionState !== "connected") setStatus("Signaling closed");
  };

  window.addEventListener("resize", () => {
    if (!driving || !dc || dc.readyState !== "open") return;
    dc.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
  });
})();
