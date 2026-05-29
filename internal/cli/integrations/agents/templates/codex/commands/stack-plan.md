---
description: Use when uncommitted changes should be split into multiple stacked branches. Trigger phrases include "plan a stack", "split these changes into PRs", "break this into stacked branches", and "these changes touch too much for one PR". Backs up the working tree, proposes a stack, and validates each branch.
---

# Stack Plan

Split uncommitted working-tree changes into multiple stacked branches. Primary objective: never lose the user's work.

## Workflow

1. Gather changes:

   ```bash
   git status --short
   git diff --cached
   git diff
   git ls-files --others --exclude-standard
   ```

   Read untracked files before planning. If there are no changes, stop.

2. Propose a plan before executing. Group by concern, dependency order, and architecture. Keep tests with implementation. File-level granularity is the default; ask the user to split manually with `git add -p` when one file contains unrelated work for multiple branches.

3. Validate branch names:

   ```bash
   git branch --list <name>
   git ls-remote --heads origin <name>
   ```

4. Detect check command: prefer `mise run check`, then `make test`, then `npm test`. Ask if none is discoverable.

5. Pre-validate the combined changes:

   ```bash
   git add -A
   <check-command>
   ```

6. After user approval, create a backup:

   ```bash
   ORIGINAL=$(git branch --show-current)
   BACKUP="stack-plan-backup-$(date +%s)"
   git checkout -b "$BACKUP"
   git add -A
   printf 'stack-plan: backup of all changes' | git commit -F -
   BACKUP_SHA=$(git rev-parse HEAD)
   git checkout "$ORIGINAL"
   ```

7. For each planned branch:

   ```bash
   git checkout "$BACKUP_SHA" -- <files-for-this-branch>
   git diff --cached --stat            # MUST be non-empty before creating
   printf '%s\n' "<message>" | stackit create -F - <name> --no-interactive
   git log -1 --stat                   # verify the new branch actually committed the files
   <check-command>
   ```

   **Verify each branch is non-empty.** `stackit create` with nothing staged
   produces an *empty* branch. If `git diff --cached --stat` is empty before
   create, or `git log -1 --stat` shows no files after, STOP — do not continue
   to the next branch. Recover with:

   ```bash
   git checkout -B "$ORIGINAL" "$BACKUP_SHA"
   ```

8. Stop conditions:
   - **Success:** every planned branch created non-empty and `<check-command>`
     passed on each. Then clean up:

     ```bash
     git branch -D "$BACKUP"
     stackit log --no-interactive
     ```

   - **Failure:** any create produced an empty branch, any check failed, or any
     step errored. STOP immediately, restore the original branch from the backup
     (`git checkout -B "$ORIGINAL" "$BACKUP_SHA"`), and report what happened. Do
     not delete the backup branch. See
     [stack-plan-recovery.md](../stackit/references/stack-plan-recovery.md).
