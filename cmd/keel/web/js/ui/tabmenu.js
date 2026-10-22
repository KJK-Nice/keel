// The tab menu (#tab-ctx in internal/ui/parts.templ): a right-click on a
// tab offers Rename, Close, Close others, Close to the right and Close all,
// for the tab it was opened on. Which tabs there are and what closing does
// stay with term.js; the caller passes:
//   tabIds()      the tab ids in strip order
//   isPage(id)    a tab page: it keeps its name, so no Rename
//   rename(id)    start renaming the tab in place
//   close(ids)    close these tabs
export function createTabMenu({ doc = document, win = window, tabIds, isPage, rename, close }) {
  let target = "";

  function setItem(sel, { hidden = false, disabled = false } = {}) {
    const el = doc.querySelector(sel);
    if (!el) return;
    el.hidden = hidden;
    if ("disabled" in el) el.disabled = disabled;
    if (disabled) el.setAttribute("aria-disabled", "true");
    else el.removeAttribute("aria-disabled");
  }

  // sync shows and enables the items for tab id, and makes it the tab the
  // items act on.
  function sync(id) {
    target = id;
    const fixed = !!isPage(id);
    const ids = tabIds();
    const idx = ids.indexOf(id);
    const others = ids.filter((tid) => tid !== id).length;
    const right = idx < 0 ? 0 : ids.length - idx - 1;
    setItem('[data-tab-action="rename"]', { hidden: fixed, disabled: fixed });
    setItem('[data-tab-menu-sep="rename"]', { hidden: fixed });
    setItem('[data-tab-action="close"]', { disabled: idx < 0 });
    setItem('[data-tab-action="close-others"]', { disabled: others === 0 });
    setItem('[data-tab-action="close-right"]', { disabled: right === 0 });
    setItem('[data-tab-action="close-all"]', { disabled: ids.length === 0 });
  }

  // run is a click on one of its items (data-tab-action). Rename waits a
  // frame, so the closing menu does not take the rename field's focus.
  function run(action, id = target) {
    if (action === "rename") {
      win.requestAnimationFrame(() => rename(id));
      return;
    }
    if (action === "close") {
      close([id]);
      return;
    }
    const ids = tabIds();
    if (action === "close-others") close(ids.filter((tid) => tid !== id));
    else if (action === "close-right") {
      const idx = ids.indexOf(id);
      if (idx >= 0) close(ids.slice(idx + 1));
    } else if (action === "close-all") close(ids);
  }

  // A right-click on the strip opens the menu only on a tab (the space
  // beside the tabs keeps no menu).
  function onContextMenu(ev) {
    if (!(ev.target instanceof win.Element)) return;
    if (!ev.target.closest("#tab-ctx-trigger")) return;
    const tab = ev.target.closest(".tab[data-tab]");
    if (!tab) {
      ev.preventDefault();
      ev.stopPropagation();
      return;
    }
    sync(tab.dataset.tab);
  }

  // The native window's titlebar can swallow a right-click on the strip:
  // the right button's pointerup opens the menu then, a frame later, unless
  // a contextmenu already did.
  function onRightUp(ev) {
    if (ev.button !== 2 || !(ev.target instanceof win.Element)) return;
    if (!ev.target.closest("#tab-ctx-trigger")) return;
    const tab = ev.target.closest(".tab[data-tab]");
    if (!tab) return;
    sync(tab.dataset.tab);
    const x = ev.clientX;
    const y = ev.clientY;
    win.requestAnimationFrame(() => {
      if (doc.getElementById("tab-ctx")?.hasAttribute("data-open")) return;
      tab.dispatchEvent(new win.MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: x, clientY: y, view: win, button: 2 }));
    });
  }

  function onClick(ev) {
    if (!(ev.target instanceof win.Element)) return;
    const item = ev.target.closest("[data-tab-action]");
    if (!item || item.disabled || item.getAttribute("aria-disabled") === "true") return;
    run(item.dataset.tabAction);
  }

  function bind() {
    doc.addEventListener("contextmenu", onContextMenu, true);
    doc.addEventListener("pointerup", onRightUp, true);
    doc.addEventListener("click", onClick);
  }

  return { sync, run, bind };
}
