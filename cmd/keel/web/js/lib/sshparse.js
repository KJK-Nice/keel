// Reads a pasted `ssh …` command, an ssh:// URL or `user@host:port` into the
// few fields Keel's host model has (host, port, user). Pure functions: no
// DOM, no state, and nothing here executes, expands or reads anything.
//
// The text is split into words the way a shell would split quotes, but never
// expanded: no variables, no globbing, no substitution. Input that holds a
// shell operator (; | & < > ` $( newline) is refused outright, so it can
// neither yield a host nor be half-read. Options that run commands, change
// trust, open forwards or are not part of the host model are dropped and
// listed in `dropped`, so the UI can show what was left out. Key files are
// never opened: -i only becomes a note.

export const MAX_INPUT = 2048;

const HOST_RE = /^[A-Za-z0-9](?:[A-Za-z0-9_.-]*[A-Za-z0-9])?$/;
const USER_RE = /^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$/;
const IPV6_RE = /^[0-9A-Fa-f:]*:[0-9A-Fa-f:.]*$/;

// validHost accepts a DNS name, an IPv4 address or an IPv6 address (no
// brackets). A leading "-" is refused so a host can never read as an option.
export function validHost(host) {
  const h = String(host ?? "");
  if (!h || h.length > 253) return false;
  if (h.includes(":")) return IPV6_RE.test(h) && /^[0-9A-Fa-f:.]+$/.test(h) && h.split(":").length <= 8 && !h.includes(":::");
  if (h.includes("..")) return false;
  return HOST_RE.test(h) && h.split(".").every((label) => label.length <= 63);
}

export function validUser(user) {
  return USER_RE.test(String(user ?? ""));
}

// validPort accepts a whole number 1 to 65535 (the New host form allows 0 to
// 65535; 0 is not a port you can connect to).
export function validPort(port) {
  const s = String(port ?? "");
  if (!/^\d{1,5}$/.test(s)) return false;
  const n = Number(s);
  return n >= 1 && n <= 65535;
}

// ---- words -----------------------------------------------------------

// Characters that make a shell do something other than pass a word.
const OPERATORS = new Set([";", "|", "&", "<", ">", "(", ")", "`", "\n", "\r"]);

// splitWords splits like a POSIX shell for quotes and backslashes only.
// Returns { words } or { error }.
function splitWords(text) {
  const words = [];
  let cur = "";
  let inWord = false;
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === " " || c === "\t" || c === " ") {
      if (inWord) {
        words.push(cur);
        cur = "";
        inWord = false;
      }
      i++;
    } else if (c === "'") {
      const end = text.indexOf("'", i + 1);
      if (end < 0) return { error: "Unclosed quote" };
      cur += text.slice(i + 1, end);
      inWord = true;
      i = end + 1;
    } else if (c === '"') {
      i++;
      let closed = false;
      while (i < text.length) {
        const d = text[i];
        if (d === "\\" && i + 1 < text.length && '"\\'.includes(text[i + 1])) {
          cur += text[i + 1];
          i += 2;
        } else if (d === '"') {
          closed = true;
          i++;
          break;
        } else {
          cur += d;
          i++;
        }
      }
      if (!closed) return { error: "Unclosed quote" };
      inWord = true;
    } else if (c === "\\") {
      if (i + 1 >= text.length) return { error: "Trailing backslash" };
      cur += text[i + 1];
      inWord = true;
      i += 2;
    } else if (OPERATORS.has(c)) {
      return { error: "Shell operators are not allowed" };
    } else {
      cur += c;
      inWord = true;
      i++;
    }
  }
  if (inWord) words.push(cur);
  return { words };
}

// ---- options ---------------------------------------------------------

// Short options that take a value, and what Keel does with them.
const WITH_ARG = {
  p: "port",
  l: "user",
  i: "identity",
  J: "jump",
  o: "o",
  L: ["forward", "opens a port forward; add it in Forwards"],
  R: ["forward", "opens a port forward; add it in Forwards"],
  D: ["forward", "opens a port forward; add it in Forwards"],
  W: ["forward", "forwards standard input and output"],
  w: ["forward", "opens a tunnel device"],
  F: ["unsupported", "reads another config file"],
  b: ["unsupported", "not part of the host model"],
  B: ["unsupported", "not part of the host model"],
  c: ["unsupported", "not part of the host model"],
  E: ["unsupported", "writes a log file"],
  e: ["unsupported", "not part of the host model"],
  I: ["unsupported", "loads a PKCS#11 library"],
  m: ["unsupported", "not part of the host model"],
  O: ["unsupported", "controls a running connection"],
  P: ["unsupported", "not part of the host model"],
  Q: ["unsupported", "not part of the host model"],
  S: ["unsupported", "uses a control socket"],
};

// Short options without a value that are dropped. Everything else is
// refused as an unknown option.
const FLAGS = {
  A: "forwards your agent",
  a: "not part of the host model",
  X: "forwards X11",
  x: "not part of the host model",
  Y: "forwards X11",
  N: "runs no shell",
  f: "goes to the background",
  n: "not part of the host model",
  s: "starts a subsystem",
  T: "not part of the host model",
  t: "not part of the host model",
  q: "not part of the host model",
  v: "not part of the host model",
  C: "not part of the host model",
  g: "not part of the host model",
  G: "prints config instead of connecting",
  K: "not part of the host model",
  k: "not part of the host model",
  M: "controls a running connection",
  V: "not part of the host model",
  y: "not part of the host model",
  4: "not part of the host model",
  6: "not part of the host model",
  1: "not part of the host model",
  2: "not part of the host model",
};

// -o keys Keel can use, and keys it names a reason for. Any other key is
// dropped as "not part of the host model".
const O_USE = new Set(["port", "user"]);
const O_REASON = {
  proxycommand: "runs a command",
  localcommand: "runs a command",
  permitlocalcommand: "runs a command",
  remotecommand: "runs a command",
  knownhostscommand: "runs a command",
  match: "runs a command",
  include: "reads another config file",
  stricthostkeychecking: "changes host key trust",
  userknownhostsfile: "changes host key trust",
  globalknownhostsfile: "changes host key trust",
  hostkeyalgorithms: "changes host key trust",
  verifyhostkeydns: "changes host key trust",
  updatehostkeys: "changes host key trust",
  checkhostip: "changes host key trust",
  hostkeyalias: "changes host key trust",
  proxyjump: "jump",
  identityfile: "identity",
  localforward: "opens a port forward; add it in Forwards",
  remoteforward: "opens a port forward; add it in Forwards",
  dynamicforward: "opens a port forward; add it in Forwards",
  forwardagent: "forwards your agent",
  forwardx11: "forwards X11",
  hostname: "replaces the host",
};

const JUMP_REASON = "jump hosts are not supported yet";

function clip(s, n = 80) {
  const t = String(s);
  return t.length > n ? t.slice(0, n - 1) + "…" : t;
}

// Splits "-o Key=Value" / "Key Value" into [key, value].
function splitO(arg) {
  const m = /^\s*([A-Za-z][A-Za-z0-9]*)\s*(?:=|\s)\s*(.*)$/.exec(arg);
  return m ? [m[1], m[2]] : [arg.trim(), ""];
}

// ---- destination -----------------------------------------------------

// parseDest reads "[user@]host[:port]", "[user@][v6][:port]" and a bare
// IPv6 address. Returns { user, host, port } (strings, port may be "") or
// { error }.
function parseDest(raw) {
  let rest = raw;
  let user = "";
  const at = rest.lastIndexOf("@");
  if (at >= 0) {
    user = rest.slice(0, at);
    rest = rest.slice(at + 1);
    if (!validUser(user)) return { error: "User name has characters Keel does not accept" };
  }
  let host = rest;
  let port = "";
  if (rest.startsWith("[")) {
    const end = rest.indexOf("]");
    if (end < 0) return { error: "Unclosed [ in address" };
    host = rest.slice(1, end);
    const tail = rest.slice(end + 1);
    if (tail) {
      if (!tail.startsWith(":")) return { error: "Unexpected text after the address" };
      port = tail.slice(1);
    }
    if (!host.includes(":")) return { error: "Brackets are for IPv6 addresses" };
  } else {
    const colons = (rest.match(/:/g) || []).length;
    if (colons === 1) {
      [host, port] = rest.split(":");
    }
  }
  if (!validHost(host)) return { error: host ? "Host name has characters Keel does not accept" : "Host is missing" };
  if (port !== "" && !validPort(port)) return { error: "Port must be 1 to 65535" };
  return { user, host, port };
}

// ---- entry points ----------------------------------------------------

function result(fields) {
  return { ok: true, host: "", port: 22, user: "", identityFile: "", jump: "", dropped: [], notes: [], ...fields };
}

const fail = (error) => ({ ok: false, error });

function normalize(raw) {
  let text = String(raw ?? "");
  // A line continuation joins lines, as a shell would.
  text = text.replace(/\\\r?\n/g, " ");
  text = text.trim();
  // A copied prompt: "$ ssh host".
  text = text.replace(/^\$[ \t ]+/, "");
  return text.trim();
}

// refuse says why text can never be read, or "" when it can be.
function refuse(text) {
  if (text.length > MAX_INPUT) return "Too long";
  if (text.includes("$(") || text.includes("${") || text.includes("`")) return "Shell substitution is not allowed";
  if (/[\r\n]/.test(text)) return "One command only";
  if (/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(text)) return "Control characters are not allowed";
  return "";
}

// parseConnect reads one pasted line.
//
// Returns null when the text does not look like a connection (it is just a
// filter for saved hosts). Otherwise { ok: false, error, source } or
//   { ok: true, source, host, port, user, identityFile, jump, dropped, notes }
// source: "command" | "url" | "target".
// dropped: [{ option, reason }]: what was left out and why.
// notes: strings the user should read (for example the key file to pick).
// The caller shows `dropped` and `notes`; nothing here is ever run.
export function parseConnect(raw) {
  const text = normalize(raw);
  if (!text) return null;

  const isCommand = /^(?:\/\S*\/)?ssh(?:[ \t ]|$)/.test(text);
  const isURL = /^ssh:\/\//i.test(text);
  const isTarget = !isCommand && !isURL && /^[^\s@:/]+@\S+$/.test(text);
  if (!isCommand && !isURL && !isTarget) {
    // "host:port" alone is a plain connect only with a user; otherwise it
    // is a filter for saved hosts.
    return null;
  }
  const source = isURL ? "url" : isTarget ? "target" : "command";
  const why = refuse(text);
  if (why) return { ...fail(why), source };
  const r = isURL ? parseURL(text) : isTarget ? parseTargetText(text) : parseCommand(text);
  return r.ok ? r : { ...r, source };
}

function parseURL(text) {
  const split = splitWords(text);
  if (split.error) return fail(split.error);
  if (split.words.length !== 1) return fail("Expected one ssh:// address");
  let rest = split.words[0].slice("ssh://".length);
  const slash = rest.indexOf("/");
  if (slash >= 0) {
    if (rest.slice(slash + 1) !== "") return fail("A path after the address is not used");
    rest = rest.slice(0, slash);
  }
  const d = parseDest(rest);
  if (d.error) return fail(d.error);
  return result({ source: "url", host: d.host, port: d.port ? Number(d.port) : 22, user: d.user });
}

function parseTargetText(text) {
  const split = splitWords(text);
  if (split.error) return fail(split.error);
  if (split.words.length !== 1) return fail("Expected user@host:port");
  const d = parseDest(split.words[0]);
  if (d.error) return fail(d.error);
  return result({ source: "target", host: d.host, port: d.port ? Number(d.port) : 22, user: d.user });
}

function parseCommand(text) {
  const split = splitWords(text);
  if (split.error) return fail(split.error);
  const words = split.words.slice(1); // drop "ssh"
  const out = result({ source: "command" });
  const dropped = out.dropped;
  const drop = (option, reason) => dropped.push({ option: clip(option), reason });

  let port = null;
  let loginUser = null;
  let dest = "";
  let jump = "";
  let identity = "";
  const oPort = { v: null };
  const oUser = { v: null };

  const applyO = (arg) => {
    const [key, value] = splitO(arg);
    const k = key.toLowerCase();
    if (O_USE.has(k)) {
      if (k === "port") oPort.v = value.trim();
      else oUser.v = value.trim();
      return;
    }
    const reason = O_REASON[k];
    if (reason === "jump") {
      jump = value.trim();
      return;
    }
    if (reason === "identity") {
      if (!identity) identity = value.trim();
      return;
    }
    drop(`-o ${key}${value ? "=" + value : ""}`, reason || "not part of the host model");
  };

  let i = 0;
  let afterOpts = false;
  while (i < words.length) {
    const w = words[i];
    if (!afterOpts && w === "--") {
      afterOpts = true;
      i++;
      continue;
    }
    if (!afterOpts && w.length > 1 && w.startsWith("-")) {
      // A cluster of short options, such as -vvv, -4N or -p2222.
      let j = 1;
      i++;
      while (j < w.length) {
        const ch = w[j];
        if (ch in WITH_ARG) {
          let arg = w.slice(j + 1);
          if (!arg) {
            if (i >= words.length) return fail(`-${ch} needs a value`);
            arg = words[i++];
          }
          const spec = WITH_ARG[ch];
          if (spec === "port") port = arg;
          else if (spec === "user") loginUser = arg;
          else if (spec === "identity") identity = arg;
          else if (spec === "jump") jump = arg;
          else if (spec === "o") applyO(arg);
          else drop(`-${ch} ${arg}`, spec[1]);
          break;
        }
        if (ch in FLAGS) {
          drop(`-${ch}`, FLAGS[ch]);
          j++;
          continue;
        }
        return fail(`Unknown option -${ch}`);
      }
      continue;
    }
    if (!dest) {
      dest = w;
      i++;
      continue;
    }
    // The first word after the destination that is not an option starts a
    // remote command. It is never run.
    drop(words.slice(i).join(" "), "remote command; Keel opens a shell and does not run it");
    break;
  }

  if (!dest) return fail("No host in the command");
  if (dest.startsWith("-")) return fail("Host looks like an option");

  let d;
  if (/^ssh:\/\//i.test(dest)) {
    const u = parseURL(dest);
    if (!u.ok) return u;
    d = { user: u.user, host: u.host, port: String(u.port) };
  } else {
    d = parseDest(dest);
    if (d.error) return fail(d.error);
  }

  // The destination's own user and port win over -l and -p, as in ssh.
  const portText = d.port || (port ?? oPort.v);
  if (portText !== null && !validPort(portText)) return fail("Port must be 1 to 65535");
  const userText = d.user || (loginUser ?? oUser.v);
  if (userText !== null && !validUser(userText)) return fail("User name has characters Keel does not accept");

  out.host = d.host;
  out.port = portText ? Number(portText) : 22;
  out.user = userText || "";

  if (jump) {
    out.jump = clip(jump, 120);
    drop(`-J ${jump}`, JUMP_REASON);
  }
  if (identity) {
    out.identityFile = clip(identity, 160);
    out.notes.push(
      `Key file ${clip(identity, 160)} was not read. Add the key in Keychain, then pick it as the host's identity.`,
    );
  }
  return out;
}

// describeDropped turns the dropped list into one line per entry for the
// note under the input and in the New host dialog.
export function describeDropped(dropped) {
  return (dropped || []).map((d) => `${d.option}: ${d.reason}`);
}
