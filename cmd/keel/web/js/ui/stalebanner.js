// A bar across the top of the window when keel serve is from an older (or
// just different) build than the app: the daemon serves the UI, so the
// window shows the old version until the runtime restarts.

export function showStaleRuntimeBanner({ doc = document, stopRuntime }) {
  if (doc.querySelector(".stale-runtime")) return;
  const bar = doc.createElement("div");
  bar.className = "stale-runtime";
  bar.setAttribute("role", "alert");

  const text = doc.createElement("p");
  text.textContent =
    "The Keel runtime that's running is from a different build than this app, so you're seeing its version of Keel. " +
    "Stop the runtime and reopen Keel to update. Open sessions will end.";

  const stop = doc.createElement("button");
  stop.type = "button";
  stop.className = "btn btn-primary";
  stop.textContent = "Stop runtime";
  stop.addEventListener("click", async () => {
    stop.disabled = true;
    try {
      await stopRuntime();
      text.textContent = "Runtime stopped. Quit and reopen Keel.";
      stop.remove();
    } catch {
      stop.disabled = false;
    }
  });

  const dismiss = doc.createElement("button");
  dismiss.type = "button";
  dismiss.className = "btn btn-ghost";
  dismiss.textContent = "Not now";
  dismiss.addEventListener("click", () => bar.remove());

  bar.append(text, stop, dismiss);
  doc.body.appendChild(bar);
}
