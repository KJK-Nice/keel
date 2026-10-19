// Does a keel-vault-changed push need term.js to reload its JSON state?
// No DOM, no timers.
//
// Every list push on /ui/stream dispatches keel-vault-changed, and term.js
// answers with loadAll. Boot runs one loadAll itself before it opens the
// stream (it needs the state to restore the workspace), so the stream's
// first push would load the same state again (issue #54).
//
// keel serve names its state with a rev (GET /api/vault, internal/httpapi
// uistream.go): it moves with every store save and tunnel change. Boot
// reads the rev just before its loadAll, and a stream's first push carries
// the rev the stream opened at. When the two match, nothing changed in
// between, so that push is skipped. Only the first push after boot is
// checked: a later one (a save, or a reconnect after sleep or a keel serve
// restart) always reloads.
export function createStateFollow() {
  let bootRev = "";
  return {
    // loaded records the rev read just before boot's loadAll.
    loaded(rev) {
      bootRev = typeof rev === "string" ? rev : "";
    },
    // shouldRefresh takes the event's detail and says whether to loadAll.
    shouldRefresh(detail) {
      const rev = bootRev;
      bootRev = "";
      return !(rev && detail?.rev === rev);
    },
  };
}
