// The vault sidebar's width, as keel-ui.sidebarWidth keeps it: a whole
// number of pixels, or null for the default (the size-sidebar token). The
// bounds come from the tokens too (size-rail up to twice size-sidebar);
// the caller reads them, and keel.css clamps to the same range.

// clampWidth is w rounded and held inside bounds.
export function clampWidth(w, { min, max }) {
  return Math.min(max, Math.max(min, Math.round(w)));
}

// savedWidth reads a stored width back: null (the default) for anything
// that is not a finite number.
export function savedWidth(raw, bounds) {
  if (raw == null || raw === "") return null;
  const n = Number(raw);
  return Number.isFinite(n) ? clampWidth(n, bounds) : null;
}

// dragWidth is the width while the handle is dragged from startX (at
// startWidth) to x.
export function dragWidth(startWidth, startX, x, bounds) {
  return clampWidth(startWidth + (x - startX), bounds);
}

// keyWidth is the width after a key on the focused handle, or undefined
// when the key does nothing: arrows step by bounds.step, Home and End go
// to the bounds, Enter goes back to the default (null).
export function keyWidth(key, width, bounds) {
  switch (key) {
    case "ArrowLeft":
      return clampWidth(width - bounds.step, bounds);
    case "ArrowRight":
      return clampWidth(width + bounds.step, bounds);
    case "Home":
      return bounds.min;
    case "End":
      return bounds.max;
    case "Enter":
      return null;
  }
  return undefined;
}
