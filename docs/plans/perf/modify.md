# `modify` — Performance Analysis

**Tier:** deep (the second hot-path mutating command after `create`; runs on every iteration).

## Call graph

```
NewModifyCmd.RunE → common.Run                       (same bootstrap as co)
  └─ actions.ModifyAction                            internal/actions/modify.go:34
       ├─ validation.ModifyBranchChain               cheap ancestry checks
       ├─ validation.MustNotHaveRebaseInProgress     fs check on .git/rebase-merge etc.
       ├─ eng.IsBranchEmpty(currentBranch)           engine_branch_status.go:245 — git rev-list / metadata compare
       ├─ git.StageChanges(opts)                     shells `git add ...` (only if --all/--update/--patch)
       ├─ eng.HasStagedChanges                       worktree.Status() — full tree walk
       │
       ├─ eng.CommitWithOptions                      internal/engine/engine_writer.go:718
       │     └─ git.runner.CommitWithOptions         internal/git/commit.go:20
       │           ├─ shells `git commit --amend …`  user hooks dominate when present
       │           ├─ revisionCache.InvalidateAll
       │           └─ ReloadRepository               closes + reopens go-git repo
       │
       └─ if children exist:
            RestackBranches                          internal/actions/common.go:96
              └─ restackBranchesWithPlan             internal/actions/common.go:111
                   ├─ validateBranchAncestry         per-branch ancestry probe
                   ├─ Engine.PlanRestack             ~3 git ops per branch (sha + parent sha + check)
                   └─ Engine.ValidateRebases         internal/engine/rebase_validator.go:72
                        ├─ git.PruneWorktrees(ctx)   one-shot per call
                        ├─ groupSpecsByDepth         in-memory
                        └─ for each level, in parallel (up to MaxConcurrency):
                             validateSingleSpec
                               ├─ wt.CreateSession   ← `git worktree add` (slow, fs-bound)
                               ├─ dryRunRebase       shells `git rebase --onto …`
                               └─ wt.Cleanup        ← `git worktree remove`
```

## Where time goes (typical leaf-branch amend with N descendants)

1. **`git worktree add` × N specs** inside `ValidateRebases`. Each worktree creation is hundreds of ms on large repos (file copy of the working tree contents). Branches at the same depth are parallelized — but every spec still gets its own worktree. For an amend with one descendant: one worktree. For five: five worktrees in parallel, capped at `MaxConcurrency`. This is the single biggest cost on a non-leaf modify.
2. **User pre-commit hook** during the `git commit --amend`. Outside stackit's control but it runs every modify.
3. **`ReloadRepository`** after the amend (`internal/git/commit.go:57`). Closes and re-opens the go-git repo, wiping the revision cache. Same cost as in `create.md` #3.
4. **`PlanRestack`** — ~3 git ops per descendant. Currently bounded by descendant count, not particularly parallel. For a deep stack, this is meaningful.
5. **`worktree.Status()`** in `HasStagedChanges` (`internal/git/staging.go:103`). Same full-tree walk pattern as in `create.md` #1.
6. **`IsBranchEmpty`** — runs even when the user passed `-c` (already requested a new commit). Wasted work.
7. **Bootstrap (`rebuildInternal`)** — fixed cost; see `co.md`.

For a **leaf branch amend** (no children): the restack block is skipped entirely. The dominant non-hook cost is `worktree.Status()` (#5) + `ReloadRepository` (#3) + bootstrap. Big win there is shared with `create`.

For a **mid-stack amend**: the worktree-validate dance dominates everything else.

## Proposed wins (ranked)

### 1. Skip `ValidateRebases` when the amend is conflict-free *(high impact, medium risk)*

After an amend, every descendant's rebase is `rebase --onto <new-parent-sha> <old-parent-sha> <branch>`. If git can compute that as a fast-forward (the old parent is an ancestor of the new parent and no descendant commits touch the same files), there is no possibility of conflict. The validator could classify specs into:

- **Trivially safe**: parent moved but descendant commits don't overlap with the diff between old/new parent — apply directly, no worktree.
- **Needs validation**: otherwise.

Cheap heuristic: compare `git diff --name-only old-parent..new-parent` with `git diff --name-only old-parent..branch` per spec. If the file sets are disjoint, no conflict is possible. The check is one `diff --name-only` per spec instead of one full worktree+rebase.

For a typical amend that only touches one file in a leaf branch, this collapses N worktrees into N file-set comparisons (each ~1ms).

### 2. Reuse a single validation worktree per depth level *(medium impact, medium risk)*

`ValidateRebasesParallel` creates one worktree per spec. Within a depth level, branches are siblings — they all share the same upstream. A single reusable worktree per level (or per goroutine in a worker pool) could `git rebase --onto … && git rebase --abort` (or reset to a known state) between specs, paying worktree creation O(levels × concurrency) instead of O(specs).

Risk: rerere state and partial-rebase state need careful reset between specs. The current per-spec isolation is the safe design — opt-in path for the validated-good case is safer.

### 3. Drop `IsBranchEmpty` when `--commit` is set *(trivial)*

`internal/actions/modify.go:57` checks emptiness even when the user already chose `-c`. Move the check inside the `if !opts.CreateCommit` branch.

### 4. Same `worktree.Status()` coalescing as `create` *(low impact for modify, shared fix)*

`HasStagedChanges` here calls `worktree.Status()`. `StageChanges` may have just finished a `git add` and knows what was staged. Returning a "staged any files?" flag from `StageChanges` would let us skip the second status walk. Same fix benefits `create`, `absorb`, `submit`.

### 5. Same `ReloadRepository` scoping as `create` *(see create.md #3)*

Invalidate just the affected branch's revision rather than dropping the whole go-git handle.

### 6. Parallelize `PlanRestack`'s per-branch git ops *(small impact)*

`PlanRestack` runs ~3 git ops per branch sequentially. Most are revision lookups that go through `revisionCache`. After `PreloadBranchData` (which modify doesn't currently call but easily could) most of these would be cache hits and the issue vanishes. Cheaper than building a fan-out.

### 7. Pre-warm the revision cache before restack *(trivial)*

Call `LoadAllBranchRevisions` (already exists, used by `log`) before `PlanRestack` so its per-branch SHA lookups are all cache hits. One go-git ref iter vs. N individual lookups.

## Validation

```
# Leaf branch (isolates non-restack work)
STACKIT_NO_LOGGING=1 hyperfine \
  'echo // noop >> file.go; stackit modify -a --no-edit'

# Mid-stack (5 descendants) — measures worktree validation cost
STACKIT_NO_LOGGING=1 hyperfine \
  'echo // noop >> file.go; stackit modify -a --no-edit'
```

Instrument: each `validateSingleSpec`, the worktree creation specifically, `ValidateRebases` total, `git commit`, `ReloadRepository`. The delta between leaf and mid-stack is essentially the worktree validation cost.
