---
description: Use when granular branches should be folded into their parent. Trigger phrases include "fold this branch", "squash into parent", and "merge this branch into the previous one". Runs `stackit fold`.
---

# Stack Fold

Combine related branches into one reviewable PR. Folding preserves commits;
`stackit squash` is optional when the user also wants one commit. Prefer grouping
by behavior and dependencies rather than treating every small diff as a fixup.

## Workflow

1. Use `stackit tree short --stack --no-interactive` for relationships. Use
   `stackit info --stack --json --no-interactive` when commit messages, scopes,
   or diff statistics are needed. Use `stackit state --json` when checking the
   working tree or an interrupted operation; filter its output to relevant branches.
2. Identify the branch to fold and its parent. Both must be modifiable in this
   worktree, unlocked, unfrozen, and in the same scope. Never fold into trunk
   unless explicitly requested (`--allow-trunk`).
3. Branches **may have children**: fold reparents them and restacks the affected
   descendants automatically. There is no leaf-only restriction.
4. Explain the proposed grouping. If the user already authorized that grouping,
   proceed; otherwise obtain approval before changing it. Do not ask again for
   an action already authorized in the conversation.
5. Check out the child and preview the operation, using separate commands:

   ```bash
   stackit checkout <branch-to-fold> --no-interactive
   stackit fold --dry-run --no-interactive
   ```

6. Apply the fold:

   ```bash
   stackit fold --no-interactive
   ```

   The parent branch name survives by default. `--keep` instead retains the
   child's name and removes the parent. If `.git` is read-only in the sandbox,
   request escalation on the first mutating command.
7. Check the result. Do not run another restack unless fold reports unfinished
   work. If it stops on a conflict, resolve it through the reported recovery
   path before continuing other operations.
8. Use `stackit tree short --stack --no-interactive` to verify relationships.
   Continue with submission if already authorized. Fold does not close the
   removed branch's PR; account for that cleanup when updating the remote stack.

A combined PR can contain multiple commits. Do not squash merely because a
branch was folded, and do not offer a redundant restack as the next step.
