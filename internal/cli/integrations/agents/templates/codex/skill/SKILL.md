---
name: stackit
description: Shared guidance for Stackit stacked-branch workflows. Use when the request mentions stacks, stacking, stacked PRs, or Stackit-specific operations and needs general Stackit policy, workflow safety rules, or a stack health check.
---

# Stackit

Stackit manages stacked Git branches: small dependent PRs instead of one large review. Use this skill for shared policy and orientation; use the narrower `stack-*` skills for specific operations.

## Always

- Pass `--no-interactive` on Stackit commands (or set `STACKIT_NO_INTERACTIVE=1` once in the environment instead of repeating the flag — interactive features also auto-disable when there's no TTY). Add `--force` for absorb and `--yes` for undo. Bare `stackit merge` (no flags) opens a TTY wizard; non-interactively, `stackit merge --yes` merges the next (bottom) ready PR, or `stackit merge ship --yes` consolidates the whole stack into one PR.
- Stage changes before `stackit create`.
- Put generated messages in `tmp/stackit-message.txt` and pass `-F tmp/stackit-message.txt`. Do not pipe into mutating Stackit commands in Codex: pipes create a separate shell command segment, so approval can attach to `printf` instead of `stackit`.
- When sandbox metadata shows `.git` is read-only, run mutating Stackit commands with escalation on the first attempt. Do not intentionally try once un-escalated just to discover the expected ref/lock failure.
- Prefer compact JSON views when `jq` is available. If `jq` is unavailable, use `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.
- After any mutation, verify with the compact current-branch view below and report only the branch, parent, children/restack flags, and next step.

## Compact Reads

Use these views instead of dumping full stack JSON when `jq` is available:

```bash
stackit state --json | jq '{current_branch,trunk,working_tree,operation}'
```

```bash
stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
```

For stack-wide health, keep each branch small:

```bash
stackit state --json | jq '{current_branch,trunk,working_tree,operation,branches:[.stack.branches[] | {name,parent,is_current,is_trunk,needs_restack,is_locked,is_frozen,pr:(.pr // null),children:(.children // [])}]}'
```

On command failure, report the failing command, the actionable failure block, and any log path. Do not paste passing package lists or full successful command output.

For validation, choose the lightest command that covers the change. Agent template edits use `mise run test:pkg ./internal/cli/integrations`; full `mise run check` is for broad cross-package changes.

## Asking Policy

Act without asking on local reversible operations: create, modify, absorb, restack of one stack, tree, and status.

Confirm before remote-affecting or destructive operations: submit, merge, restack of all stacks, anything that pushes, and anything that touches GitHub PRs.

If there is no work to act on, say so and stop.

## Health Check

Run once at the start of a Stackit-related session:

```bash
stackit state --json | jq '{current_branch,trunk,working_tree,operation}'
```

This returns the current branch, working-tree state, and any in-progress
operation. Read the compact stack-wide health view only when branch relationships
or PR/CI state matter.

## Skill Selection

| User wording | Best skill |
|---|---|
| "stack these changes", "make a stacked branch", "commit with stackit" | `stack-create` |
| "split this", "plan a stack", "break this into PRs" | `stack-plan` |
| "amend", "fix the current commit" | `stack-modify` |
| "absorb", "distribute fixes", "amend across the stack" | `stack-absorb` |
| "submit", "open PRs", "send for review" | `stack-submit` |
| "sync", "pull trunk", "clean up merged" | `stack-sync` |
| "restack", "rebase the stack" | `stack-restack` |
| "show the stack", "stack status" | `stack-status` |
| "stack is broken", "fix the stack" | `stack-fix` |
| "fold this branch", "squash into parent" | `stack-fold` |
| "tidy fixups", "clean up WIP commits" | `stack-tidy` |
| "split this commit", "move files to a new branch" | `stack-split` |
| "extract this", "move to its own branch off main" | `stack-extract` |
| "resolve conflicts", "continue rebase" | `stack-resolve` |
| "describe the stack", "generate PR descriptions" | `stack-describe` |
| "review the stack", "find PR issues" | `stack-review` |
| "verify each branch", "test the whole stack" | `stack-verify` |

When wording is ambiguous between `stack-create` and `stack-plan`, prefer `stack-create` unless the changes obviously span unrelated subsystems.

## Forbidden

| Do not use | Use instead |
|---|---|
| `git commit` for a new stacked branch | `stackit create` |
| `git checkout -b` | `stackit create` |
| `gh pr create` | `stackit submit` |
| `git rebase` | `stackit restack --upstack` or `stackit restack --all-stacks` |

`git commit` is fine for follow-up commits on an existing stacked branch.

## References

- [references/workflows.md](references/workflows.md) - common Stackit workflows
- [references/commit-style.md](references/commit-style.md) - commit and PR message style
- [references/stack-plan-recovery.md](references/stack-plan-recovery.md) - recovering from a partial `stack-plan` run

<!-- stackit-version: {{VERSION}} -->
