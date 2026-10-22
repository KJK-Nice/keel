// History and Recently deleted: one dialog (#notebook-history-modal) for a
// notebook's earlier versions and for deleted notebooks (docs/ui-redesign.md
// §18). It lists them, shows a version against the notebook now (or a
// deleted notebook as text), and restores or deletes one for good.
//
// The notebooks themselves stay with term.js; the caller passes:
//   modal                 the dialog element
//   api, toast, confirm   keel serve, a notice, the confirm dialog
//   openModal, closeModal
//   notebook(id)          the notebook record, or undefined
//   flush(id)             saves what was typed in that notebook first
//   remember(rec)         a deleted notebook restored to Notebooks
//   changed(rec)          a notebook restored to an earlier version
import { escapeHtml } from "../lib/html.js";
import { setText } from "../lib/dom.js";
import * as NB from "../lib/notebooks.js";

export function createNotebookHistory({ doc = document, modal, api, toast, confirm, openModal, closeModal, notebook, flush, remember, changed }) {
  const nbHist = { mode: "history", id: "", versions: [], selected: "", texts: new Map(), busy: false };
  const el = (id) => doc.getElementById(id);

  async function openHistory(id, opts = {}) {
    await flush(id);
    const rec = notebook(id);
    if (!rec) return;
    Object.assign(nbHist, { mode: "history", id, versions: [], selected: "", texts: new Map(), busy: false });
    setText(el("notebook-history-title"), `History · ${rec.name || "Notebook"}`);
    setText(
      el("notebook-history-desc"),
      "Keel keeps the version before every agent edit and every restore, and one when you start editing again. Up to 20 versions, for 30 days.",
    );
    paint("Loading…");
    openModal(modal);
    try {
      const res = await api(`/api/notebooks/${encodeURIComponent(id)}/history`);
      nbHist.versions = res?.versions || [];
      const pick = opts.agentEdit ? nbHist.versions.find((v) => v.event === "before-agent-edit") : nbHist.versions[0];
      paint(nbHist.versions.length ? "" : "No earlier versions yet. Keel keeps one before every agent edit and restore, and when you start editing again.");
      if (pick) await select(pick.id);
    } catch (err) {
      paint(err.message || "Could not load History");
    }
  }

  async function openTrash() {
    Object.assign(nbHist, { mode: "trash", id: "", versions: [], selected: "", texts: new Map(), busy: false });
    setText(el("notebook-history-title"), "Recently deleted");
    setText(el("notebook-history-desc"), "Deleted notebooks stay here for 30 days (at most 50). Restore puts one back in Notebooks.");
    paint("Loading…");
    openModal(modal);
    try {
      const res = await api("/api/notebook-trash");
      nbHist.versions = res?.versions || [];
      paint(nbHist.versions.length ? "" : "Nothing was deleted in the last 30 days.");
      if (nbHist.versions[0]) await select(nbHist.versions[0].id);
    } catch (err) {
      paint(err.message || "Could not load Recently deleted");
    }
  }

  function paint(empty) {
    const list = el("notebook-history-list");
    const restoreBtn = el("notebook-history-restore");
    if (restoreBtn) {
      restoreBtn.disabled = !nbHist.selected || nbHist.busy;
      restoreBtn.textContent = nbHist.mode === "trash" ? "Restore notebook" : "Restore this version";
    }
    const purgeBtn = el("notebook-history-purge");
    if (purgeBtn) {
      purgeBtn.disabled = !nbHist.selected || nbHist.busy;
      purgeBtn.textContent = nbHist.mode === "trash" ? "Delete forever" : "Delete this version";
    }
    if (!list) return;
    if (empty) {
      list.innerHTML = `<p class="k-help notebook-history-empty">${escapeHtml(empty)}</p>`;
      const diff = el("notebook-history-diff");
      if (diff) diff.innerHTML = "";
      return;
    }
    list.innerHTML = nbHist.versions
      .map((v) => {
        const l = NB.versionLabel(v);
        const title = nbHist.mode === "trash" ? v.name || "Notebook" : l.what;
        const who = nbHist.mode === "trash" ? `Deleted ${NB.whenText(v.keptAt)}` : l.when;
        return `<button type="button" class="notebook-history-row" role="option" aria-selected="${v.id === nbHist.selected}" data-notebook-version="${escapeHtml(v.id)}">
          <span class="notebook-history-who"><span class="k-badge${v.by === "agent" ? " k-badge--outline" : ""}">${escapeHtml(nbHist.mode === "trash" ? "Deleted" : l.who)}</span><span class="notebook-history-what">${escapeHtml(title)}</span></span>
          <span class="notebook-history-when">${escapeHtml(who)}</span>${l.summary ? `<span class="notebook-history-summary">${escapeHtml(l.summary)}</span>` : ""}
        </button>`;
      })
      .join("");
  }

  async function select(vid) {
    nbHist.selected = vid;
    paint("");
    const diff = el("notebook-history-diff");
    if (!diff) return;
    let text = nbHist.texts.get(vid);
    if (text == null) {
      const v = nbHist.versions.find((x) => x.id === vid);
      try {
        const full = await api(nbHist.mode === "trash" ? `/api/notebook-trash/${encodeURIComponent(vid)}` : `/api/notebooks/${encodeURIComponent(nbHist.id)}/history/${encodeURIComponent(vid)}`);
        text = NB.versionText(full || v);
      } catch (err) {
        diff.innerHTML = `<p class="k-help">${escapeHtml(err.message || "Could not load this version")}</p>`;
        return;
      }
      nbHist.texts.set(vid, text);
    }
    if (nbHist.selected !== vid) return;
    if (nbHist.mode === "trash") {
      diff.innerHTML = `<pre class="notebook-diff notebook-diff--plain">${escapeHtml(text)}</pre>`;
      return;
    }
    const cur = notebook(nbHist.id);
    const now = (cur?.cells || []).map((c) => String(c.body || "")).join("\n\n");
    const d = NB.diffLines(text, now);
    const rows = NB.diffView(d)
      .map((l) =>
        l.op === "gap"
          ? `<div class="notebook-diff-gap">${escapeHtml(`${l.count} unchanged ${l.count === 1 ? "line" : "lines"}`)}</div>`
          : `<div class="notebook-diff-line notebook-diff-line--${l.op}"><span class="notebook-diff-mark" aria-hidden="true">${l.op === "add" ? "+" : l.op === "del" ? "−" : " "}</span><span class="notebook-diff-text">${escapeHtml(l.text) || " "}</span></div>`,
      )
      .join("");
    diff.innerHTML = `<p class="notebook-diff-head">${escapeHtml(`From this version to now: ${NB.diffSummary(d)}`)}</p><div class="notebook-diff" role="region" aria-label="Changes from this version to now">${rows}</div>`;
  }

  // purge removes the selected version (or deleted notebook) for good:
  // History is plain text on disk like the notebooks.
  async function purge() {
    const vid = nbHist.selected;
    if (!vid || nbHist.busy) return;
    const trash = nbHist.mode === "trash";
    const ok = await confirm(trash ? "Delete this notebook for good? It cannot be restored." : "Delete this version for good? It cannot be restored.", {
      title: trash ? "Delete forever" : "Delete version",
      action: "Delete",
    });
    if (!ok) return;
    nbHist.busy = true;
    try {
      await api(trash ? `/api/notebook-trash/${encodeURIComponent(vid)}` : `/api/notebooks/${encodeURIComponent(nbHist.id)}/history/${encodeURIComponent(vid)}`, { method: "DELETE" });
      nbHist.versions = nbHist.versions.filter((v) => v.id !== vid);
      nbHist.texts.delete(vid);
      nbHist.selected = "";
      nbHist.busy = false;
      paint(nbHist.versions.length ? "" : trash ? "Nothing was deleted in the last 30 days." : "No earlier versions left.");
      if (nbHist.versions[0]) await select(nbHist.versions[0].id);
    } catch (err) {
      toast(err.message || "Could not delete", true);
    } finally {
      nbHist.busy = false;
      paint(nbHist.versions.length ? "" : trash ? "Nothing was deleted in the last 30 days." : "No earlier versions left.");
    }
  }

  async function restore() {
    const vid = nbHist.selected;
    if (!vid || nbHist.busy) return;
    nbHist.busy = true;
    paint("");
    try {
      if (nbHist.mode === "trash") {
        const saved = await api(`/api/notebook-trash/${encodeURIComponent(vid)}/restore`, { method: "POST" });
        remember(saved);
        closeModal(modal);
        toast(`Restored “${saved.name || "Notebook"}” to Notebooks.`);
        return;
      }
      await flush(nbHist.id);
      const rec = notebook(nbHist.id);
      const saved = await api(`/api/notebooks/${encodeURIComponent(nbHist.id)}/history/${encodeURIComponent(vid)}/restore`, { method: "POST", body: JSON.stringify({ rev: rec?.rev || 0 }) });
      changed(saved);
      closeModal(modal);
      toast("Restored. The version it replaced is in History.");
    } catch (err) {
      toast(err.message || "Could not restore", true);
    } finally {
      nbHist.busy = false;
      paint("");
    }
  }

  // bind wires the dialog's own buttons and its list.
  function bind() {
    el("notebook-history-close")?.addEventListener("click", () => closeModal(modal));
    el("notebook-history-restore")?.addEventListener("click", () => restore());
    el("notebook-history-purge")?.addEventListener("click", () => purge());
    el("notebook-history-list")?.addEventListener("click", (ev) => {
      const row = ev.target.closest("[data-notebook-version]");
      if (row) select(row.dataset.notebookVersion);
    });
  }

  return { openHistory, openTrash, bind };
}
