---
name: updating-claude-md
description: Use when adding to or editing any CLAUDE.md in this repository, when a package gained behaviour its notes do not mention, when a claim in a CLAUDE.md turned out to be wrong, or when something feels worth recording "for context".
---

# Updating a CLAUDE.md

Every CLAUDE.md from the working directory upward loads at session start, so a
root-level line is paid for by sessions that never touch that area. Two questions,
in order — most notes die at the first:

1. **Would a future session act differently for knowing it?** If opening the file
   says the same thing, or it restates structure `grep` answers, write nothing.
2. **Which is the deepest file covering every case it applies to?**

| True of… | Goes in |
|---|---|
| every package | the root CLAUDE.md |
| one package | that package's CLAUDE.md |
| one function | a comment beside it (≤ 3 lines) |

- A router lists its children, never its grandchildren.
- Record **why** and **what was measured**, not how it got built. A plan or status
  belongs in an issue, not here.
- Fixing a stale claim: fix it and say in the commit what it used to say.
