// Type without the approve dialog (docs/ui-redesign.md §6 rule 13, for
// review). Pure functions: no DOM, no state.
//
// keel serve decides whether a Type goes without the dialog
// (internal/httpapi/typedirect.go): a read-only command of one line is
// typed with one click and no Enter, and waits on the prompt line. These
// helpers only skip the round trip when the answer is already known to be
// the dialog, and name the button: "Type into pane" when no dialog should
// follow, "Type into pane…" when one will. A hint that says direct can
// still meet the dialog (keel serve rates it again, the pane may show vim);
// the ellipsis then undersells nothing, it only promised one click.

// NOT_LINE: what keeps a command from staying one line on a prompt as
// typed: C0 controls (newline, CR, tab, ESC), DEL, C1 controls, the line
// and paragraph separators, and invisible format characters. The same set
// as service.DirectLine.
const NOT_LINE = /[\u0000-\u001f\u007f-\u009f\u2028\u2029\p{Cf}]/u;

// directLine reports whether command stays one line on a shell prompt.
export function directLine(command) {
  const s = typeof command === "string" ? command : "";
  return s !== "" && !NOT_LINE.test(s);
}

// snippetLine is what a direct type of a snippet sends: its command
// without the trailing newlines that would run it.
export function snippetLine(command) {
  return String(command || "").replace(/[\r\n]+$/, "");
}

// commandOf is a proposal's typed bytes (^U, the command, a newline) as a
// direct type sends them: the command alone.
export function commandOf(bytes) {
  return String(bytes || "")
    .replace(/^\u0015/, "")
    .replace(/\n$/, "");
}

// proposalDirect: an agent proposal that keel serve should type without
// the dialog, as far as the page knows (its rating, its text).
export function proposalDirect(p) {
  return !!p && p.risk === "readonly" && !p.needsShift && !p.placeholders && !/\{\{/.test(p.command || "") && directLine(p.command);
}

// stepDirect: a group run step that should go without the group approve
// dialog. Values filled in at Type are rated again by keel serve.
export function stepDirect(step) {
  return !!step && step.risk === "readonly" && directLine(step.command);
}

// typeWord adds the ellipsis when a dialog will follow the press.
export function typeWord(label, direct) {
  return direct ? label : `${label}…`;
}

// altScreen reports whether an xterm.js terminal shows the alternate
// screen (vim, less, htop): typed bytes there are keystrokes.
export function altScreen(term) {
  return term?.buffer?.active?.type === "alternate";
}

// typedNote is the line shown after a direct type.
export function typedNote(target) {
  return `Typed into ${target || "the pane"}. Press Enter to run.`;
}

// typedNoteMany is the run page's line after one click typed into n panes.
export function typedNoteMany(n) {
  return n === 1 ? typedNote("1 pane") : `Typed into ${n} panes. Press Enter in each to run.`;
}
