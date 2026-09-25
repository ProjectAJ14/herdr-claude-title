# internal/transcript — reading Claude Code sessions

Herdr says which session a pane holds (`agent_session`, via
`herdr integration install claude`); the JSONL under
`<claude home>/projects/<slug>/<session-id>.jsonl` says what it is about.

## The format is Claude Code's, undocumented, and can change

Read today, verified on Claude Code 2.1.282:
- `type:"ai-title"` + `aiTitle` — Claude Code's generated title; the last wins.
- `type:"user"` with `origin.kind:"human"` — **only** what the user typed.
  Slash-command expansions and resume caveats also arrive as `user` lines,
  without that origin; reading them names tabs after plumbing.
- `cwd` on most lines — where the agent last worked (a worktree, not the pane).

A transcript that stops carrying these yields nothing and the sources decline:
the failure mode is the old title, never a wrong one.

## Reading rules

- **Append-only, so tail by offset.** Each poll reads only new whole lines; a
  half-written last line waits for the next poll. A file shorter than the offset
  is a different file and starts over.
- **Catch up in 2 MB chunks, one per poll — never skip to the tail.** The first
  version read only the last 2 MB of a file first seen mid-flight and so missed
  the one prompt of a 2.4 MB agentic session. `CaughtUp` says the counts are final.
- A line longer than a chunk is skipped chunk by chunk (`midLine`).
- **The session id becomes a path**, so anything not UUID-shaped is refused.
- A session not found is searched again only every 10 s: the fallback glob walks
  every project directory.
- `RecentPrompts` keeps the last 6 prompts (400 runes each) for the summarizer.
