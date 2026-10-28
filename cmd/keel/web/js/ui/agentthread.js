// The agent thread: the turns, the trace and the proposal cards of one
// conversation (docs/ui-redesign.md §5.4, §5.4.1, §5.4.2). The agent panel
// and the Agent tab page each show the window's one conversation in a
// thread view made here; ui/agentpanel.js owns the conversation and calls
// every view, so both stay the same.
//
// A view never writes to a pane. Type into pane asks the conversation
// (ctl.type), which hands the proposal to term.js: the approve-before-type
// dialog, then the session socket, where keel serve checks it with the
// agent service before writing (§6.1, §6.5, §6.6, §6.7). Every server
// string is escaped here (escapeHtml, or A.bytesHTML / A.answerHTML /
// markdownHTML, which escape first).

import { escapeHtml } from "../lib/html.js";
import { markdownHTML } from "../lib/markdown.js";
import { openExternal } from "../lib/openlink.js";
import * as A from "../lib/agent.js";
import * as N from "../lib/notes.js";
import * as R from "../lib/runbooks.js";
import * as NB from "../lib/notebooks.js";
import * as TD from "../lib/typedirect.js";

const svg = (paths) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

export const ICON = {
  ai: svg(
    '<path d="M13.427 8.08396C12.8007 5.84272 12.3226 4 11.0004 4C9.67824 4 9.19964 5.84274 8.5731 8.08399C8.31853 8.99465 8.19125 9.44998 7.82042 9.82077C7.4496 10.1916 6.99434 10.3188 6.08381 10.5732C3.84273 11.1995 2 11.6775 2 13C2 14.3225 3.84273 14.8005 6.08381 15.4268C6.99434 15.6812 7.4496 15.8084 7.82042 16.1792C8.19125 16.55 8.31853 17.0054 8.5731 17.916C9.19964 20.1573 9.67824 22 11.0004 22C12.3226 22 12.8007 20.1573 13.427 17.916C13.6815 17.0054 13.8087 16.5501 14.1795 16.1793C14.5504 15.8085 15.0056 15.6812 15.9162 15.4268C18.1573 14.8005 20 14.3225 20 13C20 11.6775 18.1573 11.1995 15.9162 10.5732C15.0056 10.3188 14.5504 10.1915 14.1795 9.8207C13.8087 9.44986 13.6815 8.99456 13.427 8.08396Z"/><path d="M19.5 2.9375V4.5M19.5 4.5V6.0625M19.5 4.5H18.25M19.5 4.5H20.75"/>',
  ),
  tick: svg('<path d="M5 14L8.5 17.5L19 6.5"/>'),
  skip: svg('<path d="M16 12H8"/><circle cx="12" cy="12" r="10"/>'),
  busy: svg(
    '<path d="M12 3V6"/><path d="M12 18V21"/><path d="M21 12L18 12"/><path d="M6 12L3 12"/><path d="M18.3635 5.63672L16.2422 7.75804"/><path d="M7.75804 16.2422L5.63672 18.3635"/><path d="M18.3635 18.3635L16.2422 16.2422"/><path d="M7.75804 7.75804L5.63672 5.63672"/>',
  ),
  x: svg('<path d="M18 6L6.00081 17.9992M17.9992 18L6 6.00085"/>'),
  at: svg(
    '<path d="M15.6 8.4V12.9C15.6 14.4 16.8 15.6 18.3 15.6C19.8 15.6 21 14.4 21 12.9V12C21 7 17 3 12 3C7 3 3 7 3 12C3 17 7 21 12 21C14 21 15.9 20.3 17.4 19.2M15.6 12C15.6 14 14 15.6 12 15.6C10 15.6 8.4 14 8.4 12C8.4 10 10 8.4 12 8.4C14 8.4 15.6 10 15.6 12Z"/>',
  ),
  cmd: svg(
    '<path d="M14 4H10C6.22876 4 4.34315 4 3.17157 5.17157C2 6.34315 2 8.22876 2 12C2 15.7712 2 17.6569 3.17157 18.8284C4.34315 20 6.22876 20 10 20H14C17.7712 20 19.6569 20 20.8284 18.8284C22 17.6569 22 15.7712 22 12C22 8.22876 22 6.34315 20.8284 5.17157C19.6569 4 17.7712 4 14 4Z"/><path d="M7 9L8.83981 10.5858C9.61327 11.2525 10 11.5858 10 12C10 12.4142 9.61327 12.7475 8.83981 13.4142L7 15"/><path d="M13 16H17"/>',
  ),
  edit: svg(
    '<path d="M16.4249 4.60509L17.4149 3.6151C18.2351 2.79497 19.5648 2.79497 20.3849 3.6151C21.205 4.43524 21.205 5.76493 20.3849 6.58507L19.3949 7.57506M16.4249 4.60509L9.76558 11.2644C9.25807 11.772 8.89804 12.4078 8.72397 13.1041L8 16L10.8959 15.276C11.5922 15.102 12.228 14.7419 12.7356 14.2344L19.3949 7.57506M16.4249 4.60509L19.3949 7.57506"/><path d="M18.9999 13.5C18.9999 16.7875 18.9999 18.4312 18.092 19.5376C17.9258 19.7401 17.7401 19.9258 17.5375 20.092C16.4312 21 14.7874 21 11.4999 21H11C7.22876 21 5.34316 21 4.17159 19.8284C3.00003 18.6569 3 16.7712 3 13V12.5C3 9.21252 3 7.56879 3.90794 6.46244C4.07417 6.2599 4.2599 6.07417 4.46244 5.90794C5.56879 5 7.21252 5 10.5 5"/>',
  ),
  copy: svg(
    '<path d="M7.5 14.5C7.5 11.2002 7.5 9.55025 8.52513 8.52513C9.55025 7.5 11.2002 7.5 14.5 7.5C17.7998 7.5 19.4497 7.5 20.4749 8.52513C21.5 9.55025 21.5 11.2002 21.5 14.5C21.5 17.7998 21.5 19.4497 20.4749 20.4749C19.4497 21.5 17.7998 21.5 14.5 21.5C11.2002 21.5 9.55025 21.5 8.52513 20.4749C7.5 19.4497 7.5 17.7998 7.5 14.5Z"/><path d="M7.5 16.5C6.10355 16.5 5.40533 16.5 4.84402 16.3036C3.83866 15.9518 3.0482 15.1613 2.69641 14.156C2.5 13.5947 2.5 12.8964 2.5 11.5V9.5C2.5 6.20017 2.5 4.55025 3.52513 3.52513C4.55025 2.5 6.20017 2.5 9.5 2.5H11.5C12.8964 2.5 13.5947 2.5 14.156 2.69641C15.1613 3.0482 15.9518 3.83866 16.3036 4.84402C16.5 5.40533 16.5 6.10355 16.5 7.5"/>',
  ),
  alert: svg(
    '<path d="M13.9248 21H10.0752C5.44476 21 3.12955 21 2.27636 19.4939C1.42317 17.9879 2.60736 15.9914 4.97574 11.9985L6.90057 8.75333C9.17559 4.91778 10.3131 3 12 3C13.6869 3 14.8244 4.91777 17.0994 8.75332L19.0243 11.9985C21.3926 15.9914 22.5768 17.9879 21.7236 19.4939C20.8704 21 18.5552 21 13.9248 21Z"/><path d="M12 9V13"/><path d="M12 16.75H12.01"/>',
  ),
  info: svg('<circle cx="12" cy="12" r="10"/><path d="M12 16V12"/><path d="M12 8.25H12.01"/>'),
  trash: svg(
    '<path d="M19.5 5.5L18.8803 15.5251C18.7219 18.0864 18.6428 19.3671 18.0008 20.2879C17.6833 20.7431 17.2747 21.1273 16.8007 21.416C15.8421 22 14.559 22 11.9927 22C9.42312 22 8.1383 22 7.17905 21.4149C6.7048 21.1257 6.296 20.7408 5.97868 20.2848C5.33688 19.3626 5.25945 18.0801 5.10461 15.5152L4.5 5.5"/><path d="M3 5.5H21M16.0557 5.5L15.3731 4.09173C14.9196 3.15626 14.6928 2.68852 14.3017 2.39681C14.215 2.3321 14.1231 2.27454 14.027 2.2247C13.5939 2 13.0741 2 12.0345 2C10.9688 2 10.436 2 9.99568 2.23412C9.8981 2.28601 9.80498 2.3459 9.71729 2.41317C9.32164 2.7167 9.10063 3.20155 8.65861 4.17126L8.05292 5.5"/><path d="M9.5 16.5L9.5 10.5"/><path d="M14.5 16.5L14.5 10.5"/>',
  ),
  redo: svg(
    '<path d="M21 8H9C5.68629 8 3 10.6863 3 14C3 17.3137 5.68629 20 9 20H13"/><path d="M17 4L18.1538 4.87652C20.0513 6.31801 21 7.03875 21 8C21 8.96125 20.0513 9.68199 18.1538 11.1235L17 12"/>',
  ),
  history: svg(
    '<path d="M3.49902 14.9656C4.72475 18.4791 8.06749 21 11.999 21C16.9696 21 20.999 16.9706 20.999 12C20.999 7.02944 16.9696 3 11.999 3C8.29827 3 4.8984 5.6756 3.68943 8.5"/><path d="M11.999 7V12L14.999 14"/><path d="M7.49751 8.74363C7.49751 8.74363 3.81388 9.3026 3.25487 8.7436C2.69585 8.1846 3.25488 4.50098 3.25488 4.50098"/>',
  ),
  shield: svg(
    '<path d="M18.7088 3.49534C16.8165 2.55382 14.5009 2 12 2C9.4991 2 7.1835 2.55382 5.29116 3.49534C4.36318 3.95706 3.89919 4.18792 3.4496 4.91378C3 5.63965 3 6.34248 3 7.74814V11.2371C3 16.9205 7.54236 20.0804 10.173 21.4338C10.9067 21.8113 11.2735 22 12 22C12.7265 22 13.0933 21.8113 13.8269 21.4338C16.4576 20.0804 21 16.9205 21 11.2371L21 7.74814C21 6.34249 21 5.63966 20.5504 4.91378C20.1008 4.18791 19.6368 3.95706 18.7088 3.49534Z"/>',
  ),
  note: svg(
    '<path d="M15.5 21H9.5C6.2 21 4.6 21 3.5 20C2.5 18.9 2.5 17.3 2.5 14V10C2.5 6.7 2.5 5.1 3.5 4C4.6 3 6.2 3 9.5 3H14.5C17.8 3 19.4 3 20.5 4C21.5 5.1 21.5 6.7 21.5 10V15L15.5 21Z"/><path d="M15 21V18.5C15 17.1 15 16.4 15.4 16C15.9 15.5 16.6 15.5 18 15.5H21.5"/><path d="M7 8H17M7 12H12"/>',
  ),
  book: svg(
    '<path d="M20.5 16.9V11C20.5 7.2 20.5 5.3 19.3 4.2C18.2 3 16.3 3 12.5 3H11.5C7.7 3 5.8 3 4.7 4.2C3.5 5.3 3.5 7.2 3.5 11V19.5"/><path d="M20.5 17H6C4.6 17 3.5 18.1 3.5 19.5C3.5 20.9 4.6 22 6 22H20.5"/><path d="M20.5 22C19.1 22 18 20.9 18 19.5C18 18.1 19.1 17 20.5 17"/><path d="M9 8H15M9 12H13"/>',
  ),
  notebook: svg(
    '<path d="M9.99219 11H13.4922M9.99219 7H16.9922"/><path d="M5.99219 6H2.99219M5.99219 12H2.99219M5.99219 18H2.99219"/><path d="M8.99219 21.9388C7.41097 21.8363 6.39738 21.562 5.66376 20.8284C4.49219 19.6569 4.49219 17.7712 4.49219 14V10C4.49219 6.22877 4.49219 4.34315 5.66376 3.17158C6.83533 2 8.72096 2 12.4922 2H12.9922C16.7634 2 18.649 2 19.8206 3.17157C20.9922 4.34315 20.9922 6.22877 20.9922 10V10.5"/>',
  ),
  lock: svg('<path d="M4.5 13.5C4.5 11.6 4.5 10.7 5.1 10.1C5.7 9.5 6.6 9.5 8.5 9.5H15.5C17.4 9.5 18.3 9.5 18.9 10.1C19.5 10.7 19.5 11.6 19.5 13.5V16C19.5 18.4 19.5 19.6 18.7 20.3C18 21 16.8 21 14.5 21H9.5C7.2 21 6 21 5.3 20.3C4.5 19.6 4.5 18.4 4.5 16V13.5Z"/><path d="M8 9.5V7C8 4.8 9.8 3 12 3C14.2 3 16 4.8 16 7V9.5"/>'),
  eye: svg(
    '<path d="M21.544 11.045C21.848 11.4713 22 11.6845 22 12C22 12.3155 21.848 12.5287 21.544 12.955C20.1779 14.8706 16.6892 19 12 19C7.31078 19 3.8221 14.8706 2.45604 12.955C2.15201 12.5287 2 12.3155 2 12C2 11.6845 2.15201 11.4713 2.45604 11.045C3.8221 9.12944 7.31078 5 12 5C16.6892 5 20.1779 9.12944 21.544 11.045Z"/><path d="M15 12C15 10.3431 13.6569 9 12 9C10.3431 9 9 10.3431 9 12C9 13.6569 10.3431 15 12 15C13.6569 15 15 13.6569 15 12Z"/>',
  ),
};

export function stepHTML(step) {
  if (step.status === "notes") return `<div class="k-agent-step k-agent-step--note">${ICON.note}<span>${escapeHtml(A.traceText(step))}</span></div>`;
  if (step.status === "runbook") return `<div class="k-agent-step k-agent-step--note">${ICON.book}<span>${escapeHtml(A.traceText(step))}</span></div>`;
  if (step.status === "notebook" || step.status === "notebook-index") return `<div class="k-agent-step k-agent-step--note" data-trace-notebook>${ICON.notebook}<span>${escapeHtml(A.traceText(step))}</span></div>`;
  const cls = step.status === "read" ? "k-agent-step--read" : "k-agent-step--skip";
  const icon = step.status === "read" ? ICON.tick : ICON.skip;
  return `<div class="k-agent-step ${cls}">${icon}<span>${escapeHtml(A.traceText(step))}</span></div>`;
}

export function errorHTML(err) {
  if (err?.code === 412 || err?.data?.reason === "no-key") {
    return `<div class="k-agent-error" role="alert"><b>No model is connected.</b><span>Add a key to ask the agent.</span></div>`;
  }
  if (err?.code === 423) return `<div class="k-agent-error" role="alert"><b>The vault is locked.</b><span>Unlock it, then ask again.</span></div>`;
  const msg = err?.message || String(err);
  return `<div class="k-agent-error" role="alert"><b>The agent could not answer.</b><span>${escapeHtml(msg)}</span></div>`;
}

// answerBlockHTML is the agent's answer as markdown (lib/markdown.js:
// model text, escaped first; no raw HTML, no images, links only to http,
// https and mailto, opened through openExternal). A code block has Copy and
// never Type: only a proposal card types, through approve-before-type
// (§6.1).
export function answerBlockHTML(answer) {
  return `<div class="k-agent-msg k-md">${markdownHTML(answer, { copyIcon: ICON.copy })}</div>`;
}

// cutHTML is the notice under an answer the provider cut (res.cut), "" for
// a finished one.
export function cutHTML(cut) {
  const msg = A.cutNotice(cut);
  return msg ? `<div class="k-note-warn" role="note" data-agent-cut>${ICON.alert}<span>${escapeHtml(msg)}</span></div>` : "";
}

// savedTurnHTML is one turn of a resumed conversation: the question with
// its time, then the trace and the answer. A Propose again turn has no
// question; its proposal is added on its own.
export function savedTurnHTML(t) {
  if (t.again) return "";
  const who = t.at ? ` · ${escapeHtml(A.stampLabel(t.at))}` : "";
  const trace = (t.trace || []).map(stepHTML).join("");
  return `<div class="k-agent-turn"><div class="k-agent-who">You${who}</div><p class="k-agent-msg">${escapeHtml(t.question || "")}</p></div>
      <div class="k-agent-turn" data-agent-turn><div class="k-agent-who">${ICON.ai}Agent</div>${trace ? `<div class="k-agent-trace">${trace}</div>` : ""}${
        t.answer ? answerBlockHTML(t.answer) : ""
      }${cutHTML(t.cut)}</div>`;
}

// sessionRowHTML is one saved conversation in a list (the panel's Recent
// conversations, the page's left column). opts.renaming shows the inline
// title field; opts.actions adds Rename and Delete.
export function sessionRowHTML(s, { current = false, renaming = false, actions = true, newTitle = "New conversation" } = {}) {
  const title = s.title || newTitle;
  const hosts = (s.hosts || [])
    .slice(0, 3)
    .map((h) => `<span class="k-badge k-badge--mono">${escapeHtml(h)}</span>`)
    .join("");
  if (renaming) {
    return `<form class="k-agent-sess-row k-agent-sess-row--edit" data-sess-rename-form="${escapeHtml(s.id)}" aria-current="${current}">
        <input autocapitalize="off" autocorrect="off" class="k-input" data-sess-title value="${escapeHtml(s.title || "")}" maxlength="120" aria-label="Conversation title" spellcheck="false" autocomplete="off" />
        <button type="submit" class="k-btn k-btn--primary k-btn--sm">Save</button>
      </form>`;
  }
  const tools = actions
    ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-sess-rename="${escapeHtml(s.id)}" aria-label="Rename ${escapeHtml(title)}" title="Rename">${ICON.edit}</button>
      <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-sess-delete="${escapeHtml(s.id)}" aria-label="Delete ${escapeHtml(title)}" title="Delete">${ICON.trash}</button>`
    : "";
  return `<div class="k-agent-sess-row${actions ? "" : " k-agent-sess-row--plain"}" aria-current="${current}" data-sess="${escapeHtml(s.id)}">
      <button type="button" class="k-agent-sess-pick" data-sess-pick="${escapeHtml(s.id)}">
        <span class="k-agent-sess-name">${escapeHtml(title)}</span>
        <span class="k-agent-sess-meta">${hosts}<span>${escapeHtml(A.sessionWhen(s.updatedAt))}</span><span>· ${escapeHtml(A.proposalsLabel(s.proposals))}</span></span>
      </button>${tools}
    </div>`;
}

// sessionGroupsHTML is the grouped list (Today / Yesterday / Older with
// counts). rowOpts(s) gives each row's options.
export function sessionGroupsHTML(groups, rowOpts) {
  return groups
    .map(
      (g) =>
        `<div class="k-agent-sess-grp" role="presentation"><span>${escapeHtml(g.label)}</span><span>${g.rows.length}</span></div>${g.rows
          .map((s) => sessionRowHTML(s, rowOpts(s)))
          .join("")}`,
    )
    .join("");
}

// createThreadView draws one conversation into el. opts:
//   doc, el                the thread element
//   ctl                    the conversation (agentpanel.js): proposal(id),
//                          paneById(id), type(id, shift), again(id),
//                          saveEdit(id, command), copy(id)
//   isMac
//   emptyHTML()            what an empty thread shows
//   typeLabel(p, pane)     the Type button's words
export function createThreadView(opts) {
  const doc = opts.doc || document;
  const el = opts.el;
  const ctl = opts.ctl;
  const shiftWord = opts.isMac ? "⇧" : "Shift";
  const typeLabel = opts.typeLabel || (() => "Type into pane");
  const recs = new Map(); // proposal id → { el, editing }
  const noteRecs = new Map(); // note proposal id → { el, editing }
  const runRecs = new Map(); // runbook use id → el (§16)
  const bookRecs = new Map(); // runbook proposal id → el (§16)
  const nbRecs = new Map(); // notebook card id → el (§18)
  let turns = 0;

  function scrollDown() {
    el.scrollTop = el.scrollHeight;
  }

  function appendHTML(html) {
    const t = doc.createElement("template");
    t.innerHTML = html.trim();
    const node = t.content.firstElementChild;
    el.appendChild(node);
    return node;
  }

  function clear() {
    el.innerHTML = "";
    recs.clear();
    noteRecs.clear();
    runRecs.clear();
    bookRecs.clear();
    nbRecs.clear();
    turns = 0;
  }

  function paintEmpty() {
    if (turns > 0) return;
    el.innerHTML = opts.emptyHTML ? opts.emptyHTML() : "";
  }

  // --- an ask in flight ---

  function askStart(question, panes) {
    if (turns === 0) el.innerHTML = "";
    turns++;
    appendHTML(`<div class="k-agent-turn"><div class="k-agent-who">You</div><p class="k-agent-msg">${escapeHtml(question)}</p></div>`);
    const turn = appendHTML(`<div class="k-agent-turn" data-agent-turn><div class="k-agent-who">${ICON.ai}Agent</div>
      <div class="k-agent-trace"><div class="k-agent-step k-agent-step--busy">${ICON.busy}<span>${
        panes ? `Reading ${panes} ${panes === 1 ? "pane" : "panes"} and asking` : "Asking without reading a pane"
      }</span></div></div></div>`);
    scrollDown();
    return turn;
  }

  function askDone(turn, res) {
    const trace = turn.querySelector(".k-agent-trace");
    trace.innerHTML =
      (res.trace || []).map(stepHTML).join("") || `<div class="k-agent-step k-agent-step--skip">${ICON.skip}<span>Read no pane</span></div>`;
    if (res.testProvider) {
      trace.insertAdjacentHTML("beforeend", `<div class="k-agent-step k-agent-step--skip">${ICON.info}<span>Test provider · nothing left this machine</span></div>`);
    }
    if (res.answer) turn.insertAdjacentHTML("beforeend", answerBlockHTML(res.answer));
    if (res.cut) turn.insertAdjacentHTML("beforeend", cutHTML(res.cut));
    if (res.runbook) placeRun(res.runbook.id, turn);
    for (const p of res.proposals || []) addProposal(p.id);
    for (const n of res.notes || []) addNote(n.id);
    for (const b of res.runbooks || []) addBookProp(b.id);
    for (const nb of res.notebooks || []) addNbProp(nb.id);
    scrollDown();
  }

  function askStopped(turn) {
    turn.querySelector(".k-agent-trace").innerHTML = `<div class="k-agent-step k-agent-step--skip">${ICON.skip}<span>Stopped</span></div>`;
    scrollDown();
  }

  function askFailed(turn, err) {
    turn.querySelector(".k-agent-trace").innerHTML = "";
    turn.insertAdjacentHTML("beforeend", errorHTML(err));
    scrollDown();
  }

  // --- a resumed conversation ---

  function renderTranscript(t) {
    clear();
    for (const turn of t.turns || []) {
      const html = savedTurnHTML(turn);
      if (html) {
        el.insertAdjacentHTML("beforeend", html);
        turns++;
      }
      for (const p of turn.proposals || []) addProposal(p.id);
      for (const n of turn.notes || []) addNote(n.id);
      for (const b of turn.runbooks || []) addBookProp(b.id);
      for (const nb of turn.notebooks || []) addNbProp(nb.id);
    }
    if (t.runbook) {
      const agentTurns = [...el.querySelectorAll("[data-agent-turn]")];
      placeRun(t.runbook.id, agentTurns[(t.runbook.turn || 1) - 1] || agentTurns[agentTurns.length - 1] || null);
    }
    const anyExpired = (t.turns || []).some((turn) => (turn.proposals || []).some((p) => p.expired));
    if (anyExpired || turns) {
      el.insertAdjacentHTML(
        "beforeend",
        `<div class="k-agent-divider" role="separator">Resumed · ${escapeHtml(A.agoLabel(t.updatedAt))}</div>
         <div class="k-agent-resumed">${ICON.shield}<span class="k-help">Earlier reads stay in this conversation as text. New reads use only the panes in scope now, on hosts that still allow the agent.</span></div>`,
      );
    }
    if (!turns) turns = 1; // keep the transcript when nothing new is asked
    scrollDown();
  }

  // --- proposals ---

  function addProposal(id) {
    if (recs.has(id)) return recs.get(id).el;
    const node = doc.createElement("div");
    node.className = "k-prop";
    node.setAttribute("role", "group");
    node.dataset.rowMenu = "card"; // the card menu: its own buttons (lib/cardmenu.js)
    node.setAttribute("aria-label", "Proposal");
    node.dataset.proposalId = id;
    el.appendChild(node);
    recs.set(id, { el: node, editing: false });
    paintCard(id);
    return node;
  }

  // A runbook step's card says which step it is (§16).
  function stepLabel(p) {
    return p.runbook?.step ? `Step ${p.runbook.step} · Run on` : "Run on";
  }

  function noteHTML(kind, icon, text) {
    return `<p class="k-prop-note k-prop-note--${kind}">${icon}<span>${text}</span></p>`;
  }

  // An expired card is a proposal saved from an earlier run: the panes may
  // have changed, so it cannot be typed. Copy, or Propose again (a fresh
  // proposal through the normal path).
  function paintExpired(node, p) {
    const badge = A.riskBadge(p.risk);
    node.classList.add("k-prop--expired");
    node.dataset.risk = p.risk;
    const when = p.proposedAt ? A.stampLabel(p.proposedAt) : "";
    const typed = p.typedAt ? `typed ${A.stampLabel(p.typedAt)}` : "not typed";
    node.innerHTML = `<div class="k-prop-head">${ICON.cmd}<span>${escapeHtml(stepLabel(p))}</span><span>${escapeHtml(p.host || "no pane")}</span>${
      p.production ? '<span class="k-badge k-badge--signal">Production</span>' : ""
    }<span class="k-badge ${badge.cls} k-prop-risk">${badge.label}</span><span class="k-badge">Expired</span></div>
      <div class="k-prop-body"><pre class="k-bytes" aria-label="Exact bytes">${A.bytesHTML(p.bytes)}</pre>${p.why ? `<p class="k-prop-why">${escapeHtml(p.why)}</p>` : ""}${noteHTML(
        "muted",
        ICON.history,
        "Panes may have changed since this was proposed.",
      )}</div>
      <div class="k-prop-foot"><span class="k-prop-count">${escapeHtml([when ? `Proposed ${when}` : "", typed].filter(Boolean).join(" · "))}</span>
        <span class="k-spacer"></span>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-prop-copy>${ICON.copy}Copy</button>
        <button type="button" class="k-btn k-btn--sm" data-prop-again>${ICON.redo}Propose again</button></div>`;
  }

  function paintCard(id) {
    const rec = recs.get(id);
    const cur = ctl.proposal(id);
    if (!rec || !cur) return;
    const { p, status } = cur;
    if (p.expired) {
      paintExpired(rec.el, p);
      return;
    }
    rec.el.classList.remove("k-prop--expired");
    const pane = ctl.paneById(p.sessionId);
    const offer = A.typeOffer(p, pane);
    const badge = A.riskBadge(p.risk);
    rec.el.dataset.risk = p.risk;
    const notes = [];
    if (offer === "shift" && !status) notes.push(noteHTML("flare", ICON.alert, `${escapeHtml(A.shiftReason(p))}. Hold ${shiftWord} when you press Type.`));
    if (offer === "following") notes.push(noteHTML("haze", ICON.eye, "You follow this pane. The agent cannot type into it."));
    if (offer === "closed") notes.push(noteHTML("haze", ICON.info, "That pane is not open in this window. Copy the command instead."));
    if (offer === "none") notes.push(noteHTML("haze", ICON.info, "No pane named for this command. Copy it instead."));
    if (p.placeholders) notes.push(noteHTML("signal", ICON.alert, "It holds a redacted placeholder such as [secret:1], which a shell would read as a pattern. Keel will not type it: edit it to the real value or an environment variable first."));
    if (status) notes.push(noteHTML(status.ok ? "tide" : "flare", status.ok ? ICON.tick : ICON.alert, escapeHtml(status.text)));
    const canType = offer === "type" || offer === "shift";
    const typeKbd = offer === "shift" ? `${shiftWord === "⇧" ? "⇧↵" : "Shift+↵"}` : "↵";
    // The bytes Type sends: the command alone, without ^U or Enter, when
    // it goes without the dialog (§6 rule 13); else the dialog's bytes.
    const shown = TD.proposalDirect(p) ? p.command : p.bytes;
    const body = rec.editing
      ? `<textarea autocapitalize="off" autocorrect="off" class="k-textarea k-textarea--mono" data-prop-edit aria-label="Edit command" spellcheck="false">${escapeHtml(p.command)}</textarea>`
      : `<pre class="k-bytes" aria-label="Exact bytes">${A.bytesHTML(shown)}</pre>`;
    const foot = rec.editing
      ? `<span class="k-spacer"></span>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-prop-cancel>Cancel</button>
         <button type="button" class="k-btn k-btn--primary k-btn--sm" data-prop-save>Save command</button>`
      : `<span class="k-prop-count">${escapeHtml(A.countLabel(shown))}</span>
         <span class="k-spacer"></span>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-prop-edit-btn aria-label="Edit command" title="Edit">${ICON.edit}</button>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-prop-copy aria-label="Copy command" title="Copy">${ICON.copy}</button>
         ${canType ? `<button type="button" class="k-btn k-btn--primary k-btn--sm" data-prop-type>${escapeHtml(typeLabel(p, pane))}<span class="k-kbd">${typeKbd}</span></button>` : ""}`;
    rec.el.innerHTML = `<div class="k-prop-head">${ICON.cmd}<span>${escapeHtml(stepLabel(p))}</span><span>${escapeHtml(p.host || pane?.label || "no pane")}</span>${
      p.production ? '<span class="k-badge k-badge--signal">Production</span>' : ""
    }<span class="k-badge ${badge.cls} k-prop-risk">${badge.label}</span></div>
      <div class="k-prop-body">${body}${p.why ? `<p class="k-prop-why">${escapeHtml(p.why)}</p>` : ""}${notes.join("")}</div>
      <div class="k-prop-foot">${foot}</div>`;
    if (rec.editing) rec.el.querySelector("[data-prop-edit]")?.focus({ preventScroll: true });
  }

  // repaint redraws every card that is not being edited (the panes, or a
  // card's state, changed).
  function repaint() {
    for (const [id, rec] of recs) if (!rec.editing) paintCard(id);
    for (const [id, rec] of noteRecs) if (!rec.editing) paintNote(id);
    paintRuns();
    for (const id of bookRecs.keys()) paintBookProp(id);
    for (const id of nbRecs.keys()) paintNbProp(id);
  }

  // --- a runbook in use (§16, board RunbookInUse) ---
  //
  // One card per use, at the top of the agent turn that started it: the
  // runbook, why it was picked, every step's progress, and what the user
  // can do now. Every step is still a proposal card of its own, typed
  // through approve-before-type; the card only asks (Propose step N),
  // marks (done, skip, retry) or stops.

  function placeRun(runId, turnEl) {
    if (!runId || runRecs.has(runId)) {
      paintRuns();
      return;
    }
    const node = doc.createElement("div");
    node.className = "k-rb-pick";
    node.setAttribute("role", "group");
    node.dataset.rowMenu = "card"; // the card menu: its own buttons (lib/cardmenu.js)
    node.dataset.runId = runId;
    const who = turnEl?.querySelector(".k-agent-who");
    if (who) who.insertAdjacentElement("afterend", node);
    else el.appendChild(node);
    runRecs.set(runId, node);
    paintRuns();
  }

  function paintRuns() {
    for (const [runId, node] of runRecs) paintRun(runId, node);
  }

  function runActionsHTML(act, busy, run) {
    const dis = busy ? " disabled" : "";
    const b = (attr, label, primary = false) => `<button type="button" class="k-btn ${primary ? "k-btn--primary" : "k-btn--ghost"} k-btn--sm" ${attr}${dis}>${escapeHtml(label)}</button>`;
    const sid = act.step ? ` data-rb-step="${escapeHtml(act.step.id)}"` : "";
    switch (act.kind) {
      case "propose":
        return `<p class="k-rb-now">${escapeHtml(`Next: step ${act.step.n} · ${act.step.title}`)}</p><div class="k-rb-actions">${b(`data-rb-propose${sid}`, `Propose step ${act.step.n}`, true)}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "proposed":
        return `<p class="k-rb-now">${escapeHtml(`Step ${act.step.n} is proposed below. Nothing is typed until you press Type.`)}</p><div class="k-rb-actions">${b(`data-rb-done${sid}`, "Mark done")}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "confirm":
        return `<p class="k-rb-now">${escapeHtml(`Step ${act.step.n} · exit 0, reported by the shell. ${run?.pinHost || "This host"} is a production host: confirm the step before going on.`)}</p><div class="k-rb-actions">${b(`data-rb-done${sid}`, "Confirm done", true)}${b(`data-rb-retry${sid}`, "Retry")}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "waiting":
        return `<p class="k-rb-now">${escapeHtml(`Step ${act.step.n} was typed. Its exit status shows here when the pane's shell reports it; otherwise mark it yourself.`)}</p><div class="k-rb-actions">${b(`data-rb-done${sid}`, "Mark done", true)}${b(`data-rb-retry${sid}`, "Retry")}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "noexit":
        return `<p class="k-rb-now">${escapeHtml(`Step ${act.step.n} ended with no exit status reported. Check the pane, then mark it.`)}</p><div class="k-rb-actions">${b(`data-rb-done${sid}`, "Mark done", true)}${b(`data-rb-retry${sid}`, "Retry")}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "failed":
        return `<p class="k-rb-now k-rb-now--flare">${escapeHtml(`Step ${act.step.n} failed${act.exit != null ? ` · exit ${act.exit}, reported by the shell` : ""}. The runbook waits for you.`)}</p><div class="k-rb-actions">${b(`data-rb-retry${sid}`, `Retry step ${act.step.n}`, true)}${b(`data-rb-done${sid}`, "Mark done")}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "check":
        return `<p class="k-rb-now">${escapeHtml(`Step ${act.step.n} has no command. Check it yourself, then mark it done.`)}</p><div class="k-rb-actions">${b(`data-rb-done${sid}`, "Mark done", true)}${b(`data-rb-skip${sid}`, "Skip")}</div>`;
      case "finished":
        return `<p class="k-rb-now">All steps are done or skipped.</p>`;
      default:
        return "";
    }
  }

  function paintRun(runId, node) {
    const cur = ctl.runbook?.() || {};
    const run = cur.run && cur.run.id === runId ? cur.run : null;
    const name = run?.name || cur.names?.get(runId) || "a runbook";
    node.setAttribute("aria-label", `Runbook ${name}`);
    if (!run || (run.status === "stopped" && !cur.noRunbook)) {
      node.classList.add("k-rb-pick--done");
      node.innerHTML = `<div class="k-rb-pick-name">${ICON.book}<span class="k-rb-pick-title">${escapeHtml(name)}</span><span class="k-badge">Stopped</span></div>`;
      return;
    }
    node.classList.remove("k-rb-pick--done");
    if (cur.noRunbook) {
      node.innerHTML = `<p class="k-rb-why">No runbook for this conversation. The agent answers from the panes in scope and your notes only.</p>
        <div class="k-rb-actions"><button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rb-use="${escapeHtml(run.runbookId)}"${cur.busy ? " disabled" : ""}>${escapeHtml(`Use ${name}`)}</button></div>`;
      return;
    }
    const rows = run.steps
      .map((_, i) => {
        const r = R.stepRow(run, i);
        return `<li class="k-rp-row" data-state="${r.state}"><span class="k-rp-mark" aria-hidden="true">${escapeHtml(r.mark)}</span><span class="k-rp-title">${escapeHtml(run.steps[i].title)}</span>${
          r.note ? `<span class="k-rp-note">${escapeHtml(r.note)}</span>` : ""
        }</li>`;
      })
      .join("");
    const act = R.nextAction(run, (pid) => !!ctl.proposal(pid) && !ctl.proposal(pid).p.expired);
    // Where the steps go: the host of the first step, pinned, with its tab.
    const pinPane = run.pinSessionId ? ctl.paneById(run.pinSessionId) : null;
    const target = run.pinHost
      ? `<p class="k-rb-target">${ICON.cmd}<span>Run on <b>${escapeHtml(run.pinHost)}</b>${pinPane?.sub ? ` · ${escapeHtml(pinPane.sub)}` : ""}</span></p>`
      : "";
    const blocked = run.blocked
      ? `<p class="k-rb-now k-rb-now--signal">${escapeHtml(run.blocked)}</p>${
          run.pinHost ? `<div class="k-rb-actions"><button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rb-move${cur.busy ? " disabled" : ""}>Move the run to another host</button></div>` : ""
        }`
      : "";
    const others = cur.choosing
      ? `<div class="k-rb-others" role="group" aria-label="Other runbooks">${
          cur.others == null
            ? `<p class="k-help">Loading…</p>`
            : cur.others.length
              ? cur.others
                  .map(
                    (o) =>
                      `<button type="button" class="k-rb-other" data-rb-use="${escapeHtml(o.id)}"${o.ok && !cur.busy ? "" : " disabled"}><span class="k-rb-other-name">${escapeHtml(o.name)}</span><span class="k-rb-other-sub">${escapeHtml(o.ok ? o.when || "" : o.reason)}</span></button>`,
                  )
                  .join("")
              : `<p class="k-help">No other runbooks. Add one on the Runbooks page.</p>`
        }</div>`
      : "";
    const err = cur.error ? `<p class="k-prop-note k-prop-note--flare">${ICON.alert}<span>${escapeHtml(cur.error)}</span></p>` : "";
    node.innerHTML = `<div class="k-rb-pick-name">${ICON.book}<span class="k-rb-pick-title">${escapeHtml(name)}</span><span class="k-badge">Runbook</span></div>
      ${target}<p class="k-rb-why">${escapeHtml(R.whyLine(run))}</p>
      <ol class="k-rp" aria-label="Steps">${rows}</ol>
      ${blocked}${runActionsHTML(act, cur.busy, run)}${err}
      <div class="k-rb-actions">
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rb-choose aria-expanded="${cur.choosing ? "true" : "false"}">Use a different runbook</button>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rb-stop${cur.busy ? " disabled" : ""}>Don't use</button>
      </div>${others}
      <p class="k-rb-safe">${ICON.shield}<span class="k-help">Runbook steps are proposals. Each one waits for you. The agent cannot skip ahead or type for you.</span></p>`;
  }

  // --- a runbook the agent proposes (§16) ---

  function addBookProp(id) {
    if (bookRecs.has(id)) return bookRecs.get(id);
    const node = doc.createElement("div");
    node.className = "k-prop k-prop--note k-prop--runbook";
    node.setAttribute("role", "group");
    node.dataset.rowMenu = "card"; // the card menu: its own buttons (lib/cardmenu.js)
    node.setAttribute("aria-label", "Proposed runbook");
    node.dataset.bookId = id;
    el.appendChild(node);
    bookRecs.set(id, node);
    paintBookProp(id);
    return node;
  }

  function paintBookProp(id) {
    const node = bookRecs.get(id);
    const cur = ctl.bookProp?.(id);
    if (!node || !cur) return;
    const { b, busy, error } = cur;
    const warn = R.proposalWarn(b);
    node.classList.toggle("k-prop--warn", warn && b.status === "open");
    if (b.status !== "open" || b.expired) {
      const text = b.status === "open" ? "Not saved. This proposal expired; add the runbook yourself on the Runbooks page." : R.proposalDone(b);
      const undo = !b.expired ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rbp-undo${busy ? " disabled" : ""}>Undo</button>` : "";
      const open = b.status === "saved" ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rbp-open>Open Runbooks</button>` : "";
      const icon = b.status === "saved" ? `<span class="k-note-done-ok">${ICON.tick}</span>` : ICON.info;
      node.classList.add("k-prop--done");
      node.innerHTML = `<div class="k-note-done" role="status">${icon}<span>${escapeHtml(text)}</span>${open}${undo}</div>`;
      return;
    }
    node.classList.remove("k-prop--done");
    const badges = `${b.fromPane ? '<span class="k-badge k-badge--flare">From pane output</span>' : warn ? '<span class="k-badge k-badge--flare">After reading panes</span>' : ""}<span class="k-badge">${escapeHtml(`${b.steps.length} ${b.steps.length === 1 ? "step" : "steps"}`)}</span>`;
    const steps = b.steps
      .map(
        (st, i) =>
          `<li class="k-rbp-step"><span class="k-rp-mark" aria-hidden="true">${i + 1}</span><span class="k-rbp-step-main"><span class="k-rp-title">${escapeHtml(st.title)}</span>${
            st.command ? `<pre class="k-bytes" aria-label="Command">${escapeHtml(st.command)}</pre>` : ""
          }</span></li>`,
      )
      .join("");
    const notes = [];
    if (warn) notes.push(`<div class="k-note-warn" role="note">${ICON.alert}<span>${escapeHtml(R.proposalWarning(b))}</span></div>`);
    if (error) notes.push(noteHTML("flare", ICON.alert, escapeHtml(error)));
    notes.push(noteHTML("muted", ICON.shield, "Nothing is saved until you press Save. A saved runbook starts with Agent may pick off; review it on the Runbooks page."));
    const save = warn
      ? `<button type="button" class="k-btn k-btn--sm" data-rbp-save${busy ? " disabled" : ""}>Save anyway</button>`
      : `<button type="button" class="k-btn k-btn--primary k-btn--sm" data-rbp-save${busy ? " disabled" : ""}>Save</button>`;
    node.innerHTML = `<div class="k-prop-head">${ICON.book}<span>Save as a runbook?</span><span class="k-prop-badges">${badges}</span></div>
      <div class="k-prop-body"><p class="k-rbp-name">${escapeHtml(b.name)}</p>${b.when ? `<p class="k-note-src">${escapeHtml(`Use when: ${b.when}`)}</p>` : ""}<ol class="k-rbp-steps">${steps}</ol>${notes.join("")}</div>
      <div class="k-prop-foot"><span class="k-spacer"></span>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-rbp-dismiss${busy ? " disabled" : ""}>Dismiss</button>${save}</div>`;
  }

  // --- what the agent did to a notebook (§18) ---
  //
  // keel serve already applied a create or an edit (the store kept the
  // version before it): the card says so, with Open and Undo. A delete the
  // agent proposes waits here: Delete asks first, Keep leaves it. Nothing
  // on the card types anything.

  function addNbProp(id) {
    if (nbRecs.has(id)) return nbRecs.get(id);
    const node = doc.createElement("div");
    node.className = "k-prop k-prop--note k-prop--notebook";
    node.setAttribute("role", "group");
    node.dataset.rowMenu = "card"; // the card menu: its own buttons (lib/cardmenu.js)
    node.dataset.nbpId = id;
    el.appendChild(node);
    nbRecs.set(id, node);
    paintNbProp(id);
    return node;
  }

  const NB_BUTTONS = {
    open: (p, busy) => `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-nbp-notebook="${escapeHtml(p.notebookId || "")}"${busy}>Open</button>`,
    undo: (p, busy) => `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-nbp-undo${busy}>Undo</button>`,
    restore: (p, busy) => `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-nbp-restore${busy}>Restore</button>`,
    keep: (p, busy) => `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-nbp-keep${busy}>Keep</button>`,
    delete: (p, busy) => `<button type="button" class="k-btn k-btn--danger k-btn--sm" data-nbp-delete${busy}>Delete…</button>`,
  };

  function paintNbProp(id) {
    const node = nbRecs.get(id);
    const cur = ctl.nbProp?.(id);
    if (!node || !cur) return;
    const { p, busy, error } = cur;
    const dis = busy ? " disabled" : "";
    const head = NB.cardHead(p);
    node.setAttribute("aria-label", head);
    const live = p.status === "created" || p.status === "edited" || p.status === "proposed";
    const warn = NB.warned(p) && live && !p.expired;
    node.classList.toggle("k-prop--warn", warn || (p.kind === "delete" && p.status === "proposed" && !p.expired));
    node.classList.toggle("k-prop--done", !live || !!p.expired);
    const badges = [];
    if (p.kind === "create" && p.status === "created") badges.push('<span class="k-badge k-badge--outline">From agent</span>');
    if (p.kind === "edit" && p.status === "edited") badges.push('<span class="k-badge k-badge--outline">Edited by agent</span>');
    if (warn) badges.push(p.fromPane ? '<span class="k-badge k-badge--flare">From pane output</span>' : '<span class="k-badge k-badge--flare">After reading panes</span>');
    if (live && p.placeholders) badges.push('<span class="k-badge k-badge--signal">Placeholders</span>');
    const notes = [];
    const status = NB.cardStatus(p);
    if (p.expired && live) notes.push(noteHTML("muted", ICON.info, escapeHtml("Keel no longer holds this conversation's changes. Use History on the notebook to see or restore a version.")));
    else if (status) notes.push(noteHTML(p.status === "refused" ? "signal" : "muted", p.status === "refused" ? ICON.alert : ICON.info, escapeHtml(status)));
    if (p.summary && p.kind !== "delete") notes.push(`<p class="k-nbp-summary">${escapeHtml(p.summary)}</p>`);
    if (p.summary && p.kind === "delete") notes.push(`<p class="k-nbp-summary">${escapeHtml(`Why: ${p.summary}`)}</p>`);
    if (warn) notes.push(`<div class="k-note-warn" role="note">${ICON.alert}<span>${escapeHtml(NB.paneWarning(p))}</span></div>`);
    if (live && p.withheld) notes.push(noteHTML("signal", ICON.info, escapeHtml("It names values Keel keeps from the agent as {{name}}. Replace them before you use a command.")));
    if (live && p.placeholders) notes.push(noteHTML("signal", ICON.info, escapeHtml("It holds placeholders such as [secret:1]. Fill them in before you use a command: Keel will not type a block that holds one.")));
    if (error) notes.push(noteHTML("flare", ICON.alert, escapeHtml(error)));
    const actions = NB.cardActions(p)
      .map((a) => NB_BUTTONS[a](p, dis))
      .join("");
    const size = p.kind === "delete" || !p.bytes ? "" : `<span class="k-prop-count">${escapeHtml(`${p.bytes} ${p.bytes === 1 ? "byte" : "bytes"}${p.rev ? ` · rev ${p.rev}` : ""}`)}</span>`;
    node.innerHTML = `<div class="k-prop-head">${p.kind === "delete" ? ICON.trash : ICON.notebook}<span>${escapeHtml(head)}</span><span class="k-prop-badges">${badges.join("")}</span></div>
      <div class="k-prop-body"><p class="k-rbp-name">${escapeHtml(p.title || "Notebook")}</p>${notes.join("")}</div>
      <div class="k-prop-foot">${size}<span class="k-spacer"></span>${actions}</div>`;
  }

  // --- notes the agent proposes (§15) ---
  //
  // Nothing is saved until the user presses Save (or Save anyway, on a note
  // whose text came from pane output). The card names whose notes it joins
  // and when they are sent.

  function addNote(id) {
    if (noteRecs.has(id)) return noteRecs.get(id).el;
    const node = doc.createElement("div");
    node.className = "k-prop k-prop--note";
    node.setAttribute("role", "group");
    node.dataset.rowMenu = "card"; // the card menu: its own buttons (lib/cardmenu.js)
    node.setAttribute("aria-label", "Proposed note");
    node.dataset.noteId = id;
    el.appendChild(node);
    noteRecs.set(id, { el: node, editing: false });
    paintNote(id);
    return node;
  }

  function paintNote(id) {
    const rec = noteRecs.get(id);
    const cur = ctl.note?.(id);
    if (!rec || !cur) return;
    const { n, busy, error } = cur;
    const warn = N.proposalWarn(n);
    rec.el.classList.toggle("k-prop--warn", warn && n.status === "open");
    if (n.status !== "open" || n.expired) {
      rec.editing = false;
      const text = n.status === "open" ? "Not saved. This proposal expired; add the note yourself in Edit host or Settings · Agent." : N.proposalDone(n);
      const undo = !n.expired ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-note-undo${busy ? " disabled" : ""}>Undo</button>` : "";
      const icon = n.status === "saved" ? `<span class="k-note-done-ok">${ICON.tick}</span>` : ICON.info;
      rec.el.classList.add("k-prop--done");
      rec.el.innerHTML = `<div class="k-note-done" role="status">${icon}<span>${escapeHtml(text)}</span>${undo}</div>`;
      return;
    }
    rec.el.classList.remove("k-prop--done");
    const badges = `${n.fromPane ? '<span class="k-badge k-badge--flare">From pane output</span>' : warn ? '<span class="k-badge k-badge--flare">After reading panes</span>' : ""}<span class="k-badge k-badge--mono">${escapeHtml(N.proposalTarget(n))}</span>`;
    const body = rec.editing
      ? `<textarea autocapitalize="off" autocorrect="off" class="k-textarea" data-note-edit aria-label="Edit note" spellcheck="true" maxlength="${N.MAX_NOTE_BYTES}">${escapeHtml(n.text)}</textarea>`
      : `<p class="k-note-text">${A.answerHTML(n.text)}</p>`;
    const notes = [];
    const origin = N.proposalOrigin(n);
    if (origin) notes.push(noteHTML("signal", ICON.info, escapeHtml(origin)));
    if (warn) notes.push(`<div class="k-note-warn" role="note">${ICON.alert}<span>${escapeHtml(N.proposalWarning(n))}</span></div>`);
    if (error) notes.push(noteHTML("flare", ICON.alert, escapeHtml(error)));
    notes.push(noteHTML("muted", ICON.shield, "Nothing is saved until you press Save. Notes live in the vault; edit them in Edit host or Settings · Agent."));
    const save = warn
      ? `<button type="button" class="k-btn k-btn--sm" data-note-save${busy ? " disabled" : ""}>Save anyway</button>`
      : `<button type="button" class="k-btn k-btn--primary k-btn--sm" data-note-save${busy ? " disabled" : ""}>Save</button>`;
    const foot = rec.editing
      ? `<span class="k-spacer"></span>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-note-cancel>Cancel</button>
         ${save}`
      : `<span class="k-prop-count">${escapeHtml(N.proposalScope(n))}</span>
         <span class="k-spacer"></span>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-note-dismiss${busy ? " disabled" : ""}>Dismiss</button>
         <button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-note-edit-btn aria-label="Edit note" title="Edit">${ICON.edit}</button>
         ${save}`;
    rec.el.innerHTML = `<div class="k-prop-head">${ICON.note}<span>Remember this?</span><span class="k-prop-badges">${badges}</span></div>
      <div class="k-prop-body">${body}<p class="k-note-src">${escapeHtml(N.proposalSource(n))}</p>${notes.join("")}</div>
      <div class="k-prop-foot">${foot}</div>`;
    if (rec.editing) rec.el.querySelector("[data-note-edit]")?.focus({ preventScroll: true });
  }

  async function saveNote(id) {
    const rec = noteRecs.get(id);
    const text = rec?.editing ? rec.el.querySelector("[data-note-edit]")?.value || "" : "";
    if (await ctl.saveNote(id, text)) {
      rec.editing = false;
      paintNote(id);
      rec.el.querySelector("[data-note-undo]")?.focus({ preventScroll: true });
    }
  }

  function setNoteEditing(id, on) {
    const rec = noteRecs.get(id);
    if (!rec) return;
    rec.editing = on;
    paintNote(id);
  }

  async function typeIt(id, shift) {
    const r = await ctl.type(id, shift);
    if (r?.refocus) recs.get(id)?.el.querySelector("[data-prop-type]")?.focus({ preventScroll: true });
  }

  async function proposeAgain(id) {
    const btn = recs.get(id)?.el.querySelector("[data-prop-again]");
    if (btn) btn.disabled = true;
    try {
      const fresh = await ctl.again(id);
      const node = fresh && recs.get(fresh)?.el;
      node?.scrollIntoView?.({ block: "nearest" });
      node?.querySelector("[data-prop-type], [data-prop-copy]")?.focus({ preventScroll: true });
    } finally {
      if (btn) btn.disabled = false;
    }
  }

  async function saveEdit(id) {
    const rec = recs.get(id);
    const command = rec?.el.querySelector("[data-prop-edit]")?.value || "";
    if (await ctl.saveEdit(id, command)) {
      rec.editing = false;
      paintCard(id);
    }
  }

  function setEditing(id, on) {
    const rec = recs.get(id);
    if (!rec) return;
    rec.editing = on;
    paintCard(id);
  }

  el.addEventListener("click", (ev) => {
    // Answer markdown: a link opens in the system browser, never in the
    // page; Copy copies a code block's text. Neither touches a pane.
    const link = ev.target.closest(".k-md a[data-k-external]");
    if (link) {
      ev.preventDefault();
      (opts.openLink || openExternal)(link.getAttribute("href"));
      return;
    }
    const mdCopy = ev.target.closest(".k-md [data-md-copy]");
    if (mdCopy) {
      const code = mdCopy.closest(".k-md-code")?.querySelector("pre code");
      if (code) ctl.copyText?.(code.textContent);
      return;
    }
    const runCard = ev.target.closest(".k-rb-pick");
    if (runCard) {
      const btn = ev.target.closest("button");
      if (!btn || btn.disabled) return;
      const step = btn.dataset.rbStep || "";
      if (btn.hasAttribute("data-rb-propose")) ctl.proposeStep?.();
      else if (btn.hasAttribute("data-rb-done")) ctl.stepAction?.(step, "done");
      else if (btn.hasAttribute("data-rb-skip")) ctl.stepAction?.(step, "skip");
      else if (btn.hasAttribute("data-rb-retry")) ctl.stepAction?.(step, "retry");
      else if (btn.hasAttribute("data-rb-choose")) ctl.toggleChoose?.();
      else if (btn.hasAttribute("data-rb-stop")) ctl.stopRunbook?.();
      else if (btn.hasAttribute("data-rb-move")) ctl.moveRun?.();
      else if (btn.dataset.rbUse) ctl.useRunbook?.(btn.dataset.rbUse);
      return;
    }
    const nbp = ev.target.closest(".k-prop--notebook")?.dataset.nbpId;
    if (nbp && nbRecs.has(nbp)) {
      const nbBtn = ev.target.closest("[data-nbp-notebook]");
      if (ev.target.closest("[data-nbp-undo]")) ctl.nbPropAction?.(nbp, "undo");
      else if (ev.target.closest("[data-nbp-delete]")) ctl.nbPropAction?.(nbp, "delete");
      else if (ev.target.closest("[data-nbp-keep]")) ctl.nbPropAction?.(nbp, "keep");
      else if (ev.target.closest("[data-nbp-restore]")) ctl.nbPropAction?.(nbp, "restore");
      else if (nbBtn) ctl.openNotebook?.(nbBtn.dataset.nbpNotebook);
      return;
    }
    const bid = ev.target.closest(".k-prop--runbook")?.dataset.bookId;
    if (bid && bookRecs.has(bid)) {
      if (ev.target.closest("[data-rbp-save]")) ctl.saveBookProp?.(bid);
      else if (ev.target.closest("[data-rbp-dismiss]")) ctl.bookPropAction?.(bid, "DELETE", "");
      else if (ev.target.closest("[data-rbp-undo]")) ctl.bookPropAction?.(bid, "POST", "/undo");
      else if (ev.target.closest("[data-rbp-open]")) ctl.openRunbooks?.();
      return;
    }
    const nid = ev.target.closest(".k-prop--note")?.dataset.noteId;
    if (nid && noteRecs.has(nid)) {
      if (ev.target.closest("[data-note-save]")) saveNote(nid);
      else if (ev.target.closest("[data-note-dismiss]")) ctl.dismissNote(nid);
      else if (ev.target.closest("[data-note-undo]")) ctl.undoNote(nid);
      else if (ev.target.closest("[data-note-edit-btn]")) setNoteEditing(nid, true);
      else if (ev.target.closest("[data-note-cancel]")) setNoteEditing(nid, false);
      return;
    }
    const id = ev.target.closest(".k-prop")?.dataset.proposalId;
    if (!id || !recs.has(id)) return;
    if (ev.target.closest("[data-prop-type]")) typeIt(id, ev.shiftKey);
    else if (ev.target.closest("[data-prop-again]")) proposeAgain(id);
    else if (ev.target.closest("[data-prop-copy]")) ctl.copy(id);
    else if (ev.target.closest("[data-prop-edit-btn]")) setEditing(id, true);
    else if (ev.target.closest("[data-prop-cancel]")) setEditing(id, false);
    else if (ev.target.closest("[data-prop-save]")) saveEdit(id);
  });
  el.addEventListener("keydown", (ev) => {
    const nid = ev.target.closest(".k-prop--note")?.dataset.noteId;
    const nrec = nid && noteRecs.get(nid);
    if (nrec?.editing) {
      if (ev.key === "Escape") {
        ev.preventDefault();
        ev.stopPropagation();
        setNoteEditing(nid, false);
      } else if (ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
        ev.preventDefault();
        saveNote(nid);
      }
      return;
    }
    const id = ev.target.closest(".k-prop")?.dataset.proposalId;
    const rec = id && recs.get(id);
    if (!rec) return;
    // ↵ / ⇧↵ on Type into pane: handled here so ⇧ is read from the key,
    // whatever the engine puts on a keyboard-made click.
    if (ev.key === "Enter" && ev.target.closest("[data-prop-type]")) {
      ev.preventDefault();
      typeIt(id, ev.shiftKey);
    }
    if (rec.editing && ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      setEditing(id, false);
    }
    if (rec.editing && ev.key === "Enter" && (ev.metaKey || ev.ctrlKey)) {
      ev.preventDefault();
      saveEdit(id);
    }
  });

  return {
    el,
    clear,
    paintEmpty,
    hasTurns: () => turns > 0,
    askStart,
    askDone,
    askStopped,
    askFailed,
    renderTranscript,
    addProposal,
    paintCard,
    addNote,
    paintNote,
    addBookProp,
    paintBookProp,
    addNbProp,
    paintNbProp,
    paintRuns,
    repaint,
    scrollDown,
  };
}
