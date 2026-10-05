# internal/summarizer — the Claude naming engine

Asks Claude, through the user's own `claude -p` and subscription, for a title
that says what a Claude Code session is about. The poll loop **never waits** on
it: `Title()` returns the cached title and queues the session when it is due.

## Cadence (`due`)

- First title: as soon as the transcript is caught up and has ≥1 typed prompt.
- Retitle: after `CLAUDE_RETITLE_EVERY_PROMPTS` (5) new prompts, sending the
  current title with the instruction to keep it unless the work moved — stable
  tabs, not a new name every few minutes.
- Never while a call is in flight; never within 2 min of a failed call for that
  session; never again this run once `claude` proved unavailable.
- One worker, one call at a time: a burst of new sessions queues (16 deep; a
  full queue means "ask next poll"), it does not fan out against the quota.

## The sealed call (`claude.go`) — do not loosen without a reason

- `--safe-mode`: no hooks, plugins, MCP servers or CLAUDE.md. Without it the
  child fires the user's SessionStart hooks — including Herdr's own integration
  and anything that spawns sessions — which is how recursive spawning happens.
  It still uses OAuth, unlike `--bare`, which would need an API key.
- `--tools ""`: the prompts are the user's words and untrusted; the model can
  run nothing, so a prompt-injected title can at worst be a bad title — which
  `title.Sanitize` + `Meaningful` + the column cut then bound.
- `--no-session-persistence`: no transcript of its own for Herdr to find.
- `--system-prompt` replaces the default prompt (~3.7k → ~1.4k input tokens).
- `--json-schema` with `maxLength`: the answer is `structured_output.title`,
  and **only** that. When the model chats instead ("I appreciate the
  instruction, but I cannot see the image…") there is no structured output;
  falling back to `result` once put that sentence on a tab. It is a failure now.
- `isTitle` (≤6 words, one line) gates every answer and every title loaded from
  `claude-titles.json`, so a bad title already on disk is dropped at start.
- Prompts go on **stdin**, never argv. `HERDR_*` is stripped from the child's
  environment, and it runs in the temp dir.
- `FindClaude` looks past PATH (`~/.local/bin`, `~/.claude/local`, Homebrew):
  a plugin inherits the Herdr server's PATH, which often lacks the shell's.

## Cost, measured on Haiku 4.5

~1.4k input + ~50–300 output tokens, 5–12 s wall time per call. With the default
cadence a 30-prompt session costs 6 calls (prompts 1, 6, 11, 16, 21, 26).

## Persistence

Titles are kept in `claude-titles.json` in the state dir (`title`,
`titled_at_prompt`) so a restart re-spends nothing. Entries are pruned when no
pane holds the session any more — except one whose call is in flight, which is
kept until the call returns: dropped early, a session that reappears would get a
fresh record and a duplicate call.

## Shutdown

`main` cancels the worker and waits on `Stopped()` (up to 6 s) before exiting.
Returning at once left an in-flight `claude` child running detached, spending
quota on an answer nobody reads.

`make live-claude` spends one real call; run it after touching `claude.go`.
