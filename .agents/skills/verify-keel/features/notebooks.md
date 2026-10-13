# Notebooks

Markdown runbooks opened in their own tab. Fenced `sh`/`bash` blocks can
fill a terminal's input after the user confirms.

## Sub-features

- `New notebook` opens a `Notebook` tab with a title field and the document.
- Rename with the `Rename notebook` field.
- Remove from the card menu (confirm).
- Also in the session sidebar (`Notebooks` tab).

## How to get to it (user POV)

Vault rail › `Notebooks`. Empty state: "No notebooks yet".

## Driving it with e2e

No test yet. Locators: `screen.getByRole('button', 'Notebooks', {
visible: true }).first()`, `getByRole('button', 'New notebook')`, then
`getByRole('textbox', 'Rename notebook')` and `getByRole('textbox',
'Markdown document')`.

## Proof

On screen: the tab opens with `# Notebook` starter text. Store:
`/api/notebooks` holds the notebook and its markdown after an edit.

## Charters

```
notebooks-edit|web|skeptic|Starting at the Vault (the app opens there), create a notebook, rename it, add a fenced sh block, close and reopen it; check the title and content against what you typed
```

## Gotchas

- Known app bug, found while writing this map:
  `cmd/keel/web/vendor/notebook/editor.js` does not parse in V8 (`node
  --check` and Chromium both fail with `SyntaxError: Unexpected token ')'`,
  line 34). Every page load logs it, and the rich editor never loads; the
  `Markdown document` field is the fallback. Until it is fixed, a notebook
  check in Chromium covers the fallback, not the editor; check the desktop
  WebKit window by hand.
