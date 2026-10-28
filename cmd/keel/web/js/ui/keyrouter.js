// The page's chrome shortcuts: a keydown (lib/shortcuts.js classifies it)
// or a native menu item runs one action, once. A held key repeats only
// what classifyShortcut lets repeat, and a menu echo of a chord the
// keyboard just ran is dropped (lib/chordguard.js). What each action does
// stays with term.js. The caller passes:
//   mac                     ⌘ is the app modifier
//   native()                the page is in the native window
//   ctx                     { gateOpen(), modalOpen(), findOpen(), startPage(), renaming() }
//   run(name)               runs a named action (runShortcut in term.js)
//   find(dir)               find-next / find-prev in the focused pane
//   tabIndex(i), recentHost(i)
//   storage, debugParam     the debug log's saved switch and ?debug=keys
//   toast(message)
//
// Shortcut debug log: off unless storage["keel-debug-keys"] is "1" or
// debugParam is set. Each chrome chord then logs one line, also shown as a
// toast so it is readable in the native window, which has no console:
// source (keyboard or menu), name, repeat, and what happened (ran, repeat
// skipped, echo dropped, declined). The last 50 lines stay in log whether
// or not the log is on.
import { chromeChordName, classifyShortcut, menuShortcutAllowed } from "../lib/shortcuts.js";
import { createChordGuard } from "../lib/chordguard.js";

const DEBUG_KEY = "keel-debug-keys";
const LOG_LINES = 50;

export function createKeyRouter({ mac, native = () => false, ctx, run, find, tabIndex, recentHost, storage, debugParam = false, toast = () => {} }) {
  const guard = createChordGuard();
  const log = [];
  let debug = false;
  try {
    debug = storage?.getItem(DEBUG_KEY) === "1" || !!debugParam;
  } catch {
    debug = !!debugParam;
  }

  function logChord(source, name, repeat, outcome) {
    const line = `keel keys: ${source} ${name} repeat=${!!repeat} ${outcome}`;
    log.push(line);
    if (log.length > LOG_LINES) log.shift();
    if (!debug) return;
    console.info(line);
    toast(line.slice("keel keys: ".length));
  }

  function toggleDebug() {
    debug = !debug;
    try {
      if (debug) storage?.setItem(DEBUG_KEY, "1");
      else storage?.removeItem(DEBUG_KEY);
    } catch {
      /* the log stays on for this window only */
    }
    toast(debug ? "Logging shortcut keys" : "Stopped logging shortcut keys");
  }

  // handle runs the chord a keydown is, and reports whether it took the
  // key (the caller then stops it).
  function handle(ev) {
    const declined = ev.target?.closest?.(".tab-rename") || ctx.gateOpen();
    const action = declined
      ? null
      : classifyShortcut(ev, {
          mac,
          modalOpen: ctx.modalOpen(),
          findOpen: ctx.findOpen,
          startPage: ctx.startPage(),
        });
    if (!action) {
      const chord = chromeChordName(ev, mac);
      if (!chord) return false;
      // A held chord's repeat runs nothing (classifyShortcut) and goes
      // nowhere else either: not to the terminal, not to the native menu.
      if (ev.repeat && !declined && !ctx.modalOpen()) {
        ev.preventDefault();
        logChord("keyboard", chord, true, "repeat skipped");
        return true;
      }
      // In the native window a chrome chord declined here must not reach
      // the menu's accelerator, which would run it anyway (shortcuts.js).
      if (native()) ev.preventDefault();
      logChord("keyboard", chord, ev.repeat, "declined");
      return false;
    }
    ev.preventDefault();
    const name = action.name === "tab-index" || action.name === "recent-host" ? `${action.name}-${action.index}` : action.name;
    guard.keyboard(name);
    logChord("keyboard", name, ev.repeat, "ran");
    if (action.name === "find-next" || action.name === "find-prev") find(action.name === "find-prev" ? "prev" : "next");
    else if (action.name === "tab-index") tabIndex(action.index);
    else if (action.name === "recent-host") recentHost(action.index);
    else run(action.name);
    return true;
  }

  // menu runs a native menu item (window.__keelShortcut). A call that
  // echoes a chord the keyboard just ran, with no input since, is dropped;
  // any other call runs.
  function menu(name) {
    if (!guard.menu(name)) {
      logChord("menu", name, false, "echo dropped");
      return;
    }
    const ok = menuShortcutAllowed(name, {
      gateOpen: ctx.gateOpen(),
      renaming: ctx.renaming(),
      modalOpen: ctx.modalOpen(),
    });
    logChord("menu", name, false, ok ? "ran" : "declined");
    if (ok) run(name);
  }

  return {
    handle,
    menu,
    // input is any other input (a keyup, a mousedown, a keydown that is no
    // chrome chord): a menu call after it is the user's own.
    input: () => guard.input(),
    log,
    debugging: () => debug,
    toggleDebug,
  };
}
