// Share and Join chrome (docs/ui-redesign.md §5.6, §5.7): the Share
// popover, the people in a shared session, the control request toast and
// the follower banner of a joined tab. Pure functions from session state to
// strings and HTML (the same k-* classes as the rest of the UI); every value
// goes through escapeHtml.
//
// Nothing here decides who may watch or type. Admission is the share hub's
// (internal/share), roles and hand-over are the runtime's
// (internal/runtime), and input stays gated in term.js (ptyWritesBlocked,
// isDriving). These helpers only describe that state.

import { escapeHtml } from "./html.js";
import { ageLabel } from "./newtab.js";

// Hugeicons stroke paths (the boards' symbols).
const PATHS = {
  steering:
    '<path d="M22 12C22 17.5228 17.5228 22 12 22C6.47715 22 2 17.5228 2 12C2 6.47715 6.47715 2 12 2C17.5228 2 22 6.47715 22 12Z"/><path d="M18.9 10.4C17.9 10.8 16.5 11.2 15.2 10.7C13.3 10 12.3 10 12 10C11.7 10 10.7 10 8.8 10.7C7.5 11.2 6.1 10.8 5.1 10.4M19 13.3C16.9 13.7 13.6 14.9 13.5 19M10.5 19C10.4 14.9 7.1 13.7 5 13.3"/>',
  eye: '<path d="M21.5 11C21.8 11.5 22 11.7 22 12C22 12.3 21.8 12.5 21.5 13C20.2 14.9 16.7 19 12 19C7.3 19 3.8 14.9 2.5 13C2.2 12.5 2 12.3 2 12C2 11.7 2.2 11.5 2.5 11C3.8 9.1 7.3 5 12 5C16.7 5 20.2 9.1 21.5 11Z"/><path d="M15 12C15 10.3 13.7 9 12 9C10.3 9 9 10.3 9 12C9 13.7 10.3 15 12 15C13.7 15 15 13.7 15 12Z"/>',
  link: '<path d="M15 17H17C19.8 17 22 14.8 22 12C22 9.2 19.8 7 17 7H15M9 17H7C4.2 17 2 14.8 2 12C2 9.2 4.2 7 7 7H9"/><path d="M8.5 12H15.5"/>',
  lock: '<path d="M12 14V17"/><path d="M16.5 9V6.5C16.5 4 14.5 2 12 2C9.5 2 7.5 4 7.5 6.5V9"/><path d="M4.3 18.8C4.5 20.5 5.9 21.8 7.6 21.9C9 22 10.4 22 12 22C13.6 22 15 22 16.4 21.9C18.1 21.8 19.5 20.5 19.7 18.8C19.9 17.8 20 16.6 20 15.5C20 14.4 19.9 13.2 19.7 12.2C19.5 10.5 18.1 9.2 16.4 9.1C15 9 13.6 9 12 9C10.4 9 9 9 7.6 9.1C5.9 9.2 4.5 10.5 4.3 12.2C4.1 13.2 4 14.4 4 15.5C4 16.6 4.1 17.8 4.3 18.8Z"/>',
  copy: '<path d="M7.5 14.5C7.5 11.2 7.5 9.55 8.53 8.53C9.55 7.5 11.2 7.5 14.5 7.5C17.8 7.5 19.45 7.5 20.47 8.53C21.5 9.55 21.5 11.2 21.5 14.5C21.5 17.8 21.5 19.45 20.47 20.47C19.45 21.5 17.8 21.5 14.5 21.5C11.2 21.5 9.55 21.5 8.53 20.47C7.5 19.45 7.5 17.8 7.5 14.5Z"/><path d="M7.5 16.5C6.1 16.5 5.41 16.5 4.84 16.3C3.84 15.95 3.05 15.16 2.7 14.16C2.5 13.59 2.5 12.9 2.5 11.5V9.5C2.5 6.2 2.5 4.55 3.53 3.53C4.55 2.5 6.2 2.5 9.5 2.5H11.5C12.9 2.5 13.59 2.5 14.16 2.7C15.16 3.05 15.95 3.84 16.3 4.84C16.5 5.41 16.5 6.1 16.5 7.5"/>',
  share:
    '<path d="M21 6.5C21 8.16 19.66 9.5 18 9.5C16.34 9.5 15 8.16 15 6.5C15 4.84 16.34 3.5 18 3.5C19.66 3.5 21 4.84 21 6.5Z"/><path d="M9 12C9 13.66 7.66 15 6 15C4.34 15 3 13.66 3 12C3 10.34 4.34 9 6 9C7.66 9 9 10.34 9 12Z"/><path d="M21 17.5C21 19.16 19.66 20.5 18 20.5C16.34 20.5 15 19.16 15 17.5C15 15.84 16.34 14.5 18 14.5C19.66 14.5 21 15.84 21 17.5Z"/><path d="M8.73 10.75L15.23 7.75M8.73 13.25L15.23 16.25"/>',
};

export function glyph(name) {
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${PATHS[name] || ""}</svg>`;
}

// roleOf is a client's role in a session, the way the share hub reports it
// to each client (internal/share/hub.go sessionPayload): the driver drives,
// the owner keeps the right to take control back, everyone else follows.
export function roleOf(id, { ownerId, driverId }) {
  if (id && id === driverId) return "driver";
  if (id && id === ownerId) return "owner";
  return "follower";
}

const ROLE_LABEL = { driver: "Driving", owner: "Owner", follower: "Following" };
const ROLE_GLYPH = { driver: "steering", owner: "steering", follower: "eye" };

export function roleChipHTML(role, label = ROLE_LABEL[role]) {
  return `<span class="k-role k-role--${role}">${glyph(ROLE_GLYPH[role])}${escapeHtml(label)}</span>`;
}

// initials for an avatar: the first letter of the first two words.
export function initials(name) {
  const words = String(name || "").trim().split(/\s+/).filter(Boolean);
  const out = words.slice(0, 2).map((w) => [...w][0] || "").join("");
  return (out || "?").toUpperCase();
}

const KIND_LABEL = { webrtc: "joined by link", web: "Keel window", tui: "terminal" };

// sharePeople lists who is attached to a session, you first, then whoever
// drives, then by arrival. Each row: { id, name, you, role, sub, viaLink }.
export function sharePeople({ attached = [], you = null, ownerId = 0, driverId = 0 }, now = Date.now()) {
  const youId = you?.id || 0;
  const rows = attached.map((c) => {
    const role = roleOf(c.id, { ownerId, driverId });
    const isYou = !!youId && c.id === youId;
    const at = Date.parse(c.attachedAt || "");
    const when = Number.isFinite(at) ? ageLabel(at, now) : "";
    let sub;
    if (isYou) sub = c.id === ownerId ? "owner · this window" : "this window";
    else sub = [c.id === ownerId ? "owner" : KIND_LABEL[c.kind] || "", when].filter(Boolean).join(" · ");
    return { id: c.id, name: isYou ? "You" : c.name || "guest", initials: initials(c.name || "guest"), you: isYou, role, sub, viaLink: c.kind === "webrtc", at: Number.isFinite(at) ? at : 0 };
  });
  const rank = (r) => (r.you ? 0 : r.role === "driver" ? 1 : 2);
  return rows.sort((a, b) => rank(a) - rank(b) || a.at - b.at);
}

// watchingCount is how many people watch through a share link.
export function watchingCount(attached = []) {
  return attached.filter((c) => c.kind === "webrtc").length;
}

// isControlRequest: the runtime broadcasts a "control" event for a request
// (RequestControl: fromId, no targetId) and for a hand-over or take-back
// (GrantControl, TakeControl: always a targetId).
export function isControlRequest(msg) {
  return msg?.type === "control" && !!msg.fromId && !msg.targetId;
}

export function clientName(attached = [], id) {
  return attached.find((c) => c.id === id)?.name || "Someone";
}

// followState describes a session from a follower's seat: who owns and who
// drives it, by name, and whether that is you.
export function followState({ attached = [], you = null, ownerId = 0, driverId = 0, title = "" }) {
  const youId = you?.id || 0;
  return {
    owner: ownerId ? clientName(attached, ownerId) : "the host",
    driver: driverId === youId && youId ? "You" : driverId ? clientName(attached, driverId) : "",
    driving: !!youId && driverId === youId,
    host: title || "session",
  };
}

// followBannerHTML is the haze banner over a joined tab (§5.7). There is no
// latency figure: the data channel does not measure one.
export function followBannerHTML(st) {
  const lead = st.driving ? "Driving" : "Following";
  const tail = st.driving ? "you have control" : "read-only";
  const ask = st.driving
    ? ""
    : `<button type="button" class="k-btn k-btn--sm" data-request="1">${glyph("steering")}Ask for control</button>`;
  return `${glyph(st.driving ? "steering" : "eye")}<span>${lead} <b>${escapeHtml(st.owner)}</b>'s session on <b>${escapeHtml(st.host)}</b> · ${tail}</span>${ask}<button type="button" class="k-btn k-btn--sm k-btn--ghost" data-join-leave="1">Leave</button>`;
}

// isTypingKey: a keydown that would type into a terminal (not a shortcut,
// not a bare modifier). Used only to tell a follower why nothing happened.
export function isTypingKey(ev) {
  if (!ev || ev.type !== "keydown" || ev.metaKey || ev.ctrlKey || ev.altKey) return false;
  if (typeof ev.key !== "string") return false;
  return [...ev.key].length === 1 || ev.key === "Enter" || ev.key === "Backspace" || ev.key === "Tab" || ev.key === "Delete";
}

function personHTML(p, { grantOne }) {
  const avatar = `<span class="k-avatar ${p.you ? "k-avatar--you" : ""}" aria-hidden="true">${escapeHtml(p.initials || initials(p.name))}</span>`;
  const grant =
    p.canGrant && !grantOne
      ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-grant="${escapeHtml(p.id)}" aria-label="Pass control to ${escapeHtml(p.name)}">${glyph("steering")}Pass control</button>`
      : "";
  return `<div class="k-person" data-person="${escapeHtml(p.id)}">
    ${avatar}
    <div class="k-person-main"><span class="k-person-name">${escapeHtml(p.name)}</span><span class="k-person-sub">${escapeHtml(p.sub)}</span></div>
    ${roleChipHTML(p.role)}
    ${grant}
  </div>`;
}

// sharePopoverHTML is the Share popover's body (§5.6). model:
//   title    the shared session's name
//   status   "pending" | "ready" | "error"
//   url      the join link once ready
//   error    the failure, when status is "error"
//   people   sharePeople rows
//   canGrant you drive or own the session (runtime GrantControl allows both)
//   canTake  you own it and someone else drives
//   canAsk   you neither own nor drive it
//   copied   the link was put on the clipboard (the Copy button says so)
export function sharePopoverHTML(model) {
  const { title = "", status = "pending", url = "", error = "", people = [], canGrant = false, canTake = false, canAsk = false, copied = false } = model;
  const badge =
    status === "ready"
      ? `<span class="k-badge k-badge--tide"><span class="k-dot k-dot--live" aria-hidden="true"></span>Live link</span>`
      : status === "pending"
        ? `<span class="k-badge"><span class="k-dot k-dot--connecting" aria-hidden="true"></span>Creating link</span>`
        : `<span class="k-badge k-badge--flare">Not shared</span>`;
  const link =
    status === "error"
      ? `<p class="k-help k-share-error" role="alert">${escapeHtml(error || "Sharing failed.")}</p>`
      : `<div class="k-field">
      <label class="k-label" for="share-link">Join link</label>
      <div class="k-inputgroup"><input class="k-input k-input--mono" id="share-link" readonly value="${escapeHtml(url)}" placeholder="Creating link…" />
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-share-copy ${url ? "" : "disabled"}>${glyph("copy")}${copied ? "Copied" : "Copy"}</button></div>
      <p class="k-help">Works until you stop sharing or close this pane, 24 hours at most. You are asked before anyone joins; a request you do not answer is turned away after 2 minutes. Peer-to-peer over WebRTC; your loopback API stays private.</p>
    </div>`;
  const rows = people.map((p) => ({ ...p, canGrant: canGrant && !p.you && p.role !== "driver" }));
  const grantable = rows.filter((p) => p.canGrant);
  const grantOne = grantable.length === 1;
  let action = "";
  if (grantOne) {
    const p = grantable[0];
    action = `<button type="button" class="k-btn k-btn--sm" data-grant="${escapeHtml(p.id)}">${glyph("steering")}Pass control to ${escapeHtml(p.name)}</button>`;
  } else if (canTake) {
    action = `<button type="button" class="k-btn k-btn--sm" data-take="1">${glyph("steering")}Take control</button>`;
  } else if (canAsk) {
    action = `<button type="button" class="k-btn k-btn--sm" data-request="1">${glyph("steering")}Ask for control</button>`;
  }
  const list = rows.length
    ? `<div class="k-share-people">
      <div class="k-menu-label">In this session · ${rows.length}</div>
      <div class="k-people">${rows.map((p) => personHTML(p, { grantOne })).join("")}</div>
    </div>`
    : "";
  return `<div class="k-share-head">
      <div class="k-share-titlerow"><span class="k-share-title" id="share-pop-title">Share session</span><span class="k-mono k-muted k-share-session">${escapeHtml(title)}</span>${badge}</div>
      <p class="k-help">People with the link see this pane's terminal only, from when you started sharing. Never your hosts, keys, vault or other panes.</p>
    </div>
    <div class="k-share-body">${link}</div>
    ${list}
    <div class="k-dialog-foot k-share-foot">
      ${status === "error" ? "" : `<button type="button" class="k-btn k-btn--danger k-btn--sm" data-share-stop>Stop sharing</button>`}
      <span class="k-spacer"></span>
      ${action}
    </div>`;
}
