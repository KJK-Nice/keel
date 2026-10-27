import { test } from "node:test";
import assert from "node:assert/strict";
import { fingerprintText, hostTestText, keyTypeLabel, trustOffer } from "../lib/hosttest.js";
import { createHostTest } from "../ui/hosttest.js";

const unknown = { kind: "unknown-host", hostname: "db:22", fingerprint: "SHA256:abc", keyType: "ssh-ed25519", trustToken: "t1" };
const changed = { kind: "host-key-mismatch", hostname: "db:22", fingerprint: "SHA256:new", expected: "SHA256:old", keyType: "ecdsa-sha2-nistp256", trustToken: "t2" };

test("key types read as ssh-keygen prints them", () => {
  assert.equal(keyTypeLabel("ssh-ed25519"), "ED25519");
  assert.equal(keyTypeLabel("sk-ecdsa-sha2-nistp256@openssh.com"), "ECDSA");
  assert.equal(keyTypeLabel("rsa-sha2-512"), "RSA");
  assert.equal(keyTypeLabel("ssh-dss"), "DSA");
  assert.equal(keyTypeLabel("x-new"), "X-NEW");
  assert.equal(keyTypeLabel(undefined), "");
});

test("each result kind has its state, label and explanation", () => {
  assert.deepEqual(hostTestText({ kind: "ok", latencyMs: 42 }), { state: "live", short: "Connected in 42 ms", detail: "" });
  assert.equal(hostTestText({ kind: "auth", message: "denied" }).short, "Authentication failed");
  assert.equal(hostTestText(unknown).state, "connecting");
  assert.equal(hostTestText(changed).short, "Host key changed");
  assert.equal(hostTestText({ kind: "timeout", message: "" }).short, "Timed out");
  assert.equal(hostTestText({ kind: "cancelled" }).state, "");
  assert.deepEqual(hostTestText({ kind: "weird", message: "boom" }), { state: "dead", short: "Could not connect", detail: "boom" });
});

test("a jump host names the hop that failed", () => {
  assert.match(hostTestText({ kind: "dns", hop: "jump", hopName: "bastion", message: "" }).detail, /^Jump host bastion: /);
  assert.match(hostTestText({ kind: "refused", hop: "target", message: "" }).detail, /^Through the jump host: /);
});

test("only an unknown or changed key with a token can be trusted", () => {
  assert.deepEqual(trustOffer(unknown), { token: "t1", hostname: "db:22", fingerprint: "SHA256:abc", replace: false });
  assert.deepEqual(trustOffer(changed), { token: "t2", hostname: "db:22", fingerprint: "SHA256:new", replace: true });
  assert.equal(trustOffer({ ...unknown, trustToken: "" }), null);
  assert.equal(trustOffer({ ...unknown, fingerprint: "" }), null);
  assert.equal(trustOffer({ kind: "auth", fingerprint: "x", trustToken: "t" }), null);
  assert.equal(trustOffer(null), null);
  assert.equal(fingerprintText(unknown), "ED25519 SHA256:abc");
  assert.equal(fingerprintText(changed), "ECDSA SHA256:new (saved: SHA256:old)");
  assert.equal(fingerprintText({ kind: "ok" }), "");
});

// Just enough DOM: the dialog's #host-test* elements by id.
function fakeDoc() {
  const ids = ["host-test", "host-test-state", "host-test-dot", "host-test-text", "host-test-detail", "host-test-message", "host-test-fp", "host-test-actions", "host-test-trust"];
  const els = Object.fromEntries(ids.map((id) => [id, { id, hidden: true, textContent: "", className: "" }]));
  return { els, getElementById: (id) => els[id] || null };
}

function setup(results, { confirm = true } = {}) {
  const doc = fakeDoc();
  const calls = [];
  const toasts = [];
  let values = { address: "db", password: "" };
  let trustedCount = 0;
  const api = async (path, opts) => {
    calls.push({ path, body: opts.body });
    if (path === "/api/known-hosts") return {};
    const next = results.shift();
    if (next instanceof Error) throw next;
    if (typeof next === "function") return next(opts);
    return next;
  };
  const t = createHostTest({
    doc,
    api,
    draft: () => values,
    confirmReplace: async () => confirm,
    trusted: () => trustedCount++,
    toast: (m, e) => toasts.push([m, !!e]),
  });
  return { t, doc, calls, toasts, setValues: (v) => (values = v), trusted: () => trustedCount };
}

test("a test sends the dialog's values and shows the result", async () => {
  const { t, doc, calls } = setup([{ kind: "ok", latencyMs: 7 }]);
  await t.run();
  assert.equal(calls[0].path, "/api/hosts/test");
  assert.deepEqual(JSON.parse(calls[0].body), { address: "db", password: "" });
  assert.equal(doc.els["host-test-text"].textContent, "Connected in 7 ms");
  assert.equal(doc.els["host-test-state"].hidden, false);
  assert.equal(doc.els["host-test-state"].className, "k-state k-state--live");
  assert.equal(doc.els["host-test"].textContent, "Test connection");
  assert.equal(doc.els["host-test-actions"].hidden, true);
});

test("an unknown key offers Trust and save, which saves that key and tests again", async () => {
  const { t, doc, calls, toasts, trusted } = setup([unknown, { kind: "ok", latencyMs: 3 }]);
  await t.run();
  assert.equal(doc.els["host-test-actions"].hidden, false);
  assert.equal(doc.els["host-test-trust"].textContent, "Trust and save");
  assert.equal(doc.els["host-test-fp"].textContent, "ED25519 SHA256:abc");
  await t.trust();
  assert.deepEqual(JSON.parse(calls[1].body), { token: "t1", hostname: "db:22", fingerprint: "SHA256:abc", replace: false });
  assert.deepEqual(toasts, [["Host key saved", false]]);
  assert.equal(trusted(), 1);
  assert.equal(calls[2].path, "/api/hosts/test");
  assert.equal(doc.els["host-test-text"].textContent, "Connected in 3 ms");
});

test("replacing a changed key asks first; no means nothing is sent", async () => {
  const { t, doc, calls } = setup([changed], { confirm: false });
  await t.run();
  assert.equal(doc.els["host-test-trust"].textContent, "Replace saved key");
  await t.trust();
  assert.equal(calls.length, 1);
});

test("an edit drops the result and its offer; an event that changes nothing keeps it", async () => {
  const { t, doc, calls, setValues } = setup([unknown]);
  await t.run();
  t.edited();
  assert.equal(doc.els["host-test-actions"].hidden, false, "same values: the result stays");
  setValues({ address: "db2", password: "" });
  t.edited();
  assert.equal(doc.els["host-test-state"].hidden, true);
  assert.equal(doc.els["host-test-actions"].hidden, true);
  await t.trust();
  assert.equal(calls.length, 1, "Trust and save has nothing to send after an edit");
});

test("pressing again cancels the test under way, and a late result is dropped", async () => {
  let release;
  const { t, doc } = setup([
    (opts) =>
      new Promise((resolve, reject) => {
        release = resolve;
        opts.signal.addEventListener("abort", () => reject(new Error("aborted")));
      }),
  ]);
  const first = t.run();
  assert.equal(doc.els["host-test"].textContent, "Cancel test");
  t.run();
  await first;
  assert.equal(doc.els["host-test-text"].textContent, "Test cancelled");
  assert.equal(doc.els["host-test"].textContent, "Test connection");
  release?.({ kind: "ok", latencyMs: 1 });
});

test("a refill while a test runs drops its result", async () => {
  let release;
  const { t, doc } = setup([() => new Promise((resolve) => (release = resolve))]);
  const run = t.run();
  t.reset();
  release({ kind: "ok", latencyMs: 1 });
  await run;
  assert.equal(doc.els["host-test-state"].hidden, true);
  assert.equal(doc.els["host-test-text"].textContent, "");
});

test("a failed request says it could not test", async () => {
  const { t, doc } = setup([new Error("vault locked")]);
  await t.run();
  assert.equal(doc.els["host-test-text"].textContent, "Could not test");
  assert.equal(doc.els["host-test-message"].textContent, "vault locked");
  assert.equal(doc.els["host-test-detail"].hidden, false);
});
