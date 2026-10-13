// Share-link parsing for term.js. Pure function: no DOM, no state.

// parseJoinLink accepts a full /join?room=… link, or a bare room id when
// fallbackOrigin (the configured signal server) is known.
export function parseJoinLink(input, fallbackOrigin) {
  const raw = String(input || "").trim();
  if (!raw) throw new Error("paste a Share link");
  try {
    const u = new URL(raw);
    const room = u.searchParams.get("room");
    if (!room) throw new Error("link has no room");
    return { origin: u.origin, room };
  } catch (err) {
    if (err.message === "link has no room") throw err;
    if (/^[0-9a-f]{8,}$/i.test(raw) && fallbackOrigin) {
      return { origin: String(fallbackOrigin).replace(/\/$/, ""), room: raw };
    }
    throw new Error("paste the full Share link");
  }
}
