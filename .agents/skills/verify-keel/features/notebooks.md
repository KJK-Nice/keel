# Notebooks

Markdown runbooks opened in their own tab, or beside a session. Fenced
`sh`/`bash` blocks can fill a terminal's input after the user confirms.

## Sub-features

- Vault sidebar › `Snippets` segment, `Notebooks` section: open a notebook
  (beside the selected session, else in its own tab), pin or unpin it,
  `New notebook`, `All notebooks`.
- Notebooks tab page: rows (pinned first) with a filter, `New notebook`
  (or ⌘N on the page), and a row menu `Actions`: Open, Rename, Pin/Unpin,
  Remove (confirm).
- `New notebook` opens a `Notebook` tab with a title field and the document.
- Rename with the `Rename notebook` field.

## How to get to it (user POV)

Vault sidebar › `Snippets` › `All notebooks`, or the palette's `Notebooks`.
Empty state: "No notebooks yet".

## Driving it with e2e

`tests/sidebar.e2e.ts` opens the page. Locators:
`const vault = screen.getByRole('complementary', 'Vault')`,
`vault.getByRole('button', 'Snippets')`, `vault.getByRole('button', 'All
notebooks')`, `screen.getByRole('region', 'Notebooks')` and inside it
`getByRole('button', 'New notebook')`, then `getByRole('textbox', 'Rename
notebook')` and `getByRole('textbox', 'Markdown document')`.

## Proof

On screen: the tab opens with the name `Notebook` as its title (the H1, `Rename notebook`) and "Write a runbook here." starter text. Store:
`/api/notebooks` holds the notebook and its markdown after an edit.

## Charters

```
notebooks-edit|web|skeptic|Starting at the Vault sidebar (the app opens with it beside a New tab), create a notebook, rename it, add a fenced sh block, close and reopen it from the sidebar; check the title and content against what you typed
```

## Gotchas

- Known app bug, found while writing this map:
  `cmd/keel/web/vendor/notebook/editor.js` does not parse. Checked per
  engine (a smoke trace's App log, Playwright builds): WebKit logs
  `SyntaxError: Unexpected token ')' at /vendor/notebook/editor.js:34:0`;
  Chromium logs `SyntaxError: Unexpected token ')'`; `node --check` fails on
  line 34 too. So it fails in both engines, on every page load, and the rich
  editor never loads; the `Markdown document` field is the fallback. Until
  it is fixed, a notebook check covers the fallback, not the editor. The
  system WKWebView in the native window is not covered; a Playwright WebKit
  parse failure makes it very likely, but check it by hand.
- The sidebar and the page both have a `New notebook` button: scope to the
  region or the sidebar.
