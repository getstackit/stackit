# `log` (and `log full` / `log short`) — Performance Analysis

**Tier:** deep (the dashboard view — run constantly, FULL hits GitHub).

## Call graph (non-interactive, the common shell case)

```
executeLog → common.Run                          (same bootstrap family as co)
  └─ actions.TreeAction
       ├─ if --json:        logActionJSON
       ├─ if interactive:   tui.NewLogModel + tea.Run      (separate TUI path)
       │
       ├─ if LogStyleFull:  Engine.PopulateRemoteShas      one shell `for-each-ref refs/remotes/...`
       ├─ tui.GetWorktreeData(eng)                         one parse over engine state
       ├─ Engine.PreloadBranchData
       │    ├─ git.LoadAllBranchRevisions                  one go-git ref iter → revision cache
       │    └─ batchReadMetadata(branches)                 already hot after bootstrap
       │
       ├─ if FULL: github.BatchGetPRChecksStatus(branchesWithPRs)
       │
       └─ utils.Run(visible branches, ...)
              └─ tui.BuildFullAnnotationWithOptions
                    └─ GetBranchAnnotationWithOptions
                          ├─ branch.GetRevision()          cache hit (preloaded)
                          ├─ branch.GetPrInfo()            cache hit (preloaded)
                          ├─ eng.GetCommitCount(branch)    commit range count, per branch
                          └─ branch.GetDiffStats()         shells `git diff --numstat` per branch
```

## Where time goes (largest -> smallest, typical FULL run)

1. **`BatchGetPRChecksStatus`**. Single network round trip but it is GitHub GraphQL — typically 300-800ms. The call is already filtered to branches with PRs and skipped when none are eligible, so the remaining win is caching across runs.
2. **Per-branch `GetDiffStats`**. Each call spawns `git diff --numstat <base> <head>`. For N branches -> N processes, ~3-8ms each plus IO. Parallelized via `utils.Run`, but still O(N) processes.
3. **Per-branch commit counting**. Normal/full renders no longer load commit messages, but `GetCommitCount` still walks the branch range per branch.
4. **`PreloadBranchData`**. Already batched well. Branch revisions load in one ref iter; metadata should usually be hot from bootstrap.
5. **Bootstrap**. `LoadModeShared` avoids local metadata at startup, but log still needs repo-wide shared metadata to render the tree.
6. **`PopulateRemoteShas` for FULL**. Single git invocation; small.

For `log short` and `log` (NORMAL): no GitHub round trip, so per-branch diff stats and commit counts dominate.

For `log --json`: same shape, including PR check status only for branches that have PR metadata.

## Proposed wins (ranked)

### 1. Batch diff stats across all branches *(high impact, medium effort)*

`GetDiffStats` is the worst N+1 in this command. Replace per-branch `git diff --numstat <base> <head>` with a single stack-level pass, or at least cache by `(base, head)` and cap subprocess fanout.

Estimated saving on a 20-branch stack: 60-160ms -> ~10-20ms.

### 2. Batch commit counts/ranges *(medium impact, medium effort)*

Commit messages are no longer populated for normal/full log, and `log short` skips this work. The remaining cost is per-branch commit counting. A stack-level `git rev-list --boundary` pass could produce counts for every branch and also feed `info` / `absorb` range needs.

### 3. Drop the redundant `batchReadMetadata` in `PreloadBranchData` *(small but free)*

`PreloadBranchData` calls `batchReadMetadata(branches)` again. By this point, bootstrap should already have populated shared metadata and the cache layer is hot. Either remove this call or make it a cheap "ensure populated" check for lazy-load modes.

### 4. Cache `BatchGetPRChecksStatus` between consecutive log invocations *(medium impact, low risk)*

A TTL cache (say 30s) keyed on the sorted branch name list would make repeated `log full` invocations during a typing/review burst instant. Invalidate on `submit`.

### 5. Worker pool bounded by repository fanout, not branch count *(small, future)*

`utils.Run(allBranches, ...)` can fan out heavily. For very large repos, bound work by `Engine.MaxConcurrency`, especially while each goroutine can fork git for diff stats.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit tree' \
  'stackit tree short' \
  'stackit tree full'
```

Instrument: `BatchGetPRChecksStatus`, the `utils.Run` block in `TreeAction`, individual `GetDiffStats` calls, and commit count calls. The cross-section between FULL and short is the GitHub cost; the cross-section between log and log short is the per-branch annotation work.
