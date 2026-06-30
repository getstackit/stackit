# `restack` — Performance Analysis

**Tier:** medium (frequent after rebases, sync, or absorb; expensive when actual rebases run).

## Call graph

```
NewRestackCmd → common.Run                           (same bootstrap as co)
  └─ actions.PlanRestack                             internal/actions/restack.go:97
       ├─ planRestackBranchGroups                    O(branches) graph walk
       └─ for each group:
            ├─ eng.SortBranchesTopologically
            └─ eng.PlanRestack                       ~3 git ops per branch (sha + parent sha + check)
  └─ if plan.HasBranches:
       actions.RestackAction                         internal/actions/restack.go:129
         ├─ TakeBestEffortSnapshot                   see create.md #2 (skipped when undo disabled)
         ├─ rerere.EnsureEnabled                     one config read/write
         │
         └─ for each group (parallel if --parallel):
              restackBranchesWithPlan                internal/actions/common.go:114
                ├─ validateBranchAncestry            cheap
                ├─ Engine.ValidateRebases            ← per-spec worktrees, with a conflict-free
                │                                       fast path for disjoint-file branches (shipped)
                ├─ (apply or conflict workflow)
                └─ engine state writes
```

## Where time goes

1. **`Engine.ValidateRebases`** dominates — same per-spec worktree creation that hurts `modify` and `absorb`. A conflict-free fast path now skips the worktree for **any** branch (single- or multi-commit) whose diff is disjoint from the parent's (`tryConflictFreeReplay`, `internal/engine/rebase_validator.go:355`). Only branches whose files overlap the parent's changes still pay a worktree each, so a "stack of 8 branches all touch the same file" still validates in dependency order with width-level parallelism.
2. **`Engine.PlanRestack`** — ~3 git ops per branch, built once in the CLI and threaded through to the action via `enginePlan` (it is not rebuilt). Still O(branches) git ops once. Each lookup hits git directly (no revision cache — see the note under removed wins).
3. **`TakeBestEffortSnapshot`** — already batches its revision reads via `BatchGetRevisions` (`internal/engine/undo.go:121`), so the cost is one `git rev-parse` plus metadata listing, not per-branch iteration. It is skipped entirely when `undo.enabled=false`. Remaining overhead: it still runs for a no-op restack (see win #1).
4. **`--parallel` with worktrees** — `restackGroupsParallel` (`internal/actions/restack.go:310`) dispatches independent stack groups to separate worktrees, created on demand via a bounded `utils.RunWithWorkers` pool. Each worktree creation is hundreds of ms; for the multi-stack case this is a feature (parallelism beats serial worktree-creates), but each group still individually pays per-spec validation.
5. **Bootstrap** — same fixed cost as `co.md`.

## Proposed wins (ranked)

### 1. Skip the snapshot when `!plan.HasWork()` *(trivial)*

`TakeBestEffortSnapshot` runs unconditionally in `RestackAction`
(`internal/actions/restack.go:153`), before the work is dispatched. The CLI
already short-circuits a no-op restack to the simple sync handler
(`internal/cli/stack/restack.go:142-144`), but it still calls `RestackAction`,
which takes the snapshot first. For an up-to-date stack the snapshot is pure
overhead.

Move `TakeBestEffortSnapshot` after a `plan.HasWork()` gate (or only take it
when about to mutate refs). The `undo.enabled=false` skip is already handled
inside `TakeBestEffortSnapshot` (`internal/actions/common.go`), so this is the
only remaining snapshot win.

### 2. Reuse a validation worktree per depth level *(shared with modify.md #1)*

Within a level (sibling branches that fall through the fast path),
`validateSingleSpec` creates a fresh worktree per spec
(`internal/engine/rebase_validator.go:367`). One worktree could validate all
sibling specs back-to-back via `git rebase --onto … && git rebase --abort`
between specs, capping worktree creation at roughly `levels × concurrency`
instead of one per fall-through spec. Note this trades some intra-level
parallelism (siblings currently validate concurrently across worktrees) for
fewer `git worktree add`/`remove` cycles, so measure before committing.

### 3. `--all-stacks` parallel mode should reuse a worktree pool *(small impact, low risk)*

`restackGroupsParallel` (`internal/actions/restack.go:310`) creates a worktree
per group on demand inside `utils.RunWithWorkers` and tears it down with
`defer cleanup()`. For large `--all-stacks` runs, a pre-allocated pool of
`jobs` worktrees reused across groups would avoid redundant
`git worktree add`/`git worktree remove` cycles. Particularly noticeable when
many groups have few branches each.

## Removed / no-longer-applicable wins

- **Pre-warm a revision cache before `PlanRestack`** — removed. There is no
  ambient/global revision cache to warm: `GetRevision` resolves through git
  each call, and `.claude/rules/code-style.md` explicitly forbids reintroducing
  an ambient revision cache or a `PreloadBranchData`-style warm-up (it was
  removed deliberately as a staleness hazard). The only safe batching here is
  `BatchGetRevisions`, which the snapshot path already uses.
- **Snapshot scoping when `undo.enabled=false`** — done.
  `TakeBestEffortSnapshot` early-returns when `ctx.Config.UndoEnabled()` is
  false (`internal/actions/common.go`).

## Validation

```
# Up-to-date stack (isolates fixed overhead)
STACKIT_NO_LOGGING=1 hyperfine 'stackit restack'

# Stack needing 5 conflict-free rebases (measures ValidateRebases cost)
STACKIT_NO_LOGGING=1 hyperfine 'stackit restack --upstack'

# Multi-stack parallel
STACKIT_NO_LOGGING=1 hyperfine 'stackit restack --all-stacks --parallel'
```

Instrument: `ValidateRebases` total + per-spec (fast-path vs worktree path),
`PlanRestack`, `TakeBestEffortSnapshot`. The delta between up-to-date and
needs-work is the validation cost.
