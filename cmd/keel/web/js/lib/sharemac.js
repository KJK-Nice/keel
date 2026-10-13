// End-to-end check of a share's WebRTC peer, independent of the signal
// server. Mirrors internal/share/verify.go; a copy is served to the browser
// join page as internal/signal/web/sharemac.js (kept identical by a test).
//
// The share link carries a secret after "#" that is never sent to the
// server. Each side sends HMAC-SHA256(secret, role + "\n" + its DTLS
// fingerprints) with its SDP and checks the other side's, so a signal
// server that swaps fingerprints to sit in the middle is detected.
//
// SHA-256 is implemented here rather than with crypto.subtle, which only
// exists in secure contexts and the desktop page is served from
// http://127.0.0.1.

const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

// sha256 returns the digest of bytes (Uint8Array) as a Uint8Array.
export function sha256(bytes) {
  const len = bytes.length;
  const padded = new Uint8Array(((len + 9 + 63) >> 6) << 6);
  padded.set(bytes);
  padded[len] = 0x80;
  const view = new DataView(padded.buffer);
  view.setUint32(padded.length - 8, Math.floor(len / 0x20000000), false);
  view.setUint32(padded.length - 4, (len << 3) >>> 0, false);
  const h = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ]);
  const w = new Uint32Array(64);
  const rotr = (x, n) => (x >>> n) | (x << (32 - n));
  for (let off = 0; off < padded.length; off += 64) {
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(off + i * 4, false);
    for (let i = 16; i < 64; i++) {
      const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
      const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
    }
    let [a, b, c, d, e, f, g, hh] = h;
    for (let i = 0; i < 64; i++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (hh + S1 + ch + K[i] + w[i]) >>> 0;
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) >>> 0;
      hh = g;
      g = f;
      f = e;
      e = (d + t1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) >>> 0;
    }
    h[0] = (h[0] + a) >>> 0;
    h[1] = (h[1] + b) >>> 0;
    h[2] = (h[2] + c) >>> 0;
    h[3] = (h[3] + d) >>> 0;
    h[4] = (h[4] + e) >>> 0;
    h[5] = (h[5] + f) >>> 0;
    h[6] = (h[6] + g) >>> 0;
    h[7] = (h[7] + hh) >>> 0;
  }
  const out = new Uint8Array(32);
  const ov = new DataView(out.buffer);
  for (let i = 0; i < 8; i++) ov.setUint32(i * 4, h[i], false);
  return out;
}

const utf8 = (s) => new TextEncoder().encode(s);
const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");

// hmacSha256Hex returns HMAC-SHA256(key, message) as hex; key and message
// are strings (UTF-8).
export function hmacSha256Hex(key, message) {
  let k = utf8(key);
  if (k.length > 64) k = sha256(k);
  const block = new Uint8Array(64);
  block.set(k);
  const inner = new Uint8Array(64 + utf8(message).length);
  const outer = new Uint8Array(64 + 32);
  for (let i = 0; i < 64; i++) {
    inner[i] = block[i] ^ 0x36;
    outer[i] = block[i] ^ 0x5c;
  }
  inner.set(utf8(message), 64);
  outer.set(sha256(inner), 64);
  return hex(sha256(outer));
}

// sdpFingerprints returns the SDP's a=fingerprint values, normalized
// (lowercase, single spaces), sorted and de-duplicated.
export function sdpFingerprints(sdp) {
  const out = new Set();
  for (const raw of String(sdp || "").split("\n")) {
    const line = raw.trim();
    if (!line.startsWith("a=fingerprint:")) continue;
    const v = line.slice("a=fingerprint:".length).trim().split(/\s+/).join(" ").toLowerCase();
    if (v) out.add(v);
  }
  return [...out].sort();
}

// sdpMac is the MAC a peer in role ("host" or "join") sends with sdp; ""
// when there is no secret or no fingerprint to vouch for.
export function sdpMac(secret, role, sdp) {
  const fps = sdpFingerprints(sdp);
  if (!secret || fps.length === 0) return "";
  return hmacSha256Hex(secret, `${role}\n${fps.join("\n")}`);
}

// checkSdpMac reports whether mac vouches for sdp from a peer in role.
export function checkSdpMac(secret, role, sdp, mac) {
  const want = sdpMac(secret, role, sdp);
  if (!want || typeof mac !== "string" || mac.length !== want.length) return false;
  let diff = 0;
  for (let i = 0; i < want.length; i++) diff |= want.charCodeAt(i) ^ mac.charCodeAt(i);
  return diff === 0;
}

// createIceQueue holds the host's ICE candidates until the offer has been
// applied (addIceCandidate fails before there is a remote description);
// answerOffer flushes it.
export function createIceQueue(pc) {
  let ready = false;
  const early = [];
  const add = async (c) => {
    try {
      await pc.addIceCandidate(c);
    } catch {
      /* a bad candidate is not fatal */
    }
  };
  return {
    async add(c) {
      if (!ready) {
        if (early.length < 64) early.push(c);
        return;
      }
      await add(c);
    },
    async flush() {
      ready = true;
      for (const c of early.splice(0)) await add(c);
    },
  };
}

// answerOffer is the joiner's half of the exchange: it refuses an offer
// whose MAC doesn't match the link secret (a signal server in the middle),
// then answers and returns the signal message to send back, MAC included.
// Pass the createIceQueue the joiner feeds candidates into.
export async function answerOffer(pc, secret, offer, ice) {
  if (!checkSdpMac(secret, "host", offer?.sdp, offer?.mac)) {
    throw new Error("host could not be verified: the link is wrong or the signal server tampered with the connection");
  }
  await pc.setRemoteDescription({ type: "offer", sdp: offer.sdp });
  await ice?.flush();
  const answer = await pc.createAnswer();
  await pc.setLocalDescription(answer);
  const sdp = pc.localDescription?.sdp || answer.sdp;
  return { kind: "answer", sdp, mac: sdpMac(secret, "join", sdp) };
}
