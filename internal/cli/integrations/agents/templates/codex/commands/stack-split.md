---
description: Use when committed changes should be split between the current branch and a new branch. Trigger phrases include "split this commit", "move part of this branch", and "separate these files into another branch". Runs `stackit split`.
---

# Stack Split

Split committed changes on the current branch into another stacked branch.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Require a clean working tree:

   ```bash
   git status --porcelain
   ```

2. Inspect branch context:

   ```bash
   stackit state --json | jq '.current_branch as $c | {current_branch:$c, branch:(.stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen})}'
   git log --oneline <parent-branch>..HEAD
   git diff --name-status <parent-branch>..HEAD
   ```

3. Propose what stays and what moves. Keep tests with implementation and keep dependent symbols together.

4. Confirm direction:

   - `--above` for follow-up work in a child branch.
   - default or `--below` for prerequisite work in a parent branch.

5. Prefer file-level split when whole files move:

   ```bash
   mkdir -p tmp
   printf '%s\n' "<message>" > tmp/stackit-message.txt
   stackit split --by-file <files> --above --name "<branch-name>" -F tmp/stackit-message.txt --no-interactive
   ```

6. For hunk-level split, write a patch and run:

   ```bash
   stackit split --patch /tmp/extract.patch --above --name "<branch-name>" -F tmp/stackit-message.txt --no-interactive
   ```

   If sandbox metadata shows `.git` is read-only, run `stackit split` with escalation on the first attempt.

7. Verify both resulting branches with the lightest detected check command.

8. Finish with the compact current-branch view. Use `stackit tree --no-interactive` only if ancestry is unclear.

Use `stackit undo --no-interactive --yes` only after explicit rollback approval.
