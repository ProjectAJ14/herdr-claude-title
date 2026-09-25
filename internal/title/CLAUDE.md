# internal/title — the naming ladder

A label reads general → particular: `place › branch › agent › activity`, one
separator throughout, the tab number in front with its own mark (`3 · `).

## The ladder (`sources.go`)

| Rank | Source | Supplies |
|---:|---|---|
| 90 | `agent_title` | activity (rare: agents mostly leave it null) |
| 85 | `claude_summary` | activity — written by `internal/summarizer` |
| 80 | `terminal_title` | activity (Claude Code's own ai-title arrives here) |
| 75 | `transcript` | activity: ai-title, else the first prompt |
| 70 | `process` | activity: a lone non-shell program (`nvim`) |
| 60 | `ssh` | place: `ssh › host` |
| 40 | `branch` | branch |
| 30 | `directory` | place: the directory basename |
| 10 | fallback | `Shell` |

A source never overrides a part a higher one supplied; a lower one can complete
the other half. The activity's source answers for the title. The Claude summary
sits above the terminal title on purpose: for Claude Code the terminal title *is*
Claude Code's own generic ai-title, and the summary is the better answer.

## Rules worth knowing before changing anything

- **Everything is untrusted.** `Sanitize` strips ANSI, controls and format
  characters (bidi overrides, zero-width spaces — except the ZWJ emoji need),
  normalises separators so a value cannot forge structure, and cuts by **columns
  and grapheme clusters**, never runes. It is idempotent. `Meaningful` refuses
  locations, program names and shell prompts. A Claude title goes through both.
- **What the row above says is dropped**: a tab drops what its workspace row
  says; a pane drops what its tab's *parts* (not finished label) say. The
  activity always stays, and a title reduced to nothing keeps what it had.
- **With `NAME_WORKSPACES` the row is read back as segments** (`rowSegments`),
  because the plugin wrote it that way; otherwise it is opaque user text matched
  whole against the place.
- **Branch**: trunk says nothing (from `origin/HEAD`, else main/master/trunk); a
  fitting name stays whole; an over-long one yields its tracker key, else its last
  segment cut at a word. The agent's worktree branch wins when it is the same
  repository. Branches stay out of ssh panes.
- **The agent's name is a part, not a prefix**, so `SHOW_AGENT_NAME=false` means
  off everywhere and repetition checks see the activity as it is.
- **The workspace ladder has no `process`**: a row that followed the foreground
  process would rewrite itself at every prompt. Long rows drop parts from the
  **front** (`FormatKeepingEnd`) — a sidebar column of rows starting the same
  would otherwise all read the same.
- **The tab number is a decorator (`WithTabNumber`), not a source**, counted
  against the width, and dropped before the name when the bar is too narrow.
