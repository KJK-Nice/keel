import { test } from "node:test";
import assert from "node:assert/strict";
import {
  clientName,
  followBannerHTML,
  followState,
  initials,
  isControlRequest,
  isTypingKey,
  roleOf,
  sharePeople,
  sharePopoverHTML,
  watchingCount,
} from "../lib/share.js";

const now = Date.parse("2026-10-14T12:00:00Z");
const ago = (min) => new Date(now - min * 60_000).toISOString();
// The owner's window (id 1, driving), a second Keel window (2), a joiner (3).
const session = {
  ownerId: 1,
  driverId: 1,
  you: { id: 1, name: "khajornkiat", role: "driver" },
  attached: [
    { id: 3, name: "Niran P.", kind: "webrtc", attachedAt: ago(4) },
    { id: 1, name: "khajornkiat", kind: "web", attachedAt: ago(30) },
    { id: 2, name: "khajornkiat", kind: "web", attachedAt: ago(10) },
  ],
};

test("roles mirror the share hub: driver, then owner, else follower", () => {
  assert.equal(roleOf(1, { ownerId: 1, driverId: 1 }), "driver");
  assert.equal(roleOf(1, { ownerId: 1, driverId: 3 }), "owner");
  assert.equal(roleOf(3, { ownerId: 1, driverId: 1 }), "follower");
  assert.equal(roleOf(0, { ownerId: 0, driverId: 0 }), "follower");
});

test("people: you first, then the driver, then by arrival, with how they joined", () => {
  const rows = sharePeople(session, now);
  assert.deepEqual(rows.map((r) => [r.name, r.role, r.sub]), [
    ["You", "driver", "owner · this window"],
    ["khajornkiat", "follower", "Keel window · 10 min ago"],
    ["Niran P.", "follower", "joined by link · 4 min ago"],
  ]);
  assert.equal(rows[0].initials, "K");
  const handed = sharePeople({ ...session, driverId: 3 }, now);
  assert.deepEqual(handed.map((r) => [r.name, r.role]), [
    ["You", "owner"],
    ["Niran P.", "driver"],
    ["khajornkiat", "follower"],
  ]);
});

test("watching counts only people who came through a link", () => {
  assert.equal(watchingCount(session.attached), 1);
  assert.equal(watchingCount([]), 0);
});

test("a control request has a sender and no target; hand-over and take-back have one", () => {
  assert.equal(isControlRequest({ type: "control", fromId: 3, message: "Niran P. requested control" }), true);
  assert.equal(isControlRequest({ type: "control", fromId: 1, targetId: 3, message: "control passed" }), false);
  assert.equal(isControlRequest({ type: "control", fromId: 1, targetId: 1, message: "took control" }), false);
  assert.equal(isControlRequest({ type: "presence", fromId: 3 }), false);
  assert.equal(clientName(session.attached, 3), "Niran P.");
  assert.equal(clientName(session.attached, 9), "Someone");
});

test("follower banner: whose session, which host, read-only, Ask and Leave", () => {
  const joiner = { ...session, you: { id: 3 }, title: "prod-db-01" };
  const st = followState(joiner);
  assert.deepEqual(st, { owner: "khajornkiat", driver: "khajornkiat", driving: false, host: "prod-db-01" });
  const html = followBannerHTML(st);
  assert.match(html, /Following <b>khajornkiat<\/b>'s session on <b>prod-db-01<\/b> · read-only/);
  assert.match(html, /data-request="1">.*Ask for control<\/button>/);
  assert.match(html, /data-join-leave="1">Leave<\/button>/);
  // Handed control: no Ask, says so.
  const driving = followBannerHTML(followState({ ...joiner, driverId: 3 }));
  assert.match(driving, /Driving <b>khajornkiat<\/b>'s session .* · you have control/);
  assert.doesNotMatch(driving, /Ask for control/);
});

test("names and hosts are escaped everywhere", () => {
  const evil = '<img src=x onerror=alert(1)>"';
  const html = followBannerHTML(followState({ ownerId: 1, driverId: 1, you: { id: 2 }, attached: [{ id: 1, name: evil }], title: evil }));
  assert.doesNotMatch(html, /<img/);
  const pop = sharePopoverHTML({
    title: evil,
    status: "ready",
    url: `https://sig/join?room=a"b#k=<x>`,
    people: sharePeople({ ...session, attached: [...session.attached, { id: 4, name: evil, kind: "webrtc" }] }, now),
    canGrant: true,
  });
  assert.doesNotMatch(pop, /<img|<x>|room=a"b/);
  assert.equal(initials(evil), "<S");
});

test("popover: link, copy state, honest scope and expiry, Stop sharing", () => {
  const html = sharePopoverHTML({ title: "prod-db-01", status: "ready", url: "https://sig/join?room=r#k=s", people: sharePeople(session, now), canGrant: true });
  assert.match(html, /Live link/);
  assert.match(html, /value="https:\/\/sig\/join\?room=r#k=s"/);
  assert.match(html, /data-share-copy >.*Copy<\/button>/s);
  assert.match(html, /this pane's terminal only/);
  assert.match(html, /Works until you stop sharing or close this pane, 24 hours at most\./);
  assert.match(html, /In this session · 3/);
  assert.match(html, /data-share-stop>Stop sharing/);
  assert.match(sharePopoverHTML({ status: "ready", url: "u", copied: true }), /Copied<\/button>/);
  // While the link is being made, Copy is disabled.
  assert.match(sharePopoverHTML({ status: "pending" }), /data-share-copy disabled/);
  // A failed share says why and offers no Stop.
  const failed = sharePopoverHTML({ status: "error", error: "set KEEL_SIGNAL to a public signaling server" });
  assert.match(failed, /role="alert">set KEEL_SIGNAL/);
  assert.doesNotMatch(failed, /Stop sharing|Join link/);
});

test("popover: Pass control to the one follower, per row when several", () => {
  const one = sharePopoverHTML({ status: "ready", url: "u", canGrant: true, people: sharePeople({ ...session, attached: session.attached.filter((c) => c.id !== 2) }, now) });
  assert.match(one, /data-grant="3">.*Pass control to Niran P\.<\/button>/);
  assert.equal((one.match(/data-grant=/g) || []).length, 1);
  const many = sharePopoverHTML({ status: "ready", url: "u", canGrant: true, people: sharePeople(session, now) });
  assert.equal((many.match(/aria-label="Pass control to /g) || []).length, 2);
  assert.doesNotMatch(many, /data-grant="1"/, "never to yourself");
  // Nobody to pass to without the right to grant.
  const follower = sharePopoverHTML({ status: "ready", url: "u", canGrant: false, canAsk: true, people: sharePeople(session, now) });
  assert.doesNotMatch(follower, /data-grant/);
  assert.match(follower, /data-request="1">.*Ask for control/);
  assert.match(sharePopoverHTML({ status: "ready", url: "u", canTake: true }), /data-take="1">.*Take control/);
});

test("typing keys: printable, Enter, Backspace; not shortcuts or modifiers", () => {
  const k = (key, mods = {}) => ({ type: "keydown", key, ...mods });
  for (const key of ["a", "Z", "1", " ", "é", "Enter", "Backspace", "Tab", "Delete"]) assert.equal(isTypingKey(k(key)), true, key);
  for (const ev of [k("c", { metaKey: true }), k("c", { ctrlKey: true }), k("x", { altKey: true }), k("Shift"), k("ArrowUp"), k("Escape"), { type: "keyup", key: "a" }]) {
    assert.equal(isTypingKey(ev), false, JSON.stringify(ev));
  }
});
