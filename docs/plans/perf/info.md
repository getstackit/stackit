# `info` / `i` — Performance Analysis

**Tier:** medium (frequent quick-status check; one branch at a time).

## Call graph

```
newInfoCmd → common.Run                              (same bootstrap as co)
  └─ actions.InfoAction                              internal/actions/info.go:61
       ├─ if --stack: StackInfoAction                (separate stack-wide path)
       ├─ ResolveBranchName                          cheap (cache lookup)
       ├─ if untracked: FetchMetadataRefs + LoadRemoteMetadataCache + ApplyRemoteMetadataIfExists
       │     ← network fetch — only on untracked-branch path
       ├─ if --json: outputBranchInfoJSON            JSON path
       │
       ├─ branch.IsBranchUpToDate                    engine_branch_status.go:148 — fresh metadata read + GetRevision
       ├─ eng.GetStackDescription
       ├─ branch.GetCommitDate                       one git op
       ├─ branch.GetPrInfo                           cache hit
       ├─ graph.ChildBranches                        in-memory
       │
       └─ if --patch: branch.GetAllCommits + eng.ShowCommits(stat?)
          if --diff:  branch.GetAllCommits + eng.GetParentCommitSHA + eng.ShowDiff(stat?)
          else:        branch.GetAllCommits(Readable)
```

## Where time goes

For the **default `stackit info` on the current branch** (no flags):
1. **Bootstrap** dominates — info does ~5 git ops after bootstrap.
2. `branch.IsBranchUpToDate` re-reads metadata + does a `GetRevision` even though bootstrap has all of this in `state.branchState`. Same N+1 pattern as `co.md` #3.
3. `branch.GetCommitDate` shells out per call.
4. `branch.GetAllCommits(Readable)` walks the commit range via go-git.

For **`info --diff` / `info --patch`**:
1. **`eng.ShowDiff` / `eng.ShowCommits`** dominates. These shell to `git show` / `git diff`. Cost scales with the diff size, not stackit overhead.
2. `branch.GetAllCommits(SHA)` then `GetParentCommitSHA` adds an extra git op pair to find the base. For `--patch`, the base lookup uses the *oldest* commit's parent SHA — a separate `git rev-parse` is needed because `GetAllCommits` returns SHAs but not their parents.

For **`info <untracked-branch>`** (`internal/actions/info.go:78–97`):
- Triggers a real **`git fetch refs/stackit/metadata`** network call. Dominates everything — many seconds on slow networks. This is the only info path that does network I/O.

## Proposed wins (ranked)

### 1. Make `IsBranchUpToDate` use cached state *(shared with co.md #3)*

`internal/engine/engine_branch_status.go:148` re-reads metadata per call. For `info`, this is one extra metadata read per invocation — small but free.

### 2. Cache `FetchMetadataRefs` per session *(small impact, low risk)*

`internal/actions/info.go:85` fetches every time an untracked branch is queried. Cache the result for the process lifetime — calling info on the same untracked branch twice should not re-fetch.

### 3. Combine `GetAllCommits + GetParentCommitSHA` into one git call *(small impact, low risk)*

For `--patch` mode, `internal/actions/info.go:197–201` does:
- `GetAllCommits(SHA)` → walk commits
- `GetParentCommitSHA(oldestSHA)` → another rev-parse

A single `git rev-list --parents` over the range produces both. Or read `meta.GetParentBranchRevision()` directly — that *is* the base revision and is already in cache after bootstrap.

### 4. `GetCommitDate` could come from the engine state cache *(trivial)*

The branch SHA is cached after bootstrap. A `git log -1 --format=%ct <sha>` is needed once per HEAD — could be batched into a `branchState` field at rebuild time if `info`/`log` become hot enough to care.

### 5. Skip the second `eng.GetBranch(branchName)` calls *(trivial)*

`internal/actions/info.go:156` and `:170` re-resolve `branchObj` twice when the earlier `branch` is already correct. Map lookups are cheap but pointless.

### 6. JSON mode does its own work (`outputBranchInfoJSON`)

Not read here — likely shares the same per-branch cost. Should reuse the same cached state as the text path.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit info' \
  'stackit info --diff' \
  'stackit info --diff --stat'
```

The delta to `stackit parent` is the post-bootstrap branch-info cost. The delta from `info` to `info --diff` is the diff render cost (usually git, not stackit).
