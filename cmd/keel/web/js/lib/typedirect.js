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

// DIRECT_COMMANDS are the command words keel serve's allowlist
// (agent.DirectSafe, internal/agent/directsafe.go) may type without the
// dialog. The page uses them only as a hint, to skip a round trip and
// name the button; keel serve checks every flag and word itself.
const DIRECT_COMMANDS = new Set(
  "ls pwd whoami id groups uptime uname hostname date df du free cat head tail wc grep egrep fgrep sort uniq cut echo ps pgrep pidof ss netstat lsblk lscpu nproc arch tty w who last dmesg stat which type basename dirname realpath readlink printenv tree lsof dig host ping systemctl journalctl docker kubectl ip".split(" "),
);
const PIPE_FILTERS = new Set("grep egrep fgrep head tail wc sort uniq cut tr".split(" "));
// Shell syntax and expansions DirectSafe refuses outside quotes; ! anywhere.
const SYNTAX = /[$`;&<>(){}\\*?[\]#~^]/;

// directHint: could keel serve type this line without the dialog? False
// means it certainly will not (so the page goes straight to the dialog);
// true means it may, and keel serve decides.
export function directHint(command) {
  if (!directLine(command) || command.includes("!") || command.includes("\\") || /\|\||\|&/.test(command)) return false;
  // Outside quotes only.
  const bare = command.replace(/'[^']*'/g, "''").replace(/"[^"$`\\]*"/g, '""');
  if (SYNTAX.test(bare) || /['"]/.test(bare.replace(/''|""/g, ""))) return false;
  const segs = bare.split("|").map((s) => s.trim());
  return segs.every((seg, i) => {
    const name = seg.split(/\s+/)[0] || "";
    return (i === 0 ? DIRECT_COMMANDS : PIPE_FILTERS).has(name);
  });
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
  return !!p && p.risk === "readonly" && !p.needsShift && !p.placeholders && !/\{\{/.test(p.command || "") && directHint(p.command);
}

// stepDirect: a group run step that should go without the group approve
// dialog. Values filled in at Type are rated again by keel serve.
// A step on more than one host, one of them production, keeps the dialog.
export function stepDirect(step, cells = []) {
  if (!step || step.risk !== "readonly" || !directHint(step.command.replace(/\{\{[a-z0-9_]+(\|q)?\}\}/gi, "x"))) return false;
  return !(cells.length > 1 && cells.some((c) => c?.proposal?.production));
}

// snippetBadge: "Approve first" only on a snippet that will certainly
// open the dialog (more than one line, or a redaction placeholder).
export function snippetBadge(command) {
  const line = snippetLine(command);
  return !directLine(line) || /\[(secret|ip|host):\d+\]/.test(line) ? "Approve first" : "";
}

// cursorLine is the text of the line the terminal's cursor is on, sent
// with a direct type so keel serve can see an input prompt the page sees.
export function cursorLine(term) {
  const buf = term?.buffer?.active;
  const line = buf?.getLine?.(buf.baseY + buf.cursorY);
  return line ? String(line.translateToString(true)).slice(0, 512) : "";
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
