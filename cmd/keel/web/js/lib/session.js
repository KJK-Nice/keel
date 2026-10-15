// The session WebSocket protocol (/ws/session, and the same JSON over a
// share's data channel). Pure functions: no DOM, no state. Binary frames
// are PTY output; text frames are JSON control messages.

// sessionSocketURL is the local session socket for a pane.
export function sessionSocketURL(loc, { runtimeId, cols, rows, name }) {
  const proto = loc.protocol === "https:" ? "wss" : "ws";
  const q = new URLSearchParams({
    id: runtimeId,
    cols: String(cols || 80),
    rows: String(rows || 24),
    name: name || "guest",
    kind: "web",
  });
  return `${proto}://${loc.host}/ws/session?${q}`;
}

// statusClass maps a session status to the pane's CSS state, or fallback
// for statuses that don't change it.
export function statusClass(status, fallback) {
  if (status === "ready") return "is-ready";
  if (status === "connecting") return "is-connecting";
  if (status === "error" || status === "exited") return "is-error";
  return fallback;
}

// presenceFields picks the attach/control fields a message carries.
export function presenceFields(msg) {
  const out = {};
  if (msg.you) out.you = msg.you;
  if (msg.attached) out.attached = msg.attached;
  if (msg.driverId != null) out.driverId = msg.driverId;
  if (msg.ownerId != null) out.ownerId = msg.ownerId;
  if (msg.title) out.title = msg.title;
  return out;
}

// parseSessionMessage parses a text frame; null for anything that isn't a
// JSON object with a type.
export function parseSessionMessage(text) {
  try {
    const msg = JSON.parse(text);
    return msg && typeof msg === "object" && typeof msg.type === "string" ? msg : null;
  } catch {
    return null;
  }
}

// dispatchSessionMessage calls the handler for msg.type:
//   injectChallenge(nonce)   reply to an inject-challenge
//   joinRequest / joinResolved / joinFailed (msg)   share admission
//   status(msg)              status change (also carries presence)
//   presence(msg)            ready, presence or control
//   agentType(msg)           keel serve typed (or refused) an agent proposal
//   agentDetect(msg)         is the prompt line plain language (§5.5)
//   agentSuggest(msg)        a suggestion for the prompt line, or why not
//   agentSuggestRisk(msg)    Jev rated a shown suggestion stricter
//   agentInsert(msg)         keel serve inserted (or refused) a suggestion
//   error(msg)               the session failed
// Unknown types are ignored. Returns whether a handler ran.
export function dispatchSessionMessage(msg, h) {
  const run = (name, arg) => {
    if (typeof h[name] !== "function") return false;
    h[name](arg);
    return true;
  };
  switch (msg?.type) {
    case "inject-challenge":
      return run("injectChallenge", msg.nonce || "");
    case "join-request":
      return run("joinRequest", msg);
    case "join-resolved":
      return run("joinResolved", msg);
    case "join-failed":
      return run("joinFailed", msg);
    case "status":
      return run("status", msg);
    case "ready":
    case "presence":
    case "control":
      return run("presence", msg);
    case "agent-type":
      return run("agentType", msg);
    case "agent-detect":
      return run("agentDetect", msg);
    case "agent-suggest":
      return run("agentSuggest", msg);
    case "agent-suggest-risk":
      return run("agentSuggestRisk", msg);
    case "agent-insert":
      return run("agentInsert", msg);
    case "error":
      return run("error", msg);
    default:
      return false;
  }
}
