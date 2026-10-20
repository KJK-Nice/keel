// Settings · Agent and the first-run Agent step (docs/ui-redesign.md §5.9,
// §5.15): markup and small decisions as pure functions of keel serve's
// GET /api/agent/settings and /api/agent/providers. No DOM, no network.
//
// A key never comes back from keel serve, so nothing here can show one: a
// provider is only ever "configured" or not. The key input is a password
// field that the controller empties as soon as the save succeeds.

import { escapeHtml } from "./html.js";

const SVG = (paths) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;

export const ICON = {
  alert: SVG(
    '<path d="M13.9 21H10.1C5.4 21 3.1 21 2.3 19.5C1.4 18 2.6 16 5 12L6.9 8.8C9.2 4.9 10.3 3 12 3C13.7 3 14.8 4.9 17.1 8.8L19 12C21.4 16 22.6 18 21.7 19.5C20.9 21 18.6 21 13.9 21Z"/><path d="M12 9V13"/><path d="M12 16.8H12.01" stroke-width="2.2"/>',
  ),
  lock: SVG(
    '<path d="M12 14V17"/><path d="M16.5 9V6.5C16.5 4 14.5 2 12 2C9.5 2 7.5 4 7.5 6.5V9"/><path d="M4.3 18.8C4.5 20.5 5.9 21.8 7.6 21.9C9 22 10.4 22 12 22C13.6 22 15 22 16.4 21.9C18.1 21.8 19.5 20.5 19.7 18.8C19.9 17.8 20 16.6 20 15.5C20 14.4 19.9 13.2 19.7 12.2C19.5 10.5 18.1 9.2 16.4 9.1C15 9 13.6 9 12 9C10.4 9 9 9 7.6 9.1C5.9 9.2 4.5 10.5 4.3 12.2C4.1 13.2 4 14.4 4 15.5C4 16.6 4.1 17.8 4.3 18.8Z"/>',
  ),
  ai: SVG(
    '<path d="M13.427 8.08396C12.8007 5.84272 12.3226 4 11.0004 4C9.67824 4 9.19964 5.84274 8.5731 8.08399C8.31853 8.99465 8.19125 9.44998 7.82042 9.82077C7.4496 10.1916 6.99434 10.3188 6.08381 10.5732C3.84273 11.1995 2 11.6775 2 13C2 14.3225 3.84273 14.8005 6.08381 15.4268C6.99434 15.6812 7.4496 15.8084 7.82042 16.1792C8.19125 16.55 8.31853 17.0054 8.5731 17.916C9.19964 20.1573 9.67824 22 11.0004 22C12.3226 22 12.8007 20.1573 13.427 17.916C13.6815 17.0054 13.8087 16.5501 14.1795 16.1793C14.5504 15.8085 15.0056 15.6812 15.9162 15.4268C18.1573 14.8005 20 14.3225 20 13C20 11.6775 18.1573 11.1995 15.9162 10.5732C15.0056 10.3188 14.5504 10.1915 14.1795 9.8207C13.8087 9.44986 13.6815 8.99456 13.427 8.08396Z"/><path d="M19.5 2.9V6.1M17.9 4.5H21.1"/>',
  ),
};

// What each provider is for, in one line. The descriptor carries the name,
// models, warning and links; the purpose is Keel's own wording.
const PURPOSE = {
  gemini: "Answers questions about the panes you pick and writes command suggestions. Keel sends your message and their redacted scrollback, nothing else.",
  jev: "Plain-language detection: notices when the line you are typing is plain language, so a suggestion can start by itself. That line is sent, redacted, and when you press ⇥ so are recent commands from the pane (unless scrollback suggestions are off) and snippets you marked for the agent.",
};

export const providerPurpose = (id) => PURPOSE[id] || "";

export const safeURL = (u) => (/^https:\/\//.test(String(u || "")) ? String(u) : "");

function linksHTML(d) {
  const out = [];
  const help = safeURL(d.keyHelpUrl);
  const terms = safeURL(d.termsUrl);
  if (help) out.push(`<a href="${escapeHtml(help)}" target="_blank" rel="noopener noreferrer" data-k-external>Get a key</a>`);
  if (terms) out.push(`<a href="${escapeHtml(terms)}" target="_blank" rel="noopener noreferrer" data-k-external>Terms</a>`);
  return out.join('<span aria-hidden="true"> · </span>');
}

export function warningHTML(d) {
  if (!d.warning) return "";
  return `<div class="k-agent-warning" role="note">${ICON.alert}<p>${escapeHtml(d.warning)}</p></div>`;
}

// keyFormHTML is the key field of a provider card (a div, not a <form>:
// the Settings page is one form already, and forms do not nest). Enter in
// the field and the button both save. It is shown when the provider
// is not connected, and when the user chose Change key. error is a line of
// plain text, "" for none.
export function keyFormHTML(d, { changing = false, error = "", busy = false } = {}) {
  const label = d.keyLabel || "API key";
  const links = linksHTML(d);
  return `<div class="k-agent-keyform" data-agent-keyform="${escapeHtml(d.id)}">
    ${warningHTML(d)}
    <label class="k-field"><span class="k-label">${escapeHtml(label)}</span>
      <input class="k-input k-agent-key" type="password" name="key" data-agent-key="${escapeHtml(d.id)}" aria-label="${escapeHtml(label)}" spellcheck="false" autocomplete="off" autocapitalize="off" placeholder="${changing ? "Paste the new key" : "Paste the key"}"${error ? ' aria-invalid="true"' : ""} />
    </label>
    <p class="k-agent-field-error" role="alert">${escapeHtml(error)}</p>
    <div class="k-agent-keyform-foot">
      <button type="button" class="k-btn k-btn--primary k-btn--sm" data-agent-save="${escapeHtml(d.id)}"${busy ? " disabled" : ""}>${changing ? "Save key" : "Connect"}</button>
      ${changing ? `<button type="button" class="k-btn k-btn--ghost k-btn--sm" data-agent-cancel="${escapeHtml(d.id)}">Cancel</button>` : ""}
      <span class="k-spacer"></span>
      <span class="k-help k-agent-links">${links}</span>
    </div>
  </div>`;
}

// providerCardHTML is one provider as a card (Settings · Agent). configured
// comes from GET /api/agent/settings keys[id].configured. ui is the
// controller's per-card state: { changing, error, busy }.
export function providerCardHTML(d, configured, ui = {}) {
  const id = escapeHtml(d.id);
  const name = d.kind === "detector" ? `${d.name}` : d.name;
  const role = d.kind === "detector" ? "Plain-language detection" : "Model";
  const badge = configured
    ? '<span class="k-badge k-badge--tide"><span class="k-dot k-dot--live" aria-hidden="true"></span>Connected</span>'
    : '<span class="k-badge k-badge--outline">Not connected</span>';
  const actions = configured
    ? `<div class="k-agent-card-actions">
        <button type="button" class="k-btn k-btn--sm" data-agent-change="${id}" aria-expanded="${ui.changing ? "true" : "false"}">Change key</button>
        <button type="button" class="k-btn k-btn--danger k-btn--sm" data-agent-disconnect="${id}">Disconnect</button>
      </div>`
    : "";
  const form = !configured || ui.changing ? keyFormHTML(d, { changing: configured, error: ui.error, busy: ui.busy }) : "";
  return `<div class="k-agent-card" role="group" aria-label="${escapeHtml(name)}" data-agent-card="${id}">
    <div class="k-agent-card-head">
      <span class="k-tile k-tile--sm" aria-hidden="true">${d.kind === "detector" ? ICON.lock : ICON.ai}</span>
      <div class="k-agent-card-title">
        <span class="k-agent-card-name">${escapeHtml(name)}<span class="k-agent-card-role">${escapeHtml(role)}</span></span>
        <span class="k-help">${escapeHtml(providerPurpose(d.id))}</span>
      </div>
      ${badge}
    </div>
    ${actions}
    ${form}
  </div>`;
}

export const DETECTION = { auto: "auto", hashOnly: "hash-only" };

// detectionNote says what "Notice automatically" does right now: it needs a
// Jev key, and without one the only lines noticed are the ones starting
// with # (docs/ui-redesign.md §7). "" when there is nothing to say.
export function detectionNote(settings, jevConfigured) {
  if (settings?.suggestions === "off") return "";
  if (settings?.detection === DETECTION.hashOnly) {
    return "Pressing ↵ by mistake is harmless: the shell treats a line that starts with # as a comment.";
  }
  if (!jevConfigured) {
    return "Jev is not connected, so Keel notices only lines that start with # for now. Connect Jev above to notice plain language automatically.";
  }
  return "";
}

export const SCOPES = [
  { value: "focused", label: "Focused pane only" },
  { value: "pick", label: "Panes I pick each time" },
  { value: "tab", label: "Every pane in the tab" },
];

export const HISTORY_CHOICES = [
  { value: "0", label: "Only while the panel is open" },
  { value: "7", label: "7 days" },
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
];

// historyValue maps the saved days onto the nearest choice that exists, so
// a value set another way still selects something.
export function historyValue(days) {
  const n = Number(days);
  if (!Number.isFinite(n) || n <= 0) return "0";
  const values = HISTORY_CHOICES.map((c) => Number(c.value)).filter((v) => v > 0);
  return String(values.reduce((best, v) => (Math.abs(v - n) < Math.abs(best - n) ? v : best), values[0]));
}

export const clampLines = (n) => Math.min(1000, Math.max(50, Math.round(Number(n) || 200)));

// The model choices of a model provider, with the saved model first when it
// is not in the list (a custom value set through the API).
export function modelChoices(d, current) {
  const models = (d?.models || []).slice();
  if (current && !models.includes(current)) models.unshift(current);
  return models;
}

// keyError turns a failed key save into a line for the card.
export function keyError(err) {
  if (err?.code === 423) return "The vault is locked. Unlock it, then try again.";
  return err?.message || "Could not save the key.";
}
