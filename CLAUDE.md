# CLAUDE.md — Herdr Claude Title (router)

This file is the **map**, not the manual. It carries only what spans packages.
Everything else lives in a nested `CLAUDE.md` next to the code it governs — those
load on demand when you open a file there, so this root stays cheap for every
session. **Read the nested file for the package you are touching, and keep it
current when you change that package.**

## What this is

A Herdr plugin, written in Go, that names every tab, pane and workspace row after
the work in it. One long-lived process polls the Herdr session over its socket
twice a second and renames what no longer fits. A deterministic ladder of sources
names everything; for Claude Code panes, **Claude itself** writes the title from
the user's prompts through `claude -p` on the user's own subscription.

It is a from-scratch rewrite of `kryptamine/herdr-auto-title`: same Herdr contract
and every feature, clearer names, and the Claude engine on top.

```
herdr-claude-title/
├── cmd/herdr-claude-title/   the binary: run (default), restart, preview — wiring only
├── internal/
│   ├── herdr/        socket client + wire types; the only package that speaks to Herdr
│   ├── poller/       the loop: snapshot → pane reads → names → ownership → renames
│   ├── session/      a snapshot as tabs/panes/workspaces; which pane speaks for a tab
│   ├── title/        the naming ladder: sources, sanitising, assembly (deterministic)
│   ├── summarizer/   the Claude engine: async worker, cadence, `claude -p` runner
│   ├── transcript/   tails Claude Code JSONL transcripts: prompts, ai-title, cwd
│   ├── ownership/    tells a user's rename from ours, persists user-owned labels
│   ├── checkout/     reads the git branch from .git files, never by running git
│   ├── singleton/    one instance per session; the restart action
│   └── config/       config.env + environment → Settings; file locations
├── scripts/probe.py  ask the live socket what the plugin sees (stdlib Python)
├── config.env.example  every setting, with its default — the user-facing reference
└── tools/go.mod      the pinned linter, kept out of the plugin's dependency tree
```

The main module has **one** dependency, `github.com/rivo/uniseg` (column widths).
Herdr builds the plugin from source at install time on Go 1.24, so a new import
is a decision, not an accident — `depguard` enforces both.

## Where the detail lives

| Working on… | Read |
|---|---|
| Anything that talks to Herdr, or a surprise about what Herdr reports | `internal/herdr/CLAUDE.md` |
| The loop, what a poll costs, what survives between polls | `internal/poller/CLAUDE.md` |
| Which pane names a tab; process-read reuse | `internal/session/CLAUDE.md` |
| How a label is chosen, the ladder, dedupe rules, widths | `internal/title/CLAUDE.md` |
| Claude titles — cadence, the sealed `claude -p` call, cost, failure | `internal/summarizer/CLAUDE.md` |
| Reading Claude Code transcripts | `internal/transcript/CLAUDE.md` |
| Manual-rename protection | `internal/ownership/CLAUDE.md` |
| Git branch reading | `internal/checkout/CLAUDE.md` |
| Single instance, restart action | `internal/singleton/CLAUDE.md` |
| Settings, config file locations | `internal/config/CLAUDE.md` |
| Adding to any CLAUDE.md — which file, and whether to write it at all | `.claude/skills/updating-claude-md/SKILL.md` |

## How the packages connect

```
main ─ config.Load ─► Settings
  │
  ├─ singleton.Take ─► claim (newest instance wins, older ones leave)
  ├─ summarizer.New + go Run ─► one background worker calling `claude -p`
  └─ poller.Run, every POLL_INTERVAL_MS:
       herdr.ReadSnapshot
       → session.RevisionTracker.Observe        (what moved since last poll)
       → ownership.*.Prune                      (drop the dead, release stale claims)
       → session.TabFrom                        (pick each tab's speaker pane)
       → pollReads.fill(speaker)                (process read, transcript, git,
                                                 summarizer.Title — cached, never waits)
       → title.Namer.NameTab / NamePanes / NameWorkspace
       → ownership.Judge → herdr.Rename*        (only when the user does not own it)
```

**Dependency direction is one-way:** `poller` → `title`, `session`, `ownership`,
`transcript`, `checkout`, `herdr`. `title` knows only `session` and `checkout`.
`summarizer` knows only `transcript`. Nothing imports `poller` but `main`.
A new package that wants to import `poller` is a design smell — raise it.

## Commands

```sh
make              # list every target
make check        # fmt + vet (incl. GOOS=windows) + lint + test -race  ← the gate
make test         # go test -race ./...
make live-claude  # one real Haiku call: proves claude -p works on this machine
make build        # ./herdr-claude-title
./herdr-claude-title preview   # what it WOULD name everything; renames nothing
make run          # run in the current Herdr session, debug logs (takes over!)
make ps / stop    # running instances
make tabs         # current tab and pane labels, from the socket
make snapshot     # the raw snapshot the plugin polls
```

`go test -race`, not `go test`: the summarizer's worker and the poll loop share
its cache.

## Working here — mandatory rules

- **Everything written into the repo is English** — code, comments, commits, docs,
  test names — whatever language the request came in.
- **Never pass a terminal-derived value to a shell or to argv.** Renames go over
  the socket. The one subprocess, `claude`, gets fixed arguments; the user's
  prompts travel on **stdin**. `depguard` allows `os/exec` in `internal/summarizer`
  only.
- **Decide from freshly read state.** Every poll reads the session and throws it
  away. Only what a snapshot cannot say is carried: pane revisions and process
  reads, transcript offsets, Claude titles, ownership. A new cache needs a reason
  of that kind, written in the package's CLAUDE.md.
- **A struct field exists only if code reads it.** Herdr's wire objects carry far
  more; mirroring them makes a type claim a dependency the code does not have.
- **A comment is at most three lines** and says what is surprising, not what is
  visible. A decision that needs a paragraph goes in the package's CLAUDE.md.
- **An interface needs two implementations or a test seam.** Today: `herdr.Client`
  (socket, dry-run, fake), `title.TabNamer` (namer, numbered), `summarizer.Runner`
  (claude CLI, fake), and the poller's consumer-side `Summarizer`/`Instance`.
- **Development runs against the user's real Herdr session**, so their labels
  change while you work. Prefer `preview`; run `make run` in the foreground only,
  and never while another auto-title plugin runs — the two lock each other's tabs.
- **The code is the source of truth, then the nested CLAUDE.md, then a comment.**
  A note that contradicts the code is a bug in the note: fix it in the same change.

## Git workflow

[Conventional Commits](https://www.conventionalcommits.org): `type(scope): subject`.

- Types: `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `chore`, `ci`.
- Scope is the package or area: `summarizer`, `title`, `poller`, `herdr`, `config`.
- Subject imperative, lowercase, no trailing period, ≤72 chars. The body says
  **why** — the diff already says what. One logical change per commit.
- Branch from `main` as `type/kebab-summary` (`feat/claude-retitle-cadence`).
  Never commit to `main` directly.
- `make check` green before every commit. `make live-claude` before merging
  anything that touches `internal/summarizer/claude.go`.

## Every answer ends with what the user has to DO — ALWAYS

- **Group by topic.** A heading per topic, bullets under it, never one long run of prose.
- **Close with `What I need from you:`** — a short list of actions, or the word `Nothing`.
- **Ten lines per topic, hard cap.** Longer means two topics, or detail nobody asked for.
- **A correction is one line**, first: "I was wrong about X; here is the truth."
- **No narrating the investigation.** The finding, not the route to it.

## Always recommend, and say what it costs — ALWAYS

Never hand over a list of options and stop. Every set of options ends with:

- **Which one**, in one line — not "it depends", not two co-favourites.
- **Why** — the reason, not the option restated.
- **What it costs** — time, quota, risk, what stays broken meanwhile.

When the quick fix and the right fix differ, give **both**, say which to do today,
and whether the other still needs a follow-up. A patch with no follow-up is a
decision never to fix it, and should be named as one.

## Check before you conclude — ALWAYS

**Never name a cause you have not verified.** This plugin runs against a live
Herdr: `make snapshot`, `make tabs`, `preview`, `herdr plugin log list` and the
transcripts under `~/.claude/projects` answer most questions in one command.

- **A sample proves what it contains, never what it lacks.** A tail read of a
  transcript missing a prompt proves nothing about the file — which is exactly the
  bug the chunked catch-up in `internal/transcript` fixed.
- **Read the code path before blaming it.** One `grep` for the caller first.
- **Check whether the defect is yours** (`git status`) before reporting it as the
  codebase's.

Say what would make the claim false, then go and look for that. When something
cannot be verified, say so and say what would settle it.

## Chat replies: short. Bullets. No noise. — ALWAYS

This governs chat only; code comments, commits and CLAUDE.md files stay thorough.

- Lead with the answer — the finding, the number, the verdict.
- One line per point. No recaps, no "so in summary".
- Asking a question: the question, then the options, no preamble.
- A gotcha is never noise — keep it, one line, at the end.
