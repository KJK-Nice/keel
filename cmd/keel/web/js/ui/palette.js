// The command palette's list: filters the catalog as the user types,
// keeps the selection, and runs the chosen item. Opening and closing the
// dialog stays with the caller (it also moves PTY focus).

import { clampCursor, filterPalette, moveCursor, optionId, paletteHTML } from "../lib/palette.js";

export function createPaletteList({ form, input, list, catalog, run }) {
  let visible = [];
  let cursor = 0;

  function paint() {
    if (!list) return;
    visible = filterPalette(catalog(), input?.value || "");
    cursor = clampCursor(cursor, visible.length);
    list.innerHTML = paletteHTML(visible, cursor, input?.value || "");
    if (visible.length) input?.setAttribute("aria-activedescendant", optionId(cursor));
    else input?.removeAttribute("aria-activedescendant");
    list.querySelector(".k-palette-item[data-highlighted]")?.scrollIntoView({ block: "nearest" });
  }

  // reset clears the query and selects the first item.
  function reset() {
    if (input) input.value = "";
    cursor = 0;
    paint();
  }

  function move(delta) {
    if (!visible.length) return;
    cursor = moveCursor(cursor, delta, visible.length);
    paint();
  }

  // run gets undefined when nothing matches; it still closes the palette.
  // opts.split is ⌥↵: open in a split.
  function choose(i, opts = {}) {
    run(visible[i], opts);
  }

  form?.addEventListener("submit", (ev) => {
    ev.preventDefault();
    choose(cursor);
  });
  input?.addEventListener("input", () => {
    cursor = 0;
    paint();
  });
  input?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && (ev.altKey || ev.metaKey || ev.ctrlKey)) {
      // ⌥↵ opens in a split. ⌘↵ and Ctrl+↵ are not bound here; they must
      // not fall through to the plain ↵.
      ev.preventDefault();
      if (ev.altKey && !ev.metaKey && !ev.ctrlKey) choose(cursor, { split: true });
      return;
    }
    if (ev.key === "ArrowDown") {
      ev.preventDefault();
      move(1);
    } else if (ev.key === "ArrowUp") {
      ev.preventDefault();
      move(-1);
    }
  });
  // pointerdown, not click: the input would blur and close things first.
  list?.addEventListener("pointerdown", (ev) => {
    const btn = ev.target.closest?.("[data-palette-index]");
    if (!btn) return;
    ev.preventDefault();
    const i = Number(btn.dataset.paletteIndex);
    if (!Number.isFinite(i)) return;
    cursor = i;
    choose(i, { split: !!ev.altKey });
  });

  return { paint, reset };
}
