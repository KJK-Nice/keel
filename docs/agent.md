# Agent (in progress)

The agent is being built in phases (`docs/ui-redesign.md` §6, §10). Its rules: the agent never writes to a PTY, never sees vault contents, and every read or insert is logged.

## The agent panel

Phase 6b adds the **agent panel** (right side, **⌘I** on macOS, **Ctrl+Shift+I** on Windows/Linux, the titlebar **Agent** button, or View › Toggle Agent Panel).

You ask about the panes in its scope (the chip "Reads N of M panes"); keel serve reads their last lines (or, when the shell reports its commands with OSC 133, the last command, its output and its exit status; see [Shell integration](shell-integration.md)), redacts them, and sends that and your question to the model provider you chose (Google Gemini or Anthropic Claude), only when you press send.

The answer can carry command proposals: Keel shows the exact bytes and a Read-only / Changes state / Destructive badge, and nothing is typed until you press **Type into pane** and approve the bytes (destructive commands, and state changes on a production host, need **⇧** as well).

The key goes in the panel's Connect field (Connect Gemini or Connect Claude) and is kept in the vault. Conversations are kept in memory only, until New conversation or `keel serve stop`.

`KEEL_AGENT_PROVIDER=fake` is for end-to-end tests only: the agent then answers from an in-process fake and nothing is sent anywhere.

## Model providers

| Provider | Models | In-pane suggestions |
|---|---|---|
| Google Gemini (default) | `gemini-3.8-flash`, `gemini-3.6-flash`, `gemini-3.5-flash-lite` | `gemini-3.5-flash-lite`, minimal thinking |
| Anthropic Claude | `claude-sonnet-5-5` (default), `claude-opus-5-5`, `claude-haiku-5-5` | `claude-haiku-5-5`, low effort |

Connect a key for either in **Settings · Agent**, then pick a model there: the Model list holds every provider's models, and picking one switches the agent to that model's provider. The first-run screen offers Gemini only; Claude is set up in Settings.

The Claude client calls the Messages API (`POST /v1/messages`, key in `x-api-key`). Keel's thinking levels map to `output_config.effort` (minimal and low → `low`, medium → `medium`, high → `high`; none → the model's default), and `max_tokens` leaves room for thinking on top of the answer, since thinking counts toward it. Thinking text is never asked for or shown.

## Claude Code (keel mcp)

`keel mcp` is an [MCP](https://modelcontextprotocol.io) server on stdio that lets Claude Code work with your Keel panes under the same rules as the agent panel (`docs/ui-redesign.md` §6, rule 14). Keel must be open and the vault unlocked; `keel mcp` never starts `keel serve` itself.

It is **off by default**. Turn it on in Settings · Agent → Claude Code → **Allow Claude Code (keel mcp)**, or on macOS from the same switch in Keel's menu bar item (docs/runtime.md). Until then every `/api/mcp/` route answers 403 and Claude Code is told `Claude Code is off in Keel: turn on "Allow Claude Code (keel mcp)" in Settings · Agent`; nothing is listed, read, proposed or logged. Unlike the agent panel, which reads only when you Ask, Claude Code reads whenever it decides to, so turn it on only while you use it, and only hosts with **Agent can read** on are visible to it (new hosts, Local Shell included, start with it on; turn it off in Edit host for hosts Claude Code should not see).

```bash
# The keel binary: on macOS the one in Keel.app (bin/Keel.app/Contents/MacOS/keel
# when built from source, or wherever you put Keel.app); elsewhere bin/keel.
claude mcp add keel -- /Applications/Keel.app/Contents/MacOS/keel mcp
```

Or in a project's `.mcp.json`:

```json
{ "mcpServers": { "keel": { "command": "/Applications/Keel.app/Contents/MacOS/keel", "args": ["mcp"] } } }
```

Set `KEEL_ADDR` / `KEEL_DATA` in the server's `env` if your Keel uses others.

| Tool | What it does |
|---|---|
| `list_panes` | Open panes on hosts with **Agent can read** on: pane id, host, production. Others are only counted. |
| `read_pane` | A pane's last lines (or its last command, from OSC 133), redacted like an Ask. Logged as `agent.read`, Source `claude-code`. |
| `propose_command` | Puts a command in the agent panel, conversation **Claude Code**. It is not run: you see the exact bytes and type it, or not, as any proposal (⇧ for destructive commands and for state changes on production hosts). |
| `proposal_status` | Whether you typed a proposal, and whether you edited it first. |

There is no tool that types: Claude Code cannot run anything in a pane. Proposals appear in the agent panel's conversation list (reopen the list to see new ones); with **Keep conversations** set to "Only while the panel is open", the conversation is not listed, so keep history on to use it.

`keel mcp` sends its own token, `keel-mcp.token` in `KEEL_DATA` (made by `keel serve` on start). It opens `/api/mcp/` only, never the vault or the rest of the API, and the UI's token does not open `/api/mcp/`. That keeps keel mcp to its routes, but it is not a sandbox: any program running as you, Claude Code's shell commands included, can read `KEEL_DATA` (and `keel-serve.token` in it). Deny it in Claude Code's permissions, for example in `~/.claude/settings.json`:

```json
{ "permissions": { "deny": ["Read(~/Library/Application Support/keel/**)"] } }
```

(`~/.config/keel` on Linux, `%AppData%\keel` on Windows), and keep approving Claude Code's shell commands yourself.

## Backend

- Each host has **Agent can read** (`agentReadable`, on by default) and **Treat as production** (`production`, off by default; turned on when a host is created in or moved into a group named `production`). Older clients that do not send the fields keep the saved values.
- Agent settings (provider, model, detection, default scope, scrollback lines 50–1000, IP/hostname redaction, history) are in `keel.json` and at `GET`/`PUT /api/agent/settings`. Secret redaction is always on.
- Provider keys (Gemini, Anthropic, Jev) are vault secrets, set with `PUT /api/agent/keys/{provider}` and removed with `DELETE`. No response ever returns a key, only `configured: true/false`.
- Agent reads and inserts are written to `keel.log` as `agent.read`, `agent.suggest`, `agent.insert` and `agent.type`, with line counts and redaction counts, never the text.
