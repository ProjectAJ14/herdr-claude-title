# internal/checkout — git state from .git files

Never runs git: reading HEAD is ~0.02 ms, `git rev-parse` ~12 ms, on a poll whose
snapshot is ~0.5 ms. Not cached between polls — a fresh read is cheaper than a
stale answer — only memoised within one poll by directory (`internal/poller`).

- Worktrees and submodules: `.git` is a file (`gitdir: …`); `commondir` leads to
  the shared refs. `SharedGitDir` is equal for a repo and all its worktrees.
- A rebase detaches HEAD but records `head-name`: that branch is still where the
  user is, so the tab keeps its name instead of a new hash every step.
- **A directory that no longer exists is refused** — otherwise the walk up lands
  on the parent repo and shows its branch (a removed worktree did exactly that).
- Paths are compared cleaned, not symlink-resolved: a mismatch only means the
  agent's branch is not used, and resolving costs stat-walks every poll.
- Reads are capped at 4 KB: the directory is pane-derived, hence untrusted.
