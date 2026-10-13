---
name: verify-keel
description: Launch, check, drive, prove, and bug bash Keel's UI (the Vault, terminal tabs, command palette) with e2e, against an isolated `keel serve` in WebKit (primary) or Chromium. Use before claiming a Keel UI or httpapi change works, when asked to verify or screenshot a Keel screen, or to bug bash Keel.
---

# Verify Keel

Read `.agents/skills/e2e/SKILL.md` first. This skill only says what is true
of Keel. The config is `e2e.config.ts` (two targets sharing one app:
`webkit`, the primary, because the macOS app is a WKWebView, and `chromium`,
a stand-in for Windows WebView2; tests `tests/**/*.e2e.ts`). Use `--target
webkit` for proofs and screenshots, and run both targets before claiming a
change works; shared helpers are in `tests/keel.ts`; the bug bash
uses `e2e.bugbash.config.ts`.

What is driven: the desktop app is a Wails webview pointed at `keel serve`'s
loopback HTTP server, and the same page loads in a browser. e2e drives that
page in headless Playwright WebKit or Chromium against a `keel serve` it starts itself. The
native window (the system WKWebView, which differs from Playwright's WebKit build; the macOS menu, ⌘-shortcuts Wails owns, the dock) is
not covered; say so when a change lives there.

## Launch

Build first, every time Go, `.templ`, JS, or CSS changed: `make build`. The
web assets are embedded in `bin/keel`, so a stale binary serves a stale UI
and tests pass against old code. `/api/health` reports the `build` id.

The target's `app.command` runs `./bin/keel serve` with:

- `KEEL_ADDR=127.0.0.1:{port}`: port `0` in `app.url`, so every run gets a
  free port and never touches the user's Keel on `127.0.0.1:47241`.
- `KEEL_DATA=.e2e/keel-data` (or `$KEEL_E2E_DATA`): a scratch `keel.json`,
  `keel.vault`, `keel.log`, and token. keel serve locks this dir, so two runs
  at once need two dirs.
- `KEEL_VAULT_PASSPHRASE` (scratch value, `$KEEL_E2E_VAULT_PASSPHRASE`
  overrides): never drop it. Without it keel serve reads, and on a fresh
  dir creates, the user's real `keel`/`vault` item in the OS keychain.
- `SHELL=/bin/sh`: Local Shell gets a plain `sh-3.2$` prompt, not the
  user's shell config.

The runner waits for `/api/health`, then stops the process after the run.
Server output goes to `.e2e/logs/keel-serve.log`. Nothing else to start: a
fresh data dir seeds the Local Shell host and four snippets.

Auth: keel serve requires a token on `/`, `/api/`, `/ws/`, `/ui/`. The config
calls `keelToken()` at load, which writes `keel-serve.token` into the data
dir before keel serve starts and registers it as the `keel-token` secret, so
traces show `<secret:keel-token>`. Every test opens the UI with
`openKeel(app)`, which loads `/?keel_token=…&name=e2e`; the server trades it
for a cookie and redirects to `/?name=e2e`. `app.open('/')` alone is a 401
page ("open this window from the Keel app").

## Doctor

```bash
make build                          # only if bin/keel is older than your change
npx e2e list                        # loads the config, starts nothing; exit 0, each test listed for webkit and chromium
npx e2e run tests/smoke.e2e.ts --target webkit  # starts keel serve, opens the Vault, pins New host + Search hosts; exit 0
```

`list` failing is the config or `npm install`. `run` failing with
`APP_UNREACHABLE` is keel serve dying on boot: read
`.e2e/logs/keel-serve.log` (a held lock means another run, or a keel serve
you started, uses the same `KEEL_DATA`). `LOCATOR_NOT_FOUND` on `New host` is
a 401 page (token) or a stale binary. Run Doctor first whenever anything
looks off. Every page load logs one `SyntaxError: Unexpected token ')'` in both engines (see
Gotchas); that alone is not a Doctor failure.

## Drive

Live, over the registered `e2e mcp` server (`.mcp.json`): `open_session
{config: "e2e.bugbash.config.ts"}`, which authenticates the browser up front
(below) so the first observation is the Vault; on `e2e.config.ts` it is the
401 page. Then `observe`, the verbs, and `locate` before writing any locator
(topic `mcp`). The catalog offers no project tools and no `type_secret`: the
config declares no credentials. A session and `npx e2e run` on the same
`KEEL_DATA` cannot both hold its lock: close the session first.

Scripted: a test under `tests/`, importing `openKeel`, `keelApi`, `keelJson`
from `./keel.ts`. Use `screen` for every control in the feature map; all
the Vault's controls have accessible names. Use `agent.act` only for a flow
that varies; it needs `AI_GATEWAY_API_KEY` (the config's model is
`gateway('openai/gpt-6-luna-fast')`). Handles that hold:

- Vault sections are buttons: `getByRole('button', 'Hosts' | 'Keychain' |
  'Port forwarding' | 'Snippets' | 'Notebooks' | 'Logs' | 'Settings')`.
- Modals (`New host`, `New identity`, `New snippet`) are `<dialog>`s with
  no accessible name: `screen.getByRole('dialog').filter({ hasText: 'New
  host' })`, fields by label inside it.
- Every card's menu button is just `Actions`: narrow the list with the
  page's search first, then `getByRole('button', 'Actions', { visible: true
  })`. Confirm dialogs are `alertdialog` with a `Remove` button.
- The terminal is `getByRole('textbox', 'Terminal input')`; its output is
  painted with WebGL and is not in the DOM. Read it from
  `keelApi(app.baseUrl, '/api/sessions/<id>/scrollback')`.

No signed-in sessions: the token cookie is the only auth, set per test by
`openKeel`.

## Evidence

Every run writes `.e2e/report.json`. A failed test gets
`.e2e/results/<test>/trace.md` with the screen tree, the app log, and a
screenshot; `--trace on` writes it for passing tests too; `--video` records
each attempt; `app.screenshot('<name>')` saves a named frame (the hosts and
terminal tests save `host-added` and `terminal-output`). The next run clears
`.e2e/results/`, Doctor included, so a proof that must outlive it runs with
`--output .e2e/proof/<name>`.

Visual-only changes (tokens, CSS, fonts): `tests/visual.e2e.ts` saves
`<theme>-hosts`, `-new-host`, `-snippets`, `-settings`, `-terminal`, and
`-palette` in dark and light. Run it with `--target webkit` on the base build with `--output
.e2e/proof/<name>-before`, then `make build` with the change and run it with
`--output .e2e/proof/<name>-after`; compare the pairs by name. It sets the
theme through `localStorage['keel-theme']`, as the Settings select does.

A proof drives the page the way a user does: the Vault section button, the
labelled field, the Save button, the card's menu, and captures the action
and the state after it, not only the final screen. Check side effects in
keel serve, read-only, with `keelApi` (the `X-Keel-Token` header, like
native clients): `/api/hosts`, `/api/identities`, `/api/snippets`,
`/api/notebooks`, `/api/sessions`, `/api/sessions/<id>/scrollback`,
`/api/logs`. On disk, `keelJson()` is `.e2e/keel-data/keel.json`; secrets
must be absent from it and live only in the encrypted `keel.vault` (the
hosts test pins this). Never drive the UI through the API or a POST; that
is not the user path.

## Bug bash

`e2e.bugbash.config.ts` spreads `e2e.config.ts`, adds
`tests/bugbash/**` to the glob, and replaces the engine (keeping each target's browser) with `web({ browser, headers:
{ 'X-Keel-Token': keelToken() }, initScripts: [<sets the keel_auth_<port>
cookie to the token>] })`: explore and MCP open the bare app URL, which is
a 401 without auth, and WebSockets (`/ws/session`, `/ws/herd`) carry no
extra headers, hence the cookie. It declares `default`, `skeptic`, and
`fuzzer` personas on `gateway('openai/gpt-6-luna-fast')` with 40 steps and
model calls. Its `context` says what local Keel cannot do: no reachable SSH
server (any host but Local Shell fails to connect), no signal server (Share
and Join do not work), no destructive terminal commands, the WebGL terminal
is invisible to the tree. Set `AI_GATEWAY_API_KEY` first.

The charters are in [features/](features/), under each feature's `Charters`
H2, in the `slug|target|agent|charter` format topic `bug-bash` step 3 reads.
No accounts to seed. Isolation: port `0` gives each explorer its own keel
serve, but each also needs its own data dir, so the fan-out sets
`KEEL_E2E_DATA=.e2e/bugbash-data/$1` per charter:

```bash
... | xargs -0 -n 4 -P 4 sh -c \
  'KEEL_E2E_DATA=".e2e/bugbash-data/$1" npx e2e explore "$4" --config e2e.bugbash.config.ts --target "$2" --agent "$3" --output ".e2e/bugbash/$1" --max-steps 6 --video --reporter list,markdown < /dev/null > ".e2e/bugbash/$1.log" 2>&1' _
```

Explorers start on the Vault (the header and cookie above): begin each
charter with "Starting at the Vault (the app opens there),". `.e2e/bugbash/`, `.e2e/bugbash-data/`,
`.e2e/proof/`, and `.e2e/keel-data/` are in `.gitignore`. A finding is a bug
once `tests/bugbash/<slug>.e2e.ts` fails with `ASSERTION_FAILED` on the
assertion that encodes it; anything else is the explorer's claim.

## Cleanup

The runner stops the keel serve it started; `close_session` stops the one a
session started. A keel serve you started by hand is yours to stop with
`KEEL_ADDR=<addr> KEEL_DATA=<dir> ./bin/keel serve stop`, never `pkill
keel` (that kills the user's own Keel and its PTYs). Then:

- `rm -rf .e2e/keel-data` resets the Vault to its seed state. Runs share it,
  so a test that failed midway leaves its host or snippet behind; tests use
  unique names and do not depend on an empty list.
- `rm -rf .e2e/bugbash-data/<slug>` per charter after the bash, and
  `.e2e/bugbash/<slug>/` for a charter whose findings were all rejected.

Keep `.e2e/results/`, `.e2e/proof/`, `.e2e/report.json`, and the videos;
that is the proof, and all of it is gitignored.

## Helpers

`tests/keel.ts`: `keelData`, `keelToken()`, `openKeel(app)`,
`keelApi(baseUrl, path)`, `keelJson()`. No scripts; the e2e CLI is the helper.

PR before/after screenshots: see `.agents/skills/pr-screenshots/SKILL.md`.
