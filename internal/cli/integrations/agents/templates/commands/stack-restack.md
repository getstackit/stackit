---
description: Rebase branches with scoped or multi-stack targeting
model: haiku
allowed-tools: Bash(stackit:*), Bash(git:*), AskUserQuestion, Skill
---

# Stack Restack

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`

## Task

Rebase branches with the narrowest safe scope to ensure proper parent-child ancestry.

**Preconditions** (check context above):
- Must have clean working directory (no uncommitted changes)
- Must be on a branch when using the current stack or `--branch`
- `--all-stacks` and `--stacks` can be used when no current branch is relevant

If preconditions fail, inform user and stop.

Otherwise, choose the narrowest safe restack scope and show the result:
- If a specific branch caused the issue or was just amended, run `stackit restack --branch <branch> --upstack --no-interactive`.
- If a whole independent stack needs restack, run `stackit restack --branch <stack-root> --upstack --no-interactive`.
- If several independent stack roots need restack, run `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`.
- If every independent stack needs restack, run `stackit restack --all-stacks --continue-on-conflict --no-interactive`.
- Only run `stackit restack --no-interactive` when the user explicitly wants the current stack restacked and no narrower branch/root is known.

Conflict handling:
- If a scoped restack enters conflict state, inform user they need to resolve conflicts and run `stackit continue`.
- If `--continue-on-conflict` reports skipped conflicts, no rebase is active for those skipped branches. To resolve one, run `stackit restack --branch <conflicted-branch> --upstack --no-interactive`, then resolve conflicts and run `stackit continue`.

## Follow-up

After successful restack, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Stack rebased. What would you like to do next?"
- Options:
  - "Submit to update PRs (Recommended)" → Invoke `/stack-submit` using Skill tool
  - "Done for now" → End with summary
