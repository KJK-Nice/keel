// Test connection in the host dialog (§5.8): a real connect with the
// dialog's unsaved values, then close. Typed secrets go only to POST
// /api/hosts/test. A refused host key can be trusted from the result; any
// edit to the form drops the result first, so Trust and save never applies
// to values the test did not use. The caller passes:
//   doc                     the document holding the dialog's #host-test* elements
//   api(path, opts)         the JSON API call
//   draft()                 the dialog's values, or null while it has no form
//   confirmReplace(offer)   asks before a changed key replaces the saved one
//   trusted()               after a key was saved (e.g. show the saved key)
//   toast(message, error)
import { fingerprintText, hostTestText, trustOffer } from "../lib/hosttest.js";

export function createHostTest({ doc = document, api, draft, confirmReplace, trusted = () => {}, toast }) {
  let running = null;
  // offer is the key the last test was refused for (lib/hosttest.js
  // trustOffer); body is the dialog's values the last test sent.
  let offer = null;
  let body = "";

  const el = (id) => doc.getElementById(id);

  function setState(kind, text) {
    const wrap = el("host-test-state");
    const dot = el("host-test-dot");
    const label = el("host-test-text");
    if (!wrap) return;
    wrap.hidden = !text;
    wrap.className = kind ? `k-state k-state--${kind}` : "k-state";
    if (dot) dot.className = kind ? `k-dot k-dot--${kind}` : "k-dot";
    if (label) label.textContent = text;
  }

  function showDetail(res, message) {
    const box = el("host-test-detail");
    const msg = el("host-test-message");
    const fp = el("host-test-fp");
    const actions = el("host-test-actions");
    const trust = el("host-test-trust");
    if (!box) return;
    box.hidden = !message;
    if (msg) msg.textContent = message || "";
    if (fp) {
      fp.textContent = fingerprintText(res);
      fp.hidden = !fp.textContent;
    }
    offer = trustOffer(res);
    if (actions) actions.hidden = !offer;
    if (trust) trust.textContent = res?.kind === "host-key-mismatch" ? "Replace saved key" : "Trust and save";
  }

  // reset cancels a test under way and clears its result.
  function reset() {
    running?.abort();
    running = null;
    body = "";
    const btn = el("host-test");
    if (btn) btn.textContent = "Test connection";
    setState("", "");
    showDetail(null);
  }

  // edited drops the result once the dialog's values differ from what the
  // test used (events that change nothing, like a change on blur, keep it).
  function edited() {
    if (!running && !offer && el("host-test-state")?.hidden) return;
    const values = draft();
    if (values && JSON.stringify(values) === body) return;
    reset();
  }

  // run starts a test, or cancels the one under way.
  async function run() {
    const btn = el("host-test");
    if (running) {
      running.abort();
      return;
    }
    const ctrl = new AbortController();
    running = ctrl;
    if (btn) btn.textContent = "Cancel test";
    setState("connecting", "Testing…");
    showDetail(null);
    const sent = JSON.stringify(draft());
    body = sent;
    try {
      const res = await api("/api/hosts/test", { method: "POST", body: sent, signal: ctrl.signal });
      // The dialog was refilled or closed meanwhile: drop the result.
      if (running !== ctrl) return;
      const out = hostTestText(res);
      setState(out.state, out.short);
      showDetail(res, out.detail);
    } catch (err) {
      if (running !== ctrl) return;
      if (ctrl.signal.aborted) {
        setState("", "Test cancelled");
      } else {
        setState("dead", "Could not test");
        showDetail(null, err.message);
      }
    } finally {
      if (running === ctrl) {
        running = null;
        if (btn) btn.textContent = "Test connection";
      }
    }
  }

  // trust saves the key the last test was refused for (asking first when
  // it replaces a saved one), then tests again.
  async function trust() {
    const current = offer;
    if (!current) return;
    const { token, hostname, fingerprint, replace } = current;
    if (replace && !(await confirmReplace(current))) return;
    try {
      await api("/api/known-hosts", {
        method: "POST",
        body: JSON.stringify({ token, hostname, fingerprint, replace }),
      });
      toast("Host key saved");
      trusted();
      await run();
    } catch (err) {
      toast(err.message, true);
    }
  }

  return {
    run,
    reset,
    edited,
    trust,
    // abort stops a test under way (the dialog closed); its result is dropped.
    abort: () => running?.abort(),
  };
}
