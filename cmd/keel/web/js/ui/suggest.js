// In-pane suggestions (docs/ui-redesign.md §5.5): one per terminal pane.
// lib/suggest.js holds the state machine; this file reads the xterm buffer
// and draws the four states over the pane with the k-sug classes.
//
// It never writes to a pane. Inserting goes through deps.insert (term.js:
// the approved-write ticket path, which keel serve authorises with the agent
// service), and ⌘Z through deps.restore (the user's own keystrokes).

import * as S from "../lib/suggest.js";
import { AGENT_ICON, AGENT_ALERT_ICON } from "./agentpanel.js";

// readPromptLine reads the cursor's logical line (wrapped rows joined) and
// splits off the prompt. null in a full-screen app (alt screen buffer) or
// when the line has no prompt.
export function readPromptLine(term) {
  const buf = term?.buffer?.active;
  if (!buf || buf.type !== "normal") return null;
  const y = buf.baseY + buf.cursorY;
  let top = y;
  while (top > 0 && buf.getLine(top)?.isWrapped) top -= 1;
  let bottom = y;
  while (buf.getLine(bottom + 1)?.isWrapped) bottom += 1;
  if (bottom - top > 8) return null;
  let text = "";
  for (let r = top; r <= bottom; r++) {
    const line = buf.getLine(r);
    if (!line) return null;
    text += line.translateToString(r === bottom);
  }
  const p = S.promptInput(text);
  if (!p) return null;
  return { ...p, top, cols: term.cols, viewportY: buf.viewportY };
}

// createPaneSuggest wires one pane. deps:
//   term, body (the .term-body element), mac
//   canSuggest()             the client drives this local pane
//   send(obj)                JSON on the pane's session socket
//   insert(sug, i, shift, words) → Promise<{ok, message, dropped}>
//   restore(bytes)
export function createPaneSuggest(deps) {
  const { term, body } = deps;
  const layer = document.createElement("div");
  layer.className = "k-sug-layer";
  layer.setAttribute("aria-live", "polite");
  body.appendChild(layer);
  layer.addEventListener("mousedown", (ev) => {
    // Keep the keyboard in the terminal; a click on an option picks it.
    ev.preventDefault();
    const opt = ev.target.closest?.("[data-option]");
    if (opt && opt.classList.contains("k-sug-opt")) machine.select(Number(opt.getAttribute("data-option")));
    term.focus?.();
  });

  const machine = S.createSuggestMachine({
    mac: deps.mac,
    readLine: () => readPromptLine(term),
    canSuggest: deps.canSuggest,
    sendDetect: (seq, line) => deps.send({ type: "agent-detect", seq, line }),
    sendSuggest: (seq, line) => deps.send({ type: "agent-suggest", seq, line }),
    insert: deps.insert,
    restore: deps.restore,
    render,
  });

  function metrics() {
    const screen = term.element?.querySelector?.(".xterm-screen");
    if (!screen || !term.cols || !term.rows) return null;
    const r = screen.getBoundingClientRect();
    const b = body.getBoundingClientRect();
    return { cw: r.width / term.cols, ch: r.height / term.rows, ox: r.left - b.left, oy: r.top - b.top, bw: b.width, bh: b.height };
  }

  // rects for cells [start, start+len) of a logical line on screen.
  function cellRects(line, start, len, m) {
    const out = [];
    for (let i = start; i < start + len; ) {
      const row = line.top + Math.floor(i / line.cols) - line.viewportY;
      const col = i % line.cols;
      const n = Math.min(line.cols - col, start + len - i);
      if (row >= 0 && row < term.rows) out.push({ row, col, n });
      i += n;
    }
    return out.map((c) => ({ left: m.ox + c.col * m.cw, top: m.oy + c.row * m.ch, width: c.n * m.cw, height: m.ch, row: c.row }));
  }

  function place(el, box) {
    el.style.left = `${box.left}px`;
    el.style.top = `${box.top}px`;
    if (box.width != null) el.style.width = `${box.width}px`;
    if (box.height != null) el.style.height = `${box.height}px`;
  }

  function marks(line, start, len, cls, m) {
    const rects = cellRects(line, start, len, m);
    for (const r of rects) {
      const el = document.createElement("div");
      el.className = `k-sug-mark ${cls}`;
      place(el, r);
      layer.appendChild(el);
    }
    return rects;
  }

  function hint(html, after, m) {
    const el = document.createElement("div");
    el.className = "k-sug-hint";
    el.innerHTML = html;
    layer.appendChild(el);
    let left = after.left + after.width + m.cw;
    let top = after.top;
    if (left + el.offsetWidth > m.bw - m.cw) {
      // No room after the words: the row below, from the line's start.
      left = m.ox + m.cw;
      top = after.top + m.ch;
    }
    place(el, { left, top, height: m.ch });
    return el;
  }

  function card(html, rects, m) {
    const wrap = document.createElement("div");
    wrap.className = "k-sug-anchor";
    wrap.innerHTML = html;
    layer.appendChild(wrap);
    const last = rects[rects.length - 1];
    const first = rects[0];
    const below = last.top + m.ch;
    const h = wrap.offsetHeight;
    const top = below + h <= m.bh || first.top - h < 0 ? below : first.top - h;
    place(wrap, { left: m.ox, top });
  }

  function render(v) {
    layer.replaceChildren();
    layer.dataset.state = v.state;
    if (v.state === "idle") return;
    const line = v.line;
    const m = metrics();
    if (!line || !m) return;
    const opts = { mac: deps.mac, icons: { ai: AGENT_ICON, alert: AGENT_ALERT_ICON } };
    if (v.state === "notice") {
      const rects = marks(line, line.start, line.input.length, "k-sug-mark--notice", m);
      if (rects.length) hint(S.noticeHintHTML(deps.mac, AGENT_ICON), rects[rects.length - 1], m);
      return;
    }
    if (v.state === "inserted") {
      if (!v.seen) return;
      const rects = marks(line, line.start, v.command.length, "k-sug-mark--inserted", m);
      if (rects.length) hint(S.insertedHintHTML(deps.mac), rects[rects.length - 1], m);
      return;
    }
    // loading, error, card, inserting: the words greyed, the card below.
    const len = Math.max(line.input.length, 1);
    const rects = marks(line, line.start, len, "k-sug-mark--ghost", m);
    if (!rects.length) return;
    if (v.state === "card" || v.state === "inserting") card(S.cardHTML(v.sug, v.selected, { ...opts, note: v.note }), rects, m);
    else card(S.pendingHTML({ mac: deps.mac, icon: AGENT_ICON, message: v.state === "error" ? v.message : "" }), rects, m);
  }

  return machine;
}
