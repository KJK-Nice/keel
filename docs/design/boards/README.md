# Design boards

Versioned snapshot of the Keel redesign canvas, so phase work uses a fixed design.

- Canvas: https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW
- Artifact version snapshotted: `1791863098-3562`
- Date: 2026-10-13
- Design system the canvas was copied from: Keel, https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1 (version `1791859965-affb`)
- `Panes.dc.html`, `workspace.css` (the `kw-map` / `kw-pane-row` rules at its end) and `canvas.json` were taken from artifact version `1792114738-a1e8` on 2026-10-16; the other boards stay at the snapshot above.
- `AgentSessions.dc.html`, `AgentPage.dc.html` and the "Agent sessions" section of `workspace.css` were copied from the approved canvas on 2026-10-18 (agent sessions phase). `canvas.json` was not updated: the canvas now holds further boards that are not in `canvas.json`.
- `Notebook.dc.html`, `NotebookTypes.dc.html` and their `notebook.css` (the `nb-*` styles) were copied on 2026-10-18 from the canvas as approved on 2026-10-16. `canvas.json` and `workspace.css` were not refreshed with them.
- `HostNotes.dc.html`, `MemoryPropose.dc.html` and the "Memory and skills" section of `workspace.css` (the `kw-note*`, `kw-mem*` and `kw-review` rules, plus the `kw-rb-*` / `kw-rp-*` rules the runbook boards use) were copied on 2026-10-18 from the canvas as approved (host notes phase, §15). `canvas.json` was not updated.
- `Runbooks.dc.html` and `RunbookInUse.dc.html` were copied on 2026-10-18 from the canvas as approved (runbooks phase, §16). Their `kw-rb-*` / `kw-rp-*` rules were already in `workspace.css`. `canvas.json` was not updated.
- `GroupRoles.dc.html`, `GroupRunbook.dc.html` and the "Groups and orchestration" section of `workspace.css` (the `kw-gx-*` rules) were copied on 2026-10-18 from the canvas as approved (groups phase, part 1, §17). `canvas.json` was not updated.
- `CaptureVars.dc.html` and `AnsibleExport.dc.html` were copied on 2026-10-18 from the canvas as approved (groups phase, part 2, §17). Their `kw-gx-*` rules (`kw-gx-var`, `kw-gx-cap*`, `kw-gx-mask`, `kw-gx-code`, `kw-gx-tabs`, `kw-gx-drawer*`) were already in `workspace.css`. `canvas.json` was not updated.

The boards are reference markup, not app code. They reference `./support.js` from the canvas runtime and use `<x-dc>` / `dc-import`, so they will not run standalone. Read them for structure, copy and state; the window implements them in `internal/native` with tokens from `docs/design/tokens.json`. Fonts are loaded from Google Fonts in the boards for preview only; the app ships woff2 locally.

`canvas.json` holds board positions and titles. `workspace.css` holds the `kw-*` layout styles the boards use.

## Boards

| Board | Spec (`docs/ui-redesign.md`) | Phase (§10) |
|---|---|---|
| `Main.dc.html` | §5.1 session workspace, §5.4 agent panel | 1 chrome, 2 sidebar, 6 agent panel |
| `Palette.dc.html` | §5.2 command palette | 3 |
| `NewTab.dc.html` | §5.3 new tab | 3 |
| `Suggest.dc.html` | §5.5 plain language to command | 6 |
| `Share.dc.html` | §5.6 share and pass control | 4 |
| `Join.dc.html` | §5.7 joined session, follower view | 4 |
| `Host.dc.html` | §5.8 edit host | 2 |
| `Settings.dc.html` | §5.9 settings, agent | 2 (tab page), 6 (agent options) |
| `Forwards.dc.html` | §5.10 port forwarding | 2 |
| `Keychain.dc.html` | §5.11 keychain | 2 |
| `Snippets.dc.html` | §5.12 snippets | 2 |
| `Logs.dc.html` | §5.13 logs | 2 |
| `Unlock.dc.html` | §5.14 unlock vault | 5 |
| `Onboard1.dc.html` | §5.15 first run, create vault | 5 |
| `Onboard2.dc.html` | §5.15 first run, bring your hosts | 5 |
| `Onboard3.dc.html` | §5.15 first run, agent | 5 (agent step wired in 6) |
| `Panes.dc.html` | §5.1 Panes in the sidebar | post-plan polish |
| `AgentSessions.dc.html` | §5.4.1 agent sessions (panel list, resume, Expired proposals) | Agent sessions |
| `AgentPage.dc.html` | §5.4.1 Agent tab page (three columns) | follow-up |
| `Notebook.dc.html` | §13 notebook reading spec (the pane beside a session) | post-plan polish |
| `NotebookTypes.dc.html` | §13 notebook type specimen (each block and its tokens) | post-plan polish |
| `HostNotes.dc.html` | §15 notes for the agent in Edit host | Host notes |
| `MemoryPropose.dc.html` | §15 a note the agent proposes (host note, user note, from pane output) | Host notes |
| `Runbooks.dc.html` | §16 the Runbooks tab page (list, editor, review banner) | Runbooks |
| `RunbookInUse.dc.html` | §16 a runbook in use in the agent panel (picked, progress, step proposal) | Runbooks |
| `GroupRoles.dc.html` | §17 the Group page (roles, group notes; group variables and the Ansible drawer are part 2) | Groups, part 1 |
| `GroupRunbook.dc.html` | §17 a runbook run across a group (mode, proposal for N panes, halt, results) | Groups, part 1 |
| `CaptureVars.dc.html` | §17 a value captured from a step's output and filled in at Type (capture box, `{{var}}` in the proposal, what the agent was sent) | Groups, part 2 |
| `AnsibleExport.dc.html` | §17 an Ansible playbook and inventory from a runbook and a group | Groups, part 2 |

## Tokens added since the canvas

The canvas design system's tokens matched the repo's on 2026-10-13; the repo is the source of truth. The boards do use values outside the token set: `kw-*` classes from `workspace.css` and some inline literals (for example `font: 600 28px/32px`, `width: 26px`, `font-size: 10px`). Treat those as layout detail to re-express with tokens, not as new tokens.

- `size-agent` (380px, the `kw-agent` width in `Main`).
- `size-sidebar` (252px, the `kw-side` width in `Main`) and `size-list` (360px, the `kw-split` list column).
- The `reading` type style (14/22, sans 400) and `size-reading` (68ch, the `nb-col` measure), for the Notebook boards.
