---
description: Use when committed changes should be split between the current branch and a new branch. Trigger phrases include "split this commit", "move part of this branch", and "separate these files into another branch". Runs `stackit split`.
---

# Stack Split

Split committed changes on the current branch into another stacked branch.

## Workflow

1. Require a clean working tree:

   ```bash
   git status --porcelain
   ```

2. Inspect branch context:

   ```bash
   stackit state --json
   git log --oneline <parent-branch>..HEAD
   git diff --name-status <parent-branch>..HEAD
   ```

3. Propose what stays and what moves. Keep tests with implementation and keep dependent symbols together.

4. Confirm direction:

   - `--above` for follow-up work in a child branch.
   - default or `--below` for prerequisite work in a parent branch.

5. Prefer file-level split when whole files move:

   ```bash
   stackit split --by-file <files> --above --name "<branch-name>" --message "<message>"
   ```

6. For hunk-level split, write a patch and run:

   ```bash
   stackit split --patch /tmp/extract.patch --above --name "<branch-name>" --message "<message>"
   ```

7. Verify both resulting branches with the detected check command.

8. Finish with `stackit tree --no-interactive`.

Use `stackit undo --no-interactive --yes` only after explicit rollback approval.
