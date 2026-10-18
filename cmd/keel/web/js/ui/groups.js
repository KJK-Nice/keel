// The Group tab page (docs/ui-redesign.md §17, board GroupRoles; shell in
// internal/ui/groupspage.templ): one folder's hosts with their roles, the
// group's notes for the agent and "Run a runbook on this group".
//
// Roles are saved one host at a time (PUT /api/groups/{id}/roles); notes
// through js/ui/notes.js (kind "group"), which keel serve redacts before it
// keeps them. Nothing here reads a pane, talks to the agent's provider or
// types: Run a runbook on this group starts a run whose every host × step
// is a proposal you approve.

import { escapeHtml } from "../lib/html.js";
import * as G from "../lib/grouprun.js";
import { createNotesEditor } from "./notes.js";

const SERVER = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M2 8C2 6.1 2 5.2 2.6 4.6C3.2 4 4.1 4 6 4H18C19.9 4 20.8 4 21.4 4.6C22 5.2 22 6.1 22 8C22 9.9 22 10.8 21.4 11.4C20.8 12 19.9 12 18 12H6C4.1 12 3.2 12 2.6 11.4C2 10.8 2 9.9 2 8Z"/><path d="M2 16C2 14.1 2 13.2 2.6 12.6C3.2 12 4.1 12 6 12H18C19.9 12 20.8 12 21.4 12.6C22 13.2 22 14.1 22 16C22 17.9 22 18.8 21.4 19.4C20.8 20 19.9 20 18 20H6C4.1 20 3.2 20 2.6 19.4C2 18.8 2 17.9 2 16Z"/><path d="M6 8H6.01M6 16H6.01M10 8H10.01M10 16H10.01" stroke-width="2.2"/></svg>';
const NEW_ROLE = "__new__";

// createGroupPage wires the page. deps:
//   api(path, opts), toast(msg, isError)
//   hosts()           → the saved hosts (state.hosts)
//   setTitle(name)    names the tab
//   startRun(run)     opens the run page on a run keel serve started
export function createGroupPage(deps) {
  const doc = deps.doc || document;
  const $ = (id) => doc.getElementById(id);
  const root = $("page-group");
  if (!root) return null;
  const el = {
    title: $("group-title"),
    desc: $("group-desc"),
    roles: $("group-roles"),
    hosts: $("group-hosts"),
    error: $("group-error"),
    run: $("group-run"),
    menu: $("group-run-menu"),
  };
  const notes = createNotesEditor({ root: $("group-notes"), api: deps.api, toast: deps.toast, kind: "group" });
  const state = { id: "", group: null, members: [], gen: 0, newRoleFor: "", menuOpen: false, runbooks: [] };

  function setError(msg) {
    if (!el.error) return;
    el.error.textContent = msg || "";
    el.error.hidden = !msg;
  }

  async function load(id = state.id) {
    if (!id) return;
    const changed = id !== state.id;
    state.id = id;
    const gen = ++state.gen;
    if (changed) {
      state.members = [];
      state.newRoleFor = "";
      notes?.load(id);
    }
    try {
      const res = await deps.api(`/api/groups/${encodeURIComponent(id)}/members`);
      if (gen !== state.gen) return;
      state.group = res?.group || null;
      state.members = res?.members || [];
      setError("");
    } catch (err) {
      if (gen !== state.gen) return;
      setError(err?.code === 423 ? "Unlock the vault to see this group." : err?.message || String(err));
    }
    paint();
  }

  function roleSelect(m, roles) {
    if (state.newRoleFor === m.hostId) {
      return `<input class="k-input k-gx-rolenew" data-role-new="${escapeHtml(m.hostId)}" aria-label="New role for ${escapeHtml(m.name)}" placeholder="e.g. worker" maxlength="32" autocomplete="off" spellcheck="false" />`;
    }
    const opts = [`<option value=""${m.role ? "" : " selected"}>No role</option>`]
      .concat(roles.map((r) => `<option value="${escapeHtml(r)}"${r === m.role ? " selected" : ""}>${escapeHtml(r)}</option>`))
      .concat([`<option value="${NEW_ROLE}">New role…</option>`]);
    return `<select class="k-gx-rolesel" data-role-host="${escapeHtml(m.hostId)}" aria-label="Role of ${escapeHtml(m.name)}">${opts.join("")}</select>`;
  }

  function paint() {
    const name = state.group?.name || "Group";
    if (el.title) el.title.textContent = name;
    deps.setTitle?.(name);
    const hosts = deps.hosts() || [];
    const byId = new Map(hosts.map((h) => [h.id, h]));
    if (el.desc) el.desc.textContent = `${G.groupLine(state.members, hosts)}.`;
    const counts = G.roleCounts(state.members);
    if (el.roles) {
      el.roles.innerHTML = counts.length
        ? counts.map((r) => `<span class="k-gx-role${r.none ? " k-gx-role--none" : ""}" role="listitem"><b>${r.count}</b>${escapeHtml(r.role)}</span>`).join("")
        : "";
    }
    const roles = G.roleNames(state.members);
    if (el.hosts) {
      el.hosts.innerHTML = state.members.length
        ? state.members
            .map((m) => {
              const h = byId.get(m.hostId) || {};
              const addr = h.address ? `${h.username ? `${h.username}@` : ""}${h.address}` : h.authType === "local" ? "this machine" : "";
              const tags = (h.tags || []).map((t) => `<span class="k-badge k-badge--mono">${escapeHtml(t)}</span>`).join("");
              const agent = h.agentReadable === false ? `<span class="k-badge">Agent cannot read</span>` : `<span class="k-badge k-badge--haze">Can read</span>`;
              const prod = h.production ? ` <span class="k-badge k-badge--signal">Production</span>` : "";
              const folder = m.folder && m.folder !== state.group?.name ? `<span class="k-gx-folder">${escapeHtml(m.folder)}</span>` : "";
              return `<tr data-member="${escapeHtml(m.hostId)}">
                <td><span class="k-gx-hostcell"><span class="k-tile k-tile--sm"${h.color ? ` style="--host:${escapeHtml(h.color)}"` : ""}>${SERVER}</span><span class="k-mono k-cell-name">${escapeHtml(m.name)}</span>${folder}</span></td>
                <td class="k-mono k-gx-addr">${escapeHtml(addr)}</td>
                <td>${roleSelect(m, roles)}</td>
                <td><span class="k-rb-chips">${tags}</span></td>
                <td>${agent}${prod}</td>
              </tr>`;
            })
            .join("")
        : `<tr class="k-table-empty"><td colspan="5"><p class="k-help k-gx-empty">No hosts in this folder yet. Drag hosts onto it in the sidebar.</p></td></tr>`;
    }
    const input = state.newRoleFor ? el.hosts?.querySelector(`[data-role-new="${CSS.escape(state.newRoleFor)}"]`) : null;
    input?.focus();
  }

  async function setRole(hostId, role) {
    const { role: r, ok } = G.cleanRole(role);
    if (!ok) {
      setError("A role is one word of letters, digits, -, _ or . (for example control-plane).");
      return;
    }
    try {
      await deps.api(`/api/groups/${encodeURIComponent(state.id)}/roles`, { method: "PUT", body: JSON.stringify({ hostId, role: r }) });
      const m = state.members.find((x) => x.hostId === hostId);
      if (m) m.role = r;
      setError("");
    } catch (err) {
      setError(err?.message || String(err));
    }
    state.newRoleFor = "";
    paint();
    el.hosts?.querySelector(`[data-role-host="${CSS.escape(hostId)}"]`)?.focus();
  }

  el.hosts?.addEventListener("change", (ev) => {
    const sel = ev.target.closest("[data-role-host]");
    if (!sel) return;
    const hostId = sel.dataset.roleHost;
    if (sel.value === NEW_ROLE) {
      state.newRoleFor = hostId;
      paint();
      return;
    }
    setRole(hostId, sel.value);
  });
  el.hosts?.addEventListener("keydown", (ev) => {
    const input = ev.target.closest("[data-role-new]");
    if (!input) return;
    if (ev.key === "Enter") {
      ev.preventDefault();
      setRole(input.dataset.roleNew, input.value);
    } else if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      const id = input.dataset.roleNew;
      state.newRoleFor = "";
      paint();
      el.hosts?.querySelector(`[data-role-host="${CSS.escape(id)}"]`)?.focus();
    }
  });
  el.hosts?.addEventListener("focusout", (ev) => {
    const input = ev.target.closest?.("[data-role-new]");
    if (input && state.newRoleFor && input.value.trim()) setRole(input.dataset.roleNew, input.value);
  });

  // --- Run a runbook on this group ---

  function closeMenu() {
    state.menuOpen = false;
    if (el.menu) el.menu.hidden = true;
    el.run?.setAttribute("aria-expanded", "false");
  }

  async function openMenu() {
    state.menuOpen = true;
    el.run?.setAttribute("aria-expanded", "true");
    if (!el.menu) return;
    el.menu.hidden = false;
    el.menu.innerHTML = `<p class="k-help k-rb-menu-head">Loading runbooks…</p>`;
    try {
      const res = await deps.api("/api/agent/runbooks");
      state.runbooks = (res?.runbooks || []).filter((b) => (b.steps || []).length);
    } catch (err) {
      state.runbooks = [];
      el.menu.innerHTML = `<p class="k-help k-rb-menu-empty">${escapeHtml(err?.message || String(err))}</p>`;
      return;
    }
    if (!state.menuOpen) return;
    el.menu.innerHTML = state.runbooks.length
      ? `<p class="k-menu-label k-rb-menu-head">Run on ${escapeHtml(state.group?.name || "this group")}</p>` +
        state.runbooks
          .map((b) => {
            const roles = [...new Set((b.steps || []).map((s) => s.role).filter(Boolean))];
            const sub = roles.length ? `Roles ${roles.join(", ")}` : "Every host";
            return `<button type="button" class="k-rb-menu-item" role="menuitem" data-run-runbook="${escapeHtml(b.id)}"><span class="k-rb-other-name">${escapeHtml(b.name)}</span><span class="k-rb-other-sub">${escapeHtml(`${b.steps.length} ${b.steps.length === 1 ? "step" : "steps"} · ${sub}`)}</span></button>`;
          })
          .join("")
      : `<p class="k-help k-rb-menu-empty">No runbooks yet. Add one on the Runbooks page.</p>`;
    el.menu.querySelector("[data-run-runbook]")?.focus();
  }

  async function start(runbookId) {
    closeMenu();
    try {
      const run = await deps.api("/api/agent/groupruns", { method: "POST", body: JSON.stringify({ runbookId, groupId: state.id, mode: "rolling" }) });
      deps.startRun?.(run);
    } catch (err) {
      deps.toast?.(err?.message || String(err), true);
    }
  }

  el.run?.addEventListener("click", (ev) => {
    ev.preventDefault();
    if (state.menuOpen) closeMenu();
    else openMenu();
  });
  el.menu?.addEventListener("click", (ev) => {
    const b = ev.target.closest("[data-run-runbook]");
    if (b) start(b.dataset.runRunbook);
  });
  el.menu?.addEventListener("keydown", (ev) => {
    const items = [...el.menu.querySelectorAll("[data-run-runbook]")];
    const i = items.indexOf(doc.activeElement);
    if (ev.key === "Escape") {
      ev.preventDefault();
      ev.stopPropagation();
      closeMenu();
      el.run?.focus();
    } else if (ev.key === "ArrowDown" && items.length) {
      ev.preventDefault();
      items[(i + 1) % items.length].focus();
    } else if (ev.key === "ArrowUp" && items.length) {
      ev.preventDefault();
      items[(i - 1 + items.length) % items.length].focus();
    }
  });
  doc.addEventListener("click", (ev) => {
    if (state.menuOpen && !ev.target.closest?.("#group-run, #group-run-menu")) closeMenu();
  });

  return {
    show: (id) => load(id || state.id),
    refresh: () => (state.id ? load(state.id) : null),
    groupId: () => state.id,
  };
}
