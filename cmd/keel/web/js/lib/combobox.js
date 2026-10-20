// k-combobox: a searchable value picker (WAI-ARIA combobox with a listbox
// popup, docs/design/README.md "Combobox"). The same markup comes from templ
// (internal/ui/combobox.templ) and from comboboxHTML() here, so a picker in
// a dialog, a settings row or a term.js HTML string is one component.
//
//   <div class="k-combobox" data-combobox>
//     <input class="k-input k-combobox-input" role="combobox" aria-controls="ID-list" …>
//     <span class="k-combobox-chevron">…</span>
//     <input type="hidden" name="…" data-combobox-value>
//     <div class="k-menu k-combobox-pop" popover="manual" hidden>
//       <div role="listbox" id="ID-list"><div role="option" data-value="…" class="k-menu-item k-combobox-option">…</div>…</div>
//       <p class="k-combobox-empty">No matches</p>
//     </div>
//   </div>
//
// The value lives in the hidden input, so the picker posts with its form
// (the Datastar @post('/ui/forms/{kind}') dialogs). The text input has no
// name: it shows the chosen label and is the filter while the list is open.
// Picking dispatches "select-change" ({value, label}) on the text input and
// "change" on the hidden input, the events the shadcn select sent, so the
// listeners written for it keep working.
//
// Every listener is delegated from the document and installed once
// (installCombobox), so a picker that innerHTML or a Datastar morph puts in
// the page works at once and one that is removed leaves nothing behind.
// The popup is a manual popover, so it paints in the top layer above a
// modal <dialog> and is not clipped by a scrolling dialog body; while it is
// open, scroll and resize listeners keep it under the input, and they are
// removed when it closes.

import { escapeHtml } from "./html.js";

const svg = (paths, cls) =>
  `<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
const CHEVRON = '<path d="M18 9.00005C18 9.00005 13.5811 15 12 15C10.4188 15 6 9 6 9"/>';
const CHECK = '<path d="M5 13.2592L7.58583 15.9568C8.2525 16.6523 8.58583 17 9.00004 17C9.41425 17 9.74759 16.6523 10.4143 15.9568L19 7"/>';

export const NO_MATCHES = "No matches";

// ---------- Pure parts (node tests: test/combobox.test.mjs) ----------

const norm = (s) => String(s ?? "").toLocaleLowerCase();

// optionMatches: a case-insensitive substring of the label or the
// secondary line (so an address finds its host). An empty query matches all.
export function optionMatches(opt, query) {
  const q = norm(query).trim();
  if (!q) return true;
  return norm(opt?.label).includes(q) || norm(opt?.desc).includes(q);
}

// filterOptions returns the indexes of the options the query shows.
export function filterOptions(options, query) {
  const out = [];
  (options || []).forEach((o, i) => {
    if (optionMatches(o, query)) out.push(i);
  });
  return out;
}

// stepActive moves the active option among the shown, enabled ones.
// items[i] is { shown, disabled }. dir: 1 / -1 (no wrap), "first", "last".
// Returns -1 when nothing can be active.
export function stepActive(items, active, dir) {
  const ok = (i) => items[i] && items[i].shown && !items[i].disabled;
  const n = items.length;
  if (dir === "first" || dir === "last") {
    const order = dir === "first" ? [...Array(n).keys()] : [...Array(n).keys()].reverse();
    return order.find(ok) ?? -1;
  }
  if (!ok(active)) return stepActive(items, -1, dir === -1 ? "last" : "first");
  for (let i = active + dir; i >= 0 && i < n; i += dir) if (ok(i)) return i;
  return active;
}

// initialActive is the option to show as active when the list opens: the
// chosen one when it can be, else the first enabled one.
export function initialActive(items, selected) {
  if (items[selected] && items[selected].shown && !items[selected].disabled) return selected;
  return stepActive(items, -1, "first");
}

// comboKey decides what a key on the input does. state: { open, active,
// items }. Returns { action, active, prevent, stop }: action is "open",
// "move", "commit", "close" or "" (leave the key to the input or the form).
export function comboKey(state, ev) {
  const { open, active, items } = state;
  const key = ev?.key;
  if (!key || ev.metaKey || ev.ctrlKey) return { action: "" };
  if (key === "ArrowDown" || key === "ArrowUp") {
    if (!open) return { action: "open", active: initialActive(items, active), prevent: true };
    if (ev.altKey && key === "ArrowUp") return { action: "close", prevent: true };
    return { action: "move", active: stepActive(items, active, key === "ArrowDown" ? 1 : -1), prevent: true };
  }
  if (!open) return { action: "" };
  if (key === "Home" || key === "End" || key === "PageUp" || key === "PageDown") {
    if (ev.shiftKey) return { action: "" };
    return { action: "move", active: stepActive(items, active, key === "Home" || key === "PageUp" ? "first" : "last"), prevent: true };
  }
  if (key === "Enter") {
    if (items[active] && items[active].shown && !items[active].disabled) return { action: "commit", active, prevent: true };
    // Open with nothing to pick: Enter must not submit the form.
    return { action: "", prevent: true };
  }
  if (key === "Escape") return { action: "close", prevent: true, stop: true };
  if (key === "Tab") return { action: "close" };
  return { action: "" };
}

// normalizeOptions turns entries ({value, label, desc, icon, disabled}) and
// an optional "None" label into the picker's option list.
export function normalizeOptions(entries, none) {
  const list = (entries || []).map((e) => ({
    value: String(e?.value ?? ""),
    label: String(e?.label ?? e?.value ?? ""),
    desc: e?.desc ? String(e.desc) : "",
    descMono: !!e?.descMono,
    icon: e?.icon || "",
    disabled: !!e?.disabled,
    missing: !!e?.missing,
  }));
  if (none && !list.some((o) => o.value === "")) list.unshift({ value: "", label: String(none), desc: "", descMono: false, icon: "", disabled: false, missing: false });
  return list;
}

// pickValue is the value a refilled picker shows. A value that is in the
// list stays. No value (a new item) takes the first option, as a select
// did. A value that is no longer in the list is never swapped for another
// real one: it becomes "" (None when there is one, else nothing chosen).
// Callers that must keep it, so Save cannot quietly change it, list it as
// a { missing: true } option ("Missing host — pick another"). With
// first: false (editing a saved item whose value is empty) nothing is
// picked for it either.
export function pickValue(options, selected, { first = true } = {}) {
  const want = String(selected ?? "");
  if ((options || []).some((o) => o.value === want)) return want;
  if (want || !first) return "";
  return options?.[0]?.value ?? "";
}

// optionHTML is one row. icon is trusted markup (an SVG from Keel's own
// glyphs); every text goes through escapeHtml.
export function optionHTML(listId, opt, i, selected) {
  const sel = opt.value === selected;
  const icon = opt.icon ? `<span class="k-i k-combobox-icon" aria-hidden="true">${opt.icon}</span>` : "";
  const desc = opt.desc ? `<span class="k-combobox-desc${opt.descMono ? " k-combobox-desc--mono" : ""}">${escapeHtml(opt.desc)}</span>` : "";
  return `<div class="k-menu-item k-combobox-option${opt.missing ? " k-combobox-option--missing" : ""}" role="option" id="${escapeHtml(`${listId}-o${i}`)}" data-value="${escapeHtml(opt.value)}" aria-selected="${sel}"${opt.disabled ? ' aria-disabled="true"' : ""}${opt.missing ? " data-missing" : ""}>${icon}<span class="k-combobox-text"><span class="k-combobox-label">${escapeHtml(opt.label)}</span>${desc}</span>${svg(CHECK, "k-i k-combobox-check")}</div>`;
}

// comboboxHTML is the whole picker for term.js HTML strings. props: id,
// name, label (aria-label) or labelledBy, placeholder, value, options,
// none, emptyLabel (no options at all), mono (the value is code),
// required, disabled, className, attrs (extra trusted attributes on the
// root, e.g. 'data-role-host="h1"').
export function comboboxHTML(props) {
  const p = props || {};
  const id = String(p.id || "");
  const listId = `${id}-list`;
  const options = normalizeOptions(p.options, p.none);
  const value = String(p.value ?? "");
  const chosen = options.find((o) => o.value === value);
  const empty = options.length ? NO_MATCHES : p.emptyLabel || NO_MATCHES;
  const placeholder = options.length ? p.placeholder || "" : p.emptyLabel || p.placeholder || "";
  const label = p.labelledBy ? ` aria-labelledby="${escapeHtml(p.labelledBy)}"` : p.label ? ` aria-label="${escapeHtml(p.label)}"` : "";
  const listLabel = p.labelledBy ? ` aria-labelledby="${escapeHtml(p.labelledBy)}"` : p.label ? ` aria-label="${escapeHtml(p.label)}"` : "";
  return `<div class="k-combobox${p.mono ? " k-combobox--mono" : ""}${p.className ? ` ${escapeHtml(p.className)}` : ""}" data-combobox${p.attrs ? ` ${p.attrs}` : ""}><input class="k-input k-combobox-input" type="text" id="${escapeHtml(id)}" role="combobox" aria-autocomplete="list" aria-expanded="false" aria-controls="${escapeHtml(listId)}"${label} value="${escapeHtml(chosen ? chosen.label : "")}" placeholder="${escapeHtml(placeholder)}" data-placeholder="${escapeHtml(p.placeholder || "")}" autocomplete="off" autocapitalize="off" autocorrect="off" spellcheck="false"${p.required ? " required" : ""}${p.disabled ? " disabled" : ""}/><span class="k-i k-combobox-chevron" aria-hidden="true">${svg(CHEVRON, "")}</span><input type="hidden"${p.name ? ` name="${escapeHtml(p.name)}"` : ""} value="${escapeHtml(chosen ? value : "")}" data-combobox-value/><div class="k-menu k-combobox-pop" popover="manual" hidden><div class="k-combobox-list" role="listbox" id="${escapeHtml(listId)}"${listLabel}>${options.map((o, i) => optionHTML(listId, o, i, value)).join("")}</div><p class="k-combobox-empty"${options.length ? " hidden" : ""}>${escapeHtml(empty)}</p></div></div>`;
}

// ---------- DOM ----------

const parts = (root) => {
  const input = root.querySelector(":scope > .k-combobox-input");
  const hidden = root.querySelector(":scope > [data-combobox-value]");
  const pop = root.querySelector(":scope > .k-combobox-pop");
  const list = pop?.querySelector("[role='listbox']");
  const empty = pop?.querySelector(".k-combobox-empty");
  return { input, hidden, pop, list, empty };
};

const optionEls = (root) => [...(parts(root).list?.querySelectorAll("[role='option']") || [])];
const optLabel = (el) => (el.querySelector(".k-combobox-label") || el).textContent.trim();
const optDesc = (el) => el.querySelector(".k-combobox-desc")?.textContent.trim() || "";

// session state of an open picker, per root
const live = new WeakMap();
let openRoot = null;

function rootOf(node) {
  return node && node.closest ? node.closest(".k-combobox[data-combobox]") : null;
}

// enhanceCombobox makes a picker's ARIA wiring whole. It changes nothing
// that is already right, so it can run after every morph.
export function enhanceCombobox(root) {
  if (!root?.querySelector) return;
  const { input, hidden, list } = parts(root);
  if (!input || !list) return;
  if (!list.id) list.id = `${input.id || "k-combobox"}-list`;
  const want = {
    role: "combobox",
    "aria-autocomplete": "list",
    "aria-controls": list.id,
    autocomplete: "off",
    autocapitalize: "off",
    autocorrect: "off",
    spellcheck: "false",
  };
  for (const [k, v] of Object.entries(want)) if (input.getAttribute(k) !== v) input.setAttribute(k, v);
  if (!input.hasAttribute("aria-expanded")) input.setAttribute("aria-expanded", "false");
  // The listbox takes the input's name: its aria-label, or the <label for>.
  if (!list.hasAttribute("aria-label") && !list.hasAttribute("aria-labelledby")) {
    const lab = input.labels?.[0];
    if (lab) {
      if (!lab.id) lab.id = `${input.id || list.id}-label`;
      list.setAttribute("aria-labelledby", lab.id);
    } else if (input.getAttribute("aria-label")) list.setAttribute("aria-label", input.getAttribute("aria-label"));
  }
  optionEls(root).forEach((el, i) => {
    if (!el.id) el.id = `${list.id}-o${i}`;
  });
  if (hidden && root !== openRoot) syncText(root);
}

export function enhanceAll(scope) {
  (scope || document).querySelectorAll?.(".k-combobox[data-combobox]").forEach(enhanceCombobox);
}

// comboboxValue is the posted value.
export function comboboxValue(root) {
  return parts(root).hidden?.value ?? "";
}

// syncText shows the chosen option's label in the input (or nothing, so
// the placeholder shows), and marks that option selected.
function syncText(root) {
  const { input, hidden } = parts(root);
  if (!input || !hidden) return;
  const value = hidden.value;
  let label = "";
  let missing = false;
  for (const el of optionEls(root)) {
    const on = el.dataset.value === value;
    if (el.getAttribute("aria-selected") !== String(on)) el.setAttribute("aria-selected", String(on));
    if (on) {
      label = optLabel(el);
      missing = el.hasAttribute("data-missing");
    }
  }
  if (input.value !== label) input.value = label;
  // A value that no longer exists (a deleted host or identity) is shown as
  // such; a required picker refuses to submit it until another is picked.
  root.toggleAttribute("data-missing", missing);
  input.setCustomValidity?.(missing && input.required ? label : "");
}

// setComboboxValue picks a value without events, as term.js does when it
// fills a dialog. An unknown value picks nothing.
export function setComboboxValue(root, value) {
  const { hidden } = parts(root);
  if (!hidden) return;
  const v = String(value ?? "");
  hidden.value = optionEls(root).some((el) => el.dataset.value === v) ? v : "";
  syncText(root);
}

// setComboboxOptions replaces the options and picks selected (or the
// first option when selected is not there, as the select did). With no
// options the input shows emptyLabel as its placeholder and the list says
// it. Returns the value picked.
export function setComboboxOptions(root, entries, selected, emptyLabel, none, pick = {}) {
  const { input, list, empty } = parts(root);
  if (!input || !list) return "";
  const options = normalizeOptions(entries, none);
  const value = pickValue(options, selected, pick);
  list.innerHTML = options.map((o, i) => optionHTML(list.id, o, i, value)).join("");
  // data-placeholder keeps the placeholder for when options come back.
  if (input.dataset.placeholder == null) input.dataset.placeholder = input.placeholder;
  input.placeholder = options.length ? input.dataset.placeholder : emptyLabel || input.dataset.placeholder;
  if (empty) {
    empty.textContent = options.length ? NO_MATCHES : emptyLabel || NO_MATCHES;
    empty.hidden = !!options.length;
  }
  setComboboxValue(root, value);
  if (root === openRoot) render(root);
  return value;
}

function items(root) {
  const st = live.get(root);
  return optionEls(root).map((el, i) => ({
    shown: !st || st.shown.has(i),
    disabled: el.getAttribute("aria-disabled") === "true",
  }));
}

function selectedIndex(root) {
  const v = comboboxValue(root);
  return optionEls(root).findIndex((el) => el.dataset.value === v);
}

function supportsPopover(pop) {
  return typeof pop?.showPopover === "function";
}

function place(root) {
  const { input, pop } = parts(root);
  if (!input || !pop || !supportsPopover(pop)) return;
  const r = input.getBoundingClientRect();
  const vh = window.innerHeight;
  const gap = 4;
  pop.style.minWidth = `${r.width}px`;
  pop.style.left = `${Math.max(gap, Math.min(r.left, window.innerWidth - pop.offsetWidth - gap))}px`;
  const below = vh - r.bottom - gap * 2;
  const above = r.top - gap * 2;
  const natural = pop.scrollHeight;
  const up = natural > below && above > below;
  pop.style.maxHeight = `${Math.max(0, up ? above : below)}px`;
  pop.style.top = up ? `${Math.max(gap, r.top - gap - Math.min(natural, above))}px` : `${r.bottom + gap}px`;
}

function render(root) {
  const st = live.get(root);
  const { input, list, empty } = parts(root);
  if (!st || !input || !list) return;
  const els = optionEls(root);
  els.forEach((el, i) => {
    const shown = st.shown.has(i);
    if (el.hidden === shown) el.hidden = !shown;
    el.toggleAttribute("data-highlighted", i === st.active);
  });
  const total = els.length;
  if (empty) {
    empty.hidden = total > 0 && st.shown.size > 0;
    if (total) empty.textContent = NO_MATCHES;
  }
  const act = els[st.active];
  if (act) {
    input.setAttribute("aria-activedescendant", act.id);
    // Keep the active row in view inside the list only (scrollIntoView
    // would also scroll the dialog behind it).
    const pop = list.parentElement;
    const top = act.offsetTop;
    const bottom = top + act.offsetHeight;
    if (top < pop.scrollTop) pop.scrollTop = top;
    else if (bottom > pop.scrollTop + pop.clientHeight) pop.scrollTop = bottom - pop.clientHeight;
  } else input.removeAttribute("aria-activedescendant");
  place(root);
}

function onWindowChange(ev) {
  if (!openRoot) return;
  if (!openRoot.isConnected) return closeCombobox(openRoot, false);
  if (ev?.type === "scroll" && parts(openRoot).pop?.contains(ev.target)) return;
  place(openRoot);
}

function onOutside(ev) {
  if (!openRoot) return;
  if (!openRoot.isConnected || !openRoot.contains(ev.target)) closeCombobox(openRoot, true);
}

export function openCombobox(root, active) {
  const { input, pop } = parts(root);
  if (!input || !pop || input.disabled || input.readOnly) return;
  if (openRoot && openRoot !== root) closeCombobox(openRoot, true);
  enhanceCombobox(root);
  const all = new Set(optionEls(root).map((_, i) => i));
  const st = live.get(root) || { shown: all, active: -1, typed: false };
  if (openRoot !== root) {
    st.shown = all;
    st.typed = false;
    live.set(root, st);
  }
  st.active = active ?? initialActive(items(root), selectedIndex(root));
  if (openRoot !== root) {
    openRoot = root;
    pop.hidden = false;
    root.setAttribute("data-open", "");
    // No popover support: the list drops inside the dialog (keel.css).
    root.toggleAttribute("data-open-inline", !supportsPopover(pop));
    input.setAttribute("aria-expanded", "true");
    if (supportsPopover(pop) && !pop.matches(":popover-open")) {
      try {
        pop.showPopover();
      } catch {
        /* not connected or already shown */
      }
    }
    document.addEventListener("pointerdown", onOutside, true);
    window.addEventListener("scroll", onWindowChange, true);
    window.addEventListener("resize", onWindowChange);
  }
  render(root);
}

// closeCombobox hides the list; revert puts the chosen label back in the
// input (typing that was not picked is dropped).
export function closeCombobox(root, revert = true) {
  const { input, pop } = parts(root);
  if (openRoot === root) {
    openRoot = null;
    document.removeEventListener("pointerdown", onOutside, true);
    window.removeEventListener("scroll", onWindowChange, true);
    window.removeEventListener("resize", onWindowChange);
  }
  live.delete(root);
  root.removeAttribute("data-open");
  root.removeAttribute("data-open-inline");
  if (pop) {
    if (supportsPopover(pop) && pop.matches(":popover-open")) {
      try {
        pop.hidePopover();
      } catch {
        /* already hidden */
      }
    }
    pop.hidden = true;
  }
  optionEls(root).forEach((el) => {
    el.hidden = false;
    el.removeAttribute("data-highlighted");
  });
  if (input) {
    input.setAttribute("aria-expanded", "false");
    input.removeAttribute("aria-activedescendant");
  }
  if (revert) syncText(root);
}

// pickOption commits an option: hidden value, label, events, close.
export function pickOption(root, index) {
  const el = optionEls(root)[index];
  const { input, hidden } = parts(root);
  if (!el || !input || !hidden || el.getAttribute("aria-disabled") === "true") return false;
  const value = el.dataset.value ?? "";
  const label = optLabel(el);
  const changed = hidden.value !== value;
  hidden.value = value;
  closeCombobox(root, true);
  input.dispatchEvent(new CustomEvent("select-change", { bubbles: true, detail: { value, label } }));
  if (changed) hidden.dispatchEvent(new Event("change", { bubbles: true }));
  return true;
}

function filter(root, query) {
  const st = live.get(root);
  if (!st) return;
  const els = optionEls(root);
  const opts = els.map((el) => ({ label: optLabel(el), desc: optDesc(el) }));
  st.shown = new Set(filterOptions(opts, query));
  st.typed = true;
  st.active = stepActive(items(root), -1, "first");
  render(root);
}

function onInput(ev) {
  const root = rootOf(ev.target);
  if (!root || ev.target !== parts(root).input) return;
  // Typing filters; it is not an edit of the form (a dialog that drops its
  // last test result on any edit must not see it).
  ev.stopPropagation();
  if (openRoot !== root) openCombobox(root);
  filter(root, ev.target.value);
}

function onKeydown(ev) {
  const root = rootOf(ev.target);
  if (!root || ev.target !== parts(root).input || ev.isComposing) return;
  const st = live.get(root);
  const open = openRoot === root;
  const res = comboKey({ open, active: open ? st.active : selectedIndex(root), items: items(root) }, ev);
  if (res.prevent) ev.preventDefault();
  // Keys the open list used are its own: no dialog, form or app shortcut
  // sees them (Escape closes the list, not the dialog).
  if (res.stop || res.action === "commit" || (open && res.prevent)) ev.stopImmediatePropagation();
  if (res.action === "open") openCombobox(root, res.active);
  else if (res.action === "move") {
    st.active = res.active;
    render(root);
  } else if (res.action === "commit") pickOption(root, res.active);
  else if (res.action === "close") closeCombobox(root, true);
}

function onPointerdown(ev) {
  const root = rootOf(ev.target);
  if (!root) return;
  const { input, pop } = parts(root);
  if (!input || input.disabled) return;
  if (pop?.contains(ev.target)) {
    // Keep focus in the input while a row (or the list's scrollbar) is used.
    ev.preventDefault();
    return;
  }
  if (ev.target.closest(".k-combobox-chevron")) {
    ev.preventDefault();
    input.focus();
    if (openRoot === root) closeCombobox(root, true);
    else openCombobox(root);
  }
}

function onClick(ev) {
  const root = rootOf(ev.target);
  if (!root) return;
  const { input, pop } = parts(root);
  if (pop?.contains(ev.target)) {
    const opt = ev.target.closest("[role='option']");
    if (opt) pickOption(root, optionEls(root).indexOf(opt));
    return;
  }
  if (ev.target === input && openRoot !== root) {
    openCombobox(root);
    input.select?.();
  }
}

function onPointermove(ev) {
  if (!openRoot) return;
  const opt = ev.target.closest?.("[role='option']");
  if (!opt || !parts(openRoot).pop?.contains(opt) || opt.getAttribute("aria-disabled") === "true") return;
  const st = live.get(openRoot);
  const i = optionEls(openRoot).indexOf(opt);
  if (st && i !== st.active) {
    st.active = i;
    render(openRoot);
  }
}

function onFocusout(ev) {
  const root = rootOf(ev.target);
  if (!root || openRoot !== root) return;
  if (ev.relatedTarget && root.contains(ev.relatedTarget)) return;
  closeCombobox(root, true);
}

// The text input is only the filter: its own "change" (on blur after
// typing) is not a pick, so no form or page listener sees it.
function onChange(ev) {
  if (ev.target?.classList?.contains("k-combobox-input")) ev.stopPropagation();
}

function onFocusin(ev) {
  const root = rootOf(ev.target);
  if (root && ev.target === parts(root).input) enhanceCombobox(root);
}

// installCombobox wires every picker in the document, now and later. Safe
// to call more than once.
export function installCombobox(doc = document) {
  if (!doc || doc.__keelCombobox) return;
  doc.__keelCombobox = true;
  doc.addEventListener("input", onInput, true);
  // Window capture, so the open list hears its keys before the app's own
  // window shortcuts (term.js closes a dialog on Escape there); install
  // before them.
  (doc.defaultView || globalThis).addEventListener?.("keydown", onKeydown, true);
  doc.addEventListener("pointerdown", onPointerdown);
  doc.addEventListener("click", onClick);
  doc.addEventListener("pointermove", onPointermove);
  doc.addEventListener("focusout", onFocusout);
  doc.addEventListener("focusin", onFocusin);
  doc.addEventListener("change", onChange, true);
  enhanceAll(doc);
}
