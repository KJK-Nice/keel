// The one way the page writes to a session (docs/ui-redesign.md §6): the
// session socket, or a joined share's data channel, and nothing else in
// the page calls send. What goes through it:
//   sendSessionJSON(pane, msg)     a control message (resize, focus, grant…)
//   sendSessionBytes(pane, bytes)  the user's keystrokes, from wirePaneTerminal
//                                  only, which the chrome can block
//   sendApprovedInject(cmd, pane)  bytes the user approved: a server nonce
//                                  first (inject-challenge), then the inject
//   typeDirect / checkDirect       a one-line type keel serve allows without
//                                  the dialog (internal/httpapi/typedirect.go)
// and the answers keel serve sends back for them (settleInjectTicket,
// settleDirect). Approve before type itself (the dialog) and the keyboard
// gate (ptyWritesBlocked) stay with term.js; the agent never reaches here
// but through them. The caller passes:
//   isTermPane, isDriving, isNotebookPane(pane)
//   connOf(pane), viewOf(pane)     the pane's connection and view
//   termTarget(pane)               the term pane a write goes to
//   focusedPane(), tabOf(pane), tabPanes(tab)
//   holdPtyFromChrome(ms)          keep the keyboard off the PTY a moment
//   toast(message)
import * as TD from "./lib/typedirect.js";

export function createPty({ isTermPane, isDriving, isNotebookPane, connOf, viewOf, termTarget, focusedPane, tabOf, tabPanes, holdPtyFromChrome, toast }) {
  // settleInjectTicket hands the server's nonce to whoever is waiting for
  // it (requestInjectTicket).
  function settleInjectTicket(pane, nonce) {
    const done = connOf(pane).injectTicketWaiter;
    connOf(pane).injectTicketWaiter = null;
    if (typeof done === "function") done(nonce);
  }

  function sendSessionJSON(pane, obj) {
    const raw = JSON.stringify(obj);
    if (pane?.remote) {
      if (connOf(pane).dc?.readyState === "open") connOf(pane).dc.send(raw);
      return;
    }
    if (connOf(pane)?.ws?.readyState === WebSocket.OPEN) connOf(pane).ws.send(raw);
  }

  function sendSessionBytes(pane, bytes) {
    if (!isTermPane(pane) || !pane?.runtimeId) return;
    if (pane?.remote) {
      if (connOf(pane).dc?.readyState === "open") connOf(pane).dc.send(bytes);
      return;
    }
    if (connOf(pane)?.ws?.readyState === WebSocket.OPEN) connOf(pane).ws.send(bytes);
  }

  function requestInjectTicket(pane, command) {
    return new Promise((resolve) => {
      const timer = setTimeout(() => {
        if (connOf(pane).injectTicketWaiter) {
          connOf(pane).injectTicketWaiter = null;
          resolve("");
        }
      }, 2500);
      connOf(pane).injectTicketWaiter = (nonce) => {
        clearTimeout(timer);
        connOf(pane).injectTicketWaiter = null;
        resolve(nonce || "");
      };
      sendSessionJSON(pane, { type: "inject-challenge", command });
    });
  }

  // opts.agent marks the bytes as an agent proposal the user approved
  // ({conversationId, proposalId, shift}): they go to that one pane only
  // (never to the panes Sync input would add), and keel serve checks the
  // proposal with the agent service before writing them. opts.suggestion
  // ({suggestionId, option, shift}) does the same for ⇥ / ⇧⇥ on an in-pane
  // suggestion (§5.5).
  async function sendApprovedInject(command, targetPane, opts = {}) {
    const pane = termTarget(targetPane || focusedPane());
    if (!pane || !isTermPane(pane)) {
      toast("Open a session first");
      return false;
    }
    const tab = tabOf(pane);
    const single = !!(opts.agent || opts.suggestion);
    const targets = single ? [pane] : tab?.sync ? tabPanes(tab).filter((p) => isTermPane(p) && p.runtimeId) : [pane];
    let sent = false;
    for (const p of targets) {
      const open = p.remote ? connOf(p).dc?.readyState === "open" : connOf(p).ws?.readyState === WebSocket.OPEN;
      if (!open || !isDriving(p) || !isTermPane(p) || (single && p.remote)) continue;
      const nonce = await requestInjectTicket(p, command);
      if (!nonce) continue;
      const msg = { type: "inject", command, nonce };
      if (opts.agent) msg.agent = opts.agent;
      if (opts.suggestion) msg.suggestion = opts.suggestion;
      if (opts.notebook) msg.notebook = true;
      sendSessionJSON(p, msg);
      sent = true;
    }
    if (!sent) {
      toast(isDriving(pane) ? "Session is not connected" : "You are following — take or request control to type");
      return false;
    }
    if (!isNotebookPane(focusedPane())) viewOf(pane).term?.focus?.();
    return true;
  }

  let directSeq = 0;

  function settleDirect(pane, msg) {
    const done = connOf(pane).directWaiters?.get(msg?.ref);
    if (!done) return;
    connOf(pane).directWaiters.delete(msg.ref);
    done(msg);
  }

  // askDirect sends a type-check or a direct inject and waits for its
  // answer (null when keel serve did not answer in time).
  function askDirect(pane, frame, ms) {
    return new Promise((resolve) => {
      const ref = `d${++directSeq}`;
      if (!connOf(pane).directWaiters) connOf(pane).directWaiters = new Map();
      const timer = setTimeout(() => {
        if (connOf(pane).directWaiters.delete(ref)) resolve(null);
      }, ms);
      connOf(pane).directWaiters.set(ref, (msg) => {
        clearTimeout(timer);
        resolve(msg);
      });
      sendSessionJSON(pane, { ...frame, direct: { ...frame.direct, ref } });
    });
  }

  function directReady(pane, command) {
    return !!pane && isTermPane(pane) && !pane.remote && isDriving(pane) && connOf(pane).ws?.readyState === WebSocket.OPEN && !TD.altScreen(viewOf(pane).term) && TD.directLine(command);
  }

  // typeDirect asks keel serve to type command into pane, without Enter.
  // direct is {kind, snippetId?, notebookId?}; agent is the agent part of
  // an inject ({conversationId | groupRunId, proposalId}). Answers
  // {ok: true} (typed), {dialog: true, reason} (nothing typed: Approve
  // before type decides) or {ok: false, message} (refused).
  async function typeDirect(pane, command, direct, agent = null) {
    if (!directReady(pane, command)) return { dialog: true, reason: "" };
    // One at a time per pane: a second click while the first is on its
    // way would type the command twice on the prompt line.
    if (connOf(pane).directBusy) return { ok: false, message: "Still typing the last command into this pane." };
    connOf(pane).directBusy = true;
    try {
      const nonce = await requestInjectTicket(pane, command);
      if (!nonce) return { dialog: true, reason: "" };
      holdPtyFromChrome();
      // What this terminal shows goes with it: keel serve ORs it with its
      // own view (alternate screen, an input prompt on the cursor line).
      const frame = { type: "inject", command, nonce, direct: { ...direct, altScreen: TD.altScreen(viewOf(pane).term), promptLine: TD.cursorLine(viewOf(pane).term) } };
      if (agent) frame.agent = agent;
      const msg = await askDirect(pane, frame, 8000);
      // No answer: it may have been typed. Never open a dialog that could
      // type it a second time.
      if (!msg) return { ok: false, message: "keel serve did not answer. Check the pane before you press Type again." };
      if (msg.dialog) return { dialog: true, reason: msg.reason || "" };
      if (!msg.ok) return { ok: false, message: msg.message || "Not typed." };
      return { ok: true };
    } finally {
      connOf(pane).directBusy = false;
    }
  }

  // checkDirect asks whether typeDirect would type now (nothing written).
  async function checkDirect(pane, command, direct, agent = null) {
    if (!directReady(pane, command)) return false;
    const frame = { type: "type-check", command, direct: { ...direct, altScreen: TD.altScreen(viewOf(pane).term), promptLine: TD.cursorLine(viewOf(pane).term) } };
    if (agent) frame.agent = agent;
    const msg = await askDirect(pane, frame, 8000);
    return !!msg?.direct;
  }

  return { sendSessionJSON, sendSessionBytes, settleInjectTicket, sendApprovedInject, settleDirect, typeDirect, checkDirect };
}
