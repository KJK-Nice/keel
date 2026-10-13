import { test } from "node:test";
import assert from "node:assert/strict";
import * as G from "../lib/groups.js";

// infra > db > replicas ; apps
const groups = [
  { id: "infra", name: "Infra" },
  { id: "db", name: "db", parentId: "infra" },
  { id: "rep", name: "Replicas", parentId: "db" },
  { id: "apps", name: "Apps" },
];
const hosts = [
  { id: "h1", name: "pg-1", address: "10.0.0.1", username: "root", groupId: "db", tags: ["postgres"] },
  { id: "h2", name: "pg-2", address: "10.0.0.2", username: "root", groupId: "rep" },
  { id: "h3", name: "web", address: "10.1.0.1", username: "deploy", groupId: "apps" },
  { id: "h4", name: "laptop", address: "127.0.0.1", username: "me" },
];

test("depths and ancestry", () => {
  assert.equal(G.groupDepth(groups, "rep"), 3);
  assert.equal(G.groupDepth(groups, ""), 0);
  assert.equal(G.groupDescendantDepth(groups, "infra"), 2);
  assert.ok(G.isGroupUnder(groups, "rep", "infra"));
  assert.ok(!G.isGroupUnder(groups, "apps", "infra"));
  assert.deepEqual([...G.subtreeGroupIds(groups, "db")].sort(), ["db", "rep"]);
  assert.equal(G.subtreeGroupIds(groups, ""), null);
});

test("cycles in group data don't hang", () => {
  const loop = [
    { id: "a", name: "A", parentId: "b" },
    { id: "b", name: "B", parentId: "a" },
  ];
  assert.equal(G.groupDepth(loop, "a"), 2);
  assert.ok(!G.isGroupUnder(loop, "a", "zzz"));
  assert.equal(G.groupOptionLabel(loop, loop[0]), "B / A");
  assert.deepEqual(G.nestGroupTree(loop, [], "", ""), []);
});

test("search matches group names above the host and tags", () => {
  assert.match(G.hostSearchHay(groups, hosts[1]), /replicas db infra/);
  assert.deepEqual(G.hostsInSubtree(groups, hosts, "infra", "").map((h) => h.id), ["h1", "h2"]);
  assert.deepEqual(G.hostsInSubtree(groups, hosts, "", "postgres").map((h) => h.id), ["h1"]);
  assert.deepEqual(G.hostsInSubtree(groups, hosts, "", "INFRA").map((h) => h.id), ["h1", "h2"]);
});

test("nestGroupTree counts hosts through the tree and filters by query", () => {
  const tree = G.nestGroupTree(groups, hosts, "", "");
  assert.deepEqual(tree.map((n) => n.group.id), ["apps", "infra"]);
  const infra = tree[1];
  assert.equal(infra.count, 2);
  assert.equal(infra.kids[0].group.id, "db");
  assert.equal(infra.kids[0].kids[0].depth, 3);

  const q = G.nestGroupTree(groups, hosts, "pg-2", "");
  assert.deepEqual(q.map((n) => n.group.id), ["infra"]);
  assert.deepEqual(q[0].kids[0].hosts, []);
  assert.deepEqual(q[0].kids[0].kids[0].hosts.map((h) => h.id), ["h2"]);
});

test("parent picker labels paths and excludes a group's own subtree", () => {
  const opts = G.groupOptionEntries(groups, "db");
  assert.deepEqual(opts, [
    { value: "apps", label: "Apps" },
    { value: "infra", label: "Infra" },
  ]);
  assert.ok(G.groupOptionEntries(groups).some((o) => o.label === "Infra / db / Replicas"));
});

test("tags and subtitles", () => {
  assert.deepEqual(G.parseHostTags(" a, ,b ,c"), ["a", "b", "c"]);
  assert.equal(G.hostSubtitle({ username: "u", address: "h", port: 2222, identityId: "i" }), "u@h:2222 · identity");
  assert.equal(G.hostSubtitle({ username: "u", address: "h", port: 22 }), "u@h");
  assert.equal(G.hostSubtitle({ authType: "local" }), "this machine");
});
