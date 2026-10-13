// The joiner's side of a share: signaling over the keel-signal WebSocket,
// then a WebRTC data channel to the host. Shared by the desktop joiner
// (term.js) and the browser join page; a copy is served by keel-signal as
// /joinpeer.js (internal/signal/web/joinpeer.js, kept identical by a test).
//
// Nothing here touches the DOM. The caller gets callbacks:
//   waiting()           the host has been asked to let us in
//   failed(message)     denied, or the host failed verification; closed
//   hostLeft()          the host ended the share
//   signalingError()    the signal socket errored
//   signalingClosed()   the signal socket closed before we connected
//   connectionLost()    the WebRTC connection failed or dropped
//   channel(dc)         the host's data channel arrived (not yet open)

import { answerOffer, createIceQueue } from "./sharemac.js";

export const STUN = [{ urls: "stun:stun.l.google.com:19302" }];

export function signalURL(origin, room, name) {
  const u = new URL(origin);
  u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
  u.pathname = "/ws";
  u.search = new URLSearchParams({ role: "join", room, name: name || "guest" }).toString();
  u.hash = "";
  return u.toString();
}

export function openJoinPeer({
  origin,
  room,
  key,
  name,
  on = {},
  WebSocketImpl = globalThis.WebSocket,
  PeerConnectionImpl = globalThis.RTCPeerConnection,
  iceServers = STUN,
}) {
  const call = (fn, ...args) => {
    try {
      on[fn]?.(...args);
    } catch (err) {
      console.error(err);
    }
  };
  const ws = new WebSocketImpl(signalURL(origin, room, name));
  const pc = new PeerConnectionImpl({ iceServers });
  const ice = createIceQueue(pc);
  let failed = false;

  const send = (msg) => {
    if (ws.readyState === 1) ws.send(JSON.stringify({ type: "signal", ...msg }));
  };
  const fail = (message) => {
    if (failed) return;
    failed = true;
    pc.close();
    ws.close();
    call("failed", message);
  };

  pc.onicecandidate = (ev) => {
    if (!ev.candidate) return;
    send({
      kind: "ice",
      candidate: ev.candidate.candidate,
      sdpMid: ev.candidate.sdpMid,
      sdpMLineIndex: ev.candidate.sdpMLineIndex,
    });
  };
  pc.onconnectionstatechange = () => {
    if (!failed && (pc.connectionState === "failed" || pc.connectionState === "disconnected")) call("connectionLost");
  };
  pc.ondatachannel = (ev) => {
    ev.channel.binaryType = "arraybuffer";
    call("channel", ev.channel);
  };

  async function onSignal(raw) {
    let msg;
    try {
      msg = JSON.parse(raw);
    } catch {
      return;
    }
    if (msg.type === "error") {
      call("signalingError", msg.name || "signaling error");
      return;
    }
    if (msg.type === "leave") {
      pc.close();
      call("hostLeft");
      return;
    }
    if (msg.type !== "signal") return;
    switch (msg.kind) {
      case "pending":
        call("waiting");
        break;
      case "denied":
        fail("The host declined your request to join.");
        break;
      case "offer":
        if (!msg.sdp) break;
        try {
          send(await answerOffer(pc, key, msg, ice));
        } catch (err) {
          fail(err.message);
        }
        break;
      case "ice":
        if (msg.candidate) {
          await ice.add({ candidate: msg.candidate, sdpMid: msg.sdpMid, sdpMLineIndex: msg.sdpMLineIndex });
        }
        break;
    }
  }

  // One message at a time: a candidate must not race ahead of its offer.
  let chain = Promise.resolve();
  ws.addEventListener("message", (ev) => {
    chain = chain.then(() => onSignal(ev.data)).catch(() => {});
  });
  ws.addEventListener("error", () => call("signalingError", "Signaling failed"));
  ws.addEventListener("close", () => {
    if (!failed && pc.connectionState !== "connected") call("signalingClosed");
  });

  return {
    pc,
    ws,
    // settled resolves once every signaling message received so far has
    // been handled (for tests).
    settled: () => chain,
    close() {
      failed = true;
      pc.close();
      ws.close();
    },
  };
}
