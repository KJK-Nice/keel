// Which terminal panes draw with xterm's WebGL renderer.
//
// Every visible pane does, solo or split, in the browser and in the native
// window. A pane that is not on screen (another tab, or hidden behind a
// zoomed pane) gives its WebGL context back, so the page holds one context
// per visible pane and never runs into WebKit's cap on live contexts, which
// drops the oldest one.
//
// The DOM renderer is only a fallback: for a pane whose WebGL context could
// not be created, or was lost maxLosses times within windowMs. Such a pane
// stays on the DOM renderer until it is closed.

export const WEBGL_MAX_LOSSES = 3;
export const WEBGL_LOSS_WINDOW_MS = 60_000;

export function createWebglGuard({
  maxLosses = WEBGL_MAX_LOSSES,
  windowMs = WEBGL_LOSS_WINDOW_MS,
  now = () => Date.now(),
} = {}) {
  let losses = [];
  let reason = "";

  return {
    // allowed is false once the pane has fallen back to the DOM renderer.
    allowed: () => !reason,
    reason: () => reason,
    // failed records a context that could not be created at all.
    failed(err) {
      if (reason) return "";
      reason = `WebGL renderer unavailable (${err?.message || err || "unknown error"})`;
      return reason;
    },
    // lost records a lost context. It returns "" while the pane may try
    // WebGL again, and the fallback reason once it may not.
    lost() {
      if (reason) return reason;
      const t = now();
      losses = losses.filter((at) => t - at < windowMs);
      losses.push(t);
      if (losses.length >= maxLosses) {
        reason = `WebGL context lost ${losses.length} times in ${Math.round(windowMs / 1000)}s`;
        return reason;
      }
      return "";
    },
  };
}

// webglPlan splits panes into those that should hold a WebGL addon (on
// screen and still allowed) and those that should not. panes are
// { id, visible, allowed, attached }; only changes are returned.
export function webglPlan(panes) {
  const attach = [];
  const drop = [];
  for (const p of panes) {
    const want = !!p.visible && !!p.allowed;
    if (want && !p.attached) attach.push(p.id);
    else if (!want && p.attached) drop.push(p.id);
  }
  return { attach, drop };
}
