// The shortcut list shown by the command palette (its key hints) and by
// Settings · Keyboard. One table, so the two cannot drift. Pure: no DOM.
//
// Each entry is { id, group, title, k } or { id, group, title, mac, other }.
//   k      the key after the app modifier: ⌘ on macOS, Ctrl+ elsewhere
//   mac /  the whole label where the key differs by platform (the agent
//   other  panel and undo, which cannot be Ctrl+I / Ctrl+Z off macOS)
// `id` is the palette item's id where there is one. Keys that are matched in
// lib/shortcuts.js and lib/suggest.js are listed here by hand; the tests
// (test/shortcutmap.test.mjs) pin them against classifyShortcut and undoKey.

export const SHORTCUT_GROUPS = ["General", "Tabs", "Vault", "Panes", "Terminal", "Agent"];

export const SHORTCUTS = [
  { id: "new-tab", group: "Tabs", title: "New tab", k: "T" },
  { id: "close-tab", group: "Tabs", title: "Close pane or tab", k: "W" },
  { id: "reopen-tab", group: "Tabs", title: "Reopen last tab", k: "⇧T" },
  { id: "prev-tab", group: "Tabs", title: "Previous tab", k: "<" },
  { id: "next-tab", group: "Tabs", title: "Next tab", k: ">" },
  { id: "first-tab", group: "Tabs", title: "First tab", k: "1" },
  { id: "last-tab", group: "Tabs", title: "Last tab", k: "9" },
  { id: "palette", group: "General", title: "Command palette", k: "K" },
  { id: "toggle-sidebar", group: "Vault", title: "Toggle vault sidebar", k: "B" },
  { id: "new-host", group: "Vault", title: "New host", k: "N" },
  { id: "quick-connect", group: "Vault", title: "Quick connect", k: "⇧O" },
  { id: "join", group: "Vault", title: "Join session", k: "J" },
  { id: "settings", group: "Vault", title: "Settings", k: "," },
  { id: "split-right", group: "Panes", title: "Split right", k: "⇧D" },
  { id: "split-down", group: "Panes", title: "Split down", k: "⇧E" },
  { id: "focus-pane-zoom", group: "Panes", title: "Focus view (zoom pane)", k: "⇧↵" },
  { id: "find-term", group: "Panes", title: "Find in terminal", k: "F" },
  { id: "font-up", group: "Terminal", title: "Larger text", k: "+" },
  { id: "font-down", group: "Terminal", title: "Smaller text", k: "-" },
  { id: "font-reset", group: "Terminal", title: "Reset text size", k: "0" },
  { id: "toggle-agent", group: "Agent", title: "Toggle agent panel", mac: "⌘I", other: "Ctrl+Shift+I" },
  { id: "ask-agent", group: "Agent", title: "Ask the agent (from the palette)", k: "↵" },
  { id: "suggest-undo", group: "Agent", title: "Put back your words after an inserted suggestion", mac: "⌘Z", other: "Ctrl+Shift+Z" },
];

// keysFor is the label for a shortcut on this platform, "" for an unknown id.
export function keysFor(id, mac) {
  const s = SHORTCUTS.find((x) => x.id === id);
  if (!s) return "";
  if (s.k != null) return (mac ? "⌘" : "Ctrl+") + s.k;
  return mac ? s.mac : s.other;
}

// keyCaps splits a label into the caps to draw: "Ctrl+Shift+I" -> Ctrl,
// Shift, I; "⌘⇧T" -> ⌘, ⇧, T; "Ctrl++" -> Ctrl, +.
export function keyCaps(label) {
  const caps = [];
  let rest = String(label || "");
  for (const mod of ["Ctrl+", "Shift+"]) {
    if (rest.startsWith(mod) && rest.length > mod.length) {
      caps.push(mod.slice(0, -1));
      rest = rest.slice(mod.length);
    }
  }
  const chars = [...rest];
  if (chars.length > 1 && /^[⌘⇧⌥⌃]/u.test(rest)) {
    let i = 0;
    while (i < chars.length - 1 && /[⌘⇧⌥⌃]/u.test(chars[i])) caps.push(chars[i++]);
    caps.push(chars.slice(i).join(""));
  } else if (rest) caps.push(rest);
  return caps;
}

// shortcutGroups is the list for Settings · Keyboard: [{ group, items:
// [{ id, title, keys }] }] in SHORTCUT_GROUPS order.
export function shortcutGroups(mac) {
  return SHORTCUT_GROUPS.map((group) => ({
    group,
    items: SHORTCUTS.filter((s) => s.group === group).map((s) => ({ id: s.id, title: s.title, keys: keysFor(s.id, mac) })),
  }));
}
