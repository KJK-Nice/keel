# Design boards

Versioned snapshot of the Keel redesign canvas, so phase work uses a fixed design.

- Canvas: https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW
- Artifact version snapshotted: `1791863098-3562`
- Date: 2026-10-13
- Design system the canvas was copied from: Keel, https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1 (version `1791859965-affb`)
- `Panes.dc.html`, `workspace.css` (the `kw-map` / `kw-pane-row` rules at its end) and `canvas.json` were taken from artifact version `1792114738-a1e8` on 2026-10-16; the other boards stay at the snapshot above.
- `AgentSessions.dc.html`, `AgentPage.dc.html` and the "Agent sessions" section of `workspace.css` were copied from the approved canvas on 2026-10-18 (agent sessions phase). `canvas.json` was not updated: the canvas now holds further boards (notes, runbooks, groups, notebook) that are not in this folder.
- `Notebook.dc.html`, `NotebookTypes.dc.html` and their `notebook.css` (the `nb-*` styles) were copied on 2026-10-18 from the canvas as approved on 2026-10-16. `canvas.json` and `workspace.css` were not refreshed with them.
- `HostNotes.dc.html`, `MemoryPropose.dc.html` and the "Memory and skills" section of `workspace.css` (the `kw-note*`, `kw-mem*` and `kw-review` rules, plus the `kw-rb-*` / `kw-rp-*` rules the next phase's runbook boards use) were copied on 2026-10-18 from the canvas as approved (host notes phase, §15). `canvas.json` was not updated.
- `Runbooks.dc.html` and `RunbookInUse.dc.html` were copied on 2026-10-18 from the canvas as approved (runbooks phase, §16). Their `kw-rb-*` / `kw-rp-*` rules were already in `workspace.css`. The next phase's boards (`GroupRoles`, `GroupRunbook`, `CaptureVars`, `AnsibleExport`) are not copied yet; `canvas.json` was not updated.
- `GroupRoles.dc.html`, `GroupRunbook.dc.html` and the "Groups and orchestration" section of `workspace.css` (the `kw-gx-*` rules) were copied on 2026-10-18 from the canvas as approved (groups phase, part 1, §17). `CaptureVars` and `AnsibleExport` belong to part 2 and are not copied yet; `canvas.json` was not updated.
- `CaptureVars.dc.html` and `AnsibleExport.dc.html` were copied on 2026-10-18 from the canvas as approved (groups phase, part 2, §17). Their `kw-gx-*` rules (`kw-gx-var`, `kw-gx-cap*`, `kw-gx-mask`, `kw-gx-code`, `kw-gx-tabs`, `kw-gx-drawer*`) were already in `workspace.css`. `canvas.json` was not updated.

The boards are reference markup, not app code. They reference `./support.js` from the canvas runtime and use `<x-dc>` / `dc-import`, so they will not run standalone. Read them for structure, copy and state; build in templ with the `k-*` classes from `docs/design/components.css`. Tokens come only from `docs/design/tokens.json`. Fonts are loaded from Google Fonts in the boards for preview only; the app ships woff2 locally.

`canvas.json` holds board positions and titles. `workspace.css` holds the `kw-*` layout styles the boards use (not part of the shipped component set).

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

## Drift from repo tokens

Compared the canvas design system (`project/ds/keel/`) with the repo on 2026-10-13. The repo is the source of truth.

| Canvas file | Repo file | Result |
|---|---|---|
| `ds/keel/tokens.json` | `docs/design/tokens.json` | byte-identical, no drift |
| `ds/keel/tokens.css` | `docs/design/tokens.css` | byte-identical, no drift |
| `ds/keel/components/bundle.css` | `docs/design/components.css` | byte-identical, no drift |
| `ds/keel/README.md` | `docs/design/README.md` | byte-identical, no drift |

No token name, token value or component rule differs. The boards do use classes and values outside the shipped set: `kw-*` classes from `workspace.css`, and some inline literals (for example `font: 600 28px/32px`, `width: 26px`, `font-size: 10px`). Treat those as layout detail to re-express with tokens or `k-*` classes when implementing, not as new tokens.

Phase 6b added `size-agent` (380px, the `kw-agent` width in `Main`). Phase 2 (Vault sidebar and tab pages) added two tokens to the repo, `size-sidebar` (252px, the `kw-side` width in `Main`) and `size-list` (360px, the `kw-split` list column), and updated the product words and fixed-chrome lines in `docs/design/README.md`. The canvas copy predates them.

The AgentSessions board's `kw-sess-*`, `kw-pop*`, `kw-agent-sub`, `kw-divider` and `kw-prop--expired` rules ship in `keel.css` as `k-agent-sess*`, `k-agent-sessions*`, `k-agent-sub`, `k-agent-divider` and `k-prop--expired`, with no new token: the 36px sub row is `size-control-lg + space-1`, the 26px group header `size-control-sm + space-0.5`, and the popover's 560px cap is 70% of the panel.

The Panes board's `kw-seg-n`, `kw-map` / `kw-map-cell` and `kw-pane-row` rules ship in `keel.css` as `k-seg-n`, `k-panemap` / `k-panemap-cell` and `k-pane-row`, with no new token: the 76px map is `size-row + size-control-lg`, the 40px row `size-control-lg + space-2`, and the cell radius is `radius-xs` (3px; the board has 2px). The map places cells from the tab's real split tree instead of the board's fixed grid.

The Notebook boards' `nb-*` rules ship in `keel.css` as `notebook-*` (the class names the editor bundle already used). Two tokens were added for them: the `reading` type style (14/22, sans 400) and `size-reading` (68ch, the `nb-col` measure). The generator now also writes every type style to `tokens.css` as `--type-<name>` (a font shorthand in rem), so `nb-*`'s `font: 600 28px/32px var(--font-display)` becomes `font: var(--type-display)`. Pixel paddings in `notebook.css` (28 / 32 / 48, 10, 22) are `space-*` sums or the nearest step; the host chip's 20px height comes from `space-0.5` padding around the `meta` line height. The proposed blocks (checklist, table, `> [!WARNING]`) are not built: see `docs/ui-redesign.md` §13.

The HostNotes and MemoryPropose boards' `kw-note*`, `kw-mem*` and `kw-review` rules ship in `keel.css` (reference in `docs/design/components.css`) as `k-notes-block`, `k-notes`, `k-note*`, `k-review`, and, for the proposal, the `k-prop` card with `k-prop--note`, `k-prop--warn`, `k-prop-badges`, `k-note-warn` and `k-note-done`, with no new token. The board's 36px host tile and `deploy@… · via bastion` line are left out: the app's Edit host dialog keeps its own title row, and the notes sit under its Agent switches in the right column.

The Runbooks and RunbookInUse boards' `kw-rb-*`, `kw-rp-*` and `kw-review` rules ship in `keel.css` (reference in `docs/design/components.css`) as `k-rb-*`, `k-rp*` and `k-rb-review`, with no new token: the list column is `size-list` (360px; the board has 380px), the settings box `size-agentpage-context` (300px; the board has 260px), the 22px step number `space-4 + space-1 + space-0.5`. The board's "Import .md" is "From a notebook…" (notebooks are the app's markdown) and "From a session…" is "From a conversation…"; the editor edits every field in place where the board shows a read view. A failed step, a skipped step and a step with no command get their own row states and actions, which the board does not draw.

The GroupRoles and GroupRunbook boards' `kw-gx-*` rules ship in `keel.css` (reference in `docs/design/components.css`) as `k-gx-*`, with no new token: the role chips and select are `size-control-sm` high, the run page's step column is `size-agentpage-context` (300px; the board has 340px), the approve dialog is `size-reading + space-8` wide. The board's production checkbox ("I checked this command for all 5 hosts") became the group approve dialog's per-host ticks (§17, §6 amendment), and its per-step Results table became one host × step matrix with the current step's actions in the last column. Not built from the boards: group variables, the Ansible inventory drawer (part 2), the k8s quorum hint, the results' Time and Last line columns and View output, and the Edit icon on the step's proposal.

The CaptureVars and AnsibleExport boards, and the GroupRoles board's group variables and drawer, ship in `keel.css` (reference in `docs/design/components.css`) as `k-gx-var`, `k-gx-cap*`, `k-gx-mask`, `k-gx-kv`, `k-gx-code`, `k-gx-tabs`, `k-gx-drawer*` and, in the Runbooks editor, `k-rb-cap*`, with no new token: the drawer is `size-list + 6 × size-agent-step` (504px; the board has 500px), the code block is at most `8 × size-row` high, the capture box's label column `3 × size-control`. Differences: the export is built by keel serve from the group and the runbook (the AnsibleExport board shows the agent drafting it in the panel), so the drawer on the group page offers both files with format, playbook and mode choices; "Save to…" is Copy and Download (no native folder picker); a captured value shows as a mask and its length (no Reveal behind the vault passphrase, no first four characters); the status bar's "2 captured values · expire 11:14" is the capture box's last line; the agent panel's "What the agent was sent" block is not drawn; the trace says "N values withheld by Keel" for them.
