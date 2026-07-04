---
description: Use when fixup or WIP commits should be cleaned up across the stack. Trigger phrases include "clean up fixups", "squash WIP commits", and "tidy the stack". Squashes branches whose history is dominated by noise commits using `stackit squash`.
---

# Stack Tidy

Squash branches whose history is dominated by fixup, WIP, or noise commits, leaving a clean per-branch story.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Inspect:

   ```bash
   git status --short
   stackit state --json | jq '{current_branch,trunk,working_tree,operation,branches:[.stack.branches[] | {name,parent,is_current,is_trunk,needs_restack,is_locked,is_frozen,children:(.children // [])}]}'
   ```

2. If there are uncommitted changes, stop and ask whether to commit, stash, or abort.

3. For each non-trunk branch in the stack, list its commits:

   ```bash
   git log --format="%H %s" <parent>..<branch>
   ```

4. Classify each commit:

   - **Cleanup/noise**: starts with the autosquash markers **fixup!** or **squash!**; case-insensitive match on `wip`, `tmp`, `temp`, `fix`, `oops`, `typo`, `lint`, `fmt`, `cleanup`, `nit`, `tweak`; contains phrases like `fix typo`, `address review`, `review feedback`, `lint fix`, `formatting`; or any single-word message under 10 characters.
   - **Meaningful**: matches Conventional Commits (`type(scope): desc`), is the first commit on the branch, or has a descriptive message over 15 characters that does not match cleanup patterns.

5. Pick a per-branch strategy:

   | Branch state | Strategy |
   |---|---|
   | 0 or 1 commits | skip |
   | 1 meaningful + only noise | squash, keep the meaningful message |
   | 0 meaningful, all noise | squash, keep oldest message |
   | 2+ meaningful commits | leave for manual review |

6. Show the user a compact plan: branch, proposed action, kept message, and count
   of noise commits. Expand commit lists only when asking about a branch that
   needs manual review.

7. Wait for explicit user approval before any squash. If the user asks to review one branch at a time, confirm each before squashing.

8. Execute bottom-up (closest to trunk first). For each squash branch:

   ```bash
   stackit checkout <branch> --no-interactive
   stackit squash --no-edit --no-interactive
   ```

   When a meaningful message should replace the existing one:

   ```bash
   mkdir -p tmp
   printf '%s\n' "<meaningful message>" > tmp/stackit-message.txt
   stackit squash -F tmp/stackit-message.txt --no-interactive
   ```

   If sandbox metadata shows `.git` is read-only, run `stackit checkout` and `stackit squash` with escalation on the first attempt.

9. Verify:

   ```bash
   stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
   ```

## Do Not

- Squash a branch the user has not approved.
- Squash branches with only one commit.
- Process top-down — children before parents corrupts ancestry.
- Modify branches flagged for manual review.
- Use `git rebase -i`; use `stackit squash`.
