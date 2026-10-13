import { test } from "node:test";
import assert from "node:assert/strict";
import { nestGroupTree } from "../lib/groups.js";
import * as V from "../lib/vaultview.js";

const evil = `<img src=x onerror=alert(1)>`;
const noRawTag = (html) => assert.ok(!html.includes("<img"), `unescaped markup in:\n${html}`);

test("hosts: folders, cards, breadcrumb and empty state", () => {
  const groups = [
    { id: "g1", name: "Infra" },
    { id: "g2", name: "db", parentId: "g1" },
  ];
  const hosts = [{ id: "h1", name: "pg", address: "10.0.0.1", username: "root", groupId: "g2", tags: ["x"] }];
  const html = V.hostsHTML({ tree: nestGroupTree(groups, hosts, "", ""), hosts, groups, focus: "g2" });
  assert.match(html, /data-group-open="">All<\/button>.*Infra.*db/s);
  assert.match(html, /data-group-id="g1"[^>]*data-group-depth="1"/);
  assert.match(html, /data-group-id="g2"[^>]*data-group-depth="2"/);
  assert.match(html, /data-connect="h1"/);
  assert.match(html, /1 Host</);
  assert.match(html, /data-tag-filter="x"/);
  assert.match(V.hostsHTML({ tree: [], hosts: [], groups, focus: "" }), /No hosts match/);
});

test("every list escapes what the vault holds", () => {
  const h = { id: evil, name: evil, address: evil, username: evil, group: evil, tags: [evil] };
  noRawTag(V.hostsHTML({ tree: nestGroupTree([{ id: evil, name: evil }], [], "", ""), hosts: [h], groups: [], focus: "" }));
  noRawTag(V.snippetsHTML([{ id: evil, name: evil, command: evil }]));
  noRawTag(V.notebooksHTML([{ id: evil, name: evil, cells: [{ body: evil }] }]));
  noRawTag(V.identitiesHTML([{ id: evil, name: evil, username: evil }]));
  noRawTag(V.tunnelsHTML([{ id: evil, name: evil, hostId: evil, destHost: evil, error: evil, status: evil }], []));
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
  assert.match(html, /Pinned · Empty notebook/);
  assert.match(html, /data-notebook-pin="c" title="Unpin">Unpin/);
  assert.match(V.notebooksHTML([]), /No notebooks yet/);
});

test("tunnels show kind, status and route", () => {
  const html = V.tunnelsHTML(
    [{ id: "t1", name: "pg", kind: "remote", hostId: "h1", bindAddr: "127.0.0.1", bindPort: 5432, destHost: "db", destPort: 5432, status: "ready" }],
    [{ id: "h1", name: "bastion" }],
  );
  assert.match(html, />R</);
  assert.match(html, />Remote</);
  assert.match(html, /bastion · 127\.0\.0\.1:5432 → db:5432/);
  assert.match(html, /#5ee0b5/);
  assert.match(V.tunnelsHTML([{ id: "t", name: "s", kind: "dynamic", hostId: "x", bindAddr: "127.0.0.1" }], []), /auto → SOCKS5/);
});

test("logs filter on kind and message", () => {
  const logs = [
    { kind: "session", message: "connected prod", time: "2026-01-01T00:00:00Z" },
    { kind: "tunnel", message: "started", time: "bad date" },
  ];
  const html = V.logsHTML(logs, "PROD");
  assert.match(html, /connected prod/);
  assert.ok(!html.includes("started"));
  assert.match(V.logsHTML(logs, "nothing"), /No log entries/);
  assert.deepEqual(V.logParts("bad date"), { date: "", time: "" });
});
