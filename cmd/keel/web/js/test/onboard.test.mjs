import { test } from "node:test";
import assert from "node:assert/strict";
import {
  addLabel,
  allState,
  defaultSelection,
  hostAddress,
  importHTML,
  importRequest,
  importedText,
  keysHTML,
  leadText,
  missingJump,
  missingPassphrase,
  notesList,
  summaryText,
} from "../lib/onboard.js";

const preview = {
  path: "~/.ssh/config",
  found: true,
  hosts: [
    { alias: "web", hostName: "web.example.net", user: "deploy", port: 22, identityFiles: ["/h/.ssh/id_web"], source: "~/.ssh/config:1" },
    { alias: "db", hostName: "10.0.0.5", user: "deploy", port: 2222, identityFiles: ["/h/.ssh/id_db"], saved: true, source: "~/.ssh/config:5" },
    {
      alias: "jumpy",
      hostName: "10.0.0.9",
      user: "ops",
      port: 22,
      proxy: "ProxyJump",
      proxyJump: "bastion",
      notes: ["ProxyJump is not supported yet; the host is added without it", 'Port "x" is not a port number; 22 used'],
      source: "~/.ssh/config:9",
    },
  ],
  patterns: [{ pattern: "*.prod", source: "~/.ssh/config:12" }],
  keys: [
    { path: "/h/.ssh/id_web", display: "~/.ssh/id_web", name: "id_web", exists: true, encrypted: false, usedBy: ["web"] },
    { path: "/h/.ssh/id_db", display: "~/.ssh/id_db", name: "id_db", exists: true, encrypted: true, usedBy: ["db"] },
    { path: "/h/.ssh/gone", display: "~/.ssh/gone", name: "gone", exists: false, problem: "file not found", usedBy: [] },
  ],
  notes: [{ source: "~/.ssh/config:14", text: "Match block skipped: Keel imports Host blocks only" }],
};

test("defaults: new direct hosts on; saved and proxy hosts off; every key copy off", () => {
  const sel = defaultSelection(preview);
  assert.deepEqual([...sel.hosts], ["web"]);
  assert.deepEqual([...sel.keys.values()].map((k) => k.on), [false, false, false]);
  assert.equal(summaryText(preview, sel), "1 of 3 selected");
  assert.equal(allState(preview, sel), "mixed");
  assert.equal(addLabel(1), "Add 1 host");
  assert.equal(addLabel(0), "Add hosts");
});

test("the request names hosts and only the keys turned on, with a passphrase only where one is needed", () => {
  const sel = defaultSelection(preview);
  sel.hosts.add("jumpy");
  assert.deepEqual(importRequest(preview, sel), { hosts: ["web", "jumpy"], keys: [] });
  sel.keys.get("/h/.ssh/id_web").on = true;
  sel.keys.get("/h/.ssh/id_db").on = true;
  sel.keys.get("/h/.ssh/gone").on = true;
  assert.equal(missingPassphrase(preview, sel), 1);
  sel.keys.get("/h/.ssh/id_db").passphrase = "pw";
  sel.keys.get("/h/.ssh/id_web").passphrase = "ignored";
  assert.equal(missingPassphrase(preview, sel), -1);
  assert.deepEqual(importRequest(preview, sel).keys, [{ path: "/h/.ssh/id_web" }, { path: "/h/.ssh/id_db", passphrase: "pw" }]);
});

test("the table marks saved hosts, flags proxies and lists patterns apart", () => {
  const html = importHTML(preview, defaultSelection(preview));
  assert.match(html, /aria-label="Add web"[^>]*data-imp-host="web"|data-imp-host="web"/);
  assert.match(html, /role="checkbox" aria-checked="true" aria-label="Add web"/);
  assert.match(html, /role="checkbox" aria-checked="false" aria-label="Add db"/);
  assert.match(html, /Already saved/);
  assert.match(html, /ProxyJump bastion, not used/);
  assert.match(html, /aria-checked="mixed" aria-label="Select all hosts"/);
  assert.match(html, /aria-label="Patterns"[\s\S]*\*\.prod/);
  assert.doesNotMatch(html.split('aria-label="Patterns"')[0], /\*\.prod/, "a pattern is not a host row");
  assert.equal(hostAddress(preview.hosts[1]), "10.0.0.5:2222");
  assert.equal(hostAddress(preview.hosts[0]), "web.example.net");
});

test("keys: a switch per key, off; a passphrase field only for keys that have one; unreadable keys disabled", () => {
  const html = keysHTML(preview, defaultSelection(preview));
  assert.equal((html.match(/role="switch" aria-checked="false"/g) || []).length, 3);
  assert.match(html, /aria-label="Copy ~\/.ssh\/id_db into the vault"/);
  assert.match(html, /Passphrase for id_db/);
  assert.doesNotMatch(html, /Passphrase for id_web/);
  assert.match(html, /aria-label="Copy ~\/.ssh\/gone into the vault" data-imp-key="2" disabled/);
  assert.match(html, /file not found/);
});

test("notes: config notes plus host notes, proxy notes left to the Via column", () => {
  assert.deepEqual(
    notesList(preview).map((n) => n.source),
    ["~/.ssh/config:14", "jumpy"],
  );
});

test("lead and result copy", () => {
  assert.equal(leadText(preview), "Keel found ~/.ssh/config with 3 hosts. Pick the ones to add. Your config file is not changed.");
  assert.equal(leadText({ path: "~/.ssh/config", found: false }), "Keel found no ~/.ssh/config. Add hosts later with New host.");
  assert.equal(importedText({ hosts: [1, 2], identities: [1] }), "Added 2 hosts and 1 key");
  assert.equal(importedText({ hosts: [1], identities: [] }), "Added 1 host");
});

test("values are escaped", () => {
  const evil = { ...preview, hosts: [{ alias: '<img src=x onerror="1">', hostName: "a", port: 22 }], keys: [], patterns: [], notes: [] };
  const html = importHTML(evil, defaultSelection(evil));
  assert.doesNotMatch(html, /<img/);
});

test("a ProxyJump Keel can keep shows its jump host instead of the flag", () => {
  const p = {
    ...preview,
    hosts: [
      { alias: "db2", hostName: "10.0.0.6", user: "d", port: 22, proxy: "ProxyJump", proxyJump: "gate", jumpSaved: "gate", source: "x" },
      { alias: "db3", hostName: "10.0.0.7", user: "d", port: 22, proxy: "ProxyJump", proxyJump: "bastion", jumpAlias: "bastion", source: "x" },
    ],
  };
  const html = importHTML(p, defaultSelection(p));
  assert.match(html, /Via gate</);
  assert.match(html, /Via bastion if added</);
  assert.doesNotMatch(html, /not used/);
});

test("a ProxyJump host whose jump is not kept needs Without the jump", () => {
  const sel = defaultSelection(preview);
  sel.hosts.add("jumpy");
  assert.equal(missingJump(preview, sel)?.alias, "jumpy");
  assert.equal(importRequest(preview, sel).withoutJump, undefined);
  sel.noJump.add("jumpy");
  assert.equal(missingJump(preview, sel), null);
  assert.deepEqual(importRequest(preview, sel).withoutJump, ["jumpy"]);
  const html = importHTML(preview, sel);
  assert.match(html, /role="checkbox" aria-checked="true" aria-label="Add jumpy without the jump"/);

  // Its config jump host being added keeps the jump; nothing to waive.
  const p = { ...preview, hosts: [...preview.hosts, { alias: "db3", hostName: "10.0.0.7", user: "d", port: 22, proxy: "ProxyJump", proxyJump: "web", jumpAlias: "web", jumpVia: "deploy@web.example.net:22", source: "x" }] };
  const s2 = defaultSelection(p);
  s2.hosts.add("db3");
  assert.equal(missingJump(p, s2), null);
  s2.hosts.delete("web");
  assert.equal(missingJump(p, s2)?.alias, "db3");
});

test("importedText names hosts that were not added", () => {
  assert.equal(importedText({ hosts: [{}], failed: [{ alias: "db", error: "its jump host bastion was not added" }] }), "Added 1 host. Not added: db (its jump host bastion was not added)");
});
