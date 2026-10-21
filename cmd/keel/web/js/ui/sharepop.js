// Share popover (docs/ui-redesign.md §5.6). Opening it shares the focused
// session, as the Share button always did (POST /api/share returns the
// live link when there already is one). The popover only shows that state
// and offers the actions that existed before: copy the link, pass or take
// control, stop sharing.
//
// The markup is lib/share.js sharePopoverHTML. This module holds the
// popover's state and wiring; panes, the API and toasts come from the
// caller:
//   pop, button, badge  the popover, the titlebar Share button and the
//                       watching badge (#control-widget)
//   pane(id)            the pane with that id, or undefined
//   shareTarget()       the pane Share acts on, or null when it cannot
//   stopTarget()        the pane Stop sharing acts on when the popover is shut
//   caps(pane)          { people, canGrant, canTake, canAsk } for the markup
//   api, copyText, toast
//   onStopped(pane)     after keel serve stopped the share
//   refocus()           gives the keyboard back to the terminal
import * as Share from "../lib/share.js";

export function createSharePopover({ doc = document, win = window, pop, button, badge, pane, shareTarget, stopTarget, caps, api, copyText, toast, onStopped, refocus }) {
  const st = { paneId: "", runtimeId: "", status: "", url: "", error: "", copied: false, seq: 0, html: "" };
  const links = new Map(); // runtimeId -> join link this window was given

  function isOpen() {
    return !!pop && !pop.hidden;
  }

  function open() {
    const p = shareTarget();
    if (!pop || !p) return;
    const known = links.get(p.runtimeId) || "";
    Object.assign(st, { paneId: p.id, runtimeId: p.runtimeId, status: known ? "ready" : "pending", url: known, error: "", copied: false, html: "" });
    pop.hidden = false;
    button?.setAttribute("aria-expanded", "true");
    render();
    // Focus moves into the popover (Escape closes it; keys no longer reach
    // the terminal while it is open).
    queueMicrotask(() => pop.focus({ preventScroll: true }));
    if (!known) start(p);
  }

  function close({ refocus: back = false } = {}) {
    if (!pop || pop.hidden) return;
    pop.hidden = true;
    button?.setAttribute("aria-expanded", "false");
    st.paneId = "";
    st.html = "";
    if (back) refocus();
  }

  function position() {
    const anchor = button && !button.hidden ? button : badge;
    if (!anchor || !pop) return;
    const r = anchor.getBoundingClientRect();
    pop.style.top = `${Math.round(r.bottom + 6)}px`;
    pop.style.right = `${Math.max(8, Math.round(win.innerWidth - r.right))}px`;
  }

  function render() {
    if (!isOpen()) return;
    const p = pane(st.paneId);
    if (!p) {
      close();
      return;
    }
    const html = Share.sharePopoverHTML({
      title: p.title || "session",
      status: st.status,
      url: st.url,
      error: st.error,
      copied: st.copied,
      ...caps(p),
    });
    position();
    if (html === st.html) return;
    const active = doc.activeElement;
    const focusKey = active?.closest?.("#share-pop") ? active.getAttribute("data-grant") || (active.id ? `#${active.id}` : "") : null;
    st.html = html;
    pop.innerHTML = html;
    if (focusKey != null) {
      const again = focusKey.startsWith("#") ? pop.querySelector(focusKey) : focusKey ? pop.querySelector(`[data-grant="${CSS.escape(focusKey)}"]`) : null;
      (again || pop.querySelector("button"))?.focus({ preventScroll: true });
    }
  }

  // follow keeps the popover on the focused pane: it closes when focus
  // moves to another pane or to a joined (remote) one, else re-renders.
  function follow(p) {
    if (!p) close();
    else if (p.remote) close();
    else if (st.paneId && st.paneId !== p.id) close();
    else render();
  }

  async function start(p) {
    const seq = ++st.seq;
    const current = () => st.seq === seq && st.runtimeId === p.runtimeId && isOpen();
    try {
      const out = await api("/api/share", {
        method: "POST",
        body: JSON.stringify({ sessionId: p.runtimeId }),
      });
      const url = out.url || "";
      if (url) links.set(p.runtimeId, url);
      if (current()) Object.assign(st, { status: url ? "ready" : "error", url, error: url ? "" : "The share started but returned no link." });
      render();
      // keel serve also puts the link on the macOS pasteboard; WKWebView
      // often refuses a clipboard write this long after the click.
      if (url && (await copyText(url)) && current()) {
        st.copied = true;
        render();
      }
    } catch (err) {
      if (current()) {
        Object.assign(st, { status: "error", error: err.message });
        render();
      } else {
        toast(err.message, true);
      }
    }
  }

  function toggle() {
    if (isOpen()) {
      close({ refocus: true });
      return;
    }
    open();
  }

  async function copyLink() {
    if (!st.url) return;
    const ok = await copyText(st.url);
    if (ok) {
      st.copied = true;
      render();
    } else {
      pop?.querySelector("#share-link")?.select();
      toast("Copy failed: the link is selected, press ⌘C", true);
    }
  }

  async function stop() {
    const p = isOpen() ? pane(st.paneId) : stopTarget();
    if (!p || p.remote) return;
    try {
      await api(`/api/share/${encodeURIComponent(p.runtimeId)}`, { method: "DELETE" });
      links.delete(p.runtimeId);
      onStopped(p);
      close({ refocus: true });
      toast("Sharing stopped. The join link no longer works.");
    } catch (err) {
      toast(err.message, true);
    }
  }

  // bind wires the button, the badge and the popover. data-grant,
  // data-take and data-request clicks are left to the caller's body
  // handler, as they were on the old titlebar people chips.
  function bind() {
    button?.addEventListener("click", toggle);
    badge?.addEventListener("click", toggle);
    pop?.addEventListener("click", (ev) => {
      if (ev.target.closest("[data-share-copy]")) copyLink();
      else if (ev.target.closest("[data-share-stop]")) stop();
    });
    pop?.addEventListener("keydown", (ev) => {
      if (ev.key !== "Escape") return;
      ev.preventDefault();
      ev.stopPropagation();
      close({ refocus: true });
    });
    doc.addEventListener(
      "pointerdown",
      (ev) => {
        if (!isOpen() || !(ev.target instanceof Element)) return;
        if (ev.target.closest("#share-pop, #btn-share, #control-widget")) return;
        close();
      },
      true,
    );
    win.addEventListener("resize", () => position());
  }

  return { isOpen, open, close, follow, render, toggle, stop, bind };
}
