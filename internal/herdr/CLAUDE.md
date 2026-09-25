# internal/herdr — the socket client

The only package that speaks to Herdr. Everything here was verified against
Herdr 0.8.2 (protocol 20); the snapshot shape and labels were re-checked on 0.9.1. **Probe before assuming**:
`make snapshot`, `scripts/probe.py`. A probe that teaches something new goes here.

## The traps

- **One request per connection.** Herdr closes after answering, so every `Call`
  dials its own. That is why nothing reconnects anywhere.
- **A call that gave up has not undone its request.** Herdr applies a request it
  has read even after the caller hangs up — a stalled server was measured applying
  renames 3–18 s late. So a failure *after* sending is `ErrUnanswered`, and
  `ownership` keeps that label as ours. A failure *before* sending (dial refused)
  must not be `ErrUnanswered`, or a user who later picks that name loses it.
- **Params are required on every method**, even `{}`.
- **Six methods, no others:** `session.snapshot`, `pane.process_info`,
  `tab.rename`, `pane.rename`, `workspace.rename`, `notification.show`.
- **No event subscription.** `events.subscribe` replays ~10 s of backlog per pane
  with no cursor to skip it. A snapshot is 0.5 ms and describes the present.
- **An unnamed tab reports its position, or `""`** once a name is cleared
  (`tab.rename` stores `""` verbatim). `TabInfo.number` is *not* the label — it
  counts every tab ever made and never repeats. Reading it as the label once
  locked every new tab.
- **A pane has one unnamed spelling, `""`.** Herdr omits `label` until a pane is
  named, and `pane.rename ""` clears rather than stores.
- **No single field is a pane's directory.** `cwd` is the shell's (a subshell
  leaves it behind); `foreground_cwd` is the deepest descendant's (an MCP server
  drags it elsewhere). Only `pane.process_info`'s last process has it right.
- **A revision says the pane drew, not what runs in it** — measured moving for 4
  of 9 process changes. See `internal/session/CLAUDE.md`.
- **`PaneInfo.title` is null in practice for Claude Code**; its topic comes
  through `terminal_title_stripped`. `agent_session` stays null until
  `herdr integration install claude`.
- **On Windows** the socket is the named pipe `\\.\pipe\` + the whole path, and
  `pane.process_info` lists only the shell or a recognised agent — no editors,
  no ssh. Names carry `.exe` and dirs a trailing `\`; `session` strips both.
- **Server identity**: device+inode of the socket file (Unix) or the pid:start
  marker written into it (Windows). A new server means a new instance is starting
  — see `internal/singleton/CLAUDE.md`.

## Conventions

- Wire types mirror only what is read, and decode `null` to `""`.
- `DryRun` wraps any `Client` and swallows renames; it is what `preview` runs.
