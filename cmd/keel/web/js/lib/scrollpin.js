// pinScroll keeps the app chrome from ever scrolling.
//
// The page, body and .main are overflow:hidden, but overflow:hidden still
// allows programmatic scrolling: a focus() or scrollIntoView() on something
// laid out past the edge (a tab being removed, a start page input before
// layout settles) scrolls the nearest hidden-overflow ancestor and the tab
// strip slides up out of the titlebar with no way to scroll it back.
//
// It listens for scroll in the capture phase (scroll does not bubble) and
// snaps any pinned element back to 0,0. Returns a function that removes the
// listener.
export function pinScroll(doc, elements) {
  const pinned = new Set(elements.filter(Boolean));
  const onScroll = (e) => {
    const el = e.target === doc ? doc.scrollingElement : e.target;
    if (!el || !pinned.has(el)) return;
    if (el.scrollTop !== 0) el.scrollTop = 0;
    if (el.scrollLeft !== 0) el.scrollLeft = 0;
  };
  doc.addEventListener("scroll", onScroll, { capture: true, passive: true });
  for (const el of pinned) {
    if (el.scrollTop) el.scrollTop = 0;
    if (el.scrollLeft) el.scrollLeft = 0;
  }
  return () => doc.removeEventListener("scroll", onScroll, { capture: true });
}
