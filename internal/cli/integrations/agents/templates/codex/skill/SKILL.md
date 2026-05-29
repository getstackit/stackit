---
name: stackit
description: Shared guidance for Stackit stacked-branch workflows. Use when the request mentions stacks, stacking, stacked PRs, or Stackit-specific operations and needs general Stackit policy, workflow safety rules, or a stack health check.
---

# Stackit

Stackit manages stacked Git branches: small dependent PRs instead of one large review. Use this skill for shared policy and orientation; use the narrower `stack-*` skills for specific operations.

## Always

- Pass `--no-interactive` on Stackit commands. Add `--force` for absorb and `--yes` for undo. Bare `stackit merge` is an interactive wizard that needs a TTY — for non-interactive merges use a subcommand: `stackit merge ship --yes` (consolidate into one PR) or `stackit merge next --yes` (merge the bottom PR).
- Stage changes before `stackit create`.
- Pipe commit messages via `-F -`: `printf '%s\n' "feat: x" | stackit create -F - --no-interactive`.
- After any mutation, run `stackit log --no-interactive` and report the resulting stack.

## Asking Policy

Act without asking on local reversible operations: create, modify, absorb, restack of one stack, log, and status.

Confirm before remote-affecting or destructive operations: submit, merge, restack of all stacks, anything that pushes, and anything that touches GitHub PRs.

If there is no work to act on, say so and stop.

## Health Check

Run once at the start of a Stackit-related session:

```bash
stackit log --json --no-interactive
```

Surface failing CI, branches ready to merge, and branches needing restack when relevant.

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
