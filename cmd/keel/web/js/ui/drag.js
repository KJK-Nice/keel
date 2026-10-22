// Dragging tabs and panes. A tab dragged along the strip moves there; one
// dropped on an edge of a pane in the shown tab docks all its panes beside
// that pane. A pane dragged by its bar onto another pane's edge moves next
// to it (the centre swaps the two); one dropped on the strip becomes a tab
// of its own there. Esc, the window losing focus or a cancelled pointer
// drop nothing. What a drop does stays with term.js; the caller passes:
//   strip                   the tab strip (#tabs)
//   terminals               the panes' container (#terminals)
//   shownPanes()            the shown session tab's panes: [{ id, tabId, wrap }]
//   canDragTab(id), canDragPane(id)
//   paneWrap(id)            a pane's element
//   paneSplits(id)          its tab has other panes (so it can leave as a tab)
//   paneDragStart(id)       a pane drag begins (term.js leaves focus view)
//   syncOverflow()          the strip's scroll shadows
//   act                     { moveTab(id, index), dockTab(id, paneId, edge),
//                             extractPane(id, index), movePane(id, paneId, edge) }

// dropEdgeForRect is the edge of rect nearest to x, y, or "center" when
// the point is well inside.
export function dropEdgeForRect(rect, x, y) {
  const relX = (x - rect.left) / Math.max(1, rect.width);
  const relY = (y - rect.top) / Math.max(1, rect.height);
  const dist = { left: relX, right: 1 - relX, top: relY, bottom: 1 - relY };
  const edge = Object.keys(dist).reduce((best, key) => (dist[key] < dist[best] ? key : best), "left");
  return dist[edge] > 0.28 ? "center" : edge;
}

const SLOP = 6;

export function createDrag({ doc = document, win = window, strip, terminals, shownPanes, canDragTab, canDragPane, paneWrap, paneSplits, paneDragStart, syncOverflow = () => {}, act }) {
  let tabDrag = null;
  let paneDrag = null;
  let ignoreClick = false;
  let overlay = null;

  const tabEl = (id) => strip.querySelector(`.tab[data-tab="${CSS.escape(id)}"]`);

  // ---- The strip -----------------------------------------------------------

  function hitStrip(x, y) {
    const box = strip?.closest(".tab-strip") || strip;
    if (!box) return false;
    const r = box.getBoundingClientRect();
    return x >= r.left && x <= r.right && y >= r.top && y <= r.bottom + 10;
  }

  function insertIndex(clientX) {
    const buttons = [...strip.querySelectorAll(".tab[data-tab]")];
    for (let i = 0; i < buttons.length; i++) {
      const r = buttons[i].getBoundingClientRect();
      if (clientX < r.left + r.width / 2) return i;
    }
    return buttons.length;
  }

  function clearMarks() {
    for (const el of strip.querySelectorAll(".is-drop-before, .is-drop-after, .is-dragging")) {
      el.classList.remove("is-drop-before", "is-drop-after", "is-dragging");
    }
  }

  function paintStripDrop(toIndex) {
    clearMarks();
    strip._keelHTML = null; // drop marks change the strip in place
    // Only a dragged tab shows where it would land (as before this module).
    if (!tabDrag) return;
    markDragging();
    const buttons = [...strip.querySelectorAll(".tab[data-tab]")];
    if (!buttons.length) return;
    if (toIndex >= buttons.length) buttons[buttons.length - 1]?.classList.add("is-drop-after");
    else buttons[toIndex]?.classList.add("is-drop-before");
  }

  function autoScroll(clientX) {
    const r = strip.getBoundingClientRect();
    const edge = 36;
    if (clientX < r.left + edge) strip.scrollLeft -= 14;
    else if (clientX > r.right - edge) strip.scrollLeft += 14;
    syncOverflow();
  }

  // markDragging marks the dragged tab, again after the strip re-renders.
  function markDragging() {
    if (!tabDrag) return;
    const el = tabEl(tabDrag.id);
    el?.classList.add("is-dragging");
    el?.setAttribute("aria-grabbed", "true");
  }

  // ---- Panes ---------------------------------------------------------------

  function ensureOverlay() {
    if (overlay) return overlay;
    overlay = doc.createElement("div");
    overlay.className = "pane-drop";
    overlay.hidden = true;
    overlay.innerHTML = `<div class="pane-drop-region"></div>`;
    doc.body.appendChild(overlay);
    return overlay;
  }

  function hidePaneDrop() {
    if (overlay) overlay.hidden = true;
  }

  function paintPaneDrop(target) {
    const box = ensureOverlay();
    const wrap = target?.paneId && target.edge ? paneWrap(target.paneId) : null;
    if (!wrap?.isConnected) {
      box.hidden = true;
      return;
    }
    const r = wrap.getBoundingClientRect();
    box.hidden = false;
    box.style.left = `${r.left}px`;
    box.style.top = `${r.top}px`;
    box.style.width = `${r.width}px`;
    box.style.height = `${r.height}px`;
    box.dataset.edge = target.edge;
  }

  // paneDropAt is the pane of the shown tab under x, y and the edge to drop
  // on; self when it is the dragged pane or a pane of the dragged tab.
  function paneDropAt(x, y, { excludePaneId = "", fromTabId = "" } = {}) {
    for (const p of shownPanes()) {
      if (!p.wrap?.isConnected) continue;
      const r = p.wrap.getBoundingClientRect();
      if (x < r.left || x > r.right || y < r.top || y > r.bottom) continue;
      if (p.id === excludePaneId || (fromTabId && p.tabId === fromTabId)) return { paneId: p.id, edge: null, self: true };
      return { paneId: p.id, edge: dropEdgeForRect(r, x, y), self: false };
    }
    return null;
  }

  // ---- Drags ---------------------------------------------------------------

  function endTabDrag() {
    if (!tabDrag) return;
    tabEl(tabDrag.id)?.removeAttribute("aria-grabbed");
    tabDrag = null;
    doc.body.classList.remove("is-tab-dragging");
    clearMarks();
    hidePaneDrop();
  }

  function endPaneDrag() {
    if (!paneDrag) return;
    paneWrap(paneDrag.paneId)?.classList.remove("is-dragging");
    paneDrag = null;
    doc.body.classList.remove("is-pane-dragging");
    hidePaneDrop();
    clearMarks();
  }

  // cancel drops a drag under way (Esc, the window losing focus) and
  // reports whether there was one. A tab that moved swallows its click.
  function cancel() {
    const any = !!(tabDrag || paneDrag);
    if (tabDrag) {
      ignoreClick = tabDrag.moved;
      endTabDrag();
    }
    endPaneDrag();
    return any;
  }

  function onTabDown(ev) {
    if (ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.altKey || ev.shiftKey) return;
    if (ev.target.closest("[data-close], .tab-rename")) return;
    const tab = ev.target.closest(".tab[data-tab]");
    if (!tab || !canDragTab(tab.dataset.tab)) return;
    tabDrag = { id: tab.dataset.tab, startX: ev.clientX, startY: ev.clientY, pointerId: ev.pointerId, moved: false };
  }

  function onPaneDown(ev) {
    if (ev.button !== 0 || ev.metaKey || ev.ctrlKey || ev.altKey || ev.shiftKey) return;
    if (ev.target.closest("button")) return;
    const bar = ev.target.closest(".pane-bar");
    const paneId = bar?.closest(".term-wrap")?.dataset.paneId;
    if (!paneId || !canDragPane(paneId)) return;
    paneDrag = { paneId, startX: ev.clientX, startY: ev.clientY, pointerId: ev.pointerId, moved: false };
    try {
      bar.setPointerCapture(ev.pointerId);
    } catch {
      /* ignore */
    }
  }

  function onMove(ev) {
    if (tabDrag && ev.pointerId === tabDrag.pointerId) {
      if (!tabDrag.moved && Math.hypot(ev.clientX - tabDrag.startX, ev.clientY - tabDrag.startY) < SLOP) return;
      if (!tabDrag.moved) {
        tabDrag.moved = true;
        doc.body.classList.add("is-tab-dragging");
      }
      ev.preventDefault();
      autoScroll(ev.clientX);
      if (hitStrip(ev.clientX, ev.clientY)) {
        hidePaneDrop();
        paintStripDrop(insertIndex(ev.clientX));
      } else {
        clearMarks();
        markDragging();
        paintPaneDrop(paneDropAt(ev.clientX, ev.clientY, { fromTabId: tabDrag.id }));
      }
      return;
    }
    if (!paneDrag || ev.pointerId !== paneDrag.pointerId) return;
    if (!paneDrag.moved && Math.hypot(ev.clientX - paneDrag.startX, ev.clientY - paneDrag.startY) < SLOP) return;
    if (!paneDrag.moved) {
      paneDrag.moved = true;
      doc.body.classList.add("is-pane-dragging");
      paneWrap(paneDrag.paneId)?.classList.add("is-dragging");
      paneDragStart(paneDrag.paneId);
    }
    ev.preventDefault();
    if (hitStrip(ev.clientX, ev.clientY)) {
      hidePaneDrop();
      if (paneSplits(paneDrag.paneId)) paintStripDrop(insertIndex(ev.clientX));
      else clearMarks();
    } else {
      clearMarks();
      paintPaneDrop(paneDropAt(ev.clientX, ev.clientY, { excludePaneId: paneDrag.paneId }));
    }
  }

  function finishTab(ev) {
    if (!tabDrag || ev.pointerId !== tabDrag.pointerId) return;
    const drag = tabDrag;
    const overStrip = hitStrip(ev.clientX, ev.clientY);
    const dock = drag.moved && !overStrip ? paneDropAt(ev.clientX, ev.clientY, { fromTabId: drag.id }) : null;
    const toIndex = drag.moved && overStrip ? insertIndex(ev.clientX) : -1;
    endTabDrag();
    if (!drag.moved) return;
    // The click that ends a drag is not a click on the tab.
    ignoreClick = true;
    win.setTimeout(() => {
      ignoreClick = false;
    }, 50);
    if (dock && !dock.self && dock.edge) act.dockTab(drag.id, dock.paneId, dock.edge);
    else if (overStrip) act.moveTab(drag.id, toIndex);
  }

  function finishPane(ev) {
    if (!paneDrag || ev.pointerId !== paneDrag.pointerId) return;
    const drag = paneDrag;
    const overStrip = hitStrip(ev.clientX, ev.clientY);
    const dock = drag.moved && !overStrip ? paneDropAt(ev.clientX, ev.clientY, { excludePaneId: drag.paneId }) : null;
    const toIndex = drag.moved && overStrip ? insertIndex(ev.clientX) : -1;
    endPaneDrag();
    if (!drag.moved) return;
    if (overStrip) act.extractPane(drag.paneId, toIndex);
    else if (dock && !dock.self && dock.edge) act.movePane(drag.paneId, dock.paneId, dock.edge);
  }

  function onCancel(ev) {
    if (tabDrag && ev.pointerId === tabDrag.pointerId) {
      ignoreClick = tabDrag.moved;
      endTabDrag();
    }
    if (paneDrag && ev.pointerId === paneDrag.pointerId) endPaneDrag();
  }

  function bind() {
    strip.addEventListener("pointerdown", onTabDown);
    strip.addEventListener("dragstart", (ev) => ev.preventDefault());
    terminals.addEventListener("pointerdown", onPaneDown);
    doc.addEventListener("pointermove", onMove, { capture: true, passive: false });
    doc.addEventListener("pointerup", finishTab, true);
    doc.addEventListener("pointerup", finishPane, true);
    doc.addEventListener("pointercancel", onCancel, true);
  }

  return {
    bind,
    cancel,
    markDragging,
    // ignoreClick: a tab drag just ended or was cancelled after moving.
    ignoreClick: () => ignoreClick,
    dropEdgeForRect,
  };
}
