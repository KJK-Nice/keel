# Native notebook editor spike (plan 022)

> The spike code named below (`internal/native/nbdoc/`, `nbspike_test.go`) was removed in plan 023. The notebook code is `internal/native/notebooks.go`, `notebookslogic.go`, `notebookpane.go`, `notebookdialogs.go` and `notebooktype.go`; what the agent does to notebooks is `agentnotebooks*.go` (plan 024).

Question: can a Keel notebook go Markdown, editor, Markdown in MyGo's `ui/richtext` (v0.8.3) with the fence languages kept and nothing else changed, and can a fence bar be drawn and clicked over the editor? Code: `internal/native/nbdoc/` (package, tests, `testdata/`), `internal/native/nbspike_test.go` (the bar under `ui.Tester`). Nothing here types into a terminal.

Differences from the plan's guesses: `Text.Paragraphs()` yields each paragraph's first rune offset, not its index, and `Text.ParagraphRange` takes a rune. `Document.Text()` locks, so it cannot be called inside `Document.Edit`. `nbdoc` reads fence info strings with `ui/markdown/parser` (`Block.Info()`), not a hand scan. `Text.Markdown()` writes no final newline (Keel trims a notebook anyway, `notebookMarkdown` in term.js), so the corpus test compares without the input's final newline.

## 1. Round trip (`TestCorpusRoundTrip`, outputs in `testdata/*.out.md`)

No words are lost in any file (STOP 2 did not apply): only markup, structure and the fence boundaries.

| File | Result |
|---|---|
| plain.md | identical |
| unicode.md | identical (Thai, emoji, tab in text and in a fence) |
| lists.md | blank line between two lists goes: `- deeper\n\n1. first` becomes `- deeper\n1. first`, same for `3. third` then `- [ ] open task`. Nesting and tasks kept. |
| fences.md | first fence kept (`sh`). The five fences after the paragraph "Between." are adjacent and become ONE `bash` block: the `go`, none, `shell {title="x"}` and `~~~sh` blocks are merged into it, their languages and boundaries gone. |
| quote.md | the `>` blank line splits one quote into two quotes; `> [!NOTE]\n> A callout.` becomes `> \[!NOTE\] A callout.` (kind lost, escaped, one line); the quoted `sh` fence is kept. |
| rule-image.md | `---` is dropped. The image is kept. |
| table.md | the table becomes tab-separated paragraphs, the header as `**Host<TAB>Port**`. Not a table any more. |
| html.md | `<details>`, `<summary>`, `<kbd>` tags dropped (text kept: `Press Ctrl+C`); the closing `</details>` leaves two extra blank lines. |
| hardbreak.md | both hard breaks (two spaces, backslash) become paragraph breaks. |
| starter.md | the starter's inline `` ```sh or ```bash `` (a code span) becomes `` `sh or` bash ``, and the blank line before the bullet list goes. The default notebook is rewritten on the first save. |

`TestRoundTripIsStable`: a second round trip equals the first for every file except html.md, whose extra blank lines go on the second pass (the test asserts that exact output).

## 2. Languages

Kept (tests pass): `sh`, `bash`, `shell`, `go`, none, `sh {title="x"}` (`TestLanguageSurvivesARoundTrip`); a longer fence for code with backticks (`TestALongerFenceWhenTheCodeHoldsBackticks`); a fence in a quote (`TestFenceInsideAQuoteAndAList`); typing in a fence (`TestTypingInsideAFenceKeepsItsLanguage`); splitting the first line at its end, middle or start (`TestSplittingAFencesFirstLine`; at the start the ID moves to the second half and `Fences` still finds the language, because it looks at every paragraph of the block); undo and redo of all of these; joining two fences keeps the first's language (`TestJoiningTwoFencesKeepsTheFirstLanguage`).

Lost:
- Two fences with one blank line between them merge into one block, the second's language lost (`TestTwoFencesInARow`, and fences.md). The editor holds code paragraphs flat; nothing says where one block ends. A runbook with consecutive fences would turn into one command block. This is the worst finding.
- Deleting a fence's first line deletes the only ID (`TestDeletingAFencesFirstLineLosesItsLanguage`). Smallest fix, proved in `TestEveryParagraphOfAFenceHavingAnIDKeepsTheLanguageWhenTheFirstLineGoes`: give every code paragraph an ID that maps to the same info string. `Document.IdentifyParagraphs` alone does not help: new IDs have no language.
- Cut and paste: the clipboard leaves IDs out, so the pasted block is a code block with no language (`TestCutAndPasteLosesTheLanguage`, which strips IDs as the clipboard does rather than using the system clipboard).
- A fence typed in the editor (` ``` ` then Enter) has no language, and there is no way to set one in `richtext`.
- A fence in a list item loses its indent (it comes out at top level, `- item\n\n```sh`).
- `~~~` becomes ` ``` `.

## 3. The bar (`TestNotebookSpike*`, three runs in a row pass)

Works. `Fences` plus `VisibleParagraphs` give each fence's first paragraph and its `Top`; an `Absolute()` row at `Top(box.Top).Right(space4)` is the bar. Asserted: both bars found, each of `Copy code` and `Type into pane` reports the right fence ID and exact code; after typing a line the click reports the new code; after inserting three paragraphs above the bar is on the fence; after scrolling the second bar is clickable and the first is not drawn; a click on the code left of the bar and a typed character change the code and reach no button. PNG: `.scratch-native/e2e/TestNotebookSpikeBar.png` (git-ignored).

Awkward:
- `VisibleParagraphs` is nil in the first build; a second frame follows by itself (the tester settles it).
- The editor opens scrolled to its caret, which starts at the end; a test (and the app) must `SetSelection(0, 0)` to open at the top.
- The bar is 24 DIPs tall over a 13 DIP line, so it covers part of the next line too.
- Collision (`TestNotebookSpikeLongFirstLineUnderTheBar`, 640 DIP wide editor, 80-character first line): the bar starts at x=452.6, the text ends at x=566.4, so 113.8 DIPs of the line are hidden. Nothing reserves room for the bar, and `richtext` has no hook to add a header row above a code block. A narrower editor would avoid it for all text.
- The bar's language picker was not built.

## 4. The web editor against `richtext` (`notebook-editor.mjs`)

Input rules (47-63):
- smart quotes, ellipsis, em dash: missing in `richtext`.
- `> ` quote: same. `- `, `+ `, `* ` bullets: same. `1. ` numbers: same (also `1)`).
- `# ` to `###### ` headings: same.
- ` ```lang ` + space makes a code block with that language: different. `richtext` makes a code block from ` ``` ` + Enter, with no language.
- `richtext` also has task (`[ ]`), `**bold**`, `*italic*`, `` `code` `` and `~~strike~~` rules the web editor lacks.

Keymap (65-79):
- Mod-b, Mod-i: same. Mod-z, Mod-y and Mod-Shift-z: same (undo and redo; undo restores IDs).
- Mod-` inline code: different, it is Cmd+E on macOS.
- Shift-Ctrl-\ code block: missing as a key (`Document.ToggleCodeBlock` exists; Cmd+Shift+\ is center align).
- Mod-Enter (leave the code block, else a hard break): missing; Enter in a code block adds a line, a second Enter on an empty line leaves it.

Blockquote callout: missing. `richtext` draws a quote as a bar 3 DIPs wide along the paragraphs (`Theme.QuoteBar`), not a box with an icon, and `[!NOTE]` is not a callout (see quote.md).

Not in the web editor but in `richtext`: tabs in code (Tab inserts a tab), lists with levels, Format menu, attachments.

## 5. 256 KiB (`TestNotebookSpike256KiB`, wall clock, one dev machine)

The starter plus a fence repeated to 262144 bytes, 7527 paragraphs: `Open` about 23 ms, first paint in a 1280x720 tester about 43 ms, one scroll frame about 25 ms, `Markdown` of the whole text about 32 ms. Fast enough that save on every edit is feasible.

## 6. Recommendation

(c) Do not use `richtext` for notebooks: use a `ui.TextArea` source editor beside a read-only `markdown.View` whose code-block hook carries the bar. `richtext` rewrites the user's Markdown on the first save (tables, rules, HTML, hard breaks, callouts, lists, the starter) and merges adjacent fences into one command block, losing their languages, which is both data loss and a wrong command to type. (b) would only avoid the loss for files that change, and the merge happens on files that look ordinary.
