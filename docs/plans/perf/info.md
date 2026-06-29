# `info` / `i` — Performance Analysis

**Tier:** medium (frequent quick-status check; one branch at a time).

## Call graph

```
newInfoCmd → common.Run                              (same bootstrap as co)
  └─ actions.InfoAction                              internal/actions/info.go:61
       ├─ if --stack: StackInfoAction                (separate stack-wide path)
       ├─ ResolveBranchName                          cheap (cache lookup)
       ├─ if untracked: FetchRemoteMetadata + ApplyRemoteMetadataForBranches
       │     ← network fetch — only on untracked-branch path (info.go:85–91)
       ├─ if --json: outputBranchInfoJSON            JSON path
       │
       ├─ branch.IsBranchUpToDate                    parent revision lookup + cached metadata
       ├─ eng.GetStackDescription
       ├─ branch.GetCommitDate                       one git op
       ├─ branch.GetPrInfo                           cache hit
       ├─ graph.ChildBranches                        in-memory
       │
       └─ if --patch: branch.GetAllCommits + eng.GetParentCommitSHA + eng.ShowCommits(stat?)
          if --diff:  branch.GetAllCommits + eng.GetParentCommitSHA + eng.ShowDiff(stat?)
          else:        branch.GetAllCommits(Readable)
```

## Where time goes

For the **default `stackit info` on the current branch** (no flags):
1. **Bootstrap** dominates — info does ~5 git ops after bootstrap.
2. `branch.IsBranchUpToDate` still does a live parent revision lookup. For one branch this is small.
3. `branch.GetCommitDate` shells out per call.
4. `branch.GetAllCommits(Readable)` walks the commit range via go-git.

For **`info --diff` / `info --patch`**:
1. **`eng.ShowDiff` / `eng.ShowCommits`** dominates. These shell to `git show` / `git diff`. Cost scales with the diff size, not stackit overhead.
2. `branch.GetAllCommits(SHA)` then `GetParentCommitSHA` adds an extra git op pair to find the base. For `--patch`, the base lookup uses the *oldest* commit's parent SHA — a separate `git rev-parse` is needed because `GetAllCommits` returns SHAs but not their parents.

For **`info <untracked-branch>`** (`internal/actions/info.go:78–92`):
- Triggers a real **`git fetch refs/stackit/metadata`** network call via `eng.FetchRemoteMetadata`. Dominates everything — many seconds on slow networks. This is the only info path that does network I/O.

## Proposed wins (ranked)

### 1. Batch the restack status in stack-wide info *(shared with cross-cutting.md #2)*

> **Status:** Partially done. `StackInfoAction` (`internal/actions/stack_info.go:48`) already
> batches the heavy per-branch reads: `BatchCommits`, `BatchDiffStats`,
> `BatchChangedFileCounts`, and `BatchGetPRSubmissionStatus` (stack_info.go:68–74). The
> default one-branch `info` path is small and needs nothing here.

The remaining N+1 is the restack/`FixedMap` loop (`internal/actions/stack_info.go:148–151`),
which calls `branch.NeedsRestack()` per branch. That resolves to `IsUpToDate` →
`git.GetRevision(parent)` once per branch (no global revision cache by design), so a stack of
N branches spawns N parent-revision lookups. Use the existing batched
`ReadBranchStatuses` (`internal/engine/engine_branch_status.go:187`, which fans the parent
revisions through `BatchGetRevisions`) to compute the `FixedMap` in one pass.

### 2. Cache `FetchRemoteMetadata` per session *(small impact, low risk)*

`internal/actions/info.go:87` calls `eng.FetchRemoteMetadata` (→ `FetchRemote`,
`internal/engine/branch_mutations.go:79`), which performs an unconditional network fetch
every time an untracked branch is queried. There is no session memoization. For the one-shot
CLI this fires at most once per invocation, so the win only matters for a long-lived engine
(the API server / watcher), where calling info on the same untracked branch twice should not
re-fetch. Add a process-lifetime guard around the metadata fetch if/when the engine is reused.

### 3. Combine `GetAllCommits + GetParentCommitSHA` into one git call *(small impact, low risk)*

For `--patch` mode, `internal/actions/info.go:190–193` does:
- `GetAllCommits(SHA)` → walk commits
- `GetParentCommitSHA(oldestSHA)` → another rev-parse

The `--diff` non-trunk branch repeats the same pair at `internal/actions/info.go:226–229`.

A single `git rev-list --parents` over the range produces both. Or read the stored parent
revision directly — `meta.GetParentBranchRevision()` *is* the base revision and is already in
cache after bootstrap (see `internal/engine/branch_tracking.go`).

### 4. `GetCommitDate` could come from the engine state cache *(trivial)*

`GetCommitDate` (`internal/engine/engine_branch_info.go:13`) shells out to
`git.GetCommitDate` per call. The branch SHA is cached after bootstrap, so a
`git log -1 --format=%ct <sha>` is needed once per HEAD — could be batched into a
`branchState` field at rebuild time if `info`/`tree` become hot enough to care.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit info' \
  'stackit info --diff' \
  'stackit info --diff --stat'
```

The delta to `stackit parent` is the post-bootstrap branch-info cost. The delta from `info` to `info --diff` is the diff render cost (usually git, not stackit).
