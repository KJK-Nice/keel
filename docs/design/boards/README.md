# Design boards

Versioned snapshot of the Keel redesign canvas, so the native window is built against a fixed design.

- Canvas: https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW
- Artifact version snapshotted: `1791863098-3562`
- Date: 2026-10-13
- Design system the canvas was copied from: Keel, https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1 (version `1791859965-affb`)
- Provenance: the boards were copied from the approved design canvas in October 2026 (2026-10-13 to 2026-10-18). `canvas.json` is not kept in sync: the canvas holds boards that are not in it.

The boards are reference markup, not app code. They reference `./support.js` from the canvas runtime and use `<x-dc>` / `dc-import`, so they will not run standalone. Read them for structure, copy and state; the window implements them in `internal/native` with tokens from `docs/design/tokens.json`. Fonts are loaded from Google Fonts in the boards for preview only; the app ships woff2 locally.

`canvas.json` holds board positions and titles. `workspace.css` holds the `kw-*` layout styles the boards use.

## Boards

| Board | Spec (`docs/ui-redesign.md`) |
|---|---|
| `Main.dc.html` | §5.1 session workspace, §5.4 agent panel |
| `Palette.dc.html` | §5.2 command palette |
| `NewTab.dc.html` | §5.3 new tab |
| `Suggest.dc.html` | §5.5 plain language to command |
| `Share.dc.html` | §5.6 share and pass control |
| `Join.dc.html` | §5.7 joined session, follower view |
| `Host.dc.html` | §5.8 edit host |
| `Settings.dc.html` | §5.9 settings, agent |
| `Forwards.dc.html` | §5.10 port forwarding |
| `Keychain.dc.html` | §5.11 keychain |
| `Snippets.dc.html` | §5.12 snippets |
| `Logs.dc.html` | §5.13 logs |
| `Unlock.dc.html` | §5.14 unlock vault |
| `Onboard1.dc.html` | §5.15 first run, create vault |
| `Onboard2.dc.html` | §5.15 first run, bring your hosts |
| `Onboard3.dc.html` | §5.15 first run, agent |
| `Panes.dc.html` | §5.1 Panes in the sidebar |
| `AgentSessions.dc.html` | §5.4.1 agent sessions (panel list, resume, Expired proposals) |
| `AgentPage.dc.html` | §5.4.1 Agent tab page (three columns) |
| `Notebook.dc.html` | §13 notebook reading spec (the pane beside a session) |
| `NotebookTypes.dc.html` | §13 notebook type specimen (each block and its tokens) |
| `HostNotes.dc.html` | §15 notes for the agent in Edit host |
| `MemoryPropose.dc.html` | §15 a note the agent proposes (host note, user note, from pane output) |
| `Runbooks.dc.html` | §16 the Runbooks tab page (list, editor, review banner) |
| `RunbookInUse.dc.html` | §16 a runbook in use in the agent panel (picked, progress, step proposal) |
| `GroupRoles.dc.html` | §17 the Group page (roles, group notes; group variables and the Ansible drawer are part 2) |
| `GroupRunbook.dc.html` | §17 a runbook run across a group (mode, proposal for N panes, halt, results) |
| `CaptureVars.dc.html` | §17 a value captured from a step's output and filled in at Type (capture box, `{{var}}` in the proposal, what the agent was sent) |
| `AnsibleExport.dc.html` | §17 an Ansible playbook and inventory from a runbook and a group |

## Tokens added since the canvas

The canvas design system's tokens matched the repo's on 2026-10-13; the repo is the source of truth. The boards do use values outside the token set: `kw-*` classes from `workspace.css` and some inline literals (for example `font: 600 28px/32px`, `width: 26px`, `font-size: 10px`). Treat those as layout detail to re-express with tokens, not as new tokens.

- `size-agent` (380px, the `kw-agent` width in `Main`).
- `size-sidebar` (252px, the `kw-side` width in `Main`) and `size-list` (360px, the `kw-split` list column).
- The `reading` type style (14/22, sans 400) and `size-reading` (68ch, the `nb-col` measure), for the Notebook boards.
