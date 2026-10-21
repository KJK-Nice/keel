// openExternal opens a link from untrusted text (an agent answer) in the
// system browser, never in the Keel page itself.
//
// In the native window the page has no Wails runtime and WKWebView does
// nothing with target=_blank, so the page posts "keel:open:<url>" on the
// message handler the window already listens on (internal/desktop/
// openurl.go), which checks the URL again and hands it to the OS. In a
// plain browser tab (keel serve opened by hand) a target=_blank anchor
// with noopener does the same.
//
// Only http:, https: and mailto: are opened (lib/markdown.js safeLinkURL);
// anything else returns false and opens nothing.

import { safeLinkURL } from "./markdown.js";

export const OPEN_PREFIX = "keel:open:";

// nativeChannel returns the window's message poster, or null in a browser.
export function nativeChannel(win) {
  if (win?.document?.documentElement?.dataset?.native !== "1") return null;
  const wk = win.webkit?.messageHandlers?.external;
  if (wk?.postMessage) return (msg) => wk.postMessage(msg);
  const cw = win.chrome?.webview;
  if (cw?.postMessage) return (msg) => cw.postMessage(msg);
  return null;
}

export function openExternal(raw, win = globalThis.window) {
  const url = safeLinkURL(raw);
  if (!url || !win) return false;
  const post = nativeChannel(win);
  if (post) {
    post(OPEN_PREFIX + url);
    return true;
  }
  const doc = win.document;
  const a = doc.createElement("a");
  a.href = url;
  a.target = "_blank";
  a.rel = "noopener noreferrer";
  doc.body.appendChild(a);
  a.click();
  a.remove();
  return true;
}
