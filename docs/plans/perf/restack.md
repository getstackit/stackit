# `restack` — Performance Analysis

**Tier:** medium (frequent after rebases, sync, or absorb; expensive when actual rebases run).

## Call graph

```
NewRestackCmd → common.Run                           (same bootstrap as co)
  └─ actions.PlanRestack                             internal/actions/restack.go:92
       ├─ planRestackBranchGroups                    O(branches) graph walk
       └─ for each group:
            ├─ eng.SortBranchesTopologically
            └─ eng.PlanRestack                       ~3 git ops per branch (sha + parent sha + check)
  └─ if plan.HasBranches:
       actions.RestackAction                         internal/actions/restack.go:121
         ├─ TakeBestEffortSnapshot                   see create.md #2
         ├─ rerere.EnsureEnabled                     one config read/write
         │
         └─ for each group (parallel if --parallel):
              restackBranchesWithPlan                internal/actions/common.go:111
                ├─ validateBranchAncestry            cheap
                ├─ Engine.ValidateRebases            ← per-spec worktrees (see modify.md #1)
                ├─ (apply or conflict workflow)
                └─ engine state writes
```

## Where time goes

1. **`Engine.ValidateRebases`** dominates — same per-spec worktree creation that hurts `modify` and `absorb`. Restack's worst case is "stack of 8 branches all touch the same file" → 8 worktrees, validated in dependency order with width-level parallelism.
2. **`Engine.PlanRestack`** — ~3 git ops per branch, run twice if the CLI builds the plan AND the action rebuilds it (which today's code explicitly avoids by passing `enginePlan` through). Still O(branches) git ops once.
3. **`TakeBestEffortSnapshot`** — same per-branch revision iteration as `create.md` #2.
4. **`--parallel` with worktrees** — `restackGroupsParallel` (not read here) dispatches independent stack groups to separate worktrees. Each worktree creation is hundreds of ms; for the multi-stack case this is a feature (parallelism beats serial worktree-creates), but each group still individually pays per-spec validation.
5. **Bootstrap** — same fixed cost as `co.md`.

## Proposed wins (ranked)

### 1. The big shared win: skip `ValidateRebases` when conflict-impossible *(shared with modify.md #1)*

Per-spec diff-file overlap check turns most stack restacks into "0 worktrees needed". For typical post-amend restacks, the descendant commits don't touch the same files as the parent's amend diff. This collapses to a few `git diff --name-only` invocations.

### 2. Pre-warm revision cache before `PlanRestack` *(small, free)*

`engine.PlanRestack` does ~3 SHA lookups per branch. After `eng.LoadAllBranchRevisions()` (already exists, called from `log`), all lookups hit cache. Add the call before `PlanRestack` in `actions.PlanRestack` (`internal/actions/restack.go:92`). Same fix benefits `modify` indirectly through `RestackBranches`.

### 3. Reuse validation worktree per depth level *(shared with modify.md #2)*

Within a level (sibling branches), one worktree could validate all sibling specs back-to-back via `git rebase --onto … && git rebase --abort` between specs. Caps worktree creation at `levels × concurrency` instead of N.

### 4. `--all-stacks` parallel mode should pre-build a worktree pool *(small impact, low risk)*

`restackGroupsParallel` creates worktrees on demand. For large `--all-stacks` runs, a pre-allocated pool of `jobs` worktrees that gets reused across groups avoids redundant `git worktree add`/`git worktree remove` cycles. Particularly noticeable when many groups have few branches each.

### 5. Snapshot scoping (shared with create.md #2)

If `undo.enabled=false`, skip `TakeBestEffortSnapshot` entirely. For a no-op restack on an up-to-date stack (`!plan.HasWork()`), snapshot is pure overhead.

### 6. Skip snapshot when `!plan.HasWork()` *(trivial)*

Already short-circuits to the simple sync handler when nothing changes. The snapshot in `RestackAction` runs before that branch is taken. Move `TakeBestEffortSnapshot` after the no-work check, or only take a snapshot when we're about to mutate refs.

## Validation

```
# Up-to-date stack (isolates fixed overhead)
STACKIT_NO_LOGGING=1 hyperfine 'stackit restack'

# Stack needing 5 conflict-free rebases (measures ValidateRebases cost)
STACKIT_NO_LOGGING=1 hyperfine 'stackit restack --upstack'

# Multi-stack parallel
STACKIT_NO_LOGGING=1 hyperfine 'stackit restack --all-stacks --parallel'
```

Instrument: `ValidateRebases` total + per-spec, `PlanRestack`, `TakeBestEffortSnapshot`. The delta between up-to-date and needs-work is the validation cost.
