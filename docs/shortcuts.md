# Tabs, splits and shortcuts

On macOS the tab strip lives in the window titlebar, beside the traffic lights. Drag empty chrome to move the window. Windows and Linux keep a separate OS titlebar.

**Hold ⌘** (Ctrl on Windows/Linux) to overlay keys on tabs and buttons.

## Keys

| macOS | Windows / Linux | Does |
|---|---|---|
| ⌘T | Ctrl+T | New tab page |
| ⌘W | Ctrl+W | Close the focused pane (killing that PTY); with one pane left, close the tab (a tab page closes too) |
| ⌘⇧T | Ctrl+Shift+T | Reopen the last closed tab or pane as a **new** session |
| ⌘1 … ⌘8 | Ctrl+1 … Ctrl+8 | Switch tabs by position (⌘1 is the first tab: the Vault is a sidebar, not a tab) |
| ⌘9 | Ctrl+9 | Last tab |
| ⌘< / ⌘> (or ⌘⇧[ / ⌘⇧]) | Ctrl+< / Ctrl+> (or Ctrl+Shift+[ / Ctrl+Shift+]) | Previous / next tab (wraps) |
| ⌘B | Ctrl+B | Show or hide the Vault sidebar |
| ⌘I | Ctrl+Shift+I | Show or hide the agent panel (Ctrl+I is Tab in a terminal, so it stays with the shell) |
| ⌘K / ⌘⇧P | Ctrl+K / Ctrl+Shift+P | Command palette |
| ⌘⇧D | Ctrl+Shift+D | Split → |
| ⌘⇧E | Ctrl+Shift+E | Split ↓ |
| ⌘⇧↵ | Ctrl+Shift+Enter | Focus view: the focused pane fills the tab |
| ⌘F | Ctrl+F | Find in the terminal (⌘G / ⌘⇧G next / previous while find is open) |
| ⌘N | Ctrl+N | New host (or the page's own New) |
| ⌘⇧O | Ctrl+Shift+O | Quick connect |
| ⌘J | Ctrl+J | Join session |
| ⌘, | Ctrl+, | Settings |
| ⌘+ / ⌘- / ⌘0 | Ctrl++ / Ctrl+- / Ctrl+0 | Terminal text bigger / smaller (10–22px) / reset to 12 |
| ⌘⇧W | Ctrl+Shift+W | Close the window (sessions stay in `keel serve`) |
| ⌥1 … ⌥6 | Alt+1 … Alt+6 | On the New tab page: open a recent host |

Drag tabs to reorder; numbered shortcuts follow the current order.

## New tab page

**⌘T** opens the **New tab page** (also what an empty window shows). Its one input takes a saved host (filters the cards), `user@host:port`, an `ssh://` URL or a pasted `ssh …` command; below it are the six most recent hosts (**⌥1**–**⌥6** open them), Quick connect, Join session, New host, and Recently closed.

A pasted command is parsed, never run: Keel shows what it kept and which options it left out (`-o ProxyCommand`, `LocalCommand`, `StrictHostKeyChecking=no`, `-L/-R/-D`, `-W`, a trailing remote command, `-J` jump hosts, which are not supported yet; `-i` becomes a note and the key file is not read), then offers a prefilled **New host** or **Connect once**.

The window always keeps a tab: with none left it shows a New tab. **⌘⇧T** reopens to the same host; the list is kept in the browser's storage as host id, title and time.

## Command palette

**⌘K** / **⌘⇧P** opens the command palette in four groups: **Open now** (every tab, and the panes of split tabs; picking one focuses it, it never starts a second session), **Hosts** (new session on a host), **Actions** (tabs, panes, vault pages, settings) and **Snippets**. **↵** opens, **⌥↵** opens a host in a split of the focused pane, **esc** closes. The sidebar filter still searches hosts (Enter connects the first match, or opens Quick connect for `user@host`); the palette includes **Search hosts**.

Palette actions that would type into a live session (**Type snippet**) always show **approve-before-type**: the bytes are previewed and nothing is written until you click **Type**. Snippet clicks in the sidebar and **Type into pane…** on the Snippets page ask the same way.

## Sessions

- Open a host: new session tab. Close tab = kill that PTY. Close the window = detach (sessions stay in `keel serve`; reopen Keel to attach, and they come back as tabs, with the tab pages you had open).
- SSH hosts keepalive every 30s (edit host to change or turn off) and auto-reconnect on a drop. `exit` on the remote shell stays dead; **Reconnect** on the pane brings it back.
- Keys typed while a pane is still connecting or reconnecting are dropped, not saved for later: the first thing that comes up may be a password or host key prompt. An approved Type (snippet, agent proposal, group run) at that moment fails and says nothing was typed; press it again once the pane is connected.
- **Split →** / **Split ↓** spawn another PTY to the same host.
- **Sync** broadcasts keystrokes (and approved snippet typing) to every driving pane in that tab.
- File → **Stop Keel runtime** (or `keel serve stop`) kills leftover sessions and the daemon.

## Settings

- Font, text size, copy-on-select, right-click paste (stored in `localStorage`).
- Appearance · **Window translucency** (desktop window only): Off, Low, Medium (default) or High, how much of the desktop shows through the window. Off is opaque. On Windows and Linux the window stays opaque while the theme differs from the system's. Stored in `localStorage` with the other window preferences and applied before the first paint.
