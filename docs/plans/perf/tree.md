# `tree` (and `tree full` / `tree short`) — Performance Analysis

**Tier:** deep (the dashboard view — run constantly, FULL hits GitHub).

## Call graph (non-interactive, the common shell case)

```
executeTree → common.Run                         (same bootstrap family as co)
  └─ actions.TreeAction
       ├─ if --json:        treeActionJSON
       ├─ if interactive:   tui.NewTreeModel + tea.Run     (separate TUI path)
       │
       ├─ tui.GetWorktreeData(eng)                         one parse over engine state
       ├─ Engine.BatchBranchStats                          one batched stats pass
       │
       ├─ if FULL: github.BatchGetPRChecksStatus(branchesWithPRs)
       │
       └─ utils.Run(visible branches, ...)
              └─ tui.BuildFullAnnotationWithOptions
                    └─ GetBranchAnnotationWithOptions
                          ├─ branch.GetRevision()          from batched stats
                          ├─ branch.GetPrInfo()            cache hit (preloaded)
                          ├─ commit count                  from batched stats
                          └─ diff stats                    from batched stats
```

## Where time goes (largest -> smallest, typical FULL run)

1. **`BatchGetPRChecksStatus`**. Single network round trip but it is GitHub GraphQL — typically 300-800ms. The call is already filtered to branches with PRs and skipped when none are eligible, so the remaining win is caching across runs.
2. **Branch stats batching**. `BatchBranchStats` resolves short SHAs, commit counts, and diff stats for the visible branches. It avoids the old per-branch `GetDiffStats` and `GetCommitCount` loops, but remains proportional to the number of rendered branches.
3. **Annotation rendering**. Normal/full renders no longer load commit messages, but each visible branch still runs through annotation construction and PR metadata reads.
4. **Bootstrap**. `LoadModeShared` avoids local metadata at startup, but tree still needs repo-wide shared metadata to render the graph.

For `tree short` and `tree` (NORMAL): no GitHub round trip, so branch stats and annotation construction dominate.

For `tree --json`: same shape as the embedded `state --json` stack snapshot, including PR check status only for branches that have PR metadata.

## Proposed wins (ranked)

### 1. Batch diff stats across all branches *(high impact, medium effort)*

`GetDiffStats` is the worst N+1 in this command. Replace per-branch `git diff --numstat <base> <head>` with a single stack-level pass, or at least cache by `(base, head)` and cap subprocess fanout.

Estimated saving on a 20-branch stack: 60-160ms -> ~10-20ms.

### 2. Batch commit counts/ranges *(medium impact, medium effort)*

Commit messages are no longer populated for normal/full tree, and `tree short` skips this work. The remaining cost is branch range counting. A stack-level `git rev-list --boundary` pass could produce counts for every branch and also feed `info` / `absorb` range needs.

### 3. Keep metadata reads on the bootstrap/shared-cache path *(small but free)*

By the time tree renders, bootstrap should already have populated shared metadata and the cache layer should be hot. Keep future tree changes from reintroducing per-branch metadata reads in the annotation path.

### 4. Cache `BatchGetPRChecksStatus` between consecutive tree invocations *(medium impact, low risk)*

A TTL cache (say 30s) keyed on the sorted branch name list would make repeated `tree full` invocations during a typing/review burst instant. Invalidate on `submit`.

### 5. Worker pool bounded by repository fanout, not branch count *(small, future)*

`utils.Run(allBranches, ...)` can fan out heavily. For very large repos, bound work by `Engine.MaxConcurrency`, especially while each goroutine can fork git for diff stats.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit tree' \
  'stackit tree short' \
  'stackit tree full'
```

Instrument: `BatchGetPRChecksStatus`, the `utils.Run` block in `TreeAction`, batched branch stats, and annotation construction. The cross-section between FULL and short is the GitHub cost; the cross-section between tree and tree short is the per-branch annotation work.
