import { test } from "node:test";
import assert from "node:assert/strict";
import {
  CLOSED_MAX,
  RECENT_CARDS,
  actionsHTML,
  ageLabel,
  cardHTML,
  closedHTML,
  hostPrefill,
  liveClosed,
  normalizeClosed,
  notesHTML,
  offerHTML,
  pushClosed,
  recentCards,
  targetText,
} from "../lib/newtab.js";
import { parseConnect } from "../lib/sshparse.js";

const host = (id, name, extra = {}) => ({ id, name, address: `${id}.example`, port: 22, username: "u", ...extra });
const hosts = ["a", "b", "c", "d", "e", "f", "g", "h"].map((id) => host(id, `host-${id}`));
const local = host("loc", "Local Shell", { authType: "local" });
const match = (h, q) => h.name.toLowerCase().includes(q.toLowerCase());

test("recent cards are the recent hosts plus the local shell, at most six", () => {
  const all = [...hosts, local];
  const cards = recentCards(all, ["c", "a", "gone", "b", "d", "e", "f", "g"], "", match, local);
  assert.equal(cards.length, RECENT_CARDS);
  assert.deepEqual(cards.map((h) => h.id), ["loc", "c", "a", "b", "d", "e"]);
});

test("with no recent hosts the local shell is the only card", () => {
  assert.deepEqual(recentCards([...hosts, local], [], "", match, local).map((h) => h.id), ["loc"]);
  assert.deepEqual(recentCards([], [], "", match, null), []);
});

test("a query filters every host, recent ones first", () => {
  const cards = recentCards([...hosts, local], ["c"], "host-", match, local);
  assert.equal(cards[0].id, "c");
  assert.equal(cards.length, RECENT_CARDS);
  assert.deepEqual(recentCards(hosts, [], "host-b", match, null).map((h) => h.id), ["b"]);
  assert.deepEqual(recentCards(hosts, [], "nothing", match, null), []);
});

test("a card shows its host, live count, shortcut and escapes text", () => {
  const html = cardHTML(host("x", "<b>x</b>", { color: '"><i>' }), 1, { sub: "u@x.example", live: 3, alt: "⌥", picked: true });
  assert.match(html, /data-start-connect="x"/);
  assert.match(html, /data-highlighted/);
  assert.match(html, /3 live · u@x\.example/);
  assert.match(html, /k-keyhint[^>]*>⌥2</);
  assert.ok(!html.includes("<b>x"));
  assert.ok(!html.includes("<i>"));
  assert.match(cardHTML(host("y", "y"), 0, { connecting: true }), /Connecting…/);
  assert.ok(!cardHTML(host("z", "z"), 6, {}).includes("k-keyhint"));
});

test("the start actions carry their shortcuts", () => {
  const html = actionsHTML("⌘");
  assert.match(html, /data-start-quick[\s\S]*Quick connect<span class="k-kbd">⌘⇧O</);
  assert.match(html, /data-start-join[\s\S]*Join session<span class="k-kbd">⌘J</);
  assert.match(html, /data-start-newhost[\s\S]*New host<span class="k-kbd">⌘N</);
  assert.ok(!/agent/i.test(html));
  assert.match(actionsHTML("Ctrl+"), /Ctrl\+⇧O/);
});

test("closed entries are normalised, deduped and limited", () => {
  assert.deepEqual(normalizeClosed(null), []);
  assert.deepEqual(normalizeClosed([{ hostId: "" }, null, 3, { hostId: 5 }]), []);
  const one = normalizeClosed([{ hostId: "a", title: "A", kind: "weird", at: "x" }]);
  assert.deepEqual(one, [{ hostId: "a", title: "A", kind: "tab", at: 0 }]);
  let list = [];
  for (let i = 0; i < CLOSED_MAX + 5; i++) list = pushClosed(list, { hostId: `h${i}`, title: "", kind: "tab", at: i });
  assert.equal(list.length, CLOSED_MAX);
  assert.equal(list[0].hostId, `h${CLOSED_MAX + 4}`);
});

test("closing the same host again moves it to the front instead of repeating it", () => {
  let list = pushClosed([], { hostId: "a", title: "A", kind: "tab", at: 1 });
  list = pushClosed(list, { hostId: "b", title: "B", kind: "tab", at: 2 });
  list = pushClosed(list, { hostId: "a", title: "A", kind: "tab", at: 3 });
  assert.deepEqual(list.map((e) => [e.hostId, e.at]), [["a", 3], ["b", 2]]);
  list = pushClosed(list, { hostId: "a", title: "A", kind: "pane", at: 4 });
  assert.equal(list.length, 3);
  assert.deepEqual(pushClosed(list, { hostId: "" }), list);
});

test("liveClosed drops hosts that were removed", () => {
  const list = [{ hostId: "a", title: "", kind: "tab", at: 1 }, { hostId: "z", title: "", kind: "tab", at: 2 }];
  assert.deepEqual(liveClosed(list, ["a"]).map((e) => e.hostId), ["a"]);
});

test("ageLabel", () => {
  const now = 10_000_000_000;
  assert.equal(ageLabel(now - 5_000, now), "just now");
  assert.equal(ageLabel(now - 12 * 60_000, now), "12 min ago");
  assert.equal(ageLabel(now - 3 * 3_600_000, now), "3 h ago");
  assert.equal(ageLabel(now - 30 * 3_600_000, now), "yesterday");
  assert.equal(ageLabel(now - 5 * 86_400_000, now), "5 days ago");
  assert.equal(ageLabel(now + 5_000, now), "just now");
});

test("closed rows name the host, mark split panes and show ⌘⇧T on the newest", () => {
  const now = 1_000_000;
  const html = closedHTML(
    [
      { hostId: "a", title: "old title", kind: "tab", at: now - 12 * 60_000 },
      { hostId: "b", title: "", kind: "pane", at: now - 40 * 60_000 },
    ],
    (id) => ({ a: "staging-api-01", b: "prod-db-01" })[id],
    now,
    "⌘",
  );
  assert.match(html, /data-start-reopen="0"[\s\S]*staging-api-01[\s\S]*12 min ago[\s\S]*⌘⇧T/);
  assert.match(html, /data-start-reopen="1"[\s\S]*prod-db-01 · split pane[\s\S]*40 min ago/);
  assert.equal((html.match(/⌘⇧T/g) || []).length, 1);
  assert.equal(closedHTML([], () => "", now, "⌘"), "");
  const many = Array.from({ length: 6 }, (_, i) => ({ hostId: `h${i}`, title: "t", kind: "tab", at: now }));
  assert.equal((closedHTML(many, () => "n", now, "⌘").match(/data-start-reopen/g) || []).length, 3);
});

test("targetText and hostPrefill", () => {
  assert.equal(targetText({ user: "deploy", host: "10.0.4.21", port: 2222 }), "deploy@10.0.4.21:2222");
  assert.equal(targetText({ user: "", host: "prod", port: 22 }), "prod");
  assert.equal(targetText({ user: "u", host: "::1", port: 2200 }), "u@[::1]:2200");
  const p = parseConnect("ssh -i ~/.ssh/k -J bastion deploy@10.0.4.21 -p 2222");
  assert.deepEqual(hostPrefill(p), { name: "10.0.4.21", address: "10.0.4.21", port: 2222, username: "deploy" });
});

test("the offer lists what was left out and does nothing by itself", () => {
  const p = parseConnect("ssh -o ProxyCommand='nc x 22' -o StrictHostKeyChecking=no -L 1:x:2 -i ~/.ssh/k deploy@host ls -la");
  const html = offerHTML(p);
  assert.match(html, /data-start-offer>/);
  assert.match(html, /Read as <span[^>]*>deploy@host<\/span>\. Nothing has run\./);
  assert.match(html, /Left out \(4\)/);
  for (const frag of ["ProxyCommand=nc x 22", "StrictHostKeyChecking=no", "-L 1:x:2", "ls -la", "runs a command", "changes host key trust", "opens a port forward", "remote command"]) {
    assert.ok(html.includes(frag), frag);
  }
  assert.match(html, /not read/);
  assert.match(html, /data-start-offer-save/);
  assert.match(html, /data-start-offer-once/);
});

test("without a user there is no Connect once", () => {
  const html = offerHTML(parseConnect("ssh host"));
  assert.ok(!html.includes("data-start-offer-once"));
  assert.match(html, /Add a user/);
  assert.equal(notesHTML(parseConnect("ssh host")), "");
});

test("a jump host is shown as not supported yet", () => {
  assert.match(notesHTML(parseConnect("ssh -J bastion host")), /-J bastion[\s\S]*not supported yet/);
});

test("hostile text is escaped in the offer", () => {
  const p = parseConnect(`ssh host '<img src=x onerror=alert(1)>'`);
  const html = offerHTML(p);
  assert.ok(!html.includes("<img"));
  assert.match(html, /&lt;img/);
});
