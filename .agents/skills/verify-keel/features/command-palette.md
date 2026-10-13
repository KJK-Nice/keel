# Command palette

⌘K opens a filterable list of commands and hosts: tabs, Vault sidebar and tab pages,
new host, quick connect, join.

## Sub-features

- Open with ⌘K (Meta+k) from any focused control.
- Filter by typing in the combobox; Enter runs the selected option.
- Sections: Tabs (New tab, New Local Shell, Close pane or tab, Reopen last
  tab, Previous/Next tab), Vault (Toggle vault sidebar ⌘B, Search hosts, New
  host, Quick connect, Join session), and more below.

## How to get to it (user POV)

Press ⌘K anywhere in the app. No account.

## Driving it with e2e

No test yet. Locators, from a trace screen:
`screen.getByRole('searchbox', 'Search hosts').press('Meta+k')` opens it;
`screen.getByRole('dialog', 'Command palette')`,
`getByRole('combobox', 'Run a command or connect a host…')`,
`getByRole('option', 'New Local Shell')`; options with a shortcut include
it in the name (`'New tab ⌘T'`). Live walk: `press Meta+k`, `observe`,
`type` a filter, `press Enter`.

## Proof

On screen: the dialog opens with the combobox focused, typing narrows the
options, Enter does the option's action (a new tab, the host dialog) and
the palette closes.

## Charters

```
palette-run|web|default|Starting at the Vault sidebar (the app opens with it beside a New tab), press Meta+K and run each Tabs and Vault option once; report options that do nothing, do something else, or leave the palette open
```

## Gotchas

- The `.press('Meta+k')` must target a focused control; a press on the body
  may not reach the listener.
