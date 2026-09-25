# internal/poller — the loop

One goroutine. Every `POLL_INTERVAL_MS`: snapshot → prune ownership → assemble
tabs → read the panes worth reading → name → judge ownership → rename what
differs. **The interval is also the rename rate** — a tab changes at most once
per poll however fast its pane churns.

## What a poll spends

- **One snapshot**, always. A settled session costs that and nothing else:
  deduplication (`d.Label == s.Label` → skip) is what keeps it silent.
- **A process read per filled pane**, reused while the revision holds and for at
  most 2 s. Without pane naming only each tab's speaker is filled; with it (the
  default) every unclaimed pane is — the floor is one read per pane.
- **A user-owned tab is skipped unread**, but its speaker is still read when
  panes are named, because panes are named against it.
- **`pollReads` memoises within one poll only**: each pane filled once, each
  directory's checkout read once. It dies with the poll, so it cannot go stale.
- Reads that take no context (files) are bounded by not starting them once the
  poll's 5 s deadline has passed.

## Rules that were learned the hard way

- **No failure is fatal**, the first included: Herdr's socket can lag the process
  it just launched. Failures are logged as the run doubles (`failureRun`).
- **`FirstPollDone` only after a complete poll.** A poll cut short would leave the
  tabs it missed looking new and "already named by the user".
- **Workspaces go last**, so a poll cut short gives up a row, not a tab. Every
  workspace is *judged* each poll even when not named — one holding two tabs
  today may hold one tomorrow.
- **The cached Claude title is read on every fill, caught up or not.** Gating
  the read on `conv.CaughtUp` made a poll that landed mid-write drop the title,
  rename the tab to the rules' answer and back on the next poll. Whether to
  *queue* a new call is the summarizer's decision, and it waits for `CaughtUp`.
- **A successor ends the run**: a newer claim, or a different server on the
  socket. Exit status 0 — the successor is already naming the session.
