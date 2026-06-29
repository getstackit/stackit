# `modify` — Performance Analysis

**Tier:** deep (the second hot-path mutating command after `create`; runs on every iteration).

> **Audit note (2026-06):** This plan was re-checked against the code. Several
> wins already shipped or were made obsolete by the removal of the global
> revision cache / go-git reload machinery. Remaining work is wins #2 and #5.

## Call graph

```
NewModifyCmd.RunE → common.Run                       (same bootstrap as co)
  └─ actions.ModifyAction                            internal/actions/modify.go:34
       ├─ validation.ModifyBranchChain               cheap ancestry checks
       ├─ validation.MustNotHaveRebaseInProgress     fs check on .git/rebase-merge etc.
       ├─ if amending: eng.IsBranchEmpty(currentBranch)   engine_branch_status.go:351
       ├─ eng.StageChanges(opts)                     git add ... (only if --all/--update/--patch)
       ├─ eng.HasStagedChanges                       git diff --cached --quiet (staging.go:55)
       │
       ├─ eng.CommitWithOptions                      internal/engine/commit_ops.go:19
       │     └─ git.runner.CommitWithOptions         internal/git/commit.go:20
       │           └─ shells `git commit --amend …`  user hooks dominate when present
       │
       └─ if children exist:
            RestackBranches                          internal/actions/common.go:99
              └─ restackBranchesWithPlan             internal/actions/common.go:114
                   ├─ validateBranchAncestry         per-branch ancestry probe
                   ├─ Engine.PlanRestack             internal/engine/restack_plan.go:10 (~several git ops per branch)
                   └─ Engine.ValidateRebases         internal/engine/rebase_validator.go:73
                        ├─ git.PruneWorktrees(ctx)   one-shot per call
                        ├─ groupSpecsByDepth         in-memory
                        └─ for each level, in parallel (up to MaxConcurrency):
                             validateSingleSpec       rebase_validator.go:335
                               ├─ tryConflictFreeReplay  ← fast path, no worktree (rebase_validator.go:425)
                               ├─ wt.CreateSession   ← `git worktree add` (slow path only)
                               ├─ dryRunRebase       shells `git rebase --onto …`
                               └─ wt.Cleanup        ← `git worktree remove`
```

## Where time goes (typical leaf-branch amend with N descendants)

1. **`git worktree add` per spec** inside `ValidateRebases`, **only when the fast
   path misses**. `validateSingleSpec` now tries `tryConflictFreeReplay` first
   (see "Already shipped" below): for a single-commit descendant whose files are
   disjoint from the parent's new changes, no worktree is created at all. The
   worktree cost only applies to multi-commit descendants or overlapping file
   sets. When it does apply, branches at the same depth are parallelized but each
   still gets its own worktree.
2. **User pre-commit hook** during the `git commit --amend`. Outside stackit's control but it runs every modify.
3. **`PlanRestack`** — several git ops per descendant, run sequentially. For a
   deep stack, this is meaningful. See win #5.
4. **Bootstrap (`rebuildInternal`)** — fixed cost; see `co.md`.

`HasStagedChanges` is now a cheap `git diff --cached --quiet` index probe, no
longer a full working-tree walk, so it is no longer a notable cost.

For a **leaf branch amend** (no children): the restack block is skipped entirely.
For a **mid-stack amend** with multi-commit or overlapping descendants: the
worktree-validate dance still dominates.

## Already shipped (do not re-plan)

- **Conflict-free fast path** — `validateSingleSpec` calls `tryConflictFreeReplay`
  (`internal/engine/rebase_validator.go:355,425`), which compares
  `GetChangedFiles(old-parent..new-parent)` against
  `GetChangedFiles(old-parent..branch)` and, when the file sets are disjoint for a
  single-commit branch, computes the rebased tree via `merge-tree --write-tree` +
  `commit-tree` with no worktree. This is the original win #1.
- **`HasStagedChanges` coalescing** — moot: `HasStagedChanges` is now
  `git diff --cached --quiet` (`internal/git/staging.go:55`), not a `Status()`
  tree walk. The expensive walk the old win targeted no longer exists.
- **`ReloadRepository` scoping** — moot: `ReloadRepository`, the go-git reopen,
  and the global `revisionCache` were removed entirely. `git.CommitWithOptions`
  (`internal/git/commit.go:20`) no longer reloads anything. Revisions are read
  per-call via `git rev-parse` with batch readers (`BatchGetRevisions`) where
  multiple branches are needed.
- **Revision-cache pre-warming** — obsolete: there is no longer a global revision
  cache to pre-warm (`LoadAllBranchRevisions` / `PreloadBranchData` were removed
  and must not be reintroduced — see `.claude/rules/code-style.md`,
  "Branch-state reads return values, not a global cache").

## Proposed wins (ranked)

### 1. Extend conflict-free replay to multi-commit branches *(medium impact, high risk)*

> **Status:** Partially done. The single-commit conflict-free fast path is
> shipped (`tryConflictFreeReplay`, `internal/engine/rebase_validator.go:425`).
> It bails out for multi-commit branches (`len(commits) != 1` → falls back to the
> worktree path).

Remaining work: handle multi-commit descendants without a worktree. This requires
iterating each commit's diff and confirming the *aggregate* branch file set is
disjoint from the parent's new changes, then replaying commits onto the new
parent via repeated `commit-tree` (or a single squashed `merge-tree` when commit
identity can be preserved). Higher risk than the single-commit case because each
intermediate commit's tree must be reconstructed in order; keep it strictly
opt-in to the proven-disjoint case and fall back to the worktree path on any
uncertainty.

### 2. Reuse a single validation worktree per depth level *(medium impact, medium risk)*

> **Status:** Not started. `validateSingleSpec`
> (`internal/engine/rebase_validator.go:367`) still calls
> `CreateTemporaryWorktreeWithOptions` once per spec that reaches the slow path.

For specs that miss the fast path, `ValidateRebasesParallel` creates one worktree
per spec. Within a depth level, branches are siblings sharing the same upstream. A
reusable worktree per worker (worker pool) could `git rebase --onto … && git
rebase --abort`/reset between specs, paying worktree creation O(levels ×
concurrency) instead of O(slow-path specs).

Risk: rerere state and partial-rebase state need careful reset between specs. The
current per-spec isolation is the safe design — make this an opt-in path for the
validated-good case. Note the impact is now smaller than originally estimated,
since the fast path already eliminates worktrees for the common single-commit
disjoint case.

### 3. Reduce `PlanRestack`'s per-branch git ops *(small impact)*

> **Status:** Not started. `PlanRestack` (`internal/engine/restack_plan.go:10`)
> loops over branches sequentially; `planRestackBranch` does several git ops per
> branch (`GetRevision`, `ReadMetadata`, `IsAncestor`, `GetMergeBase`).

The original plan suggested pre-warming via `PreloadBranchData` — **that approach
is no longer available and is explicitly forbidden** (no ambient revision cache).
Instead:

- Batch the revision lookups up front with `Engine.GetRevisions` /
  `git.BatchGetRevisions` (one `rev-parse` for all branches) and the metadata
  reads with `BatchReadMetadata`, then pass the resolved value maps into
  `planRestackBranch`.
- If further parallelism is wanted, use a **bounded** fan-out (`utils.Run`), not
  one goroutine per branch — see `.claude/rules/code-style.md` "Bound parallel
  fan-out".

Cheapest first step is the batch reads; only add fan-out if profiling still shows
`PlanRestack` as hot.

## Validation

```
# Leaf branch (isolates non-restack work)
STACKIT_NO_LOGGING=1 hyperfine \
  'echo // noop >> file.go; stackit modify -a --no-edit'

# Mid-stack (5 multi-commit descendants) — measures worktree validation cost
STACKIT_NO_LOGGING=1 hyperfine \
  'echo // noop >> file.go; stackit modify -a --no-edit'
```

Use multi-commit descendants for the mid-stack case so the conflict-free fast
path does not absorb the cost being measured. Instrument: each
`validateSingleSpec`, the worktree creation specifically, `ValidateRebases` total,
`PlanRestack`, and `git commit`. The delta between leaf and mid-stack is
essentially the worktree validation cost that survives the fast path.
