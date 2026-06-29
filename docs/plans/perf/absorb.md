# `absorb` — Performance Analysis

**Tier:** medium (less frequent than `modify`, but expensive when it runs — touches multiple commits and restacks).

## Call graph

```
NewAbsorbCmd → common.Run                            (same bootstrap as co)
  └─ absorb.Action                                   internal/actions/absorb/absorb.go:32
       ├─ validation.AbsorbChain                     bunch of preflight checks
       ├─ actions.TakeBestEffortSnapshot             see create.md #2
       ├─ Engine.Graph(SortStrategyAlphabetical)     in-memory
       │
       ├─ eng.HasStagedChanges                       worktree.Status() — full tree walk
       ├─ git.StageChanges (only if --all/--patch)
       ├─ eng.HasStagedChanges                       worktree.Status() AGAIN
       ├─ eng.ParseStagedHunks                       shells `git diff --cached`
       │
       ├─ graph.Range(current, {RecursiveParents})   in-memory walk
       ├─ for each downstack branch:
       │     branch.GetAllCommits(SHA)               commit range walk (per branch, in go-git)
       │
       ├─ for each hunk:
       │     eng.FindTargetCommitForHunk             tries applying hunk to successive commits — most expensive part per-hunk
       │
       ├─ eng.FindBranchesForCommits(targets)        single batched scan (was per-hunk lookup)
       │
       ├─ if --dry-run: print + maybe JSON; return
       │
       ├─ eng.StashPush                              shells `git stash push`
       ├─ for each modified branch (topo order):
       │     eng.ApplyHunksToBranch                  native checkout → amend → ...  ← N branches × git ops
       ├─ eng.StashPop                               shells `git stash pop`
       ├─ eng.Rebuild("")                            full rebuild after the rewrites
       └─ RestackBranches(upstack)                   internal/actions/common.go:96
              └─ ValidateRebases via per-spec worktrees   (same cost as modify.md #1)
```

## Where time goes

1. **`ApplyHunksToBranch` loop** — for each modified branch, it checks out, amends, restacks. Checkout now uses native Git, but an absorb that touches 3 branches still pays 3 checkouts plus 3 amends.
2. **`RestackBranches` → `ValidateRebases`** — worktree-per-spec (`modify.md` #1).
3. **`FindTargetCommitForHunk`** — proportional to (hunks × downstack commits). For a heavy absorb (lots of staged hunks, deep stack), this dominates the pre-apply phase. Algorithm-bound; speedup needs algorithmic insight, not engine plumbing.
4. **`worktree.Status()` × 2** (`HasStagedChanges` called before *and* after staging at `absorb.go:63` and `:78`). Same fix as `create.md` #1.
5. **`eng.Rebuild("")`** after applying hunks — full rebuild of branch metadata cache because branch refs were rewritten. Necessary, but `Rebuild("")` does the full `GetAllBranchNames + batchReadMetadata + batchReadLocalMetadata` pass — could be scoped to "only refresh branches we touched plus their descendants".
6. **`StashPush` / `StashPop`** — fixed ~10–30ms per side. Needed for safety.
7. **`for each downstack branch: GetAllCommits`** — per-branch commit walk through go-git. Often cache-cold. A combined `git rev-list <oldest-ancestor>..<current> --boundary` would do it in one walk.

## Proposed wins (ranked)

### 1. Same worktree-validation fix benefits the post-absorb restack *(shared with modify.md #1)*

`RestackBranches` here goes through the same `ValidateRebases` worktree-per-spec path. Trivially-safe rebases (very common after absorb — most hunks land in a single ancestor and don't change descendant-touched files) could skip validation entirely.

### 2. Drop the redundant pre-staging `HasStagedChanges` call *(small, low risk)*

`absorb.go:63` checks before staging, `:78` checks after. The first call's
*result is already discarded* (`_, err := eng.HasStagedChanges(...)`) — only its
error is propagated; the "Nothing to absorb" decision is made entirely off the
second call at `:78`. So the first `HasStagedChanges` is a pure `worktree.Status()`
full-tree walk with no behavioral effect. Delete it outright. (Same underlying
`worktree.Status()` cost as `create.md` #1.)

### 3. Scope `eng.Rebuild("")` to touched branches *(medium, medium risk)*

After absorb, only the modified branches and their descendants have changed. A `RebuildBranches([]string)` that re-reads metadata + revisions for just that subset would skip O(N - touched) of the full rebuild. Useful here, in `restack`, in `modify`, in `sync`.

### 4. Single downstack `git rev-list` instead of per-branch `GetAllCommits` *(small impact, simple)*

`absorb.go:121–130` calls `GetAllCommits(SHA)` per branch and then walks. One `git rev-list --boundary <trunk>..<current>` returns all SHAs in one process. The per-branch attribution can be done from the parent-revision metadata already in the cache.

> Note: the separate target→branch attribution step (`FindBranchesForCommits`,
> `engine_reader.go:201`) *also* calls `GetAllCommits` per branch. If win #4
> builds a `commitSHA → branchName` map during the single rev-list walk, that
> batched scan can be dropped too — folding former win #5 into this one.

## Validation

```
# Absorb that hits one branch (no restack work)
STACKIT_NO_LOGGING=1 hyperfine 'stackit absorb --force --dry-run'

# Absorb that crosses 3 branches with 5 descendants (worst case)
STACKIT_NO_LOGGING=1 hyperfine 'stackit absorb --force'
```

Instrument: each `ApplyHunksToBranch`, the `RestackBranches` block, `FindTargetCommitForHunk`, and the `eng.Rebuild("")` call. Dry-run isolates the planning phase from the apply phase.
