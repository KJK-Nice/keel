// Share-link parsing for term.js. Pure function: no DOM, no state.

// parseJoinLink reads a /join?room=…#k=… link. The #k secret never reaches
// the signal server; it authenticates the host's WebRTC peer (sharemac.js),
// so a link without one is refused rather than joined unverified.
export function parseJoinLink(input) {
  const raw = String(input || "").trim();
  if (!raw) throw new Error("paste a Share link");
  let u;
  try {
    u = new URL(raw);
  } catch {
    throw new Error("paste the full Share link");
  }
  const room = u.searchParams.get("room");
  if (!room) throw new Error("link has no room");
  const key = new URLSearchParams(u.hash.replace(/^#/, "")).get("k") || "";
  if (!/^[0-9a-f]{32}$/.test(key)) {
    throw new Error("link has no key (#k=…): ask the host for a new Share link");
  }
  return { origin: u.origin, room, key };
}
