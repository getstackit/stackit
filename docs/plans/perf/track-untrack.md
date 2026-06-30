# `track` and `untrack` — Performance Analysis

**Tier:** medium (less frequent than navigation/modify). `untrack`'s K+1-rebuild
N+1 has since been fixed; the remaining work is in interactive `track` plus a
shared scoped-rebuild item.

## `track`

### Call graph

```
newTrackCmd → common.Run → track.Action               internal/actions/track/action.go:18
  ├─ if --parent given:
  │    ├─ eng.GetRevision(parent) + eng.GetRevision(branch)
  │    ├─ eng.IsAncestor(parentRev, branchRev)        shells `git merge-base --is-ancestor`
  │    └─ eng.TrackBranch                              metadata write (1 ref)
  │
  ├─ if --force:
  │    ├─ eng.FindMostRecentTrackedAncestors          action.go:84, walks ref ancestry
  │    └─ eng.TrackBranch
  │
  └─ interactive (no --parent):
       trackBranchRecursively                          internal/actions/track/action.go:111
         ├─ eng.FindMostRecentTrackedAncestors         action.go:122
         ├─ handler.PromptSelectParent                 user-blocking
         ├─ eng.TrackBranch
         ├─ for each branch in AllBranches:           ← N branches (action.go:170)
         │     eng.GetMergeBase(candidate, branchName) ← `git merge-base` per branch (N calls, action.go:177)
         │     eng.GetRevision(branchName)             recomputed per iter (action.go:182)
         │     comparison
         └─ recurse into untracked children
```

### Where time goes

1. **Per-candidate `eng.GetMergeBase`** in the child-detection loop
   (`action.go:177`). For each branch in the repo a separate
   `git merge-base <candidate> <branchName>` is shelled (`GetMergeBase` calls
   straight through to git with no memoization — `internal/engine/engine_git_ops.go:128`).
   On a 50-branch repo that's 50 git processes. Dominant cost in interactive track.
2. **`FindMostRecentTrackedAncestors`** — runs once (or twice — `:84` and `:122`
   across the force/interactive flows). Walks ancestry — bounded by parent-chain
   depth, not by N.
3. **`eng.TrackBranch`** is cheap (one metadata ref write).
4. **Bootstrap** — fixed cost; small relative to the merge-base loop above.

### Wins

#### 1. Replace per-candidate `GetMergeBase` with a single `rev-list` *(high impact, low risk)*

`internal/actions/track/action.go:170–191` is the classic "find all descendants
of a commit" question. One `git rev-list --children <branchName>..HEAD` (or
`git for-each-ref --contains <branchName-sha>`) gives all branches that contain
the branch's tip in one process. N+1 → 1.

#### 2. Hoist `GetRevision(branchName)` out of the candidate loop *(trivial)*

`eng.GetRevision(eng.GetBranch(branchName))` is recomputed inside the loop
(`action.go:182`) on every candidate even though `branchName` is invariant for
the whole loop. `GetRevision` is **not** cached (it shells
`git rev-parse` via `runner.getRevision` → `resolveRefSHA` each call —
`internal/git/commit_info.go:56`), so this is N redundant rev-parse calls. Resolve
it once before the loop.

> Note: win #1 subsumes this if implemented — the rev-list rewrite removes the
> per-candidate merge-base/revision comparison entirely. Keep #2 only as the
> minimal standalone fix if #1 is deferred.

## `untrack`

### Call graph

```
newUntrackCmd → common.Run → untrack.Action            internal/actions/untrack/action.go:18
  ├─ eng.Graph + graph.Range(RecursiveChildren)        in-memory
  ├─ collect descendant names + branch name
  └─ eng.UntrackBranches(allNames)                      engine/branch_tracking.go:82
       ├─ git.DeleteRefsBatch(metadataRefNames)         1 batched ref delete
       └─ eng.rebuild()                                 1 rebuild for the whole set
```

### Where time goes

1. **`eng.rebuild()` inside `UntrackBranches`** — `internal/engine/branch_tracking.go:97`.
   Now runs **once** for the whole untrack (batched), not K+1 times. The remaining
   cost is that the single rebuild is still a *full* repo pass
   (`GetAllBranchNames + batchReadMetadata + batchReadLocalMetadata`) rather than
   scoped to the touched branches — see win #1.
2. Bootstrap — fixed cost.

### Wins

#### 1. Scope the rebuild to touched branches *(shared with cross-cutting #8 / absorb)*

> **Status:** The N+1 (one full rebuild per descendant) is already fixed.
> `untrack.Action` collects all names and calls the batched
> `eng.UntrackBranches` (`internal/actions/untrack/action.go:58`), which does a
> single `git.DeleteRefsBatch` plus a single `eng.rebuild()`
> (`internal/engine/branch_tracking.go:82–98`). Remaining work below is only the
> scoped-rebuild improvement.

The engine still does a full `rebuild()` after the batch delete. If the engine
grew a `RebuildBranches([]string)` (cross-cutting item #8, also wanted by
`absorb` / `modify` / `sync`), the untrack path would re-read only the affected
metadata rather than the whole repo's worth. This method does **not** exist yet
(only `rebuild()` / `rebuildInternal()` in `internal/engine/engine_internal.go`).

## Validation

```
# Track on a deep repo
STACKIT_NO_LOGGING=1 hyperfine 'stackit track <branch> --parent <parent>'      # cheap path
STACKIT_NO_LOGGING=1 hyperfine 'stackit track <branch> --force'               # ancestor walk
STACKIT_NO_LOGGING=1 hyperfine 'stackit track <branch>'                       # interactive path — N×merge-base

# Untrack a 10-branch substack
STACKIT_NO_LOGGING=1 hyperfine 'stackit untrack <root> -f'
```

Instrument: the merge-base loop in `trackBranchRecursively` (still N×merge-base),
and every `eng.rebuild()` invocation. The untrack rebuild count is already 1
(not K+1); a `RebuildBranches` would make that single rebuild scoped rather than
full-repo.
