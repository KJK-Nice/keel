// Menu echo guard for term.js. Pure: no DOM, no clock.
//
// The native window's menu items carry the same accelerators as the chords
// term.js runs (internal/desktop/desktop.go). WKWebView hands a ⌘ chord to
// the menu only when the page did not preventDefault its keydown, and
// ui/keyrouter.js does, so one press should run once. Keel has seen the
// menu run a chord the page had already run (Cmd+T opened two tabs, fixed
// in a0f9127), so this guard drops such an echo should one still come.
//
// It works on source and sequence, never on time:
//
//   keyboard(name)  the keyboard ran chord `name` (ui/keyrouter.js)
//   input()         any other input: a keydown that is not a chrome chord
//                   (nor a held key's repeat), a keyup or a mousedown.
//                   Clears every pending chord: a menu call after it is
//                   the user's own.
//   menu(name)      the menu asks to run `name` (window.__keelShortcut).
//                   Returns false when it is the echo of a chord the
//                   keyboard ran with no input since, and forgets that
//                   chord, so a second menu call runs.
//
// Two chords in a row (⌘⇧D then ⌘⇧↵, ⌘⇧ held) both stay pending, so a
// late echo of either is dropped: a chord's keydown is not "other input".
// Neither is a held key's auto-repeat: it is the same press.
export function createChordGuard() {
  let pending = [];
  return {
    keyboard(name) {
      pending.push(name);
    },
    input() {
      pending = [];
    },
    menu(name) {
      const i = pending.indexOf(name);
      if (i < 0) return true;
      pending.splice(i, 1);
      return false;
    },
    pending() {
      return [...pending];
    },
  };
}
