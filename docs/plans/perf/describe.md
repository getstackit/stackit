# `describe` — Performance Analysis

**Tier:** medium (low frequency; one stack at a time; metadata push at the end).

## Call graph

```
newDescribeCmd → common.Run → describe.Action          internal/actions/describe/describe.go:26
  ├─ eng.IsTrunk + currentBranch.IsTracked            cache reads
  ├─ eng.GetStackRootForBranch                        in-memory traversal
  │
  ├─ --show: branches-only bootstrap, print + return  (no network)
  ├─ --clear:
  │    ├─ eng.ClearStackDescription                   metadata write (stack-meta ref)
  │    └─ markStackAndPushMetadata                    see below
  ├─ -m / -d / stdin:
  │    └─ applyStackDescription
  │         ├─ eng.SetStackDescription                metadata write (stack-meta ref)
  │         └─ markStackAndPushMetadata
  └─ interactive:
       ├─ eng.GetStackDescription                     cache read       describe.go:96
       ├─ tui.OpenEditor                              user-blocking
       └─ applyStackDescription

markStackAndPushMetadata:                             describe.go:135
  ├─ eng.Graph + graph.CollectBranches(root).Names()  in-memory walk over stack
  ├─ eng.MarkBranchesForPRBodyUpdate(branchNames)     batched: one blob-batch + one UpdateRefsBatch
  └─ actions.PushMetadataOnly                          one `git push` of metadata refs
```

## Where time goes

1. **`actions.PushMetadataOnly`** — one network round trip. Same cost story as `scope.md` #1.
2. **`SetStackDescription` / `ClearStackDescription`** — one stack-meta ref write
   (`WriteStackMeta` → `CreateBlob` + `UpdateRef`).
3. **`MarkBranchesForPRBodyUpdate`** — already a single batched write across all
   stack branches (blob-batch + atomic `UpdateRefsBatch`).

For a stack with 10 branches the metadata phase does: 1 stack-meta ref write +
1 batched local-metadata write (covering all 10 branches) + 1 push. The push
dominates by an order of magnitude.

## Wins (ranked)

### 1. Fold the stack-description write and the PR-update flag write into one ref batch *(shared with scope.md #1)*

> **Status:** Not started. The premise in the original plan ("they touch
> overlapping metadata, one tx over `{stackRoot, ...branches}`") was wrong and has
> been corrected here.

The two writes target **different** refs and run as two separate, non-atomic
phases today:

- `SetStackDescription` writes the **stack-meta ref** keyed by stack ID
  (`StackMetaRefName(stackID)`) via `WriteStackMeta` (`internal/engine/stack_id.go:39`,
  `internal/git/stack_metadata.go:81`).
- `MarkBranchesForPRBodyUpdate` writes the **per-branch local-metadata refs**
  via `WriteLocalMetadataBlobsBatch` + `UpdateRefsBatch`
  (`internal/engine/pr_flags.go:14`).

They can still be collapsed into a single atomic `UpdateRefsBatch`:
`WriteStackMetaBlob` (`internal/git/stack_metadata.go:122`) already returns a blob
SHA without touching a ref — exactly for transactional writes. Build one
`[]git.RefUpdate` containing the stack-meta ref update plus every local-metadata
ref update and commit them in one `UpdateRefsBatch` call. This removes one git
process (the standalone `WriteStackMeta` `UpdateRef`) and makes the
description-set + mark-for-update atomic. The push afterward is unchanged.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit describe --show' \
  'stackit describe -m "Test"'
```

The delta isolates the metadata write + push from the bootstrap baseline.
Instrument each ref write and `PushMetadataForBranches`; after win #1 the
description-set path should show a single combined `UpdateRefsBatch` rather than a
`WriteStackMeta` `UpdateRef` followed by a separate batch.
