import { test } from "node:test";
import assert from "node:assert/strict";
import * as V from "../lib/vaultview.js";

// The vault lists are templ (internal/ui/lists.templ, tested in Go) pushed
// over the Datastar stream. These cover the markup term.js still paints
// from the page's selection.

const evil = `<img src=x onerror=alert(1)>`;
const noRawTag = (html) => assert.ok(!html.includes("<img"), `unescaped markup in:\n${html}`);

test("what term.js paints escapes what the vault holds", () => {
  noRawTag(V.snippetPreview(evil).html);
  noRawTag(V.identityDetailHTML({ id: evil, name: evil, username: evil }, [{ id: "h", name: evil, authType: "identity", identityId: evil }]));
});

test("notebooks sort pinned first, then newest, then by name", () => {
  const nbs = [
    { id: "a", name: "old", updatedAt: "2026-01-01" },
    { id: "b", name: "new", updatedAt: "2026-02-01" },
    { id: "c", name: "pinned", pinned: true, updatedAt: "2025-01-01" },
  ];
  assert.deepEqual(V.sortedNotebooks(nbs).map((n) => n.id), ["c", "b", "a"]);
});

test("identity detail shows no secret, and an identity in use cannot be deleted", () => {
  const ids = [
    { id: "p", name: "break-glass", kind: "password", password: "x" },
    { id: "k", name: "deploy", kind: "key", username: "ops", privateKey: "x", passphrase: "x" },
  ];
  const hosts = [
    { id: "h1", name: "db-1", authType: "identity", identityId: "k" },
    { id: "h2", name: "db-2", authType: "identity", identityId: "k" },
    { id: "h3", name: "web", authType: "password" },
  ];
  assert.deepEqual(V.identityHosts(hosts, "k").map((h) => h.id), ["h1", "h2"]);
  const used = V.identityDetailHTML(ids[1], hosts);
  assert.match(used, /Used by 2 hosts/);
  assert.match(used, /data-delete-identity="k" aria-disabled="true"/);
  assert.match(used, /Stored in the vault/);
  assert.ok(!used.includes(">x<"), "no secret value in the detail");
  const free = V.identityDetailHTML(ids[0], hosts);
  assert.ok(!free.includes("aria-disabled"), "an unused identity can be deleted");
  assert.match(V.identityDetailHTML(undefined, []), /Select an identity/);
});

test("tunnel route and state words for the sidebar section", () => {
  const t = { kind: "remote", bindAddr: "0.0.0.0", bindPort: 5432, destHost: "db", destPort: 5432 };
  assert.equal(V.tunnelRoute(t), "0.0.0.0:5432 → db:5432");
  assert.equal(V.tunnelRoute({ kind: "dynamic", bindAddr: "127.0.0.1" }), "127.0.0.1:auto → SOCKS5");
  assert.equal(V.tunnelRoute({ ...t, listenAddr: "127.0.0.1:60001" }), "127.0.0.1:60001 → db:5432");
  assert.equal(V.tunnelState({ status: "ready" }).label, "Running");
  assert.equal(V.tunnelState({ status: "ready" }).on, true);
  assert.equal(V.tunnelState({ status: "error", error: "refused" }).label, "Failed · refused");
  assert.equal(V.tunnelState({ status: "connecting" }).filter, "running");
  assert.equal(V.tunnelState({}).label, "Stopped");
});

test("snippet preview is the exact bytes typed, one Enter per line", () => {
  const view = V.snippetPreview("ls -la\ndf -h");
  assert.equal(view.lines, 2);
  assert.equal(view.bytes, "ls -la\ndf -h\n".length);
  assert.match(view.html, /ls -la<span class="k-ctl" aria-label="Enter">↵<\/span>df -h<span class="k-ctl"/);
  assert.match(view.summary, /^2 lines · 13 bytes\./);
  assert.equal(V.snippetPreview("").lines, 0);
});
