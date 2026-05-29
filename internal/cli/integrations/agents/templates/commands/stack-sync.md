---
description: Sync with trunk and cleanup merged branches
model: haiku
allowed-tools: Bash(stackit:*), Bash(git:*), AskUserQuestion, Skill
---

# Stack Sync

## Context
- Current branch: !`git branch --show-current`
- Stack state: !`stackit log --no-interactive`

## Task

Sync the stack with trunk: pull latest, cleanup merged branches, restack.

1. Run `stackit sync --dry-run --json --restack --no-interactive` to preview cleanup and restack work.
2. Parse `would_clean`, `would_restack`, `would_restack_stacks`, and `skipped_stacks` from the JSON preview.
3. Treat `would_restack_stacks` as preview-only. Do not feed those roots directly into a post-sync `restack --stacks` decision, because cleanup, reparenting, or dirty-stack skipping can change the current roots.
4. If ALL branches would be deleted, confirm with user first using `AskUserQuestion`.
5. Run `stackit sync --no-restack --no-interactive` so cleanup happens once and restack can use refreshed scope.
6. Recompute current restack scope with a second `stackit sync --dry-run --json --restack --no-interactive` and parse the refreshed `would_restack`, `would_restack_stacks`, and `skipped_stacks`.
7. If branches remain and refreshed `would_restack` is non-empty, choose the restack scope from the refreshed JSON:
   - If refreshed `would_restack_stacks` has exactly one root, run `stackit restack --branch <that-root> --upstack --no-interactive`.
   - If refreshed `would_restack_stacks` lists several roots, run `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`.
   - If every remaining independent stack needs restack or the refreshed root list is unavailable, run `stackit restack --all-stacks --continue-on-conflict --no-interactive`.
8. Show final state with `stackit log --json --no-interactive`.

If `--continue-on-conflict` reports skipped conflicts, no rebase is active for those skipped branches. To resolve one, run `stackit restack --branch <conflicted-branch> --upstack --no-interactive`, then resolve conflicts and run `stackit continue`.

These steps are sequential — each depends on the previous step's result, so run them one at a time rather than batching.

## Follow-up

After sync completes, if branches remain, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Stack synced. What would you like to do next?"
- Options:
  - "Submit updates (Recommended)" → Invoke `/stack-submit` using Skill tool
  - "Done for now" → End with summary
