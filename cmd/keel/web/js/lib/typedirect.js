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

// The direct grammar, as keel serve's agent.DirectSafe
// (internal/agent/directsafe.go) has it. The page uses it only as a hint,
// to skip a round trip and name the button; keel serve decides.
const EXACT = new Set(["pwd", "whoami", "id", "date", "uptime", "hostname", "uname -a", "df -h", "free -h", "ps aux", "docker ps", "docker ps -a"]);
const KUBE = new Set("pods nodes svc services deployments ns namespaces ingress pvc events jobs cronjobs statefulsets daemonsets replicasets".split(" "));
const HOMES = ["/root", "/home", "/users", "/var/root", "/private/var/root"];
const SECRETS = ["/proc", "environ", "/.ssh", "/.aws", "/.kube", "/.gnupg", "/.config/gh", "/.docker", "/.netrc", "/.git-credentials", "/etc/shadow", "/etc/gshadow", "/etc/sudoers",
  "id_", ".pem", ".key", ".p12", ".pfx", "credential", "token", "secret", "password", ".env", "kubeconfig", "shadow", "_history", ".pgpass", ".npmrc", ".pypirc", ".my.cnf", ".ssh"];

function safePath(p) {
  if (!/^[A-Za-z0-9_./-]{1,128}$/.test(p) || p.startsWith("-") || p.includes("//")) return false;
  if (p.split("/").some((seg) => seg === "." || seg === "..")) return false;
  const c = p.length > 1 ? p.replace(/\/$/, "").toLowerCase() : p;
  if (HOMES.some((h) => c === h || c.startsWith(h + "/"))) return false;
  return !SECRETS.some((x) => ("/" + c).includes(x));
}

function kubeGet(w) {
  if (w.length < 2 || w[0] !== "get" || !KUBE.has(w[1])) return false;
  let ns = false;
  let wide = false;
  for (let i = 2; i < w.length; i++) {
    if (w[i] === "-n" && !ns && /^[a-z0-9-]{1,63}$/.test(w[i + 1] || "")) {
      ns = true;
      i++;
    } else if (w[i] === "-A" && !ns) ns = true;
    else if (w[i] === "-o" && !wide && w[i + 1] === "wide") {
      wide = true;
      i++;
    } else return false;
  }
  return true;
}

// directHint: could keel serve type this line without the dialog? False
// means it certainly will not (the page goes straight to the dialog).
export function directHint(command) {
  if (typeof command !== "string" || command.length > 256 || !/^[\x21-\x7e]+( [\x21-\x7e]+)*$/.test(command)) return false;
  if (EXACT.has(command)) return true;
  const w = command.split(" ");
  if (w[0] === "ls") {
    const rest = ["-l", "-la", "-lah"].includes(w[1]) ? w.slice(2) : w.slice(1);
    return rest.length === 0 || (rest.length === 1 && safePath(rest[0]));
  }
  if (w[0] === "kubectl") return kubeGet(w.slice(1));
  if (w[0] === "systemctl") return w.length === 4 && w[1] === "status" && /^[A-Za-z0-9@._][A-Za-z0-9@._-]{0,127}$/.test(w[2]) && w[3] === "--no-pager";
  return false;
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
// open the dialog: one outside the direct grammar.
export function snippetBadge(command) {
  return directHint(snippetLine(command)) ? "" : "Approve first";
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
