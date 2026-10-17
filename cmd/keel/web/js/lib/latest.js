// A debounced "latest request wins" runner for type-to-filter inputs.
//
// The host search fired one /ui/hosts request per keystroke and painted
// every response, so a slow early response could land after a newer one
// and overwrite the list with stale results. schedule(job) waits until
// typing pauses for delayMs, aborts the request still in flight, and hands
// the job an AbortSignal plus isStale(), which turns true as soon as a newer
// job starts: the job checks it before touching the DOM.

export function createLatestRequest({
  delayMs = 120,
  setTimer = (fn, ms) => setTimeout(fn, ms),
  clearTimer = (id) => clearTimeout(id),
  makeController = () => new AbortController(),
} = {}) {
  let timer = null;
  let seq = 0;
  let controller = null;

  function start(job) {
    timer = null;
    controller?.abort();
    controller = makeController();
    const mine = ++seq;
    const isStale = () => mine !== seq;
    return Promise.resolve()
      .then(() => job(controller.signal, isStale))
      .catch((err) => {
        if (isStale() || err?.name === "AbortError") return undefined;
        throw err;
      });
  }

  return {
    // schedule runs job after the debounce; the returned promise resolves to
    // the job's result, or undefined if a newer job replaced it.
    schedule(job) {
      if (timer !== null) clearTimer(timer);
      seq++; // a pending or running older job is stale from now on
      return new Promise((resolve, reject) => {
        timer = setTimer(() => start(job).then(resolve, reject), delayMs);
      });
    },
    // now runs job immediately (Enter, Escape), still superseding older ones.
    now(job) {
      if (timer !== null) clearTimer(timer);
      return start(job);
    },
  };
}
