---
name: conflict-resolution
description: Use when committing, pushing, or resolving Git merge, rebase, or cherry-pick conflicts.
user-invocable: false
metadata:
  internal: true
---

- Commit focused changes on a feature branch and submit them with `git push no-mistakes <branch>`; do not push feature branches directly to `origin`.
- Resolve each conflict hunk manually. Never replace an entire conflicted file with `ours` or `theirs`.
