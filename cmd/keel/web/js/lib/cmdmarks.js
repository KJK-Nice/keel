// Command marks in a terminal: OSC 133 shell-integration marks (A prompt
// start, B command start, C output start, D;<exit> command end) become a
// gutter mark per command and an exit chip on failed ones, and give
// previous / next command navigation. docs/ui-redesign.md §14.
//
// The marks come from the remote's output, so they are untrusted: the
// payload is parsed strictly (the same rules as internal/shellmark), the
// only thing they can do is draw a mark, and the chip text is built from
// digits only. Nothing here writes to the PTY.

const MAX_PAYLOAD = 2048;
const MAX_EXIT = 2147483647;
// Prompt markers kept per terminal; older ones are disposed (xterm also
// disposes a marker when its line leaves the scrollback).
const MAX_PROMPTS = 1000;

// parseMark reads the payload of OSC 133 (the text after "133;"). It
// returns { kind, exit } or null. exit is a number only for D with 1–10
// digits within int32; anything else is no exit status.
export function parseMark(data) {
  if (typeof data !== "string" || data.length === 0 || data.length > MAX_PAYLOAD) return null;
  if (!/^[\x20-\x7e]+$/.test(data)) return null;
  const kind = data[0];
  if (data.length > 1 && data[1] !== ";") return null;
  if (kind === "A" || kind === "B" || kind === "C") return { kind, exit: null };
  if (kind !== "D") return null;
  const param = data.length > 2 ? data.slice(2).split(";")[0] : "";
  let exit = null;
  if (/^[0-9]{1,10}$/.test(param)) {
    const n = Number(param);
    if (n <= MAX_EXIT) exit = n;
  }
  return { kind, exit };
}

// exitLabel is the chip text for a failed command.
export function exitLabel(exit) {
  return `exit ${exit}`;
}

// attachCommandMarks wires OSC 133 handling into an xterm Terminal. It
// returns { jump(dir), count(), dispose() } or null when the terminal
// lacks the APIs (parser hooks, markers, decorations).
export function attachCommandMarks(term) {
  if (!term?.parser?.registerOscHandler || typeof term.registerMarker !== "function") return null;
  const prompts = [];
  let cur = null;

  const live = (m) => m && !m.isDisposed && m.line >= 0;

  function prompt() {
    const marker = term.registerMarker(0);
    if (!marker) return null;
    const p = { marker, ran: false, done: false };
    prompts.push(p);
    while (prompts.length > MAX_PROMPTS) prompts.shift().marker.dispose?.();
    return p;
  }

  function decorate(p, exit) {
    if (typeof term.registerDecoration !== "function" || !live(p.marker)) return;
    const failed = exit !== null && exit !== 0;
    const mark = term.registerDecoration({ marker: p.marker, x: 0, width: 1, layer: "top" });
    mark?.onRender?.((el) => {
      if (el.dataset.kCmd) return;
      el.dataset.kCmd = failed ? "failed" : "ok";
      el.classList.add("k-cmdmark", failed ? "k-cmdmark--failed" : "k-cmdmark--ok");
      const dot = el.ownerDocument.createElement("span");
      dot.className = "k-cmdmark-dot";
      el.appendChild(dot);
      el.title = exit === null ? "Command finished · exit status not reported" : `Command finished · exit ${exit}`;
    });
    if (!failed) return;
    const label = exitLabel(exit);
    const chip = term.registerDecoration({ marker: p.marker, anchor: "right", x: 0, width: label.length + 2, layer: "top" });
    chip?.onRender?.((el) => {
      if (el.dataset.kCmd) return;
      el.dataset.kCmd = "chip";
      el.classList.add("k-cmdchip-cell");
      const badge = el.ownerDocument.createElement("span");
      badge.className = "k-badge k-badge--flare k-badge--mono k-cmdchip";
      badge.textContent = label;
      el.appendChild(badge);
    });
  }

  const handler = term.parser.registerOscHandler(133, (data) => {
    const m = parseMark(data);
    if (!m) return true;
    switch (m.kind) {
      case "A":
        cur = prompt();
        break;
      case "B":
        if (!cur || cur.ran) cur = prompt();
        break;
      case "C":
        if (!cur || cur.ran) cur = prompt();
        if (cur) cur.ran = true;
        break;
      case "D":
        // Only a command that started (C) and has not ended gets a mark:
        // Enter on an empty line, or a repeated D, draws nothing.
        if (cur && cur.ran && !cur.done) {
          cur.done = true;
          decorate(cur, m.exit);
        }
        break;
    }
    return true;
  });

  // jump scrolls to the previous (dir < 0) or next (dir > 0) prompt
  // relative to the top of the viewport. Returns false when there is none.
  function jump(dir) {
    const buf = term.buffer?.active;
    if (!buf || typeof term.scrollToLine !== "function") return false;
    const top = buf.viewportY;
    const lines = prompts.filter((p) => live(p.marker)).map((p) => p.marker.line);
    let target = null;
    if (dir < 0) {
      for (const l of lines) if (l < top) target = l;
    } else {
      for (const l of lines) if (l > top) { target = l; break; }
    }
    if (target === null) return false;
    term.scrollToLine(target);
    return true;
  }

  return {
    jump,
    count: () => prompts.filter((p) => live(p.marker)).length,
    dispose() {
      handler?.dispose?.();
      for (const p of prompts) p.marker.dispose?.();
      prompts.length = 0;
      cur = null;
    },
  };
}
