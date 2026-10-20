// markdownHTML renders the agent's answer (model output: untrusted) as a
// small, safe subset of markdown for the chat thread. Pure: no DOM, no
// state, so node tests cover it (test/markdown.test.mjs).
//
// Safety comes from the shape of the code, not from a sanitizer pass:
//   - every piece of source text goes through escapeHtml before it is
//     output, and the only tags ever produced are the ones written below;
//   - raw HTML is never passed through: `<b>` shows as the text "<b>";
//   - images are never made: `![alt](url)` shows as that text, so no
//     remote URL is fetched;
//   - a link is made only for http:, https: and mailto: URLs that parse
//     and hold no whitespace, control character, credentials or redaction
//     placeholder. It carries data-k-external, so a click goes through the
//     app's explicit open (lib/openlink.js), never a navigation of the page;
//   - redaction placeholders ([secret:1], [ip:2], [host:3]) and withheld
//     values ({{name}}, {{name|q}}) are atoms: shown as written, never a
//     link or emphasis, never resolved;
//   - a fenced code block gets a Copy button only. Nothing here can write
//     to a pane: typing stays with the proposal cards and approve-before-type
//     (docs/ui-redesign.md §6.1).
//
// Supported: paragraphs (a single line break is kept as a break), ATX
// headings (scaled down for the panel by CSS), emphasis, strong,
// strikethrough, inline code, fenced code (``` and ~~~; an unclosed fence
// runs to the end, so partial text never breaks out of it), bullet and
// ordered lists (nested by indent), blockquotes, rules, links, autolinks,
// bare URLs and GFM tables. Not supported (left as text): indented code,
// setext headings, reference links, footnotes, HTML.

import { escapeHtml } from "./html.js";

const MAX_DEPTH = 6; // nested lists / quotes past this are plain paragraphs
const MAX_INLINE_DEPTH = 8;
// Delimiters and brackets look this far ahead for their closer, so a long
// answer full of lone `*` or `[` stays near linear.
const SCAN = 4000;
// A link's text and URL are looked for this far: a lone `[` costs at most
// this much.
const SCAN_LINK = 1000;

// The placeholder families of internal/agent/redact (placeholder and
// validLabel): always shown as written.
const PLACEHOLDER_Y = /\[(?:secret|ip|host):\d+\]/y;
const WITHHELD_Y = /\{\{[a-z_][a-z0-9_]{0,31}(?:\|q)?\}\}/y;
// Whitespace, C0/C1 controls and invisible format characters (soft hyphen,
// zero-width, bidi controls, BOM): never part of a link Keel opens.
const INVISIBLE = new RegExp("[\\s\\x00-\\x1f\\x7f-\\x9f\\xad\\u200b-\\u200f\\u2028-\\u202e\\u2060-\\u206f\\ufeff]");
const HAS_PLACEHOLDER =/\[(?:secret|ip|host):\d+\]|\{\{[a-z_][a-z0-9_]{0,31}(?:\|q)?\}\}/;

// safeLinkURL returns the URL to put in href, or "" when the target is not
// one Keel opens: only http, https and mailto, parsed, with no whitespace,
// control or invisible character, no user:password@, and no redaction
// placeholder (the placeholder must stay visible as text).
export function safeLinkURL(raw) {
  const s = String(raw ?? "");
  if (!s || s.length > 2048) return "";
  if (INVISIBLE.test(s)) return "";
  if (HAS_PLACEHOLDER.test(s)) return "";
  if (!/^(?:https?:\/\/|mailto:)/i.test(s)) return "";
  let u;
  try {
    u = new URL(s);
  } catch {
    return "";
  }
  if (u.protocol === "mailto:") return u.pathname && !u.pathname.startsWith("/") ? u.href : "";
  if (u.protocol !== "http:" && u.protocol !== "https:") return "";
  if (!u.hostname || u.username || u.password) return "";
  return u.href;
}

function linkHTML(href, inner) {
  const h = escapeHtml(href);
  return `<a class="k-md-link" href="${h}" title="${h}" target="_blank" rel="noopener noreferrer" data-k-external>${inner}</a>`;
}

// --- inline ---

const PUNCT = /[!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~]/;
const isSpace = (c) => c === undefined || /\s/.test(c);
const isWord = (c) => c !== undefined && /[\p{L}\p{N}]/u.test(c);

const TICKS_Y = /`+/y;
const AUTOLINK_Y = /<((?:https?:\/\/|mailto:)[^\s<>]+)>/iy;
const BARE_Y = /(?:https?:\/\/|mailto:)[^\s<>"'`]+/iy;

// at runs a /y regex at index i, without slicing the string.
function at(re, s, i) {
  re.lastIndex = i;
  return re.exec(s);
}

// skipCode returns the index of the last backtick of the code span that
// opens at i, or -1 when its run of backticks has no closer.
function skipCode(s, i) {
  const run = at(TICKS_Y, s, i)[0];
  let j = i + run.length;
  while ((j = s.indexOf(run, j)) >= 0) {
    if (s[j + run.length] !== "`") return j + run.length - 1;
    while (s[j] === "`") j++;
  }
  return -1;
}

// findClose finds the index of the delimiter `d` that closes an opener
// ending at `from`, skipping code spans, so `*a `*` b*` pairs right.
// memo keeps, per delimiter, where a failed search stopped: whether a
// position closes does not depend on the opener, so a later opener of the
// same delimiter resumes there instead of scanning the same text again.
function findClose(s, from, d, intraword, memo) {
  const stop = Math.min(s.length, from + SCAN);
  let i = Math.max(from, memo.get(d) ?? 0);
  for (; i < stop; i++) {
    const c = s[i];
    if (c === "\\") {
      i++;
      continue;
    }
    if (c === "`") {
      const end = skipCode(s, i);
      if (end > 0) i = end;
      else while (s[i + 1] === "`") i++;
      continue;
    }
    if (s.startsWith(d, i) && i > from && !isSpace(s[i - 1])) {
      if (!intraword && isWord(s[i + d.length])) continue;
      // `**` closes on the last two of `***`; `*` never closes on `**`.
      if (s[i + d.length] === d[0]) {
        if (d.length === 1) i++;
        continue;
      }
      return i;
    }
  }
  memo.set(d, i);
  return -1;
}

// matchBracket returns the index of the `]` that closes the `[` at i, or -1.
function matchBracket(s, i) {
  let depth = 0;
  const stop = Math.min(s.length, i + SCAN_LINK);
  for (let j = i; j < stop; j++) {
    const c = s[j];
    if (c === "\\") {
      j++;
      continue;
    }
    if (c === "`") {
      const end = skipCode(s, j);
      if (end > 0) j = end;
      else while (s[j + 1] === "`") j++;
      continue;
    }
    if (c === "[") depth++;
    else if (c === "]" && --depth === 0) return j;
  }
  return -1;
}

// linkDest reads `(url "title")` at i: { url, end } with end past the `)`,
// or null.
function linkDest(s, i) {
  if (s[i] !== "(") return null;
  const stop = Math.min(s.length, i + SCAN);
  let j = i + 1;
  while (s[j] === " ") j++;
  let url = "";
  if (s[j] === "<") {
    const end = s.indexOf(">", j);
    if (end < 0 || end > stop || s.slice(j, end).includes("\n")) return null;
    url = s.slice(j + 1, end);
    j = end + 1;
  } else {
    let depth = 0;
    const start = j;
    for (; j < stop; j++) {
      const c = s[j];
      if (c === "\\") {
        j++;
        continue;
      }
      if (/\s/.test(c)) break;
      if (c === "(") depth++;
      else if (c === ")") {
        if (depth === 0) break;
        depth--;
      }
    }
    url = s.slice(start, j);
  }
  while (s[j] === " ") j++;
  if (s[j] === '"' || s[j] === "'") {
    const end = s.indexOf(s[j], j + 1);
    if (end < 0 || end > stop) return null;
    j = end + 1;
    while (s[j] === " ") j++;
  }
  if (s[j] !== ")") return null;
  return { url: url.replace(/\\([!-/:-@[-`{-~])/g, "$1"), end: j + 1 };
}

// bareURL reads a URL in running text at i. It stops at whitespace, quotes
// and angle brackets, and leaves trailing punctuation and an unbalanced
// closing bracket outside.
function bareURL(s, i) {
  const m = at(BARE_Y, s, i);
  if (!m) return null;
  let url = m[0];
  const count = (ch) => url.split(ch).length - 1;
  for (;;) {
    const last = url[url.length - 1];
    if (/[.,:;!?*_~]/.test(last)) url = url.slice(0, -1);
    else if (last === ")" && count("(") < count(")")) url = url.slice(0, -1);
    else if (last === "]" && count("[") < count("]")) url = url.slice(0, -1);
    else break;
  }
  return url;
}

function inline(s, depth = 0, inLink = false) {
  if (depth > MAX_INLINE_DEPTH) return escapeHtml(s);
  const memo = new Map();
  let out = "";
  let text = "";
  const flush = () => {
    out += escapeHtml(text);
    text = "";
  };
  for (let i = 0; i < s.length; ) {
    const c = s[i];

    // A line break is kept as a break in chat (`\` or two spaces at the
    // end of a line, the markdown hard break, mean the same).
    if (c === "\n") {
      text = text.replace(/(?:\\|[ \t]+)$/, "");
      flush();
      out += "<br>";
      i++;
      while (s[i] === " " || s[i] === "\t") i++;
      continue;
    }

    // Backslash escape of ASCII punctuation.
    if (c === "\\" && i + 1 < s.length && PUNCT.test(s[i + 1])) {
      text += s[i + 1];
      i += 2;
      continue;
    }

    // Placeholders and withheld values: atoms, as written.
    if (c === "[" || c === "{") {
      const atom = at(PLACEHOLDER_Y, s, i) || at(WITHHELD_Y, s, i);
      if (atom) {
        text += atom[0];
        i += atom[0].length;
        continue;
      }
    }

    // Code span: a run of n backticks closed by exactly n.
    if (c === "`") {
      const run = at(TICKS_Y, s, i)[0];
      const last = skipCode(s, i);
      if (last < 0) {
        text += run;
        i += run.length;
        continue;
      }
      let code = s.slice(i + run.length, last + 1 - run.length).replace(/\n/g, " ");
      if (code.length > 2 && code[0] === " " && code[code.length - 1] === " " && code.trim()) code = code.slice(1, -1);
      flush();
      out += `<code>${escapeHtml(code)}</code>`;
      i = last + 1;
      continue;
    }

    // Image: never made. The whole `![alt](url)` stays text.
    if (c === "!" && s[i + 1] === "[") {
      const close = matchBracket(s, i + 1);
      const dest = close > 0 ? linkDest(s, close + 1) : null;
      if (dest) {
        text += s.slice(i, dest.end);
        i = dest.end;
        continue;
      }
    }

    // Link: [text](url). It stays text when the URL is not one Keel opens.
    if (c === "[" && !inLink) {
      const close = matchBracket(s, i);
      const dest = close > 0 ? linkDest(s, close + 1) : null;
      if (dest) {
        const href = safeLinkURL(dest.url);
        const label = s.slice(i + 1, close);
        if (href && label.trim() && !HAS_PLACEHOLDER.test(label)) {
          flush();
          out += linkHTML(href, inline(label, depth + 1, true));
        } else {
          text += s.slice(i, dest.end);
        }
        i = dest.end;
        continue;
      }
    }

    // Autolink: <https://…> or <mailto:…>. Any other <…> is text.
    if (c === "<" && !inLink) {
      const m = at(AUTOLINK_Y, s, i);
      const href = m && safeLinkURL(m[1]);
      if (href) {
        flush();
        out += linkHTML(href, escapeHtml(m[1]));
        i += m[0].length;
        continue;
      }
    }

    // Bare URL at a word start.
    if (!inLink && (c === "h" || c === "H" || c === "m" || c === "M") && !isWord(s[i - 1])) {
      const url = bareURL(s, i);
      const href = url && safeLinkURL(url);
      if (href) {
        flush();
        out += linkHTML(href, escapeHtml(url));
        i += url.length;
        continue;
      }
    }

    // Emphasis: ** __ ~~ (strong, strong, del), then * _ (em). An `_`
    // inside a word (snake_case) is text.
    const d2 = s.slice(i, i + 2);
    const d = d2 === "**" || d2 === "__" || d2 === "~~" ? d2 : c === "*" || c === "_" ? c : "";
    if (d) {
      if (!isSpace(s[i + d.length]) && (d[0] !== "_" || !isWord(s[i - 1]))) {
        const end = findClose(s, i + d.length, d, d[0] !== "_", memo);
        if (end > 0) {
          const tag = d === "~~" ? "del" : d.length === 2 ? "strong" : "em";
          flush();
          out += `<${tag}>${inline(s.slice(i + d.length, end), depth + 1, inLink)}</${tag}>`;
          i = end + d.length;
          continue;
        }
      }
      // No closer: the run is text (so `**` is not tried again as `*`).
      text += d;
      i += d.length;
      continue;
    }

    text += c;
    i++;
  }
  flush();
  return out;
}

// --- blocks ---

const FENCE = /^ {0,3}(`{3,}|~{3,})(.*)$/;
const FENCE_CLOSE = /^ {0,3}(`{3,}|~{3,})[ \t]*$/;
const HEADING = /^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$/;
const RULE = /^ {0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/;
const QUOTE = /^ {0,3}> ?(.*)$/;
const ITEM = /^( *)([-*+]|\d{1,9}[.)])(?:[ \t]+(.*))?$/;
const TABLE_DELIM = /^ {0,3}\|?[ \t]*:?-+:?[ \t]*(?:\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$/;
const blank = (l) => /^[ \t]*$/.test(l);
const indentOf = (l) => l.length - l.trimStart().length;

function isFence(line) {
  const m = line.match(FENCE);
  return !!m && !(m[1][0] === "`" && m[2].includes("`"));
}

function isTable(line, next) {
  return line.includes("|") && next !== undefined && next.includes("-") && TABLE_DELIM.test(next);
}

// interrupts: may this line end a paragraph without a blank line before
// it? As in CommonMark, an ordered list does so only when it starts at 1,
// and a list item only when it has text.
function interrupts(line, next) {
  if (isFence(line) || HEADING.test(line) || RULE.test(line) || QUOTE.test(line) || isTable(line, next)) return true;
  const m = line.match(ITEM);
  return !!m && indentOf(line) < 4 && !!m[3]?.trim() && (!/\d/.test(m[2]) || parseInt(m[2], 10) === 1);
}

function cells(line) {
  let l = line.trim();
  if (l.startsWith("|")) l = l.slice(1);
  if (l.endsWith("|") && !l.endsWith("\\|")) l = l.slice(0, -1);
  const out = [];
  let cur = "";
  for (let i = 0; i < l.length; i++) {
    if (l[i] === "\\" && l[i + 1] === "|") {
      cur += "\\|";
      i++;
    } else if (l[i] === "|") {
      out.push(cur.trim());
      cur = "";
    } else if (l[i] === "`") {
      // A pipe inside a code span does not split the cell.
      const end = skipCode(l, i);
      if (end > 0) {
        cur += l.slice(i, end + 1);
        i = end;
      } else cur += l[i];
    } else cur += l[i];
  }
  out.push(cur.trim());
  return out;
}

function tableHTML(head, delim, rows) {
  const aligns = cells(delim).map((c) => (c.startsWith(":") && c.endsWith(":") ? "center" : c.endsWith(":") ? "right" : ""));
  const n = cells(head).length;
  const row = (tag, line) => {
    const cs = cells(line);
    let r = "";
    for (let k = 0; k < n; k++) {
      const a = aligns[k] ? ` class="k-md-${aligns[k]}"` : "";
      r += `<${tag}${a}>${inline(cs[k] ?? "")}</${tag}>`;
    }
    return `<tr>${r}</tr>`;
  };
  return `<div class="k-md-table"><table><thead>${row("th", head)}</thead>${
    rows.length ? `<tbody>${rows.map((l) => row("td", l)).join("")}</tbody>` : ""
  }</table></div>`;
}

function codeHTML(lang, code, opts) {
  const l = /^[A-Za-z0-9_+.#-]{1,24}$/.test(lang) ? lang : "";
  return `<div class="k-md-code"><div class="k-md-code-bar"><span class="k-md-code-lang">${escapeHtml(l)}</span><button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-md-copy aria-label="Copy code" title="Copy">${opts.copyIcon || ""}</button></div><pre><code>${escapeHtml(code)}</code></pre></div>`;
}

// listItems reads the items of one list starting at lines[i]; returns the
// items (each its lines, de-indented) and the index after the list.
function listItems(lines, i) {
  const first = lines[i].match(ITEM);
  const ordered = /\d/.test(first[2]);
  const marker = first[2].slice(-1);
  const base = first[1].length;
  const items = [];
  while (i < lines.length) {
    const im = lines[i].match(ITEM);
    if (!im || im[1].length !== base || /\d/.test(im[2]) !== ordered || im[2].slice(-1) !== marker) break;
    const gap = lines[i].length - im[1].length - im[2].length - (im[3] ?? "").length;
    const contentIndent = base + im[2].length + (gap >= 1 && gap <= 4 ? gap : 1);
    const body = [im[3] ?? ""];
    i++;
    let sawBlank = false;
    while (i < lines.length) {
      const l = lines[i];
      if (blank(l)) {
        sawBlank = true;
        body.push("");
        i++;
        continue;
      }
      const ind = indentOf(l);
      if (ind >= contentIndent || (ind > base && ITEM.test(l))) {
        body.push(l.slice(Math.min(ind, contentIndent)));
        sawBlank = false;
        i++;
        continue;
      }
      // A sibling item, or anything after a blank line, ends this item.
      if (sawBlank || ITEM.test(l) || interrupts(l, lines[i + 1])) break;
      body.push(l); // lazy continuation of the item's paragraph
      i++;
    }
    while (body.length && blank(body[body.length - 1])) body.pop();
    items.push(body);
  }
  return { ordered, start: ordered ? parseInt(first[2], 10) : 1, items, next: i };
}

function blocks(lines, opts, depth) {
  let out = "";
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (blank(line)) {
      i++;
      continue;
    }

    // Fenced code. An unclosed fence runs to the end of the text.
    if (isFence(line)) {
      const m = line.match(FENCE);
      const fence = m[1];
      const indent = indentOf(line);
      const lang = m[2].trim().split(/\s+/)[0] || "";
      const body = [];
      i++;
      while (i < lines.length) {
        const c = lines[i].match(FENCE_CLOSE);
        if (c && c[1][0] === fence[0] && c[1].length >= fence.length) {
          i++;
          break;
        }
        const l = lines[i];
        body.push(l.slice(Math.min(indent, indentOf(l))));
        i++;
      }
      out += codeHTML(lang, body.join("\n"), opts);
      continue;
    }

    let m = line.match(HEADING);
    if (m) {
      const level = m[1].length;
      out += `<h${level} class="k-md-h">${inline((m[2] || "").trim())}</h${level}>`;
      i++;
      continue;
    }

    if (RULE.test(line)) {
      out += "<hr>";
      i++;
      continue;
    }

    if (QUOTE.test(line)) {
      const body = [];
      while (i < lines.length && !blank(lines[i])) {
        const q = lines[i].match(QUOTE);
        if (q) body.push(q[1]);
        else if (!interrupts(lines[i], lines[i + 1])) body.push(lines[i]); // lazy continuation
        else break;
        i++;
      }
      out += depth < MAX_DEPTH ? `<blockquote>${blocks(body, opts, depth + 1)}</blockquote>` : `<p>${inline(body.join("\n"))}</p>`;
      continue;
    }

    if (ITEM.test(line) && indentOf(line) < 4 && depth < MAX_DEPTH) {
      const list = listItems(lines, i);
      i = list.next;
      const lis = list.items
        .map((body) => {
          const inner = blocks(body, opts, depth + 1);
          // A tight item's one paragraph is shown without its <p>.
          const tight = inner.match(/^<p>([\s\S]*?)<\/p>((?:<(?:ul|ol)[\s>][\s\S]*)?)$/);
          return `<li>${tight && !tight[1].includes("</p>") ? tight[1] + tight[2] : inner}</li>`;
        })
        .join("");
      out += list.ordered ? `<ol${list.start !== 1 ? ` start="${list.start}"` : ""}>${lis}</ol>` : `<ul>${lis}</ul>`;
      continue;
    }

    // GFM table: a header row with pipes, then a delimiter row.
    if (isTable(line, lines[i + 1])) {
      const head = line;
      const delim = lines[i + 1];
      const rows = [];
      i += 2;
      while (i < lines.length && !blank(lines[i]) && lines[i].includes("|")) rows.push(lines[i++]);
      out += tableHTML(head, delim, rows);
      continue;
    }

    // Paragraph: until a blank line or a block that may interrupt it.
    const para = [line];
    i++;
    while (i < lines.length && !blank(lines[i]) && !interrupts(lines[i], lines[i + 1])) para.push(lines[i++]);
    out += `<p>${inline(para.join("\n").trim())}</p>`;
  }
  return out;
}

// markdownHTML renders text. opts.copyIcon is the SVG markup for the code
// block's Copy button (a constant from the caller, never model text).
export function markdownHTML(text, opts = {}) {
  const src = String(text ?? "")
    .replace(/\r\n?/g, "\n")
    .replace(/\u0000/g, "\ufffd");
  return blocks(src.split("\n"), opts, 0);
}
