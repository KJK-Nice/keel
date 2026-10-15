// Plain language → command in a pane (docs/ui-redesign.md §5.5). Pure
// functions: no DOM, no state. ui/suggest.js renders with them; the node
// tests pin them.
//
// Nothing here writes to a pane. keel serve decides whether a line is
// noticed (agent service Detect), writes a suggestion only after the
// user's ⇥ / ⇧⇥ (AuthorizeInsert, then the approved-write ticket), and checks
// the same ⇧ rule again before it writes.

import { escapeHtml } from "./html.js";
import { riskBadge } from "./agent.js";

// DETECT_PAUSE_MS is the typing pause before the prompt line is checked.
// Detection never runs per key; the server's thresholds and timeouts are
// in internal/agent/service/suggestq.go.
export const DETECT_PAUSE_MS = 350;
// DETECT_CACHE_MS keeps an answer for the same words, so moving the
// cursor or a redraw does not ask again.
export const DETECT_CACHE_MS = 2 * 60 * 1000;
// OFF_BACKOFF_MS: after keel serve says suggestions are off for a pane
// (setting off, no model key, host not readable), wait before asking again.
export const OFF_BACKOFF_MS = 15 * 1000;
export const MAX_LINE = 300;

// A shell prompt ends at the first "$ ", "# ", "% ", "> ", "❯ " or "» "
// on the line: user@host:~$ , [user@host dir]# , sh-3.2$ , ❯ .
const PROMPT_RE = /^(.*?[$#%>❯»] )/;

// promptInput splits the cursor's logical line (wrapped rows joined) into
// the prompt and what was typed after it. null when no prompt is found.
// start is the index of the input in text (a cell column for ASCII).
export function promptInput(text) {
  const s = String(text || "");
  const m = PROMPT_RE.exec(s);
  if (!m) return null;
  const start = m[1].length;
  const input = s.slice(start).replace(/\s+$/, "");
  return { prompt: m[1], input, start };
}

// worthDetecting says whether a line is worth one detection call: a
// #-line, or two or more words, within MAX_LINE.
export function worthDetecting(input) {
  const s = String(input || "").trim();
  if (s.length < 3 || s.length > MAX_LINE) return false;
  if (/[\u0000-\u001f\u007f]/.test(s)) return false;
  if (s.startsWith("#")) return s.replace(/^#+/, "").trim().length > 0;
  return s.split(/\s+/).length >= 2;
}

// insertGate decides what a ⇥ / ⇧⇥ press does on a suggestion option:
// { ok: true } inserts; { ok: false, reason: "shift" } shows "press ⇧⇥".
// Plain ⇥ never inserts an option that needs ⇧ (destructive, or a state
// change on a production host, §6.5).
export function insertGate(option, shift) {
  if (!option) return { ok: false, reason: "none" };
  if (option.needsShift && !shift) return { ok: false, reason: "shift" };
  return { ok: true };
}

// restoreBytes is what ⌘Z sends to put the user's own words back: Ctrl-U
// to clear the inserted command, then the words. Control characters are
// dropped, so a restore can never press Enter.
export function restoreBytes(words) {
  return "\u0015" + String(words || "").replace(/[\u0000-\u001f\u007f]/g, "");
}

// undoKey reports the "back to your words" key: ⌘Z on macOS. Elsewhere
// the app modifier is Ctrl, and Ctrl+Z suspends the foreground job
// (SIGTSTP), so it stays with the shell and undo is Ctrl+Shift+Z, as
// Ctrl+Shift+I is the agent panel there.
export function undoKey(ev, mac) {
  if (!ev || ev.altKey || String(ev.key || "").toLowerCase() !== "z") return false;
  if (mac) return !!ev.metaKey && !ev.ctrlKey && !ev.shiftKey;
  return !!ev.ctrlKey && !!ev.shiftKey && !ev.metaKey;
}

// optionKey maps a key in the suggestion card to a new selection: "1".."3"
// pick that option, ↑ / ↓ move. null for any other key.
export function optionKey(ev, selected, n) {
  if (!ev || ev.metaKey || ev.ctrlKey || ev.altKey || n < 1) return null;
  if (ev.key === "ArrowDown") return (selected + 1) % n;
  if (ev.key === "ArrowUp") return (selected - 1 + n) % n;
  if (!ev.shiftKey && /^[1-9]$/.test(ev.key || "")) {
    const i = Number(ev.key) - 1;
    return i < n ? i : null;
  }
  return null;
}

// keyLabels are the glyphs on macOS and words elsewhere.
export function keyLabels(mac) {
  return mac
    ? { tab: "⇥", shift: "⇧", esc: "esc", enter: "↵", up: "↑", down: "↓", undo: ["⌘", "Z"] }
    : { tab: "Tab", shift: "Shift", esc: "Esc", enter: "Enter", up: "↑", down: "↓", undo: ["Ctrl", "Shift", "Z"] };
}

const kbd = (k) => `<span class="k-kbd">${escapeHtml(k)}</span>`;

// noticeHintHTML is State 1's hint after the noticed words.
export function noticeHintHTML(mac, icon = "") {
  const k = keyLabels(mac);
  return `${icon}<span>Looks like a request</span>${kbd(k.tab)}<span>suggest a command</span>`;
}

// insertedHintHTML is State 3's hint after the inserted command.
export function insertedHintHTML(mac) {
  const k = keyLabels(mac);
  return `<span>Inserted, not run</span>${kbd(k.enter)}<span>run</span>${k.undo.map(kbd).join("")}<span>back to your words</span>`;
}

// riskWords is the badge text. Destructive says what it does when the
// command makes it plain.
export function riskWords(option) {
  if (option?.risk === "destructive") {
    const c = String(option.command || "");
    if (/(^|[\s;&|(])(sudo\s+(-\S+\s+)*)?(rm|rmdir|unlink|shred|find\b.*-delete|pg_archivecleanup)\b/.test(c)) return "Deletes files";
    return "Destructive";
  }
  return riskBadge(option?.risk).label;
}

// cardHTML is State 2 (Suggest) or State 4 (Destructive): the selected
// option's command, risk badge and reason, the other options numbered by
// their place, and the keys. icons: { ai, alert } SVG strings.
export function cardHTML(sug, selected, opts = {}) {
  const options = Array.isArray(sug?.options) ? sug.options : [];
  const sel = options[selected] || options[0];
  if (!sel) return "";
  const k = keyLabels(!!opts.mac);
  const icons = opts.icons || {};
  const badge = riskBadge(sel.risk);
  const danger = sel.risk === "destructive";
  const others = options
    .map((o, i) => ({ o, i }))
    .filter(({ i }) => i !== selected)
    .map(
      ({ o, i }) =>
        `<div class="k-sug-opt" data-option="${i}">${kbd(String(i + 1))}<span class="k-sug-opt-cmd">${escapeHtml(o.command)}</span></div>`,
    )
    .join("");
  let foot;
  if (sel.needsShift) {
    const why = danger ? "Plain " + k.tab + " never inserts a destructive command" : "Production host: " + k.tab + " alone does not insert a state change";
    foot =
      `<span>${kbd(k.shift)}${kbd(k.tab)}insert anyway</span>` +
      (options.length > 1 ? `<span>${kbd(k.up)}${kbd(k.down)}other options</span>` : "") +
      `<span>${kbd(k.esc)}keep my words</span>` +
      `<span class="k-sug-rule">${escapeHtml(why)}</span>`;
  } else {
    foot =
      `<span>${kbd(k.tab)}insert</span>` +
      (options.length > 1 ? `<span>${kbd(k.up)}${kbd(k.down)}other options</span>` : "") +
      `<span>${kbd(k.esc)}keep my words</span>`;
  }
  const note = opts.note ? `<div class="k-sug-note" role="status">${escapeHtml(opts.note)}</div>` : "";
  const placeholders = sel.placeholders
    ? `<div class="k-sug-note">Holds a redacted value such as [secret:1]. Replace it after inserting.</div>`
    : "";
  const label = danger ? "Suggested command that " + riskWords(sel).toLowerCase() : "Suggested command";
  return `<div class="k-sug${danger ? " k-sug--danger" : ""}" role="dialog" aria-label="${escapeHtml(label)}" data-risk="${escapeHtml(sel.risk || "changes")}" data-option="${selected}">
<div class="k-sug-head">${icons.ai || ""}<span>Suggested from your words</span><span class="k-badge ${badge.cls}">${danger ? icons.alert || "" : ""}${escapeHtml(riskWords(sel))}</span></div>
<div class="k-sug-cmd">${escapeHtml(sel.command)}</div>
${sel.why ? `<div class="k-sug-why">${escapeHtml(sel.why)}</div>` : ""}${placeholders}${note}
${others ? `<div class="k-sug-alt">${others}</div>` : ""}
<div class="k-sug-foot">${foot}</div>
</div>`;
}

// pendingHTML is the card while the suggestion is on its way, or when it
// failed (message set).
export function pendingHTML(opts = {}) {
  const k = keyLabels(!!opts.mac);
  const body = opts.message
    ? `<div class="k-sug-why" role="alert">${escapeHtml(opts.message)}</div>`
    : `<div class="k-sug-why" role="status">Suggesting a command…</div>`;
  return `<div class="k-sug" role="dialog" aria-label="Suggested command" aria-busy="${opts.message ? "false" : "true"}">
<div class="k-sug-head">${opts.icon || ""}<span>Suggested from your words</span></div>
${body}
<div class="k-sug-foot"><span>${kbd(k.esc)}keep my words</span></div>
</div>`;
}

const isTab = (ev) => ev.key === "Tab" && !ev.ctrlKey && !ev.metaKey && !ev.altKey;
const isModifier = (ev) => ["Shift", "Meta", "Control", "Alt", "CapsLock", "Fn"].includes(ev.key);

// createSuggestMachine is one pane's suggestion state (§5.5): idle →
// notice → loading → card → inserting → inserted, plus error. It holds no
// DOM; deps do the I/O:
//
//   readLine()        → { input, start, top, cols } for the cursor's line at
//                       a shell prompt, or null (no prompt, alt screen)
//   canSuggest()      → the client drives a local terminal pane (§6.6)
//   sendDetect(seq, line), sendSuggest(seq, line)   on the session socket
//   insert(sug, index, shift) → Promise<{ ok, message }>   ⇥ / ⇧⇥: keel serve
//                       authorises and writes ^U + the command, not run
//   restore(bytes)    the user's ⌘Z: their own words back, as keystrokes
//   render(view)      draw view = { state, line, words, sug, selected, note,
//                       message, command }
//   setTimer(fn, ms) / clearTimer(id) / now()
//   mac               key glyphs and the undo key
//
// onKey(ev) returns true when it used the key (the caller then stops
// xterm and the browser). ⇥ is used only while a line is noticed or a
// card shows; everywhere else it stays the shell's completion key.
export function createSuggestMachine(deps) {
  const now = deps.now || (() => Date.now());
  const setTimer = deps.setTimer || ((fn, ms) => setTimeout(fn, ms));
  const clearTimer = deps.clearTimer || ((id) => clearTimeout(id));
  let st = { state: "idle" };
  let seq = 0;
  let pending = null; // { seq, input } of the detect in flight
  let timer = null;
  let offUntil = 0;
  let dismissed = "";
  const cache = new Map(); // input → { plain, at }

  const view = (extra = {}) => ({ ...st, line: deps.readLine?.() || null, ...extra });
  const set = (next) => {
    st = next;
    deps.render?.(view());
  };
  const idle = () => {
    if (st.state !== "idle") set({ state: "idle" });
  };
  const restore = () => {
    const words = st.words;
    dismissed = words;
    idle();
    deps.restore?.(restoreBytes(words));
  };

  function detectNow() {
    timer = null;
    if (!deps.canSuggest?.()) return idle();
    if (st.state !== "idle" && st.state !== "notice") return;
    const line = deps.readLine?.();
    if (!line || !worthDetecting(line.input)) return idle();
    const input = line.input;
    if (input === dismissed || now() < offUntil) return;
    const hit = cache.get(input);
    if (hit && now() - hit.at < DETECT_CACHE_MS) {
      if (hit.plain) set({ state: "notice", words: input });
      return;
    }
    seq += 1;
    pending = { seq, input };
    deps.sendDetect?.(seq, input);
  }

  function schedule() {
    if (timer != null) clearTimer(timer);
    timer = setTimer(detectNow, DETECT_PAUSE_MS);
  }

  function startSuggest() {
    seq += 1;
    pending = null;
    set({ state: "loading", words: st.words, seq });
    deps.sendSuggest?.(seq, st.words);
  }

  async function doInsert(shift) {
    const { sug, selected, words } = st;
    const opt = sug.options[selected];
    const gate = insertGate(opt, shift);
    if (!gate.ok) {
      const k = keyLabels(!!deps.mac);
      set({ ...st, note: `Press ${k.shift}${k.tab} to insert this command.` });
      return;
    }
    set({ ...st, state: "inserting", note: "" });
    let r;
    try {
      r = await deps.insert(sug, selected, !!shift);
    } catch (err) {
      r = { ok: false, message: err?.message || String(err) };
    }
    if (st.state !== "inserting") return;
    if (r?.ok) set({ state: "inserted", words, command: opt.command, at: now(), seen: false });
    else set({ state: "card", sug, selected, words, note: r?.message || "Not inserted." });
  }

  return {
    get state() {
      return st.state;
    },
    view,
    // onInput: the user typed (term.onData).
    onInput(data) {
      if (String(data).includes("\r")) {
        if (timer != null) clearTimer(timer);
        timer = null;
        dismissed = "";
        return idle();
      }
      if (st.state === "inserted" || st.state === "notice") idle();
      if (st.state === "idle") schedule();
    },
    // onRender: the terminal redrew; follow the line or let go of it.
    onRender() {
      if (st.state === "idle") return;
      if (!deps.canSuggest?.()) return idle();
      const line = deps.readLine?.();
      if (!line) return idle();
      if (st.state === "notice") {
        if (line.input !== st.words) return idle();
        return deps.render?.(view());
      }
      if (st.state === "inserted") {
        if (line.input === st.command) {
          st = { ...st, seen: true };
          return deps.render?.(view());
        }
        if (st.seen || now() - st.at > 3000) return idle();
        return;
      }
      deps.render?.(view());
    },
    onKey(ev) {
      if (!ev || ev.type !== "keydown" || st.state === "idle") return false;
      if (!deps.canSuggest?.()) {
        idle();
        return false;
      }
      switch (st.state) {
        case "notice":
          if (isTab(ev)) {
            startSuggest();
            return true;
          }
          if (ev.key === "Escape") {
            dismissed = st.words;
            idle();
          }
          return false;
        case "loading":
        case "inserting":
          if (isTab(ev)) return true;
          if (isModifier(ev)) return false;
          if (st.state === "loading") seq += 1;
          dismissed = st.words || "";
          idle();
          return ev.key === "Escape";
        case "error":
          if (isModifier(ev)) return false;
          dismissed = st.words || "";
          idle();
          return isTab(ev) || ev.key === "Escape";
        case "card": {
          if (isTab(ev)) {
            doInsert(ev.shiftKey);
            return true;
          }
          if (ev.key === "Escape") {
            dismissed = st.words;
            idle();
            return true;
          }
          const next = optionKey(ev, st.selected, st.sug.options.length);
          if (next != null) {
            set({ ...st, selected: next, note: "" });
            return true;
          }
          if (isModifier(ev)) return false;
          dismissed = st.words;
          idle();
          return false;
        }
        case "inserted":
          if (undoKey(ev, !!deps.mac)) {
            restore();
            return true;
          }
          return false;
      }
      return false;
    },
    // undo is the undo key's action, for the native Edit › Undo menu.
    undo() {
      if (st.state !== "inserted") return false;
      restore();
      return true;
    },
    select(i) {
      if (st.state === "card" && i >= 0 && i < st.sug.options.length) set({ ...st, selected: i, note: "" });
    },
    onDetect(msg) {
      if (!pending || msg?.seq !== pending.seq) return;
      const input = pending.input;
      pending = null;
      if (msg.source === "off") offUntil = now() + OFF_BACKOFF_MS;
      if (msg.source === "jev" || msg.source === "hash" || msg.source === "hash-only") {
        cache.set(input, { plain: !!msg.plain, at: now() });
        if (cache.size > 200) cache.delete(cache.keys().next().value);
      }
      if (!msg.plain || st.state !== "idle" || !deps.canSuggest?.()) return;
      const line = deps.readLine?.();
      if (line?.input === input) set({ state: "notice", words: input });
    },
    onSuggest(msg) {
      if (st.state !== "loading" || msg?.seq !== st.seq) return;
      if (msg.ok && msg.suggestion?.options?.length) {
        set({ state: "card", sug: msg.suggestion, selected: 0, words: st.words, note: "" });
        return;
      }
      if (["off", "no-key", "not-readable", "following"].includes(msg.reason)) offUntil = now() + OFF_BACKOFF_MS;
      set({ state: "error", words: st.words, message: msg.message || "No suggestion came back." });
    },
    reset() {
      if (timer != null) clearTimer(timer);
      timer = null;
      pending = null;
      idle();
    },
  };
}
