# Keel

Local SSH workspace: Go owns SSH and PTYs, the window is a Wails v3 webview on a loopback HTTP server, the UI is templ + shadcn-templ + Datastar, terminals are xterm.js. See README.md for build, run and shortcuts.

## Checks

Run before every PR: `make test`, `make lint`, `make check-generated` (committed `*_templ.go` and `cmd/keel/web/css/output.css` must match the pinned tools; run `make build` after changing `.templ` or CSS sources).

## UI work

The UI is being redesigned. For any UI change:

- Read `docs/ui-redesign.md` first. It holds the screens, the agent invariants (§6) and the phased build plan (§10). Work one phase per branch and PR.
- Tokens come only from `docs/design/tokens.json` (`tokens.css` is the same as CSS variables). Never add a color, size or radius literal; add or reuse a token.
- `docs/design/components.css` is the reference for `k-*` component styles and `docs/design/README.md` holds the usage rules. Copy from them; do not reinvent.
- Keep one token layer. shadcn-templ variables map onto the Keel tokens; do not add a second set of variables.
- Fonts ship locally as woff2. `components.css` imports Google Fonts for previews only; the app must not load fonts from the network.
- UI built as HTML strings in `cmd/keel/web/js/term.js` uses the same `k-*` classes as templ.
- Vault lists come from the Datastar stream (`internal/httpapi/uistream.go`, docs/ui-redesign.md §12): render a list in templ and let the stream push it, never build list HTML in JS. Vault dialogs post with `@post('/ui/forms/{kind}')` (`forms.go`). term.js JSON state refreshes on `keel-vault-changed`; do not call `loadAll` after a save.
- Agent features: the agent never writes to a PTY, never sees vault contents, and every read or insert is logged. Treat §6 as acceptance criteria.
