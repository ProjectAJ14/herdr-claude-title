# internal/session — the session model

A snapshot as `Tab`, `Pane`, `Workspace`, rebuilt every poll. `RevisionTracker`
is the one thing here that outlives a poll.

## Which pane speaks for a tab (`Tab.Speaker`)

Picked once in `TabFrom`, so what is read and what is named cannot differ:
1. the focused pane; 2. a pane whose agent is `working` or `blocked`; 3. any.
Each rule takes the most recently changed pane it accepts; ties break on the
lowest ID. `PREFER_AGENT_PANE` puts "any pane with an agent, in any state" first,
so an editor opened beside the agent does not steal the title.

## Process-read reuse

`pane.process_info` is the only source of a process name and costs a request per
pane. A read is reused while the pane's revision holds **and** for at most 2 s
(`processReuseLimit`). Both, because a revision alone missed 5 of 9 process
changes in a measured live session. Raising the limit trades freshness for
requests; measure before touching it.

A revision that goes *backwards* is a new pane wearing a recycled id, so any
difference counts as a change, not just an advance.
