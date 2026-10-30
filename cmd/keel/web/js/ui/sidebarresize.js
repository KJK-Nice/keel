// The vault sidebar's resize handle (#sidebar-sash, a role="separator"
// beside #vault-sidebar): drag it, or focus it and use the arrows, Home
// and End; double-click or Enter goes back to the default width. The
// width is --sidebar-width on the root (keel.css clamps it to the same
// bounds). The caller passes:
//   doc            the document
//   bounds()       { min, max, def, step } in px, read from the tokens
//   width()        the saved width, or null for the default
//   save(w)        keeps w (a number, or null for the default)
//   live()         the sidebar changed size (refit the terminals); called
//                  at most once a frame while dragging, and once at the end
import { dragWidth, keyWidth, savedWidth } from "../lib/sidebarwidth.js";

export function createSidebarResize({ doc = document, bounds, width, save, live = () => {} }) {
  const root = doc.documentElement;
  const sash = doc.getElementById("sidebar-sash");
  const sidebar = doc.getElementById("vault-sidebar");
  // The saved width held to the bounds, or null for the default.
  const current = () => savedWidth(width(), bounds());

  // apply shows w (null: the default) and says so to assistive tech.
  function apply(w) {
    const b = bounds();
    if (w == null) root.style.removeProperty("--sidebar-width");
    else root.style.setProperty("--sidebar-width", `${w}px`);
    if (!sash) return;
    sash.setAttribute("aria-valuemin", String(b.min));
    sash.setAttribute("aria-valuemax", String(b.max));
    sash.setAttribute("aria-valuenow", String(w ?? b.def));
  }

  function set(w) {
    save(w);
    apply(w);
    live();
  }

  if (sash) {
    sash.addEventListener("pointerdown", (ev) => {
      if (ev.button !== 0) return;
      ev.preventDefault();
      sash.setPointerCapture?.(ev.pointerId);
      sash.classList.add("is-dragging");
      const startX = ev.clientX;
      const start = sidebar?.getBoundingClientRect().width || current() || bounds().def;
      let frame = 0;
      let next = null;
      const move = (e) => {
        next = dragWidth(start, startX, e.clientX, bounds());
        apply(next);
        if (!frame) {
          frame = requestAnimationFrame(() => {
            frame = 0;
            live();
          });
        }
      };
      const up = () => {
        sash.removeEventListener("pointermove", move);
        sash.removeEventListener("pointerup", up);
        sash.removeEventListener("pointercancel", up);
        sash.classList.remove("is-dragging");
        if (frame) cancelAnimationFrame(frame);
        if (next != null) set(next);
      };
      sash.addEventListener("pointermove", move);
      sash.addEventListener("pointerup", up);
      sash.addEventListener("pointercancel", up);
    });
    sash.addEventListener("dblclick", () => set(null));
    sash.addEventListener("keydown", (ev) => {
      const b = bounds();
      const next = keyWidth(ev.key, current() ?? b.def, b);
      if (next === undefined) return;
      ev.preventDefault();
      set(next);
    });
  }

  apply(current());
  return { apply: () => apply(current()) };
}
