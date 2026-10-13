import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { openJoinPeer, signalURL } from "../lib/joinpeer.js";
import { sdpMac } from "../lib/sharemac.js";

const KEY = "00112233445566778899aabbccddeeff";
const HOST_SDP = "v=0\r\na=fingerprint:sha-256 AA:BB\r\n";
const JOIN_SDP = "v=0\r\na=fingerprint:sha-256 CC:DD\r\n";

class FakeWS {
  constructor(url) {
    this.url = url;
    this.readyState = 1;
    this.sent = [];
    this.listeners = {};
    this.closed = false;
  }
  addEventListener(type, fn) {
    (this.listeners[type] ||= []).push(fn);
  }
  send(data) {
    this.sent.push(JSON.parse(data));
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.readyState = 3;
    this.emit("close", {});
  }
  emit(type, ev) {
    for (const fn of this.listeners[type] || []) fn(ev);
  }
  deliver(msg) {
    this.emit("message", { data: JSON.stringify(msg) });
  }
}

class FakePC {
  constructor(cfg) {
    this.cfg = cfg;
    this.connectionState = "new";
    this.calls = [];
    this.closed = false;
  }
  async setRemoteDescription(d) {
    this.calls.push(["remote", d.sdp]);
  }
  async createAnswer() {
    return { type: "answer", sdp: JOIN_SDP };
  }
  async setLocalDescription(d) {
    this.localDescription = d;
    this.calls.push(["local"]);
  }
  async addIceCandidate(c) {
    this.calls.push(["ice", c.candidate]);
  }
  close() {
    this.closed = true;
  }
}

function open(events = []) {
  const on = {};
  for (const name of ["waiting", "failed", "hostLeft", "signalingError", "signalingClosed", "connectionLost", "channel"]) {
    on[name] = (...args) => events.push([name, ...args]);
  }
  const peer = openJoinPeer({
    origin: "https://sig.example",
    room: "r1",
    key: KEY,
    name: "bob",
    on,
    WebSocketImpl: FakeWS,
    PeerConnectionImpl: FakePC,
  });
  return { peer, ws: peer.ws, pc: peer.pc, events };
}

test("signalURL builds the join socket URL", () => {
  assert.equal(signalURL("https://sig.example", "r 1", "b&b"), "wss://sig.example/ws?role=join&room=r+1&name=b%26b");
  assert.equal(signalURL("http://127.0.0.1:47242/", "r", ""), "ws://127.0.0.1:47242/ws?role=join&room=r&name=guest");
});

test("waits, then answers a verified offer; early candidates wait for it", async () => {
  const { peer, ws, pc, events } = open();
  ws.deliver({ type: "signal", kind: "pending" });
  ws.deliver({ type: "signal", kind: "ice", candidate: "early" });
  ws.deliver({ type: "signal", kind: "offer", sdp: HOST_SDP, mac: sdpMac(KEY, "host", HOST_SDP) });
  ws.deliver({ type: "signal", kind: "ice", candidate: "late" });
  await peer.settled();
  assert.deepEqual(events, [["waiting"]]);
  assert.deepEqual(pc.calls, [["remote", HOST_SDP], ["ice", "early"], ["local"], ["ice", "late"]]);
  assert.deepEqual(ws.sent, [{ type: "signal", kind: "answer", sdp: JOIN_SDP, mac: sdpMac(KEY, "join", JOIN_SDP) }]);
});

test("an offer that fails verification closes everything, once", async () => {
  const { peer, ws, pc, events } = open();
  ws.deliver({ type: "signal", kind: "offer", sdp: HOST_SDP, mac: sdpMac("ffeeddccbbaa99887766554433221100", "host", HOST_SDP) });
  await peer.settled();
  assert.equal(events.length, 1);
  assert.equal(events[0][0], "failed");
  assert.match(events[0][1], /could not be verified/);
  assert.ok(pc.closed && ws.closed);
  assert.deepEqual(ws.sent, []);
});

test("denied ends the join without a 'signaling closed' afterwards", async () => {
  const { peer, ws, events } = open();
  ws.deliver({ type: "signal", kind: "denied" });
  await peer.settled();
  assert.deepEqual(events, [["failed", "The host declined your request to join."]]);
});

test("host leaving, signaling errors and drops are reported", async () => {
  const { peer, ws, pc, events } = open();
  ws.deliver({ type: "error", name: "room not found" });
  ws.deliver({ type: "leave" });
  await peer.settled();
  pc.connectionState = "failed";
  pc.onconnectionstatechange();
  ws.close();
  assert.deepEqual(events, [["signalingError", "room not found"], ["hostLeft"], ["connectionLost"], ["signalingClosed"]]);
});

test("close() is quiet", () => {
  const { peer, ws, events } = open();
  peer.close();
  assert.ok(ws.closed);
  assert.deepEqual(events, []);
});

test("the data channel is handed over in binary mode, and ICE goes out", () => {
  const { ws, pc, events } = open();
  const dc = {};
  pc.ondatachannel({ channel: dc });
  assert.equal(dc.binaryType, "arraybuffer");
  assert.equal(events[0][1], dc);
  pc.onicecandidate({ candidate: { candidate: "c1", sdpMid: "0", sdpMLineIndex: 0 } });
  pc.onicecandidate({ candidate: null });
  assert.deepEqual(ws.sent, [{ type: "signal", kind: "ice", candidate: "c1", sdpMid: "0", sdpMLineIndex: 0 }]);
});

test("internal/signal/web/joinpeer.js is the same file", () => {
  const desktop = readFileSync(new URL("../lib/joinpeer.js", import.meta.url), "utf8");
  const page = readFileSync(new URL("../../../../../internal/signal/web/joinpeer.js", import.meta.url), "utf8");
  assert.equal(page, desktop, "copy cmd/keel/web/js/lib/joinpeer.js to internal/signal/web/joinpeer.js");
});
