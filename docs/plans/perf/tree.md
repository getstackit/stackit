# `tree` (and `tree full` / `tree short`) — Performance Analysis

**Tier:** deep (the dashboard view — run constantly, FULL hits GitHub).

> **Status:** Most planned wins are already implemented. Diff stats, commit
> counts, and SHAs are resolved in a single batched, `(base, head)`-cached pass
> (`Engine.BatchBranchStats`); annotation metadata reads go through the shared
> metadata cache; and the annotation worker pool is already bounded by
> `GOMAXPROCS`. The only remaining proposed win is cross-invocation caching of
> GitHub PR-check status (see Proposed wins below).

## Call graph (non-interactive, the common shell case)

```
executeTree → common.Run                         (same bootstrap family as co)
  └─ actions.TreeAction                           (internal/actions/tree.go:88)
       ├─ if --json:        treeActionJSON → BuildTreeJSON
       ├─ if interactive:   tui.NewTreeModel + tea.Run     (separate TUI path)
       │
       ├─ tui.GetWorktreeData(eng)                         one parse over engine state
       ├─ Engine.BatchBranchStats(visibleBranches)         one batched stats pass
       │
       ├─ if FULL: github.BatchGetPRChecksStatus(branchesWithPRs)
       │
       └─ utils.Run(visible branches, ...)                 GOMAXPROCS-bounded pool
              └─ buildTreeAnnotation
                    └─ tui.BuildFullAnnotation / GetBranchAnnotation
                          ├─ stat.ShortSHA                 from batched stats
                          ├─ branch.GetPrInfo()            shared metadata cache
                          ├─ stat.CommitCount              from batched stats
                          └─ stat.LinesAdded/Deleted       from batched stats
```

## Where time goes (largest -> smallest, typical FULL run)

1. **`BatchGetPRChecksStatus`**. Single network round trip but it is GitHub GraphQL — typically 300-800ms. The call is already filtered to branches with PRs (`BranchFilter{ExcludeTrunk: true, RequirePR: true}`) and skipped when none are eligible, so the remaining win is caching across runs.
2. **Branch stats batching**. `BatchBranchStats` resolves short SHAs, commit counts, and diff stats for the visible branches in one batched, `(base, head)`-cached pass. It remains proportional to the number of rendered branches but does no per-branch N+1 git.
3. **Annotation rendering**. Normal/full renders skip commit messages (`AnnotationOptions{SkipCommitMessages: true}`), so each visible branch only does cache-backed PR-metadata reads plus assembly from the batched stats.
4. **Bootstrap**. `LoadModeShared` avoids local metadata at startup, but tree still needs repo-wide shared metadata to render the graph.

For `tree short` and `tree` (NORMAL): no GitHub round trip. `tree short` skips `BatchBranchStats` entirely (`opts.Style != TreeStyleShort` gate), so annotation construction dominates.

For `tree --json`: same shape as the embedded `state --json` stack snapshot (`BuildTreeJSON`), including PR check status only for branches that have PR metadata.

## Proposed wins (ranked)

### 1. Cache `BatchGetPRChecksStatus` between consecutive tree invocations *(medium impact, low risk)*

A TTL cache (say 30s) keyed on the sorted branch name list would make repeated `tree full` invocations during a typing/review burst near-instant. Invalidate on `submit`.

> **Note on feasibility:** the CLI runs each `tree` as a fresh process, so an
> in-memory cache on `StackitGitHubClient` would not survive between
> invocations — the cache must be **on-disk** (or served by the long-lived API
> server, where an in-memory TTL cache does help). Today
> `BatchGetPRChecksStatus` (internal/github/client_real.go:125) calls
> `BatchGetPRChecksStatusGraphQL` directly with no caching layer, so this is
> not started.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit tree' \
  'stackit tree short' \
  'stackit tree full'
```

Instrument: `BatchGetPRChecksStatus`, the `utils.Run` block in `TreeAction`, batched branch stats (`BatchBranchStats`), and annotation construction. The cross-section between FULL and short is the GitHub cost; the cross-section between tree and tree short is the per-branch annotation work.
