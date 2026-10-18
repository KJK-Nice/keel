// The group run page (docs/ui-redesign.md §17, board GroupRunbook; shell in
// internal/ui/groupspage.templ): one runbook walked across a group's hosts,
// rolling or parallel, with every host's result per step.
//
// keel serve holds the run (/api/agent/groupruns/{id}). While the run is
// active the page asks it every 1.5 s to read the shells' OSC 133 results
// and make the current step's due proposals for the panes open in this
// window (POST …/propose; keel serve resolves each pane's host itself).
// Typing is the user's: Type into N panes opens the group approve dialog,
// which shows the exact bytes once and one row per host that the user
// ticks; each ticked host is then typed on its own, with its own server
// nonce and its own check in keel serve (term.js typeGroupProposal). Open
// a pane on a host that has none is also the user's action.

import { escapeHtml } from "../lib/html.js";
import * as A from "../lib/agent.js";
import * as G from "../lib/grouprun.js";
import { ICON } from "./agentthread.js";

const POLL_MS = 1500;

// createGroupRunPage wires the page. deps:
//   api(path, opts), toast(msg, isError), confirm(msg, opts) → Promise<bool>
//   isMac
//   panes()            → [{sessionId, hostId, driving, remote}] open here
//   openHost(hostId)   opens a session on a host (the user's click)
//   typeGroup({runId, proposal, shift}) → Promise<{ok, message}>
//   isOpen()           → whether the run's tab is still open
//   setTitle(text)     names the tab
//   openModal(el), closeModal(el)
export function createGroupRunPage(deps) {
  const doc = deps.doc || document;
  const $ = (id) => doc.getElementById(id);
  const root = $("page-grouprun");
  if (!root) return null;
  const el = {
    steps: $("grouprun-steps"),
    detail: $("grouprun-detail"),
    dialog: $("group-approve-modal"),
    form: $("group-approve-form"),
    bytes: $("group-approve-bytes"),
    count: $("group-approve-count"),
    rows: $("group-approve-rows"),
    shift: $("group-approve-shift"),
    ok: $("group-approve-ok"),
    cancel: $("group-approve-cancel"),
  };
  const state = { run: null, timer: 0, busy: false, error: "", typing: false, approve: null };

  function paneIds() {
    return (deps.panes() || []).filter((p) => p.driving && !p.remote).map((p) => p.sessionId);
  }

  function paneOpen(sessionId) {
    return paneIds().includes(sessionId);
  }

  // call asks keel serve and repaints only when something changed, so a
  // poll does not pull a button from under the pointer or the focus.
  async function call(path, opts) {
    const before = JSON.stringify([state.run, state.error]);
    try {
      const run = await deps.api(path, opts);
      if (run?.id) state.run = run;
      state.error = "";
    } catch (err) {
      state.error = err?.message || String(err);
    }
    if (JSON.stringify([state.run, state.error]) !== before) paint();
    schedule();
  }

  function tick() {
    const run = state.run;
    if (!run) return;
    if (deps.isOpen && !deps.isOpen()) return;
    if (run.status !== "active") return;
    if (state.typing || state.approve) return schedule();
    call(`/api/agent/groupruns/${encodeURIComponent(run.id)}/propose`, { method: "POST", body: JSON.stringify({ panes: paneIds() }) });
  }

  function schedule() {
    clearTimeout(state.timer);
    if (state.run?.status === "active") state.timer = setTimeout(tick, POLL_MS);
  }

  // --- painting ---

  function stepsHTML(run) {
    const changes = run.steps.filter((s) => s.risk && s.risk !== "readonly").length;
    const started = run.startedAt ? new Date(run.startedAt) : null;
    const at = started && !Number.isNaN(started.getTime()) ? `${String(started.getHours()).padStart(2, "0")}:${String(started.getMinutes()).padStart(2, "0")}` : "";
    const rows = run.steps
      .map((st, i) => {
        const m = G.stepMark(run, i);
        const sub = st.command ? `${G.targetLabel(st)}${i === run.current ? ` · ${G.modeShort(run.mode)}` : ""}` : "No command · you check it";
        return `<li class="k-gx-step" data-state="${m.state}"${i === run.current ? ' aria-current="step"' : ""}>
          <span class="k-rb-n k-gx-n" aria-hidden="true">${escapeHtml(m.mark)}</span>
          <div class="k-gx-step-body"><h3 class="k-gx-step-title">${escapeHtml(st.title)}</h3><p class="k-gx-step-sub">${escapeHtml(sub)}</p></div>
        </li>`;
      })
      .join("");
    return `<div class="k-gx-steps-head">
        <span class="k-menu-label">Runbook on ${escapeHtml(run.group)}</span>
        <h1 class="k-gx-title">${escapeHtml(run.name)}</h1>
        <span class="k-help">${escapeHtml(`${run.steps.length} ${run.steps.length === 1 ? "step" : "steps"} · ${changes ? `${changes} ${changes === 1 ? "changes" : "change"} state` : "all read-only"}${at ? ` · started ${at} by you` : ""}`)}</span>
      </div>
      <ol class="k-gx-steplist" aria-label="Steps">${rows}</ol>
      <div class="k-gx-steps-foot">
        <p class="k-rb-safe k-help">${ICON.shield}<span>Each step is one proposal per host. Nothing is typed until you tick the host and press Type, and each host's result comes from its shell's exit code.</span></p>
        ${run.status === "active" ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-run-stop>Stop runbook</button>` : ""}
      </div>`;
  }

  function cellActions(run, st, c) {
    if (run.status !== "active" || run.steps[run.current] !== st || !c) return "";
    const name = escapeHtml(c.host || "host");
    const b = (action, label, aria, cls = "k-btn--ghost") => `<button type="button" class="k-btn ${cls} k-btn--sm" data-host-action="${action}" data-host="${escapeHtml(c.hostId)}" aria-label="${aria}">${label}</button>`;
    switch (c.state) {
      case "typed":
        if (c.confirm) return b("confirm", "Confirm", `Confirm ${name} done`, "") + b("retry", "Retry", `Retry ${name}`);
        return b("done", "Mark done", `Mark ${name} done`) + b("retry", "Retry", `Retry ${name}`);
      case "failed":
        return b("retry", "Retry", `Retry ${name}`, "") + b("skip", "Skip", `Skip ${name}`) + b("done", "Mark done", `Mark ${name} done`);
      case "proposed":
        return b("skip", "Skip", `Skip ${name}`);
      case "waiting":
        return c.hostId ? b("skip", "Skip", `Skip ${name}`) : "";
    }
    return "";
  }

  function matrixHTML(run) {
    // Rows: the run's target hosts (any step), in member order.
    const targets = [];
    const seen = new Set();
    for (const m of run.members) {
      if (m.excluded) continue;
      if (run.steps.some((st) => (st.cells || []).some((c) => c.hostId === m.hostId) || (st.candidates || []).some((h) => h.hostId === m.hostId))) {
        targets.push(m);
        seen.add(m.hostId);
      }
    }
    const head = run.steps.map((st, i) => `<th scope="col" title="${escapeHtml(st.title)}"${i === run.current ? ' aria-current="step"' : ""}>Step ${i + 1}</th>`).join("");
    const cur = run.current >= 0 ? run.steps[run.current] : null;
    const body = targets
      .map((m) => {
        const cells = run.steps
          .map((st) => {
            const c = (st.cells || []).find((x) => x.hostId === m.hostId);
            if (!c) {
              const maybe = (st.candidates || []).some((h) => h.hostId === m.hostId);
              return `<td class="k-gx-cell"><span class="k-gx-st k-gx-st--wait">${maybe ? "· maybe" : ""}</span></td>`;
            }
            const v = G.cellView(c);
            return `<td class="k-gx-cell"><span class="${v.cls}" data-cell-state="${escapeHtml(v.state)}">${escapeHtml(`${v.glyph} ${v.text}`)}</span></td>`;
          })
          .join("");
        const c = cur ? (cur.cells || []).find((x) => x.hostId === m.hostId) : null;
        return `<tr data-host-row="${escapeHtml(m.hostId)}"><th scope="row" class="k-mono k-cell-name">${escapeHtml(m.host)}${m.production ? ' <span class="k-badge k-badge--signal k-gx-prod">Production</span>' : ""}</th><td class="k-mono k-gx-rolecell">${escapeHtml(m.role || "")}</td>${cells}<td class="k-cell-end"><span class="k-gx-actions">${cellActions(run, cur, c)}</span></td></tr>`;
      })
      .join("");
    const left = run.members.filter((m) => m.excluded);
    const leftLine = left.length ? `<p class="k-help k-gx-left">Left out: ${left.map((m) => `${escapeHtml(m.host)} (${escapeHtml(m.excluded)})`).join(", ")}. The run never types into them.</p>` : "";
    return `<section class="k-box k-gx-results" aria-labelledby="grouprun-results-h">
      <div class="k-gx-boxhead"><h3 class="k-gx-h3" id="grouprun-results-h">Results</h3><span class="k-help">${escapeHtml(cur ? `Step ${run.current + 1}: ${G.summary(cur)}` : G.statusLine(run))}</span></div>
      <div class="k-gx-tablewrap"><table class="k-table k-gx-matrix" aria-label="Result per host and step">
        <thead><tr><th scope="col">Host</th><th scope="col">Role</th>${head}<th scope="col" aria-label="Actions"></th></tr></thead>
        <tbody>${body || `<tr class="k-table-empty"><td colspan="${run.steps.length + 3}"><p class="k-help k-gx-empty">No host in this group is a target: none has the roles these steps name, or the agent may read none of them.</p></td></tr>`}</tbody>
      </table></div>
      ${leftLine}
      <p class="k-help k-gx-reported">Exit codes are reported by each pane's shell (OSC 133 marks): they are not proof. On a production host a reported exit 0 waits for you to confirm it.</p>
    </section>`;
  }

  function currentHTML(run) {
    if (run.status === "finished") return `<div class="k-review k-gx-banner" role="status">${ICON.tick}<span><b>All steps are done or skipped.</b> Logs keep what was typed on each host.</span></div>`;
    if (run.status === "stopped") return `<div class="k-review k-gx-banner" role="status">${ICON.info}<span><b>Runbook stopped at step ${run.current + 1 || run.steps.length}.</b> Logs keep what was typed on each host. Nothing more is proposed.</span></div>`;
    const st = run.steps[run.current];
    if (!st) return "";
    const i = run.current;
    const mode = `<div class="k-gx-mode" role="group" aria-label="How to run on several hosts">
        <button type="button" aria-pressed="${run.mode === "rolling"}" data-mode="rolling">Rolling</button>
        <button type="button" aria-pressed="${run.mode === "parallel"}" data-mode="parallel">Parallel</button>
      </div>`;
    const head = `<div class="k-gx-curhead"><h2 class="k-gx-h2">Step ${i + 1} · ${escapeHtml(st.title)}</h2>${st.command ? mode : ""}</div>
      ${st.command ? `<p class="k-help k-gx-modehelp">${escapeHtml(G.MODE_HELP[run.mode] || "")}</p>` : ""}
      ${st.description ? `<p class="k-gx-desc">${escapeHtml(st.description)}</p>` : ""}`;
    if (!st.command) {
      return `${head}<div class="k-review k-gx-banner" role="note">${ICON.info}<span>This step has no command. Check it yourself, then mark it done.${st.expect ? ` Expected: ${escapeHtml(st.expect)}.` : ""}</span>
        <button type="button" class="k-btn k-btn--sm" data-step-action="done">Mark step done</button><button type="button" class="k-btn k-btn--ghost k-btn--sm" data-step-action="skip">Skip step</button></div>`;
    }
    if (!(st.cells || []).length) {
      return `${head}<div class="k-review k-gx-banner" role="note">${ICON.alert}<span>${escapeHtml(st.role ? `No host in this group that the agent may read has the role ${st.role}.` : "No host in this group is one the agent may read.")} Skip the step to go on.</span>
        <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-step-action="skip">Skip step</button></div>`;
    }
    const ready = G.typeable(run, paneOpen);
    const names = (st.cells || []).map((c) => c.host).filter(Boolean);
    const hostsText = names.length > 3 ? `${names[0]} … ${names[names.length - 1]}` : names.join(", ") || "one host";
    const prod = ready.filter((c) => c.proposal.production).length;
    const shift = ready.some((c) => c.proposal.needsShift);
    const riskBadge = st.risk === "readonly" ? `<span class="k-badge k-badge--tide k-prop-risk">Read-only</span>` : st.risk === "destructive" ? `<span class="k-badge k-badge--flare k-prop-risk">Destructive</span>` : `<span class="k-badge k-badge--signal k-prop-risk">Changes</span>`;
    const bytes = ready[0]?.proposal.bytes || `\u0015${st.command}\n`;
    const label = ready.length ? `Type into ${ready.length} ${ready.length === 1 ? "pane" : "panes"}` : "Nothing to type now";
    const note = prod
      ? `<p class="k-prop-note k-prop-note--signal">${ICON.alert}<span>${prod} of these ${prod === 1 ? "host is" : "hosts are"} production.${shift ? " Typing there needs ⇧." : ""} Tick each host in the next step only after you checked the command for it.</span></p>`
      : "";
    const card = `<div class="k-prop k-gx-prop" role="group" aria-label="Step ${i + 1} proposal" data-risk="${escapeHtml(st.risk || "")}">
        <div class="k-prop-head">${ICON.cmd}<span>Run on</span>${st.role ? `<span class="k-badge k-badge--mono">${escapeHtml(st.role)}</span>` : `<span class="k-badge k-badge--mono">every host</span>`}<span class="k-mono">${escapeHtml(st.oneHost ? `one of ${hostsText}` : hostsText)}</span>${riskBadge}</div>
        <div class="k-prop-body"><pre class="k-bytes" aria-label="Exact bytes">${A.bytesHTML(bytes)}</pre>${note}</div>
        <div class="k-prop-foot"><span class="k-prop-count">${escapeHtml(`${G.bytesLine(bytes)} · same bytes in every pane`)}</span><span class="k-gx-grow"></span>
          <button type="button" class="k-btn k-btn--primary k-btn--sm" data-type-all${ready.length && !state.typing ? "" : " disabled"}>${escapeHtml(label)}${shift ? '<span class="k-kbd" aria-hidden="true">⇧</span>' : ""}</button></div>
      </div>`;
    const halt = G.haltLine(run);
    const haltHTML = halt
      ? `<div class="k-gx-halt" role="alert">${ICON.alert}<span class="k-gx-grow"><b>${escapeHtml(halt.title)}</b>${halt.sub ? `<br />${escapeHtml(halt.sub)}` : ""}</span>
          <button type="button" class="k-btn k-btn--sm" data-host-action="retry" data-host="${escapeHtml(halt.hostId)}">Retry host</button>
          <button type="button" class="k-btn k-btn--ghost k-btn--sm" data-host-action="skip" data-host="${escapeHtml(halt.hostId)}">Skip host</button>
          <button type="button" class="k-btn k-btn--danger k-btn--sm" data-run-stop>Stop runbook</button></div>`
      : "";
    const need = run.needPane || [];
    const needHTML = need.length
      ? `<div class="k-review k-gx-need" role="note">${ICON.info}<span class="k-gx-grow"><b>${escapeHtml(need.length === 1 ? `${need[0].host} has no pane open here.` : `${need.length} hosts have no pane open here.`)}</b><br />Open a session on each host to go on. Opening a session is up to you; nothing is typed into it until you approve.</span>
          <span class="k-gx-needlist">${need.map((h) => `<button type="button" class="k-btn k-btn--sm" data-open-host="${escapeHtml(h.hostId)}" aria-label="Open a pane on ${escapeHtml(h.host)}">Open ${escapeHtml(h.host)}</button>`).join("")}</span></div>`
      : "";
    return head + card + haltHTML + needHTML;
  }

  function paint() {
    const run = state.run;
    if (!run) return;
    deps.setTitle?.(`Run · ${run.name}`);
    if (el.steps) el.steps.innerHTML = stepsHTML(run);
    if (el.detail) {
      const err = state.error ? `<p class="k-error" role="alert">${escapeHtml(state.error)}</p>` : "";
      el.detail.innerHTML = `<div class="k-gx-cur" aria-live="polite">${currentHTML(run)}</div>${err}${matrixHTML(run)}`;
    }
  }

  // --- the group approve dialog ---

  function paintApprove() {
    const a = state.approve;
    if (!a) return;
    const rows = a.rows;
    if (el.bytes) el.bytes.innerHTML = A.bytesHTML(rows[0]?.bytes || "");
    if (el.count) el.count.textContent = `${G.bytesLine(rows[0]?.bytes || "")} · ${G.sameBytes(rows) ? "the same bytes in every pane" : "bytes differ per pane"}`;
    if (el.rows) {
      el.rows.innerHTML = rows
        .map(
          (r, k) => `<div class="k-gx-approve-row">
            <button type="button" class="k-check" role="checkbox" aria-checked="${r.checked}" data-approve-row="${k}" aria-label="Type into ${escapeHtml(r.host)}"><span class="k-i">${ICON.tick}</span></button>
            <span class="k-mono k-gx-approve-host">${escapeHtml(r.host)}</span>
            <span class="k-help k-gx-approve-pane">${escapeHtml(r.paneLabel || "")}</span>
            ${r.production ? '<span class="k-badge k-badge--signal">Production</span>' : ""}${r.needsShift ? '<span class="k-badge k-badge--mono">⇧</span>' : ""}
          </div>`,
        )
        .join("");
    }
    const n = rows.filter((r) => r.checked).length;
    const shift = G.needsShift(rows);
    if (el.ok) {
      el.ok.disabled = n === 0;
      el.ok.innerHTML = `${escapeHtml(G.typeLabel(rows))}${shift ? '<span class="k-kbd" aria-hidden="true">⇧↵</span>' : ""}`;
    }
    if (el.shift) {
      el.shift.hidden = !shift && !a.shiftHint;
      el.shift.textContent = shift ? "A ticked host is production (or the command is destructive): hold ⇧ and press Type." : "";
      el.shift.classList.toggle("is-alert", !!a.shiftHint);
    }
  }

  function approve(rows) {
    if (!el.dialog || typeof el.dialog.showModal !== "function") return Promise.resolve(null);
    const labels = new Map((deps.panes() || []).map((p) => [p.sessionId, p.sub || ""]));
    for (const r of rows) {
      const label = labels.get(r.sessionId) || "";
      r.paneLabel = label && label !== r.host ? `tab ${label}` : "";
    }
    return new Promise((resolve) => {
      state.approve = { rows, resolve, shiftHint: false };
      paintApprove();
      deps.openModal?.(el.dialog);
      (el.rows?.querySelector('[aria-checked="false"]') || el.ok)?.focus();
    });
  }

  function settleApprove(result) {
    const a = state.approve;
    state.approve = null;
    if (el.dialog?.open) deps.closeModal?.(el.dialog);
    a?.resolve(result);
  }

  function submitApprove(shiftHeld) {
    const a = state.approve;
    if (!a) return;
    const picked = a.rows.filter((r) => r.checked);
    if (!picked.length) return;
    if (G.needsShift(a.rows) && !shiftHeld) {
      a.shiftHint = true;
      paintApprove();
      if (el.shift) el.shift.textContent = "Hold ⇧ and press Type: a ticked host is production, or the command is destructive.";
      return;
    }
    settleApprove({ rows: picked, shift: !!shiftHeld });
  }

  el.rows?.addEventListener("click", (ev) => {
    const b = ev.target.closest("[data-approve-row]");
    if (!b || !state.approve) return;
    const r = state.approve.rows[Number(b.dataset.approveRow)];
    r.checked = !r.checked;
    paintApprove();
    el.rows.querySelector(`[data-approve-row="${b.dataset.approveRow}"]`)?.focus();
  });
  el.ok?.addEventListener("click", (ev) => {
    ev.preventDefault();
    submitApprove(ev.shiftKey);
  });
  el.form?.addEventListener("submit", (ev) => {
    ev.preventDefault();
  });
  el.form?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.isComposing && !ev.target.closest?.("#group-approve-cancel")) {
      ev.preventDefault();
      submitApprove(ev.shiftKey);
    }
  });
  el.cancel?.addEventListener("click", () => settleApprove(null));
  el.dialog?.addEventListener("close", () => {
    if (state.approve) settleApprove(null);
  });

  async function typeAll() {
    const run = state.run;
    if (!run || state.typing) return;
    const ready = G.typeable(run, paneOpen);
    if (!ready.length) return;
    const ok = await approve(G.approveRows(ready));
    if (!ok) return;
    state.typing = true;
    paint();
    const failed = [];
    for (const r of ok.rows) {
      const cell = ready.find((c) => c.proposal.id === r.proposalId);
      if (!cell) continue;
      const res = await deps.typeGroup({ runId: run.id, proposal: cell.proposal, shift: ok.shift });
      if (!res?.ok) failed.push(`${r.host}: ${res?.message || "not typed"}`);
    }
    state.typing = false;
    if (failed.length) deps.toast?.(`Not typed on ${failed.join("; ")}`, true);
    await call(`/api/agent/groupruns/${encodeURIComponent(run.id)}`);
  }

  // --- actions ---

  root.addEventListener("click", async (ev) => {
    const run = state.run;
    if (!run) return;
    const t = ev.target;
    const st = run.current >= 0 ? run.steps[run.current] : null;
    if (t.closest("[data-type-all]")) return void typeAll();
    const mode = t.closest("[data-mode]");
    if (mode) {
      if (mode.dataset.mode !== run.mode) call(`/api/agent/groupruns/${encodeURIComponent(run.id)}/mode`, { method: "PUT", body: JSON.stringify({ mode: mode.dataset.mode }) });
      return;
    }
    const host = t.closest("[data-host-action]");
    if (host && st) {
      call(`/api/agent/groupruns/${encodeURIComponent(run.id)}/steps/${encodeURIComponent(st.id)}`, { method: "POST", body: JSON.stringify({ hostId: host.dataset.host, action: host.dataset.hostAction }) });
      return;
    }
    const step = t.closest("[data-step-action]");
    if (step && st) {
      call(`/api/agent/groupruns/${encodeURIComponent(run.id)}/steps/${encodeURIComponent(st.id)}`, { method: "POST", body: JSON.stringify({ hostId: "", action: step.dataset.stepAction }) });
      return;
    }
    const open = t.closest("[data-open-host]");
    if (open) {
      deps.openHost?.(open.dataset.openHost);
      return;
    }
    if (t.closest("[data-run-stop]")) {
      const sure = await (deps.confirm ? deps.confirm("Nothing more is proposed in this run. What was typed stays typed, and Logs keep it.", { title: "Stop this runbook?", action: "Stop runbook" }) : true);
      if (sure) call(`/api/agent/groupruns/${encodeURIComponent(run.id)}`, { method: "DELETE" });
    }
  });

  return {
    // show opens a run (from the group page) or repaints the current one.
    show(run) {
      if (run?.id) state.run = run;
      state.error = "";
      paint();
      tick();
    },
    refresh: () => tick(),
    runId: () => state.run?.id || "",
  };
}
