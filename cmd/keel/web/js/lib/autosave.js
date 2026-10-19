// When does an edited notebook save? No DOM; timers are injected.
//
// Every notebook save is a store save, and every store save pushes all the
// vault lists to every window and makes each one reload its JSON state
// (internal/httpapi/uistream.go). Saving on every 400 ms pause in typing
// made one of those pushes per pause (issue #82), so edits settle first: save runs idleMs after the last edit, and at least every
// maxWaitMs while edits keep coming, so a long burst is never only in the
// page. One save runs at a time; an edit made during a save queues one more
// after it, so an older body never lands after a newer one. flush saves a
// pending edit now (the pane closes, the page hides).
export function createAutosave({ save, idleMs = 1000, maxWaitMs = 5000, timers = globalThis }) {
  let idle = 0;
  let max = 0;
  let dirty = false;
  let running = null;

  function clear() {
    if (idle) timers.clearTimeout(idle);
    if (max) timers.clearTimeout(max);
    idle = 0;
    max = 0;
  }

  function fire() {
    clear();
    // The running save checks dirty again when it ends.
    if (running) return running;
    if (!dirty) return Promise.resolve();
    running = (async () => {
      try {
        while (dirty) {
          dirty = false;
          try {
            await save();
          } catch {
            /* save reports its own errors */
          }
        }
      } finally {
        running = null;
      }
    })();
    return running;
  }

  return {
    // schedule records an edit.
    schedule() {
      dirty = true;
      if (idle) timers.clearTimeout(idle);
      idle = timers.setTimeout(fire, idleMs);
      if (!max) max = timers.setTimeout(fire, maxWaitMs);
    },
    // flush saves a pending edit now; it resolves when nothing is pending.
    flush() {
      return fire();
    },
    get pending() {
      return dirty || !!running;
    },
  };
}
