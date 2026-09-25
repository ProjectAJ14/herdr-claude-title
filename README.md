# Claude Title for Herdr

[![CI](https://github.com/ProjectAJ14/herdr-claude-title/actions/workflows/ci.yml/badge.svg)](https://github.com/ProjectAJ14/herdr-claude-title/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/ProjectAJ14/herdr-claude-title)](https://goreportcard.com/report/github.com/ProjectAJ14/herdr-claude-title)
[![Go version](https://img.shields.io/github/go-mod/go-version/ProjectAJ14/herdr-claude-title)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A [Herdr](https://herdr.dev) plugin that names your tabs, panes and workspace
rows after the work in them — and lets **Claude** write the title of every
Claude Code session from what you actually asked it, using your own
subscription through `claude -p`.

```
~/work/dashboard                         →  1 · dashboard
~/work/dashboard on feature/MC-13200     →  2 · dashboard › MC-13200
nvim editing auth.provider.ts            →  3 · nvim › auth.provider.ts
Claude Code fixing a flaky login test    →  4 · Add token refresh retry
ssh into prod-01                         →  5 · ssh › prod-01
```

A rewrite of [kryptamine/herdr-auto-title](https://github.com/kryptamine/herdr-auto-title)
with every feature it has, plus the Claude naming engine. Works on macOS, Linux
and Windows; one dependency; no network use except the `claude -p` call.

## Install

Needs Herdr 0.8.2+, Go 1.24+, and for Claude titles the `claude` CLI signed in.

```sh
herdr plugin install ProjectAJ14/herdr-claude-title
herdr integration install claude          # tells Herdr which session a pane holds
herdr plugin action invoke herdr.claude-title.restart
```

Herdr only starts plugins when its server starts; the last line starts this one
now. **Run one auto-title plugin at a time.** Two would each read the other's
renames as yours and lock every tab — disable `herdr.auto-title` first.

Try it before switching: `./herdr-claude-title preview` (from the plugin
directory, inside a Herdr pane) prints what it would name everything, Claude
titles included, and renames nothing.

## How Claude names a session

- On your first prompt the session is queued for a title; until Claude answers
  (5–10 s on Haiku) the rules name the tab, so nothing waits.
- Every 5 new prompts it is asked again, shown the current title, and told to
  keep it unless the work moved on — the title follows the conversation
  without flickering.
- It reads only what you typed (first prompt and the last six), never the
  agent's replies or tool output.
- The call is sealed: `--safe-mode` (no hooks, plugins, MCP or CLAUDE.md),
  `--tools ""` (it can run nothing), `--no-session-persistence` (no transcript
  of its own). One call at a time; titles are cached across restarts.
- No `claude` on the machine, or `NAMING_ENGINE=rules`: the deterministic
  rules name everything, exactly like the original plugin.

## What the rules do

- The number in front is the tab's position — the key that switches to it.
- The default branch is left out; long branches shrink to their ticket key
  (`bugfix-asa-cpanel-uapi-mc-13675` → `MC-13675`).
- A tab with several panes is named after the focused pane, else a busy agent,
  else the pane that changed last.
- Each pane gets its own name, so the goto panel stops listing every agent as
  `claude`.
- Rename a tab, pane or row yourself and it is left alone. Clear the name to
  hand it back.

> [!WARNING]
> The first start renames every pane, including ones you named by hand before
> installing. After that, a rename you make is protected.

## Configuration

Copy [`config.env.example`](config.env.example) to
`~/.config/herdr-claude-title/config.env` and uncomment what you need; every
setting is listed there with its default. The file is read once — restart after
editing:

```sh
herdr plugin action invoke herdr.claude-title.restart
```

| Setting (`HERDR_CLAUDE_TITLE_…`) | Default  | What it does                                         |
| -------------------------------- | -------- | ---------------------------------------------------- |
| `NAMING_ENGINE`                  | `claude` | `claude` or `rules`                                  |
| `CLAUDE_MODEL`                   | `haiku`  | Model used for titles                                |
| `CLAUDE_RETITLE_EVERY_PROMPTS`   | `5`      | New prompts before asking again                      |
| `CLAUDE_TIMEOUT_SECONDS`         | `60`     | Give up on one call after this                       |
| `CLAUDE_BINARY`                  | found    | Path to `claude`                                     |
| `TAB_MAX_COLUMNS`                | `30`     | Longest tab title, in columns                        |
| `SHOW_TAB_NUMBER`                | `true`   | Position in front of the title                       |
| `SHOW_AGENT_NAME`                | `false`  | `claude ›` in front of what it is doing              |
| `BRANCH_MAX_COLUMNS`             | `12`     | Longest branch; `0` hides branches                   |
| `NAME_PANES`                     | `true`   | Name panes as well as tabs                           |
| `PREFER_AGENT_PANE`              | `false`  | Name a tab after its agent even when unfocused       |
| `NAME_WORKSPACES`                | `false`  | Name one-tab workspace rows                          |
| `WORKSPACE_MAX_COLUMNS`          | `20`     | Longest workspace row                                |
| `READ_TRANSCRIPTS`               | `true`   | Read Claude Code transcripts (needed for Claude)     |
| `EXTRA_CLAUDE_HOMES`             | none     | More Claude config homes, `:`-separated              |
| `LOG_LEVEL`                      | `info`   | `debug`, `info`, `warn`, `error`                     |
| `POLL_INTERVAL_MS`               | `500`    | How often the session is read                        |
| `USER_RENAMES_FILE`              | beside   | Where your manual renames are remembered             |

Coming from `herdr-auto-title`: `MAX_LENGTH` → `TAB_MAX_COLUMNS`,
`BRANCH_MAX` → `BRANCH_MAX_COLUMNS`, `POSITION` → `SHOW_TAB_NUMBER`,
`AGENT_NAME` → `SHOW_AGENT_NAME`, `PANES` → `NAME_PANES`,
`PREFER_AGENT` → `PREFER_AGENT_PANE`, `WORKSPACES` → `NAME_WORKSPACES`,
`WORKSPACE_MAX_LENGTH` → `WORKSPACE_MAX_COLUMNS`, `TRANSCRIPT` →
`READ_TRANSCRIPTS`, `CLAUDE_DIRS` → `EXTRA_CLAUDE_HOMES`, `MANUAL_FILE` →
`USER_RENAMES_FILE`, `POLL_MS` → `POLL_INTERVAL_MS`, `DEBUG=true` →
`LOG_LEVEL=debug`.

## Restart on a key

```toml
# Herdr's config.toml
[[keys.command]]
key = "prefix+R"
type = "plugin_action"
command = "herdr.claude-title.restart"
description = "restart claude title"
```

## Development

```sh
make check        # fmt, vet (incl. Windows), lint, test -race — the gate
make live-claude  # one real Haiku call
```

Architecture, the rules and every package's gotchas are in [CLAUDE.md](CLAUDE.md)
and the `CLAUDE.md` beside each package. Issues and pull requests welcome.

## License

[MIT](LICENSE). Built on the design of
[herdr-auto-title](https://github.com/kryptamine/herdr-auto-title) by Alexander
Satretdinov.

