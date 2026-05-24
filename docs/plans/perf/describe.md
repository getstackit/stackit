# `describe` — Performance Analysis

**Tier:** medium (low frequency; one stack at a time; metadata push at the end).

## Call graph

```
newDescribeCmd → common.Run → describe.Action          internal/actions/describe/describe.go:26
  ├─ eng.IsTrunk + currentBranch.IsTracked            cache reads
  ├─ eng.GetStackRootForBranch                        in-memory traversal
  │
  ├─ --show: print + return                           (no network)
  ├─ --clear:
  │    ├─ eng.ClearStackDescription                   metadata transaction
  │    └─ markStackAndPushMetadata                    see below
  ├─ -m / -d:
  │    └─ applyStackDescription
  │         ├─ eng.SetStackDescription                metadata transaction
  │         └─ markStackAndPushMetadata
  └─ interactive:
       ├─ eng.GetStackDescription                     cache read
       ├─ tui.OpenEditor                              user-blocking
       └─ applyStackDescription

markStackAndPushMetadata:
  ├─ graph.CollectBranches(root)                      in-memory walk over stack
  ├─ eng.BatchMarkNeedsPRBodyUpdate(branchNames)      ← writes ONE meta ref PER branch
  └─ actions.PushMetadataOnly                         `git push refs/stackit/metadata/<root>`
```

## Where time goes

1. **`actions.PushMetadataOnly`** — one network round trip. Same cost story as `scope.md` #1.
2. **`BatchMarkNeedsPRBodyUpdate(branchNames)`** — currently writes one ref per branch. Let me verify what "Batch" means here in practice.

Looking at `engine_writer.go:1062` (not pasted here), `BatchMarkNeedsPRBodyUpdate` should ideally write all the flags in a single `UpdateRefsBatch` call. The CLAUDE.md "Batch Operations" rule explicitly calls this out. If it does — good. If it just loops `MarkNeedsPRBodyUpdate` internally, that's an N+1.

3. **`SetStackDescription` (or `ClearStackDescription`)** — one metadata transaction.
4. **Bootstrap + open editor** — same fixed costs as everywhere else.

For a stack with 10 branches, the metadata phase writes 1 description + 10 mark-flags + 1 push. The push dominates by an order of magnitude.

## Wins (ranked)

### 1. Ensure `BatchMarkNeedsPRBodyUpdate` is genuinely batched *(check first; high impact if it isn't)*

This is one of the "use batch APIs" examples from `.claude/rules/code-style.md`. Confirm via `internal/engine/engine_writer.go:1062` that the implementation uses `UpdateRefsBatch` or `withMetadataTx` over the whole branch set. If it's a loop, the cost on a 20-branch stack is 20 × ref writes + 20 × tx overhead = ~50–200ms wasted. (The fact that it's called `Batch...` suggests it is — verify before optimizing.)

### 2. Combine `SetStackDescription` + `BatchMarkNeedsPRBodyUpdate` in one tx *(shared with scope.md #1)*

The description write and the mark-for-PR-update write touch overlapping metadata. One transaction over `{stackRoot, ...branches}` is enough.

### 3. Defer the metadata push for `--show` — already done *(trivial confirmation)*

`--show` short-circuits before any writes/pushes. Good.

### 4. Bootstrap "lite" mode benefits `--show` *(shared with co.md #2)*

`--show` only needs the current branch and its stack root. Doesn't need full metadata for every branch in the repo.

### 5. Editor mode: don't re-fetch description after editor closes *(trivial)*

`describe.go:96` reads `existingDesc` before opening the editor; `applyStackDescription` writes the new one. No redundant read on this path. Good.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit describe --show' \
  'stackit describe -m "Test"'
```

The delta isolates the metadata write + push from the bootstrap baseline. Instrument: each metadata transaction, `PushMetadataRefs`, and confirm `BatchMarkNeedsPRBodyUpdate` is a single ref-write batch (it should be — confirm via `git update-ref --stdin` or equivalent).
