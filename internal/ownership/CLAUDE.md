# internal/ownership — manual-rename protection

Rename a tab, pane or workspace row yourself and the plugin leaves it alone.
There is no rename event: **a user rename is a label that moved between two polls
to something the plugin neither wrote nor wanted.**

## The rule (`judgeByMovement`, tabs and panes)

A label is ours to replace when it is: what we want now; a rename we sent that
got no answer (Herdr may apply it late); unnamed (`""` or the tab's position);
unchanged since the last poll; or seen on the very first poll. Anything else is
the user's, recorded with the label it was claimed with.

## Traps this design walked into

- **The first poll claims nothing** — every tab carries a stale label at start —
  but a tab *first seen after* the first poll already carrying a name was named
  by whoever made it, and is claimed.
- **A late rename** landing after its call timed out once locked 10 of 12 tabs on
  stale numbers. `RenameUnanswered` keeps that label as ours until it lands.
- **Clearing a name is how a user hands a thing back.** `Prune` releases a claim
  whose owner no longer carries the claimed label; an unnamed label is never
  claimed. That same check keeps a reloaded claim off a stranger: Herdr reuses ids.
- **A rename made while the plugin is down is not remembered** — it cannot be
  told from a reused id, and locking a stranger's tab is worse.

## Workspaces are judged on first sight (`judgeFirstSight`)

Herdr labels an unrenamed row after its directory basename and never revisits it,
so on first sight anything else is the owner's. The last label *we* wrote is
persisted (`workspace_labels_written`) so a restarted plugin recognises its own
row. A row with no directory yet is `Undecided` and not written over. Without a
renames file the row is never named — memory alone cannot survive a restart.

The file (`user-renames.json`) is written via temp + rename; failure is silent.
Editing it under a running plugin achieves nothing: it is rewritten from memory.
