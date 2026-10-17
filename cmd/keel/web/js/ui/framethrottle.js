// Runs a job on an animation frame at most once per intervalMs. The first
// call after a quiet spell (a key echo) runs on the next frame; calls during
// the interval collapse into one trailing run, so the last call is always
// followed by a run that sees its state.
//
// term.js uses one per pane for the native WebGL invalidate, which redraws
// every row and rebuilds the canvas layer: too costly for every frame of
// streaming output.

export function createFrameThrottle({
  intervalMs,
  now = () => performance.now(),
  raf = (fn) => requestAnimationFrame(fn),
  setTimer = (fn, ms) => setTimeout(fn, ms),
}) {
  let frameQueued = false;
  let timer = false;
  let lastRun = -Infinity;
  let job = null;

  const frame = () => {
    frameQueued = true;
    raf(() => {
      frameQueued = false;
      lastRun = now();
      job?.();
    });
  };

  return {
    schedule(fn) {
      job = fn;
      if (frameQueued || timer) return;
      const wait = intervalMs - (now() - lastRun);
      if (wait <= 0) {
        frame();
        return;
      }
      timer = true;
      setTimer(() => {
        timer = false;
        frame();
      }, wait);
    },
  };
}
