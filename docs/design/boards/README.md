# Design boards

Versioned snapshot of the Keel redesign canvas, so phase work uses a fixed design.

- Canvas: https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW
- Artifact version snapshotted: `1791863098-3562`
- Date: 2026-10-13
- Design system the canvas was copied from: Keel, https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1 (version `1791859965-affb`)
- `Panes.dc.html`, `workspace.css` (the `kw-map` / `kw-pane-row` rules at its end) and `canvas.json` were taken from artifact version `1792114738-a1e8` on 2026-10-16; the other boards stay at the snapshot above.
- `AgentSessions.dc.html`, `AgentPage.dc.html` and the "Agent sessions" section of `workspace.css` were copied from the approved canvas on 2026-10-18 (agent sessions phase). `canvas.json` was not updated: the canvas now holds further boards (notes, runbooks, groups, notebook) that are not in this folder.
- `Notebook.dc.html`, `NotebookTypes.dc.html` and their `notebook.css` (the `nb-*` styles) were copied on 2026-10-18 from the canvas as approved on 2026-10-16. `canvas.json` and `workspace.css` were not refreshed with them.

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
