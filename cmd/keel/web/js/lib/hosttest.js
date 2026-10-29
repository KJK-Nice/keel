// Test connection's results (§5.8), as the host dialog words them. No DOM:
// ui/hosttest.js shows them.

// keyTypeLabel is the short name ssh-keygen -l prints in brackets.
export function keyTypeLabel(type) {
  const t = String(type || "");
  if (t === "ssh-ed25519" || t.startsWith("sk-ssh-ed25519")) return "ED25519";
  if (t.startsWith("ecdsa-") || t.startsWith("sk-ecdsa-")) return "ECDSA";
  if (t === "ssh-rsa" || t.startsWith("rsa-")) return "RSA";
  if (t === "ssh-dss") return "DSA";
  return t.toUpperCase();
}

// hostTestText is what the dialog shows for a POST /api/hosts/test result:
// the state dot (state), its label (short) and the explanation (detail).
export function hostTestText(res) {
  const where = res.hop === "jump" ? `Jump host ${res.hopName}: ` : res.hop === "target" ? `Through the jump host: ` : "";
  switch (res.kind) {
    case "ok":
      return { state: "live", short: `Connected in ${res.latencyMs} ms`, detail: "" };
    case "auth":
      return { state: "dead", short: "Authentication failed", detail: `${where}the server refused the password or key. ${res.message}` };
    case "unknown-host":
      return {
        state: "connecting",
        short: "Host key unknown",
        detail: `${where}${res.hostname} presented a key Keel has not seen. Compare it with the server's own fingerprint before you trust it.`,
      };
    case "host-key-mismatch":
      return {
        state: "dead",
        short: "Host key changed",
        detail: `${where}${res.hostname} presented a different key than the one saved. Only replace it if you know the server was reinstalled.`,
      };
    case "timeout":
      return { state: "dead", short: "Timed out", detail: `${where}no answer within 10 seconds. ${res.message}` };
    case "dns":
      return { state: "dead", short: "Host not found", detail: `${where}the name does not resolve. ${res.message}` };
    case "refused":
      return { state: "dead", short: "Connection refused", detail: `${where}nothing accepts SSH on that port. ${res.message}` };
    case "cancelled":
      return { state: "", short: "Test cancelled", detail: "" };
    default:
      return { state: "dead", short: "Could not connect", detail: `${where}${res.message}` };
  }
}

// hostKeyOffered reports whether a result is a host key the user may
// trust: an unknown or changed key with its fingerprint.
const hostKeyOffered = (res) => !!(res && (res.kind === "unknown-host" || res.kind === "host-key-mismatch") && res.fingerprint);

// fingerprintText is the offered key as the dialog shows it, or "".
export function fingerprintText(res) {
  if (!hostKeyOffered(res)) return "";
  const type = res.keyType ? `${keyTypeLabel(res.keyType)} ` : "";
  return `${type}${res.fingerprint}${res.expected ? ` (saved: ${res.expected})` : ""}`;
}

// trustOffer is what Trust and save sends for a result, exactly as the
// server saw the key (its address:port, fingerprint and offer token), or
// null when there is nothing to trust. replace is a changed key.
export function trustOffer(res) {
  if (!hostKeyOffered(res) || !res.trustToken) return null;
  return { token: res.trustToken, hostname: res.hostname, fingerprint: res.fingerprint, replace: res.kind === "host-key-mismatch" };
}
