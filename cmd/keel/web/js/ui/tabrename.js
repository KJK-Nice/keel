// Renaming a tab in place: its label becomes a text field that grows with
// what is typed. ↵ or a click elsewhere keeps the name, Esc puts the old
// one back; Space types a space (the strip would take it as a click). The
// caller passes:
//   strip              the tab strip (#tabs)
//   title(id)          the name the field starts with
//   canRename(id)      false for a tab page or a tab that is gone
//   apply(id, value, ok)   keep the name (ok) or not
//   done()             after apply: the strip renders again
//   prepareInput(el)   e.g. turn the system's text assist off
export function createTabRename({ doc = document, strip, title, canRename, apply, done, prepareInput = () => {} }) {
  let editing = null;
  let finishCurrent = null;

  const fit = (input) => {
    input.style.width = "0px";
    input.style.width = `${Math.max(1, Math.ceil(input.scrollWidth))}px`;
  };

  // begin turns tab id's label into the field, unless a rename is already
  // under way.
  function begin(id) {
    if (editing || !canRename(id)) return;
    const btn = strip.querySelector(`.tab[data-tab="${CSS.escape(id)}"]`);
    const label = btn?.querySelector(".tab-label");
    if (!label) return;
    editing = id;
    const input = doc.createElement("input");
    input.type = "text";
    input.className = "tab-rename";
    input.size = 1;
    prepareInput(input);
    input.value = title(id);
    input.setAttribute("aria-label", "Rename tab");
    label.replaceWith(input);
    strip._keelHTML = null; // the strip no longer matches its markup
    input.focus({ preventScroll: true });
    input.select();
    fit(input);
    let finished = false;
    const finish = (ok) => {
      if (finished) return;
      finished = true;
      // A rename that was forgotten (its tab closed) leaves a later one be.
      if (finishCurrent === finish) {
        editing = null;
        finishCurrent = null;
      }
      apply(id, input.value, ok);
      done();
    };
    finishCurrent = finish;
    btn.addEventListener(
      "click",
      (ev) => {
        if (ev.target === btn) {
          ev.preventDefault();
          ev.stopPropagation();
        }
      },
      true,
    );
    input.addEventListener("pointerdown", (ev) => ev.stopPropagation());
    input.addEventListener("keyup", (ev) => {
      ev.stopPropagation();
      if (ev.key === " " || ev.code === "Space") ev.preventDefault();
    });
    input.addEventListener("keypress", (ev) => ev.stopPropagation());
    input.addEventListener("input", () => fit(input));
    input.addEventListener("keydown", (ev) => {
      ev.stopPropagation();
      if (ev.key === " " || ev.code === "Space") {
        ev.preventDefault();
        const start = input.selectionStart ?? input.value.length;
        const end = input.selectionEnd ?? start;
        input.setRangeText(" ", start, end, "end");
        fit(input);
        return;
      }
      if (ev.key === "Enter") {
        ev.preventDefault();
        input.blur();
      } else if (ev.key === "Escape") {
        ev.preventDefault();
        finish(false);
      }
    });
    input.addEventListener("blur", () => finish(true));
  }

  // keep ends a rename with the name typed so far (a click elsewhere on the
  // strip).
  function keep() {
    finishCurrent?.(true);
  }

  // forget drops a rename of a tab that is closing, keeping nothing.
  function forget(id) {
    if (editing !== id) return;
    editing = null;
    finishCurrent = null;
  }

  return { begin, keep, forget, editing: () => editing };
}
