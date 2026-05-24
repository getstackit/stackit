# `log` (and `log full` / `log short`) — Performance Analysis

**Tier:** deep (the dashboard view — run constantly, FULL hits GitHub).

## Call graph (non-interactive, the common shell case)

```
executeLog → common.Run                          (same bootstrap as co — see co.md)
  └─ actions.LogAction                            internal/actions/log.go:82
       ├─ if --json:        logActionJSON         internal/actions/log.go:239
       ├─ if interactive:   tui.NewLogModel + tea.Run      (separate TUI path)
       │
       ├─ if LogStyleFull:  Engine.PopulateRemoteShas      one shell `for-each-ref refs/remotes/...`
       ├─ tui.GetWorktreeData(eng)                          one parse over engine state
       ├─ Engine.PreloadBranchData                          internal/engine/engine_branch_info.go:135
       │    ├─ git.LoadAllBranchRevisions                  one go-git ref iter → revision cache
       │    └─ batchReadMetadata(branches)                  already done in rebuildInternal
       │
       ├─ if FULL: github.BatchGetPRChecksStatus(branchNames)   1 GraphQL call (latency-bound)
       │
       └─ utils.Run(allBranches, …)                         parallel worker pool, N branches
              └─ tui.BuildFullAnnotation                    internal/tui/tree_renderer.go:288
                    └─ GetBranchAnnotation                  internal/tui/tree_renderer.go:327
                          ├─ branch.GetRevision()           cache hit (preloaded)
                          ├─ branch.GetPrInfo()             cache hit (preloaded)
                          ├─ branch.GetAllCommits(Readable) engine_branch_info.go:159 — go-git log range, per branch
                          └─ branch.GetDiffStats()          engine_branch_info.go:60 — shells `git diff --numstat` per branch
```

## Where time goes (largest → smallest, typical FULL run)

1. **`BatchGetPRChecksStatus`** (`internal/actions/log.go:143`). Single network round trip but it's GitHub GraphQL — typically 300–800ms. Dominates wall time for `log full`. Already batched; the only further wins are caching across runs (next-token / etag) or scoping to visible branches.
2. **Per-branch `GetDiffStats`** (`internal/engine/engine_branch_info.go:60`). Each call spawns `git diff --numstat <base> <head>` (`internal/git/runner.go:873`). For N branches → N processes, ~3–8ms each plus IO. On a 20-branch stack this is 60–160ms of pure subprocess overhead. Parallelized via `utils.Run`, but still O(N) processes.
3. **Per-branch `GetAllCommits(Readable)`** (`internal/engine/engine_branch_info.go:159`). Goes through `git.GetCommitRange`, which is in-process via go-git but still does a commit walk per branch. Mostly memory-bound but adds up with deep histories.
4. **`PreloadBranchData`** (`internal/engine/engine_branch_info.go:135`). Already batched well. Branch revisions load in one ref iter; metadata reuses the cache populated during `rebuildInternal`. The metadata call here is essentially a no-op on the hot path — could be removed.
5. **`rebuildInternal` in bootstrap** — see `co.md`. Same fixed cost on every invocation.
6. **`PopulateRemoteShas` for FULL** (`internal/actions/log.go:104`). Single git invocation; small.
7. **Summary loop** (`internal/actions/log.go:184`). Calls `ctx.Engine.GetBranch(name)` per annotation — map lookup, trivial.

For `log short` and `log` (NORMAL): no GitHub round trip, no PR check, so the per-branch git work (diff stats, commit walk) dominates.

For `log --json`: identical shape — same `PreloadBranchData`, same per-branch parallel fan-out via `utils.Run`, plus the same `BatchGetPRChecksStatus`. Same wins apply.

## Proposed wins (ranked)

### 1. Batch diff stats across all branches *(high impact, medium effort)*

`GetDiffStats` is the worst N+1 in this command. Replace the per-branch `git diff --numstat <base> <head>` with a single `git diff --numstat --cc` walk or a per-stack-root invocation that emits one numstat block per branch, parsed once. Even a naive "build the (base,head) tuples and concurrent-launch up to maxConcurrency processes" caps overhead, but a single combined call is the real win. Cache the result keyed by `(base,head)` so subsequent log calls in the same session reuse it.

Estimated saving on a 20-branch stack: 60–160ms → ~10–20ms.

### 2. Defer `GetAllCommits` to when commit messages are actually displayed *(high impact, low risk)*

`GetBranchAnnotation` (`internal/tui/tree_renderer.go:350`) calls `GetAllCommits` to populate `CommitMessages` for the detailed view. In NORMAL/FULL non-interactive renders only `CommitCount` is used in the tree. Split it: cheap-path returns count from a `git rev-list --count` (or use the existing `engine.state.branchState` if revisions match metadata), full message list only when the renderer asks for it. Also: `GetCommitRange` walks commits in-process — fine in isolation, but multiplied by N branches it's the second-biggest CPU cost.

For `log short` (where `HideSummary` is set in `RenderOptions`), this whole call can be skipped entirely.

### 3. Drop the redundant `batchReadMetadata` in `PreloadBranchData` *(small but free)*

`internal/engine/engine_branch_info.go:148` calls `batchReadMetadata(branches)` again. By this point, `rebuildInternal` has already populated `e.state` from metadata and the cache layer (`metadataCache`) is hot. Either remove this call entirely or make it a cheap "ensure populated" check.

### 4. Cache `BatchGetPRChecksStatus` between consecutive log invocations *(medium impact, low risk)*

A TTL cache (say 30s) keyed on the sorted branch name list would make repeated `log full` invocations during a typing/review burst instant. Invalidate on `submit`. Optional: only fetch PR check status for branches that have a PR number in metadata — skips work for unsubmitted branches.

### 5. Skip GraphQL entirely when no branch has a PR *(trivial, low impact)*

`branchNames` in `internal/actions/log.go:136` includes branches with no PR. Pre-filter by checking `branch.GetPrInfo() != nil` from the already-loaded metadata before the network call.

### 6. Worker pool bounded by repository fanout, not branch count *(small, future)*

`utils.Run(allBranches, …)` spawns one goroutine per branch. For very large repos (hundreds of branches), bound it via `Engine.MaxConcurrency` — currently effectively unbounded for in-memory work but matters when each goroutine forks a git process (item #1).

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit log' \
  'stackit log short' \
  'stackit log full'
```

Instrument: `BatchGetPRChecksStatus`, the `utils.Run` block in `LogAction`, individual `GetDiffStats` calls (log a per-branch timing). The cross-section between FULL and short is the GitHub cost; the cross-section between log and log short is the per-branch annotation work.
