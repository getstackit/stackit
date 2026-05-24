# `track` and `untrack` — Performance Analysis

**Tier:** medium (less frequent than navigation/modify, but `untrack` has a real N+1 today).

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
  │    ├─ eng.FindMostRecentTrackedAncestors          walks ref ancestry
  │    └─ eng.TrackBranch
  │
  └─ interactive (no --parent):
       trackBranchRecursively                          internal/actions/track/action.go:103
         ├─ eng.FindMostRecentTrackedAncestors
         ├─ handler.PromptSelectParent                 user-blocking
         ├─ eng.TrackBranch
         ├─ for each branch in AllBranches:           ← N branches
         │     eng.GetMergeBase(candidate, branchName) ← `git merge-base` per branch (N calls)
         │     eng.GetRevision(branchName)             cached on second iter
         │     comparison
         └─ recurse into untracked children
```

### Where time goes

1. **Per-candidate `eng.GetMergeBase`** in the child-detection loop (`action.go:169`). For each branch in the repo, a separate `git merge-base <candidate> <branchName>` is called. On a 50-branch repo, that's 50 git merge-base processes. This is the dominant cost in interactive track.
2. **`FindMostRecentTrackedAncestors`** — runs once (or twice — `:76` and `:114` in the same flow). Walks ancestry — bounded by parent-chain depth, not by N.
3. **`eng.TrackBranch`** is cheap (one metadata ref write).
4. **Bootstrap** — fixed cost; small relative to the merge-base loop above.

### Wins

#### 1. Replace per-candidate `GetMergeBase` with a single `rev-list` *(high impact, low risk)*

`internal/actions/track/action.go:159–183` is the classic "find all descendants of a commit" question. One `git rev-list --children <branchName>..HEAD` (or `git for-each-ref --contains <branchName-sha>`) gives all branches that contain the branch's tip in one process. N+1 → 1.

#### 2. Cache `GetRevision` in the candidate loop *(trivial)*

`eng.GetRevision(eng.GetBranch(branchName))` is called inside the loop (`:174`) per candidate. Move it outside; it doesn't change.

#### 3. Pre-warm with `LoadAllBranchRevisions` *(small impact, free)*

Track does several `GetRevision` calls; if `LoadAllBranchRevisions` is called once at the start of `Action`, every subsequent revision lookup is a cache hit (one go-git ref iter vs many separate ones).

## `untrack`

### Call graph

```
newUntrackCmd → common.Run → untrack.Action            internal/actions/untrack/action.go:18
  ├─ eng.Graph + graph.Range(RecursiveChildren)        in-memory
  ├─ for each descendant:
  │     eng.UntrackBranch(name)                        engine_writer.go:148
  │       ├─ git.DeleteMetadata                        1 ref delete
  │       └─ eng.rebuild()                             ← FULL REBUILD per descendant
  └─ eng.UntrackBranch(branchName)                     another rebuild
```

### Where time goes

1. **`eng.rebuild()` inside `UntrackBranch`** — see `internal/engine/engine_writer.go:155`. Each call does the full `GetAllBranchNames + batchReadMetadata + batchReadLocalMetadata` pass. Untracking a stack of K descendants triggers `K+1` full rebuilds. On a 30-branch repo untracking a 10-branch substack, that's 11 × (full branch enumeration + metadata read for every branch). The biggest performance bug in either command.
2. Bootstrap — fixed cost.

### Wins

#### 1. Batch the metadata deletes and rebuild once *(high impact, low risk)*

Replace the per-branch `eng.UntrackBranch(name)` loop with:
- a single `git.DeleteRefsBatch(metadataRefNames)` (the engine already exposes `DeleteRefsBatch`),
- one `eng.rebuild()` afterwards.

For untracking 10 branches: 10 rebuilds → 1 rebuild, plus 10 → 1 ref deletes. The fix is one helper method on the engine: `BatchUntrackBranches([]string) error`.

#### 2. Scope the rebuild to touched branches *(shared with absorb.md #4)*

If the engine grew a `RebuildBranches([]string)`, even the single-branch untrack path would only re-read the affected metadata rather than the whole repo's worth.

## Validation

```
# Track on a deep repo
STACKIT_NO_LOGGING=1 hyperfine 'stackit track <branch> --parent <parent>'      # cheap path
STACKIT_NO_LOGGING=1 hyperfine 'stackit track <branch> --force'               # ancestor walk
STACKIT_NO_LOGGING=1 hyperfine 'stackit track <branch>'                       # interactive path — N×merge-base

# Untrack a 10-branch substack
STACKIT_NO_LOGGING=1 hyperfine 'stackit untrack <root> -f'
```

Instrument: the merge-base loop in `trackBranchRecursively`, every `eng.rebuild()` invocation, every `eng.UntrackBranch` call. The untrack rebuild count should be 1, not K+1, after the fix.
