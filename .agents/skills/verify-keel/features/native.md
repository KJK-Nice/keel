# Native client (keel-native)

The MyGo window in `internal/native`, a second client of `keel serve`
(`docs/native.md`). It is verified with Go tests, not with e2e or a browser.

## What is driven

`keel-native`'s real view, real terminal view (libghostty) and real client,
under MyGo's `ui.Tester`, which renders the view in memory and sends it
pointer and keyboard input (`Click`, `Key`, `Type`, `Press`/`Move`/`Release`,
`Image`). Each test starts its own scratch `keel serve` from `bin/keel`: a
free loopback port, data in `.scratch-native/e2e/<test>/data`,
`KEEL_KEYRING=off`, a scratch vault passphrase, `SHELL=/bin/sh`. It stops it
by the handle it started, never by name. Effects are checked on the server
(`Sessions()`, `/api/sessions/<id>/scrollback`), not only in the model.

## How to run

```bash
make native-e2e    # builds bin/keel, bin/keel-sshtest and bin/keel-signal, then go test -tags nativee2e ... TestE2E
go test -tags nativee2e -count=1 -run TestE2ESplitAndFocus ./internal/native/
```

The second form needs `bin/keel`: run `make native-e2e` once, and again after
any change to `keel serve`. The first run fetches libghostty-vt into the user
cache. A native change is working only when `make native-e2e` and
`go test -count=1 ./internal/native/...` both pass.

## Translucency

Checked by pixel tests, not by eye: `TestGroundsFollowTheWindowAlphaWithoutStacking`
(`internal/native/workspace_test.go`) reads rendered pixels and asserts the
ground alpha equals the window alpha, so a tint painted twice fails. Try other
values with `KEEL_NATIVE_OPACITY` (30-100) and `KEEL_NATIVE_BLUR` (0-64) or the
`keel-native.json` prefs file in the data dir. The blur and the real
see-through window are person-only checks.

## Themes, UI size and the look

Both themes are tested by pixels: `theme_test.go` sets the choice (`setTheme`)
and reads the title bar, sidebar, a tile, the palette, Unlock and First run
against `lightHex` and `darkHex`; `system` follows `ui.Tester.SetDark`;
`TestTokensMatchJSON` checks both sets; `TestNoHexColourOutsideTokens` keeps
hex out of views. `uisize_test.go` checks UI size scales rem sizes, not px ones;
`TestSplitsTileTheStage` and `assertFits` check that splits fit the window.
Check a new view in both themes (`TestE2EThemeSwitch` writes the `-light` shots).

**Look at the reference image next to the native shot before claiming a screen
matches**: open `docs/images/<screen>.png` and `.scratch-native/e2e/<Test>.png`
with the image reader and list every difference (layout, spacing, size, colour,
icon, wording, missing elements). Read the app's templ and `keel.css` too: the
images are older than the code. Record it in "Compared with the app" in
`docs/native.md`: fixed, known gap (phase) or needs a decision.

## The bundle

`make native-app`, then each as its own command: `"bin/Keel Native.app/Contents/MacOS/keel-native" --library-path`
prints a path inside the bundle; `codesign --verify --deep --strict "bin/Keel Native.app"`
exits 0; `otool -L "bin/Keel Native.app/Contents/MacOS/keel-native" | grep -c libghostty`
prints 0; with a scratch daemon, `env -u MYGO_GHOSTTY_VT KEEL_NATIVE_CAPTURE=.scratch-native/app.png`
and the bundled binary exits 0 and the PNG shows a prompt. Opening it from Finder is person-only.

## Evidence

`.scratch-native/e2e/<Test>.png` (one per test) and the daemon log
`.scratch-native/e2e/<Test>/serve.log`. Look at the PNGs before claiming a
visual change works, next to `docs/images/hero-dark.png` or the matching
webview screenshot. The tree is git-ignored.

## Adding a scenario

In `internal/native/e2e_test.go` (build tag `nativee2e`, test files only):

- `startServe(t, passphrase)` starts the daemon; `newWin(t, addr, dir, wrap)`
  opens a window on it. Never share a daemon between tests.
- Everything touching the workspace or the Tester runs on the test goroutine.
  Background work reaches it through `pump(cond, what)`, which runs posted
  work and frames until `cond` holds. No sleeps outside `pump` and `settle`.
- Type only through `tt.Type` and `tt.Key`, as a person does; never add a
  function to non-test code that writes into a session.
- Assert on server state where the effect is on the server. End with
  `shot(name)`.

## The palette and the New tab page

- ⌘K: `e.key(ui.Cmd, ui.KeyK)`, then `pump` on `e.w.pal != nil && e.tt.Focused("Command palette")`. Type with
  `e.tt.Type`; ↵ is `e.key(0, ui.KeyEnter)`, ⌥↵ is `e.key(ui.Alt, ui.KeyEnter)`, esc is `ui.KeyEscape`. Rows are
  labelled `palette row <title>`; `e.w.pal.visible()` is the rows in order. Window keys wait while it is open.
- Prove "Open now starts nothing" on the server: `sorted(e.sessionIDs())` before and after, then `settle`. Prove
  "typing reaches no session" by comparing every session's `scrollback` before and after.
- The page (an empty window, ⌘T, `+`): label `start page`, cards `start card <name>`, closed rows `reopen <name>`,
  ⌥1-⌥6 are `e.key(ui.Alt, ui.Key1)`. ⌘T twice within 300 ms is one tab (a key repeat): use `e.click("New tab")`
  for the second. `e.newSession()` starts the Local Shell from a New tab.
- A pasted command: `e.tt.Type("ssh -p … user@127.0.0.1 uptime")` shows the offer; check `Sessions()` is 0 and
  `sshProcesses(t)` did not change. Connect once opens `dialog Quick connect` (fields `Target`, `Password`): type the
  fixture's password with `e.tt.Type(f.password)` only; never print it. Then the host key dialog as below.

## SSH, host keys and two windows

- `bin/keel-sshtest -out <file>` (built by `make native-e2e`) runs a throwaway SSH server on loopback.
  `startSSH(t, addr, dataDir)` runs it, stops it by its handle, and creates a password host for it with
  `POST /api/hosts` (`noAutoReconnect: true`, so a dropped server stays failed). The client never creates
  hosts. Never print the file or the request body: they hold the throwaway password.
- A first connection stops at "Unknown host key". The test clicks "Trust and connect" in the dialog, as a
  person does (`connectSSH`); it never calls `/api/known-hosts` itself. A `trustToken` is a one-time
  capability: never log it, never put it in a failure message.
- Two windows on one daemon: call `newWin` twice with the same `addr` and `dir`, and drive both with
  `pumpAll(t, []*win{a, b}, cond, what)` (each window's posted work runs on the test goroutine).
- keel serve lets only the session's owner take control, and the window that opened a session owns it. A
  second window is a follower with "Ask for control"; `TestE2EFollowerAndTakeControl` has the owner pass
  control with `GrantControl` (a control frame, never a PTY write), then take it back with "Take control".

## Menus, tab drags and the saved workspace

- Menus: `tt.RightClick("pane bar 1")` (a pane bar), `tt.RightClickAt(x, y)` (a tab pill: equal names are
  equally wide, so `tabRect(i)` offsets the first), then `tt.Menu()` lists the labels (`-` is a separator)
  and `tt.ChooseMenuItem("Close pane")` picks one; a disabled item returns an error, which is how the
  tab menu's disabled items are tested. The More tool is `tt.Click("More pane actions")`.
- Reorder: `Press`, two `Move`s, `Release` from one pill to another's left or right half.
- The saved workspace: close the window with `w.closeWindow()` (it writes the file), then `newWin` again on
  the same `addr` and `dir`: the second window restores from `<dir>/keel-native-workspace.json`. Compare
  trees by session id (`structure`), never by local pane id. `newWin` sets the data dir; a test that
  must not restore uses a fresh `startServe`.
- Find and text size: the find bar's count comes from the terminal (`p.search.status()`); the bar makes
  the terminal a row shorter, so `sh` redraws its prompt: compare scrollbacks through the `redraw`
  replacer. Text size shows on the server as the session's `cols`.

## Unlock and the host dialog

- A locked daemon: `scratchDir(t)`, then `startServeAt(t, dir, "serve", true)` creates the vault with
  `KEEL_VAULT_PASSPHRASE` (the constant `e2ePass`; a vault made from the environment has no first run, and
  `POST /api/first-run/done` is harmless), `stop()`, then
  `startServeAt(t, dir, "serve-locked", false)` on the same data stays locked. `TestE2EUnlock` types the
  passphrase with `e.tt.Type` and ↵ only; no test prints it, and the serve logs never hold it
  (`grep -rn native-e2e-scratch .scratch-native/e2e --include='*.log'` finds none).
- The host dialog: `startSSHOnly(t, dir)` runs the fixture without saving a host. Click a field's label to give
  the field the keyboard (`e.fillField("Hostname / IP", …)`; the password field is `Host password`), pickers are
  `Folder picker`, `Jump host picker`, `Identity picker`. Type the fixture's password only through
  `e.fillField`; check `e.texts()` does not contain it and `GET /api/hosts` never returns it.
- The host list poll: `e.w.startPoll(200*time.Millisecond)` runs Run's poll faster; create or delete a host
  with `e.raw` and `pump` until the tree follows. Close with `e.w.closeWindow()`.

## First run, Keychain and Logs (`e2e_pages_test.go`)

- First run needs a daemon with no vault and no passphrase: `startFirstRunServe(t)` starts one with
  `KEEL_SSH_DIR` at `sshFixtureDir(t, dir)`, a copy of `tests/fixtures/ssh-home/.ssh` with two fresh keys made in
  Go (one with a passphrase), as `scripts/e2e-serve.sh` does. Never the real `~/.ssh` or the OS keychain; the
  harness runs `KEEL_KEYRING=off`, so only the passphrase vault is machine-tested (the keychain vault is a person's check).
- Type the vault passphrase through `e.fillField("Passphrase", …)` (the scratch values are the constants in
  `e2e_pages_test.go`); the import's key passphrase is the field `Passphrase for <key>`. `noSecretDrawn` checks the
  window's text. A step is a scrolling page: use `newWinH(…, 1500)` to read a long one, or `shot` at 800.
- Keychain: the identity dialog's fields are `Identity name`, `Identity username`, `Identity password` (the plain
  names also appear on the page behind it, so a click by that name would land there). Used by N hosts is read from
  the saved hosts; the fixture's password is the identity's password and is never written to a file
  (`startSSHOnly` deletes `sshtest.json` after reading it).
- Pages: `e.click("Keychain")` or `"Logs"` (the sidebar footer), tabs are `tab Keychain`; `e.w.pageTab("logs")`.

## Snippets and Approve before type (`e2e_snippets_test.go`)

- "Nothing typed before Type" is asserted on the session's scrollback (`e.scrollback(sid)`): open the dialog
  (`e.click("Type into pane…")`, `e.waitApprove()`), `e.settle(time.Second)` (it pumps frames; the one allowed
  wait), then require the scrollback lacks the command's marker. Use `echo name-$((40+2))`: the echoed command
  line holds `$((40+2))`, only the output holds `name-42`; "name-" before Type means a byte got through.
- After Type the bytes are Ctrl-U plus the command and a newline (keel serve adds nothing), so the shell runs it
  without an Enter: wait for the output with `e.waitScrollback`. `TestE2ETypeClearsTheLineFirst` types `garbage`
  without Enter first and requires no `garbage echo` line.
- A new vault holds four seeded snippets (list files, disk, memory, top cpu): look a snippet up by name
  (`e.snippetNamed`, `e.inWindow`), never by count or position; `e.addSnippet` POSTs one.
- Labels: the page's rows are `snippet <name>`, the sidebar's `sidebar snippet <name>`, the dialog's fields
  `Snippet dialog name` / `Snippet dialog command`, the page's editor `Snippet name` / `Snippet commands`. A
  follower's dialog says "You are following — take or request control to type"; `Type` does nothing.
- **One-click type** (`e2e_direct_test.go`, plan 017): a one-line read-only snippet (the seeded `disk`, `df -h`) from the palette
  (`e.typeFromPalette("disk")`) types with no dialog and no Enter. "Typed but not run" is asserted on the scrollback: wait for
  the command text (`df -h` appears on the prompt line), `e.settle(time.Second)`, then require `Filesystem` (the first line `df`
  prints) is absent and `e.noDialog(...)`; the shot `…-typed` is taken here, before Enter. Then `e.pressEnterInPane()` (it waits
  for the review focus to come back) and `e.waitScrollback(sid, "Filesystem")`. The log row is `Kind` `snippet` / `agent.type` with
  `Source` `direct` (`e.directLogged`). A snippet outside the grammar or on two lines opens the dialog (`badge Approve first`); `less`
  on a file the test made (`LESS= less <file>`) puts the pane on the alternate screen (`e.w.focusedPane().screen()`), and the snippet
  then opens the dialog and `df -h` never reaches the pane. The fake agent cannot propose a grammar command: Edit the card into
  `df -h` (keel serve rates the edit again) and the button reads `Type into pane` with no "…". A follower gets the dialog's refusal.
  The pane's prompt line is not cleared (no Ctrl-U): `# ` typed first, then the snippet, reads `# df -h`.
- `guard_test.go` keeps two writers, one file and one caller each (`-run 'TestOnly|TestApprovedInject|TestDirect|TestTypeNow|TestNoControl|TestAgentAnd|TestConnWrite'`).

## Forwards, Panes, Settings and toasts (`e2e_n5d_test.go`)

- A forward needs a daemon, a host whose SSH server allows forwarding and a target. `startSSHOnly` runs the fixture and
  `addJumpHost` saves its **jump** server (the target refuses direct-tcpip) as `e2e-jump`. Every port is on 127.0.0.1 and from
  `freePort(t)` or `net.Listen("127.0.0.1:0")`: the forward's local port, the test's `startPong` listener (it answers
  `pong-<n>`) and the daemon. Never a wildcard bind, never 47241, 47298 or 47299. Trust the key first by opening the host and
  clicking "Trust and connect", then close the pane. Prove it on the wire: `dialPong(local)` answers while on and is refused off.
- Page and rows: `e.openForwardsPage()`; labels `forward <name>`, `<name> on` (the switch), `sidebar forward <name>`,
  `status forwards`, the dialog's fields `Forward name`, `Forward host picker`, `Bind port`, `Dest port`. The sidebar section and
  the page both hold a `<name> on` switch: a click takes the first (the sidebar's). A failed start also shows the cause in a toast.
- Panes: `Panes 3` is the segment, `Focus pane 2, <host>` a map cell, `pane row <host>` a row (rows of one host share the
  label: tell them apart by position, `paneRowHeight+space1`). Clicking into a body shows Panes; a split does not.
- Settings: ⌘, opens it; `settings nav <Section>`, sliders `<Name> slider` (a click puts one in the middle, the arrows step it),
  `Copy on select switch`, `Display name input`, `Signal server input`, `Stop runtime…` then the dialog's `Stop runtime`. Window
  opacity needs a translucent window: `e.useSavedWindowPrefs()` does what `Run` does. Read the title bar's alpha from `tt.Image()`.
  Only ever stop the test's own daemon.
- Toasts: label `toast <text>`; `e.tt.Advance(toastTimeout+time.Second)` expires them (the clock is the Tester's, not real time).

## Share and Join (`e2e_share_test.go`)

- Two daemons, as two machines: `newShareE2E(t, true)` starts `bin/keel-signal` on a free loopback port (`make native-e2e`
  builds it), the owner's `keel serve` with `KEEL_SIGNAL` pointing at it, and the joiner's own `keel serve` and data dir.
  Window A is the owner, B joins. A `pbcopy` that does nothing leads the owner's `PATH` (keel serve copies a new link to
  the system pasteboard; a test must not).
- **Off-machine traffic.** Every joiner peer is made by `win.joiner` with an empty ICE list and loopback candidates
  (`noOffMachine` asserts both); the signal server is the test's own. The single request that leaves the machine is
  the STUN binding keel serve's own share hub makes after Allow (`internal/share/hub.go` `acceptPeer`; the maintainer
  allowed it, plan 014 amendment 1). Never a public signal server, never a STUN server in a joiner peer.
- Allow scenarios: `r.admitAndWait(t)` presses Allow and waits (30 s, `pump30`) until B is a follower and A has two
  attached. B's terminal text is `win.tees[i].text()` (a copy of what the joined terminal read); `bodyInk` counts
  its drawn pixels. Tests: `TestE2EShareAndJoin`, `...JoinerCannotTypeUntilHandedOver`, `...DenyAllowAndStop`,
  `...WrongKey`, `...StopWhileARequestWaits`. The notice's toast and the banner both have an "Ask for control":
  `tt.Advance(toastTimeout+…)` first.
- The link: `tt.Clipboard()` holds it; keep it in a variable, never print or write it. The popover shows the whole link (as the app does), so a shot holds the scratch room's.
- Labels: the title bar's `Share` and `N watching`, `dialog Share session`, `Join link field`, `share foot` (the foot's
  buttons share their names with the pane's banner: click by position), `Share requests` and `alertdialog <name>` for
  the prompts, `Allow`, `Deny`, `Hand over`, `Keep driving`; B's `dialog Join a session` (⌘J, field `Join link`), `banner
  joining`, `banner failed`, `tab <title> · joined`. Joining is ⌘J, `e.fillField("Join link", link)`, `e.click("Join")`.
- Person only: two real machines, NAT, a public signal server, the browser join page against a native owner.

## The agent panel (`e2e_agent_test.go`, `agentpanel_test.go`)

- **The fake provider is mandatory.** `startServeAt` gives every daemon `KEEL_AGENT_PROVIDER=fake`: keel serve then answers
  from an in-process fake and sends nothing anywhere. A scenario stores only `fakeAgentKey` (made up), by the API
  (`e.putAgentKey()`) or through the Connect card (`TestE2EAgentNoKey`). Never start a daemon without the variable once
  a key could be stored; never a real key, never a real model. A person tries one real question before the cutover.
- The fake's words (`internal/agent/provider/providertest/scripted.go`): "Test provider: nothing left this machine. I
  was given N pane excerpt(s) and K redacted value(s)." with `pane excerpt` / `excerpts` and `value` / `values`; it
  proposes `echo keel-agent-ok` (read-only), `rm -rf /tmp/keel-agent-e2e` for a question with "delete" (destructive),
  `touch /tmp/keel-agent-e2e-touched` for "touch" (changes state), none for "no command".
- **Nothing typed before approval** is asserted on the scrollback: `e.settle(time.Second)` (pumping, the allowed
  wait) after the card shows, again with the dialog open, and after Cancel; the marker is `"\nkeel-agent-ok"` (the output
  line: the echoed command has `echo ` before it). After the approved Type, `e.waitScrollback`.
- **Log rows** are asserted with `e.logKinds()` (`GET /api/logs`): `agent.read` and `agent.suggest` once the card shows,
  `agent.type` 0 until the approved Type and then 1; an ask with nothing in scope, or with "Agent can read" off, adds no
  `agent.read`. A key is looked for in `keel.log`, `keel-native.json`, the daemon's `serve.log` and the window's text.
- `/tmp` paths: `scratchPath` fails the test if `/tmp/keel-agent-e2e` or `-touched` already exists (a test never touches
  what it did not make), creates the first empty, and removes both in cleanup (`os.Remove`, never recursive). The
  destructive scenario runs `rm -rf /tmp/keel-agent-e2e` on that empty folder and checks it is gone.
- Labels: `agent panel`, `Message the agent`, `Choose what the agent can read`, `dialog Panes the agent reads`, rows
  `pick <host> · <tab>` (with the session id when two read the same), presets `Focused pane` / `Every pane in tab` /
  `None`, `Proposal`, `Edit command` (button, then the field), `Save command`, `Copy command`, `Type into pane…` (⇧:
  `e.tt.ClickWith(ui.Shift, …)`), `proposal status`, `agent answer`, `Gemini API key input`, `Connect`. The dialog is
  `dialog Type into the terminal?`, `e.waitApprove()`, buttons `Cancel` / `Type`. A host dialog switch shares its label with
  its row text: press it by position (`setLocalProduction`). Two panes of one host share `Reads N of M panes`: count, never position.
- Unit tests (`agentlogic_test.go` is `test/agent.test.mjs` for the panel; `agentpanel_test.go` is the panel on the fake
  backend) check what an ask sends (exactly three keys: no pane text), Stop, Edit through the server, Copy, the ⇧ rule, the
  refusals, the key field, and that a joined tab has no button. `guard_test.go` keeps one writer and one caller.

## Saved conversations, the Agent page, Settings · Agent (`e2e_agentconv_test.go`, `agentconv_test.go`, plan 018)

- Same rules as the panel above: fake provider, made-up key only, no model reached, no call to keel mcp's routes (the Claude
  Code scenario wraps the window's backend, `recBackend`, and asserts the patch sent is `claudeCode` and nothing else).
- Labels: the title button `Recent conversations` (its popover is `dialog Recent conversations`; `Search conversations`), a row
  `conversation <title>` with `Rename <title>` and `Delete <title>`, `Open all in a tab`, `Delete all` (the confirm's button
  shares its words: click it after the popover closed, which `askDeleteAll` does), `Proposal expired`, `Copy`, `Propose again`
  (an expired card has no `Type into pane…`, no `Edit command`, no `Copy command`). The page: `agent page`, `Message the agent on
  the page`, `host filter <host>` / `host filter All`, `Rename this conversation`, `Delete this conversation`, `Continue in panel`,
  `Retention settings`. Settings: `settings nav Agent`, `agent card <provider>`, `badge Connected <provider>`, `Change key <p>`,
  `Disconnect <p>`, `<key label> input` (masked), `Connect <p>`, `<name> switch`, `<name> slider`, `<name> picker`,
  `Default panes per conversation <choice>`, `Delete all history…`.
- **An expired proposal** needs keel serve not to hold it. Two ways, both in `TestE2EAgentExpiredProposal`: stop the daemon
  (`startServeAt` returns `stop`; `e.convList()` first so the save is flushed) and start it again on the same data dir
  (`startServeAt(t, dir, "serve2", true)`, the same `dir`): the card is Expired, and Propose again gives a Copy-only card
  because the pane died with the daemon; or start 17 conversations on one daemon (keel serve keeps 16 in memory), which
  drops the first while its pane is open, so Propose again gives a card that can be typed through the dialog.
- Wait for the UI, never sleep: after Delete all wait for the toast "Agent history deleted", or a click on the title button
  races the post that closes the list. The first frame after opening the page has no width and no list: use
  `r.openPageSettled()` (unit) or `e.pump` on a label before a click. A long Settings page: `tt.SetSize(1280, 3200)` before
  clicking a control, 1280×720 for the shots (`TestE2EAgentSettings-dark`, `-light`) that sit beside `reference/settings-agent-*`.
- Unit tests: the pure rules are `agent.test.mjs`' saved-conversation cases (`agentconvlogic_test.go`) and
  `agentsettings.test.mjs` (`agentsettingshelpers_test.go`); the fake keeps the list and settings in `fakeConvs`.

## In-pane suggestions (`e2e_suggest_test.go`, `suggestpane_test.go`, `suggestgate_test.go`, plan 019)

- The daemon runs `KEEL_AGENT_PROVIDER=fake` (every e2e daemon does), and `suggestSetup` stores two made-up keys (`fakeAgentKey`,
  `fakeJevKey`) and the settings through the API; no provider is reached and no `/api/mcp/` route is called. The fake answers
  "show the files here" with `echo keel-suggest-ok` and two alternatives, "clean up old files" with the destructive
  `rm -rf /tmp/keel-suggest-e2e` (a command the test never runs: no Enter, Ctrl-U after), `# …` lines in hash-only mode.
- The window's clock is the tester's and stands still: use `pumpClock` / `settleClock` (they `Advance` 25 ms a turn), or the 350 ms
  typing pause never ends. Type with `typeKeys` (key presses, which is what the pane's `OnKey` sees), not `tt.Type` (text alone).
- "Inserted but not run" is asserted on the scrollback and the log: after the second ⇥ the prompt line (`e.tail`, the window's own
  terminal) ends with the command, `settleClock(1s)` passes, `ranOutput` finds **no line that is exactly the command's output**, and
  `agent.suggest` and one `agent.insert` (bytes = Ctrl-U + command) are in `GET /api/logs`; only then the test presses Enter and the
  output appears. Never press Enter on the destructive card.
- "Typing is not delayed" is asserted by typing 200 characters of `echo …` at once with suggestions on: the echo in the raw
  scrollback, stripped of escapes, line ends and spaces (`bare`), equals what was typed, in order, and the Enter's output is the words
  and nothing else. The unit twin: `TestTypingIsNeverHeldBackOrChanged` (no key is taken while nothing is noticed; the pane's key hook
  only reports, `Write` never runs).
- Alignment is `suggestgate_test.go`: a cell measured from the font (`gridFor`) must equal the plugin's own caret rect after a resize, a text
  size change and at scales 1 and 2. Re-run it, and look at `TestE2ESuggestInsertNotRun-notice.png` and `-card.png` (the mark sits on the
  words, the card under the line), after a change of font, text size or MyGo version.
- Not covered: a real provider's latency, and the look over a real terminal on a real window.

## Notes and runbooks (`e2e_memory_test.go`, `notes_test.go`, `runbooks_test.go`, plan 020)

- The daemon runs `KEEL_AGENT_PROVIDER=fake`. The fake proposes a note when the question says "remember" ("please remember this": a
  host note for pane 1, `keel-note-e2e: backups run at 02:00 from pgbackrest`; "remember my preference": a note about you;
  "remember from the pane": a host note flagged as taken from pane output), proposes a two-step runbook for "save this as a
  runbook", and says how many notes it was given ("I was also given 1 host note and 0 of your notes."). Start a runbook from the
  page's **Use in agent**: the ask carries `runbookId`, and **keel serve** (not the fake) proposes the current step; the fake only
  says which step is current. The Local Shell has no shell marks, so a typed step waits for **Mark done**.
- A proposal is kept only by its card's Save: assert `GET /api/agent/notes` and `GET /api/agent/runbooks` are empty after a frame,
  after `settle(1s)` and after Dismiss, then have one note after Save ("Save anyway" on any card after a pane was read) and none after
  Undo. A closed card has an Undo of its own, so ask the second question in **New conversation** before clicking Undo.
- Redaction is keel serve's: store a made-up password identity (`e2eNoteSecret`), add a note holding it in Edit host, and assert the
  list shows `[secret:` and "Note added. 1 secret-looking value was replaced before saving.", and the value is in neither
  `GET /api/agent/notes`, `GET /api/logs` nor the window (`noSecretDrawn`). Never print the value.
- keel serve refuses a runbook step that holds `[secret:1]` (400, from the API and from the editor), so a walk cannot reach a step
  with a placeholder; that card (no Type button, the app's sentence, never typed) is `TestAStepWithAPlaceholderHasNoTypeButtonAndIsNeverTyped`.
- Clicking: the dialog's switch and the Delete all notes dialog's action share their words with a row's text under the scrim, so
  press them with `ClickAt` (as `setLocalProduction` does). Use `newWinH(…, 2600)` for the Runbooks page, the walk and Settings ·
  Agent: the run card and the notes block must be on screen. A walk's cards stay in the thread: type the newest with
  `typeStepCardLast`. Look at `…-page`, `…-walk`, `…-card` and `TestE2EHostNotes-dialog`.

## Folders, the Group page and group runs (`e2e_groups_test.go`, `folders_test.go`, `grouppage_test.go`, `grouprun_test.go`, plan 021)

- A group of test hosts is a folder of **Local Shells** made through the API, no SSH fixture: `POST /api/hosts {name, authType:
  "local", agentReadable: true}` (`apiLocalHost`), `apiGroup`, `apiMove`, then `e.reread("folder <name>")`. A runbook is
  `SaveRunbook` with `echo` steps (`apiRunbook`). `runSetup` does all of it, starts the run from the Group page and polls fast
  (`startPoll(200ms)`: the run page proposes every 1.5 s in the app). Open each host by clicking `host <name>` (`openRunPanes`),
  then come back with `backToRun`. A real `/bin/sh` runs what is typed, so the bytes (Ctrl-U, the echo, a newline) end in the
  scrollback; no shell sends OSC 133 marks, so a host is finished with **Mark done <host>**. Scratch data only, the fake provider.
- The dialog is `e.w.approve.group` (`dialogRows`). The assertions that matter, in this order: nothing is ticked with two or more
  hosts and `approveType(false/true)` types nothing with nothing ticked; the unticked host's scrollback never holds the command;
  `GET /api/logs` has one `agent.type` per host × step and no command text; with a variable, the run page shows `{{port}}` and
  no value, the dialog shows the filled bytes, the scrollback has them and no log row does.
- A **follower** is made by a second client: `e.cl.Attach(sid, 80, 24)` then `conn.TakeControl()`; a **closed** pane by
  `e.w.closePane`. Do both while the dialog is open: their rows say why and are not counted. Take the unit tests'
  `runRig` for a fake pane with `injectErr` when a refusal for one host is needed.
- The Ansible export is keel serve's: set a group variable, then put the same value in the vault as a host's password
  (`POST /api/hosts` authType password) and the export leaves the variable out with a comment; assert the value is nowhere.
  A secret-looking variable (`AKIAIOSFODNN7EXAMPLE`, AWS's documented example) is refused by keel serve with "Keep secrets in the vault".
- Drag: `e.drag(from, to)` presses, moves past the slop, moves over the target and releases. A host or a folder drops on a
  folder or on the tree. A toast sits over the drawer's Copy button: close it (`Close notification`) before the next press.
  Look at `…-dialog`, `TestE2EGroupPageRolesNotesVars`, `TestE2EFolders` and `TestE2EAnsibleExport`.

## References and key caps (plan 016)

- Before claiming a screen matches the app, open the native shot beside the app's image: `docs/images/<screen>.png` (five) and, where they exist, `docs/images/reference/<screen>-<theme>.png`. Record each visible difference in the table in `docs/native.md`. A screen with no reference is compared only with the rules and tokens; say so.
- Refresh the references where `node_modules` exists (a worktree has none: link the main checkout's read-only, use `npx --no-install`, remove the link after, never stage it; never install): `make build`, then `npx --no-install e2e run tests/readme.e2e.ts --target webkit --output .e2e/proof/readme` (seeded made-up hosts, `scripts/e2e-serve.sh`: `KEEL_KEYRING=off`, scratch data, fake agent). The `reference screens` tests write `ref-<screen>-<theme>` shots under `.e2e/proof/readme/results/*/attempt-1/screenshots/`; copy them to `docs/images/reference/` without the number and `ref-`. The tests scrub path, host, user and join-link values from the DOM first, but look at every PNG for real data before committing it.
- Key caps (`keycaps.go`): the clock is the tester's, so hold with `tt.HoldModifiers(ui.Cmd)`, `Frame()`, then `tt.Advance(capDelay + capFade)`; caps are elements labelled `key cap <keys>` (`tt.Texts()`, `tt.Find`). A key pressed with `tt.KeyDown` or a click with `tt.ClickAtWith(ui.Cmd, x, y)` hides them; `tt.SetFocused(false)` too. In the e2e a menu shortcut (⌘⇧D) leaves its key "down", so the hold ignores keys down when it began. Shots: `TestE2EKeyCaps-dark` and `-light`.

## Not covered

Say so when a change lives there: the OS window (traffic lights, full
screen, title bar drag, the macOS menu), the blur and the see-through window, input
methods, the system clipboard, real GPU text rendering, Linux and Windows.
`KEEL_NATIVE_CAPTURE=<png> ./bin/keel-native` on a real window is the only
check of the page inside a real window, and it does not draw the window
chrome.
