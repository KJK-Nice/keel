# Design boards

Versioned snapshot of the Keel redesign canvas, so phase work uses a fixed design.

- Canvas: https://claude.ai/artifact/GJaVi8YQSrxoNiDVXoi2sW
- Artifact version snapshotted: `1791863098-3562`
- Date: 2026-10-13
- Design system the canvas was copied from: Keel, https://claude.ai/artifact/3RUKjVH2znuvieFDaDjkS1 (version `1791859965-affb`)

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
