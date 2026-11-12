# Keel

Local SSH workspace: Go owns SSH and PTYs in a daemon (`keel serve`, a loopback HTTP/WebSocket API behind a token) and the window is native (`internal/native`: MyGo, pure Go, libghostty terminals). The daemon serves no UI. See CONTRIBUTING.md for build and checks, README.md plus `docs/*.md` for features, configuration and shortcuts, and `docs/native.md` for what the window is and does.

## Checks

Run before every PR: `make test`, `make lint` and `make native-e2e` (the window end to end under MyGo's tester against a scratch `keel serve`; PNGs in `.scratch-native/e2e/`). Run `make native-automation` too when a change touches the menu bar, the window's lifecycle or key handling. Set `KEEL_KEYRING=off` for every command you run from a script or an agent: it keeps the vault off the OS keychain.

## UI work

- Read `docs/native.md` first: it is the UI doc (what is built, how it behaves, how to drive it). `docs/ui-redesign.md` holds the screens, the agent invariants (§6) and the design intent; the native code cites its sections, and the webview it was written for is gone.
- Tokens come only from `docs/design/tokens.json`; `internal/native/tokens.go` mirrors it and `TestTokensMatchJSON` keeps them equal. Never add a colour, size or radius literal; add or reuse a token. `docs/design/README.md` holds the brand rules (voice, ground steps, type, depth).
- Fonts ship embedded as woff2 in `internal/native/fonts`; the app must not load fonts from the network.
- The daemon has no web UI and must not get one: lists and state come from its JSON API (`GET /api/...`; `GET /api/vault` reports a `rev` that changes with every vault or tunnel change, which the window polls). The only credential is the `X-Keel-Token` header.
- Agent features: the agent never writes to a PTY, never sees vault contents, and every read or insert is logged. Treat §6 as acceptance criteria. `internal/native/guard_test.go` holds the writers: a change to what can put bytes in a PTY changes that file's rules deliberately, never by accident.
