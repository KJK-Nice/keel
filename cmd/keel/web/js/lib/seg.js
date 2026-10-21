// A k-seg segmented control used as a value picker for a few fixed
// choices (2–4: theme, smoothing, identity kind, forward kind). Markup
// (internal/ui/combobox.templ segField / segControl):
//
//   <div class="k-seg" id="…" role="group" data-seg>
//     <button type="button" data-seg-value="a" aria-pressed="true">A</button>…
//     <input type="hidden" name="…" value="a" data-seg-input>   (inside a form)
//   </div>
//
// A press sets aria-pressed and the hidden input, then dispatches
// "select-change" ({value, label}) on the group and "change" on the
// input, the same events a k-combobox sends. One delegated listener.

export function segValue(root) {
  const input = root?.querySelector?.(":scope > [data-seg-input]");
  if (input) return input.value;
  return root?.querySelector?.(':scope > [data-seg-value][aria-pressed="true"]')?.dataset.segValue ?? "";
}

// setSegValue presses a value without events. An unknown value is ignored.
export function setSegValue(root, value) {
  const btns = [...(root?.querySelectorAll?.(":scope > [data-seg-value]") || [])];
  const v = String(value ?? "");
  if (!btns.some((b) => b.dataset.segValue === v)) return false;
  for (const b of btns) b.setAttribute("aria-pressed", String(b.dataset.segValue === v));
  const input = root.querySelector(":scope > [data-seg-input]");
  if (input) input.value = v;
  return true;
}

function onClick(ev) {
  const btn = ev.target.closest?.("[data-seg-value]");
  const root = btn?.parentElement;
  if (!root?.matches?.(".k-seg[data-seg]") || btn.disabled) return;
  const value = btn.dataset.segValue;
  const before = segValue(root);
  setSegValue(root, value);
  root.dispatchEvent(new CustomEvent("select-change", { bubbles: true, detail: { value, label: btn.textContent.trim() } }));
  const input = root.querySelector(":scope > [data-seg-input]");
  if (input && before !== value) input.dispatchEvent(new Event("change", { bubbles: true }));
}

export function installSeg(doc = document) {
  if (!doc || doc.__keelSeg) return;
  doc.__keelSeg = true;
  doc.addEventListener("click", onClick);
}
