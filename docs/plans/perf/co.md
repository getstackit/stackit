# `co` / `checkout` — Performance Analysis

**Tier:** deep (hot path; runs many times per session).

## Call graph

```
NewCheckoutCmd.RunE
  └─ common.Run                              internal/cli/common/common.go:33
       ├─ app.GetContextWithWriter           internal/app/context.go:377
       │    ├─ git.NewRunner / DiscoverRepoRoot
       │    ├─ config.LoadConfig
       │    ├─ output.NewFileLogger
       │    └─ engine.NewEngine              internal/engine/engine_impl.go:141
       │         ├─ g.InitDefaultRepo()           (go-git open)
       │         ├─ g.GetCurrentBranch()          (read HEAD)
       │         ├─ rebuildInternal(false)        internal/engine/engine_internal.go:13
       │         │    ├─ GetAllBranchNames()
       │         │    ├─ batchReadMetadata(branches)
       │         │    └─ batchReadLocalMetadata(branches)
       │         └─ maybeAutoFetchRemoteMetadata()    fast path = 1 config read
       └─ Engine.IsInManagedWorktree()       internal/cli/common/common.go:42
  └─ common.Checkout → actions.CheckoutAction internal/actions/checkout.go:51
       ├─ resolveBranchName                  internal/actions/checkout.go:264
       │    └─ eng.AllBranches() (already cached, cheap)
       ├─ getWorktreeSwitchInfo              internal/actions/checkout.go:200
       │    ├─ Engine.GetStackRootForBranch
       │    └─ Engine.GetWorktreeForStack    (metadata + fs.Stat)
       ├─ Engine.CheckoutBranch              internal/engine/engine_writer.go:268
       │    └─ git.runner.CheckoutBranch     internal/git/branches.go:45
       │         ├─ worktree.Status()        ← O(working tree) go-git walk
       │         └─ worktree.Checkout()      ← O(working tree) go-git checkout
       └─ printBranchInfo                    internal/actions/checkout.go:151  (skipped if --quiet)
            └─ up to 11 × IsBranchUpToDate
                 └─ git.GetRevision + readMetadata   per branch, no batching
```

## Where time goes (largest → smallest, typical case)

1. **`worktree.Status()` before checkout** (`internal/git/branches.go:58`). go-git walks the entire working tree to decide `Keep: !status.IsClean()`. On a large repo this is the single most expensive call. The check is purely to choose `Keep`.
2. **`rebuildInternal` on every command** (`internal/engine/engine_internal.go:13`). Reads every branch's `refs/stackit/metadata/<branch>` and `refs/stackit/local-metadata/<branch>`. Parallelized but still O(N branches). For `co <exact-name>` none of this is needed.
3. **`worktree.Checkout()` via go-git** (`internal/git/branches.go:62`). Pure-Go checkout is typically slower than shelling out to `git checkout`.
4. **`printBranchInfo`** (`internal/actions/checkout.go:151`). Up to 11 round-trips through `IsUpToDate` (one for the target, ten ancestors). Each call does a fresh `git.GetRevision` + `readMetadata`; cached engine state is not consulted.
5. **`IsInManagedWorktree()`** runs unconditionally in `common.Run` even when the user just wants to flip HEAD inside the main repo.

## Proposed wins (ranked by expected impact ÷ risk)

### 1. Eliminate the pre-checkout `Status()` walk *(high impact, low risk)*

`internal/git/branches.go:45` — remove the `worktree.Status()` call and either:
- always pass `Keep: true` and let go-git surface a clear conflict, or
- shell out to `git checkout <branch>` and rely on git's own dirty-tree handling (still cheaper than a full status walk).

Either change saves the dominant cost on large working trees.

### 2. Lite bootstrap path that skips `rebuildInternal` *(high impact, medium risk)*

Add an opt-in "no metadata" engine init that only resolves trunk + repo root. `co <exact-branch>` can use it because resolution only needs `GetAllBranchNames` (or even just a `git show-ref` for the one branch), not parsed metadata. Falls back to full rebuild on `--quiet=false` (so `printBranchInfo` still works) or on fuzzy/scope resolution.

Every command benefits; `co` benefits most because the rest of its work is tiny.

### 3. `IsUpToDate` should use in-memory state, not re-read metadata *(medium impact, low risk)*

`internal/engine/engine_branch_status.go:148` calls `readMetadata` per branch even though `rebuildInternal` already loaded all metadata into `e.state.branchState`. Source the stored parent revision from `state.branchState` and batch the live parent-revision lookup via a single `git for-each-ref` over the parent set.

### 4. `printBranchInfo` short-circuits *(small impact, free)*

In `internal/actions/checkout.go:151`, the up-to-date check on the target branch and the ten downstack ancestors is informational. Wire it behind `--quiet` (already partially done) and a config flag. For users on deep stacks this removes 11 git calls per checkout.

### 5. Skip `IsInManagedWorktree` when not needed *(small impact, free)*

`common.Run` calls it for every command. Move the call into the commands that branch on worktree state (`co`, `worktree open`, `worktree attach`) instead of running it globally.

### 6. Skip double `AllBranches` traversal *(trivial)*

`resolveBranchName` builds a name slice and scans it linearly. With an exact name in hand, do one map lookup via `Engine.GetBranch` first and return immediately — only fall back to the slice scan for fuzzy/scope matching.

## How to validate

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit co <branch> --quiet' \
  'stackit co <branch>' \
  'git checkout <branch>'
```

The delta between `co --quiet` and `git checkout` is the bootstrap cost; the delta between `co` and `co --quiet` is `printBranchInfo`. Instrument `rebuildInternal`, `worktree.Status`, `worktree.Checkout` with `time.Since(...)` log lines while iterating.
