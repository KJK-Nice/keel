import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash, createHmac, randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { answerOffer, checkSdpMac, createIceQueue, hmacSha256Hex, sdpFingerprints, sdpMac, sha256 } from "../lib/sharemac.js";

const hex = (b) => Buffer.from(b).toString("hex");

test("sha256 matches node:crypto across block boundaries", () => {
  for (const n of [0, 1, 55, 56, 63, 64, 65, 119, 120, 1000]) {
    const data = randomBytes(n);
    assert.equal(hex(sha256(new Uint8Array(data))), createHash("sha256").update(data).digest("hex"), `len ${n}`);
  }
});

test("hmacSha256Hex matches node:crypto, including long keys", () => {
  for (const key of ["k", "00112233445566778899aabbccddeeff", "x".repeat(100)]) {
    for (const msg of ["", "host\nsha-256 ab:cd", "ü".repeat(70)]) {
      assert.equal(hmacSha256Hex(key, msg), createHmac("sha256", key).update(msg).digest("hex"));
    }
  }
});

const sampleSDP =
  "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\n" +
  "a=fingerprint:sha-256 AB:CD:EF:01\r\n" +
  "m=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n" +
  "a=fingerprint:sha-256  ab:cd:ef:01\r\n" +
  "a=fingerprint:sha-512 99:88\r\n";

test("sdpFingerprints normalizes, sorts and de-duplicates", () => {
  assert.deepEqual(sdpFingerprints(sampleSDP), ["sha-256 ab:cd:ef:01", "sha-512 99:88"]);
  assert.deepEqual(sdpFingerprints("v=0\r\n"), []);
});

test("sdpMac matches the Go host (internal/share/verify_test.go vector)", () => {
  assert.equal(
    sdpMac("00112233445566778899aabbccddeeff", "host", sampleSDP),
    "bfbfbdb60dc9081a69052a881a101013c4e0d64a153a6cecee59689822f0df5a",
  );
});

test("checkSdpMac rejects tampering", () => {
  const secret = "00112233445566778899aabbccddeeff";
  const mac = sdpMac(secret, "join", sampleSDP);
  assert.equal(checkSdpMac(secret, "join", sampleSDP, mac), true);
  assert.equal(checkSdpMac(secret, "host", sampleSDP, mac), false, "wrong role");
  assert.equal(checkSdpMac("ff" + secret.slice(2), "join", sampleSDP, mac), false, "wrong secret");
  assert.equal(checkSdpMac(secret, "join", "a=fingerprint:sha-256 00:00\r\n", mac), false, "swapped fingerprint");
  assert.equal(checkSdpMac(secret, "join", sampleSDP, ""), false, "missing mac");
  assert.equal(checkSdpMac("", "join", sampleSDP, sdpMac("", "join", sampleSDP)), false, "no secret");
});

test("the join page serves an identical copy", () => {
  const desktop = readFileSync(new URL("../lib/sharemac.js", import.meta.url), "utf8");
  const page = readFileSync(new URL("../../../../../internal/signal/web/sharemac.js", import.meta.url), "utf8");
  assert.equal(page, desktop, "copy cmd/keel/web/js/lib/sharemac.js to internal/signal/web/sharemac.js");
});

function fakePeer(answerSdp) {
  const calls = [];
  return {
    calls,
    localDescription: null,
    async setRemoteDescription(d) { calls.push(["remote", d]); },
    async createAnswer() { return { type: "answer", sdp: answerSdp }; },
    async setLocalDescription(d) { this.localDescription = d; calls.push(["local", d]); },
  };
}

test("answerOffer answers a verified offer with the joiner's MAC", async () => {
  const secret = "00112233445566778899aabbccddeeff";
  const answerSdp = "v=0\r\na=fingerprint:sha-256 11:22\r\n";
  const pc = fakePeer(answerSdp);
  const out = await answerOffer(pc, secret, { sdp: sampleSDP, mac: sdpMac(secret, "host", sampleSDP) });
  assert.deepEqual(out, { kind: "answer", sdp: answerSdp, mac: sdpMac(secret, "join", answerSdp) });
  assert.equal(pc.calls[0][0], "remote");
});

test("answerOffer refuses an offer without a matching MAC and never touches the peer", async () => {
  const secret = "00112233445566778899aabbccddeeff";
  for (const offer of [
    { sdp: sampleSDP },
    { sdp: sampleSDP, mac: sdpMac("ffeeddccbbaa99887766554433221100", "host", sampleSDP) },
    { sdp: sampleSDP, mac: sdpMac(secret, "join", sampleSDP) },
  ]) {
    const pc = fakePeer("x");
    await assert.rejects(answerOffer(pc, secret, offer), /could not be verified/);
    assert.equal(pc.calls.length, 0);
  }
});

test("createIceQueue holds candidates until answerOffer applies the offer", async () => {
  const secret = "00112233445566778899aabbccddeeff";
  const pc = fakePeer("v=0\r\na=fingerprint:sha-256 11:22\r\n");
  pc.addIceCandidate = async (c) => pc.calls.push(["ice", c]);
  const ice = createIceQueue(pc);
  await ice.add({ candidate: "early" });
  assert.equal(pc.calls.length, 0);
  await answerOffer(pc, secret, { sdp: sampleSDP, mac: sdpMac(secret, "host", sampleSDP) }, ice);
  assert.deepEqual(pc.calls.map((c) => c[0]), ["remote", "ice", "local"]);
  await ice.add({ candidate: "late" });
  assert.deepEqual(pc.calls.at(-1), ["ice", { candidate: "late" }]);
});
