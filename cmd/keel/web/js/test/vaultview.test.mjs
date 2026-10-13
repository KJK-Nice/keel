import { test } from "node:test";
import assert from "node:assert/strict";
import { nestGroupTree } from "../lib/groups.js";
import * as V from "../lib/vaultview.js";

const evil = `<img src=x onerror=alert(1)>`;
const noRawTag = (html) => assert.ok(!html.includes("<img"), `unescaped markup in:\n${html}`);

test("hosts: a sidebar tree of loose hosts, then nested groups, and an empty state", () => {
  const groups = [
    { id: "g1", name: "Infra" },
    { id: "g2", name: "db", parentId: "g1" },
  ];
  const hosts = [{ id: "h1", name: "pg", address: "10.0.0.1", username: "root", groupId: "g2", tags: ["x"] }];
  const loose = [{ id: "h0", name: "Local Shell", authType: "local" }];
  const html = V.hostsHTML({ tree: nestGroupTree(groups, hosts, "", ""), loose });
  assert.ok(html.indexOf('data-connect="h0"') < html.indexOf('data-group-id="g1"'), "loose hosts come first");
  assert.match(html, /data-group-id="g1"[^>]*data-group-depth="1"/);
  assert.match(html, /data-group-id="g2"[^>]*data-group-depth="2"/);
  assert.match(html, /data-group-toggle="g1" aria-expanded="true"/);
  assert.match(html, /data-connect="h1" title="root@10\.0\.0\.1"/);
  assert.match(html, /data-host-live="h1" hidden/);
  assert.match(html, /data-drag-host="h1"/);
  assert.match(html, /data-delete="h1"/);
  assert.ok(!html.includes('data-delete="h0"'), "Local Shell cannot be removed");
  assert.match(V.hostsHTML({ tree: [], loose: [] }), /No hosts match/);
});

test("every list escapes what the vault holds", () => {
  const h = { id: evil, name: evil, address: evil, username: evil, group: evil, tags: [evil], color: evil };
  noRawTag(V.hostsHTML({ tree: nestGroupTree([{ id: evil, name: evil }], [], "", ""), loose: [h] }));
  noRawTag(V.snippetsHTML([{ id: evil, name: evil, command: evil }]));
  noRawTag(V.snippetPreview(evil).html);
  noRawTag(V.notebooksHTML([{ id: evil, name: evil, cells: [{ body: evil }] }]));
  noRawTag(V.identitiesHTML([{ id: evil, name: evil, username: evil }], []));
  noRawTag(V.identityDetailHTML({ id: evil, name: evil, username: evil }, [{ id: "h", name: evil, authType: "identity", identityId: evil }]));
  noRawTag(V.tunnelsHTML([{ id: evil, name: evil, hostId: evil, destHost: evil, error: evil, status: "error" }], []));
  noRawTag(V.logsHTML([{ kind: evil, message: evil, time: "2026-01-01T00:00:00Z" }], ""));
});

test("notebooks: pinned first, then newest, with a preview line", () => {
  const nbs = [
    { id: "a", name: "old", updatedAt: "2026-01-01" },
    { id: "b", name: "new", updatedAt: "2026-02-01", cells: [{ body: "\n  first line\nsecond" }] },
    { id: "c", name: "pinned", pinned: true, updatedAt: "2025-01-01" },
  ];
  assert.deepEqual(V.sortedNotebooks(nbs).map((n) => n.id), ["c", "b", "a"]);
  const html = V.notebooksHTML(nbs);
  assert.ok(html.indexOf('data-notebook-open="c"') < html.indexOf('data-notebook-open="b"'));
  assert.match(html, /first line/);
  assert.match(html, /<span class="k-badge">Pinned<\/span>/);
  assert.match(html, /data-notebook-pin="c">Unpin/);
  assert.match(V.notebooksHTML([]), /No notebooks yet/);
});

test("identities: keys then passwords, host counts, and a detail that shows no secret", () => {
  const ids = [
    { id: "p", name: "break-glass", kind: "password", password: "x" },
    { id: "k", name: "deploy", kind: "key", username: "ops", privateKey: "x", passphrase: "x" },
  ];
  const hosts = [
    { id: "h1", name: "db-1", authType: "identity", identityId: "k" },
    { id: "h2", name: "db-2", authType: "identity", identityId: "k" },
    { id: "h3", name: "web", authType: "password" },
  ];
  const list = V.identitiesHTML(ids, hosts);
  assert.ok(list.indexOf("SSH keys") < list.indexOf("Passwords"));
  assert.match(list, /SSH key · ops · 2 hosts/);
  assert.match(list, /Password · 0 hosts/);
  assert.match(list, /<span class="k-badge">passphrase<\/span>/);
  const used = V.identityDetailHTML(ids[1], hosts);
  assert.match(used, /Used by 2 hosts/);
  assert.match(used, /data-delete-identity="k" aria-disabled="true"/);
  assert.match(used, /Stored in the vault/);
  const free = V.identityDetailHTML(ids[0], hosts);
  assert.ok(!free.includes("aria-disabled"), "an unused identity can be deleted");
  assert.match(V.identitiesHTML([], []), /No identities yet/);
  assert.match(V.identityDetailHTML(undefined, []), /Select an identity/);
});

test("tunnels show flag, route, state word and the every-interface warning", () => {
  const html = V.tunnelsHTML(
    [{ id: "t1", name: "pg", kind: "remote", hostId: "h1", bindAddr: "0.0.0.0", bindPort: 5432, destHost: "db", destPort: 5432, status: "ready" }],
    [{ id: "h1", name: "bastion" }],
  );
  assert.match(html, />-R</);
  assert.match(html, /0\.0\.0\.0:5432<span class="k-flow-arrow" aria-hidden="true">→<\/span>db:5432/);
  assert.match(html, /Every interface/);
  assert.match(html, /k-dot--live[^>]*><\/span>Running/);
  assert.match(html, /aria-checked="true"[^>]*data-tunnel-stop="t1"/);
  assert.match(html, /data-status="running"/);
  const stopped = V.tunnelsHTML([{ id: "t", name: "s", kind: "dynamic", hostId: "x", bindAddr: "127.0.0.1" }], []);
  assert.match(stopped, /auto<span class="k-flow-arrow" aria-hidden="true">→<\/span>SOCKS5/);
  assert.ok(!stopped.includes("Every interface"));
  assert.match(stopped, /data-tunnel-start="t"/);
  assert.equal(V.tunnelState({ status: "error", error: "refused" }).label, "Failed · refused");
  assert.equal(V.tunnelState({ status: "connecting" }).filter, "running");
});

test("snippet preview is the exact bytes typed, one Enter per line", () => {
  const view = V.snippetPreview("ls -la\ndf -h");
  assert.equal(view.lines, 2);
  assert.equal(view.bytes, "ls -la\ndf -h\n".length);
  assert.match(view.html, /ls -la<span class="k-ctl" aria-label="Enter">↵<\/span>df -h<span class="k-ctl"/);
  assert.match(view.summary, /^2 lines · 13 bytes\./);
  assert.equal(V.snippetPreview("").lines, 0);
  assert.equal(V.snippetFirstLine("a\nb"), "a …");
});

test("logs filter on kind and message, and group kinds for the chips", () => {
  const logs = [
    { kind: "session", message: "connected prod", time: "2026-01-01T00:00:00Z", hostId: "h1" },
    { kind: "tunnel", message: "started", time: "bad date" },
  ];
  const html = V.logsHTML(logs, "PROD", { hosts: [{ id: "h1", name: "prod-1" }] });
  assert.match(html, /connected prod/);
  assert.match(html, /data-log-kind="sessions"/);
  assert.match(html, />prod-1</);
  assert.ok(!html.includes("started"));
  assert.match(V.logsHTML(logs, "nothing"), /No log entries/);
  assert.deepEqual(V.logParts("bad date"), { date: "", time: "" });
  assert.equal(V.logGroup("identity"), "vault");
  assert.equal(V.logGroup("tunnel"), "forwards");
  assert.equal(V.logGroup("info"), "other");
});
