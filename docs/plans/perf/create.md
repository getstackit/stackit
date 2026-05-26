# `create` — Performance Analysis

**Tier:** deep (the second most-run hot-path command after `co`).

## Call graph

```
NewCreateCmd.RunE → common.Run                       (same bootstrap as co)
  └─ create.Action                                   internal/actions/create/create.go:38
       ├─ validation.MustBeOnBranch
       ├─ actions.TakeBestEffortSnapshot
       │     └─ engine.TakeSnapshot                  internal/engine/undo.go:108
       │           ├─ for each branch: branch.GetRevision()        N revision lookups (not preloaded)
       │           ├─ git.ListMetadata                              one go-git ref iter
       │           ├─ json.MarshalIndent + os.WriteFile             snapshot file
       │           └─ enforceMaxStackDepth                          os.ReadDir + sort + maybe os.Remove
       │
       ├─ eng.HasStagedChanges                       internal/git/staging.go:102
       │     └─ worktree.Status()                    ← O(working tree)
       ├─ if interactive + no staged:
       │     eng.HasUnstagedChanges                  → another worktree.Status()
       │
       ├─ stage (optional, only if --all/--update/--patch):
       │     git.StageChanges                        forks `git add ...`
       │
       ├─ getCommitMessageForBranch                  reads file/stdin/editor depending on flags
       │
       ├─ determineBranch
       │     └─ pattern.GetBranchName                may shell to git config for user info
       │
       ├─ eng.AllBranches() + slices.ContainsFunc    O(N) duplicate check, cached
       │
       ├─ eng.CreateAndCheckoutBranch                internal/engine/engine_writer.go:296
       │     └─ git.runner.CreateAndCheckoutBranch   internal/git/branches.go:91
       │           └─ native `git checkout -b <branch>`
       │
       ├─ if staged: eng.Commit                      internal/engine/engine_writer.go:709
       │     └─ git.runner.Commit                    internal/git/commit.go:68
       │           ├─ shells `git commit -m ...`     ← user hooks dominate
       │           ├─ revisionCache.InvalidateAll
       │           └─ ReloadRepository               ← closes + reopens go-git repo
       │
       ├─ eng.TrackBranch                            internal/engine/engine_writer.go:91
       │     ├─ git.GetCurrentBranch                 cheap (HEAD)
       │     ├─ optional GetAllBranchNames           usually a no-op (already cached)
       │     └─ SetParent → metadata transaction     1 ref write
       │
       ├─ if --scope: eng.SetScope                   another metadata transaction
       └─ if --insert: handleInsert + extra checkout back to original branch
```

## Where time goes (largest → smallest, typical interactive create)

1. **`git commit` subprocess + user hooks** — the user's pre-commit hook usually dominates wall time when present. Stackit can't change that, but it can avoid duplicate work around it (see #4).
2. **`ReloadRepository`** (`internal/git/runner.go:480`) after every commit. Closes the go-git `*Repository`, invalidates the entire revision cache, and re-opens. Re-opens of large repos cost real I/O; throwing away `revisionCache.AllBranchRevisions` is wasteful since only the freshly committed branch's SHA changed.
3. **`worktree.Status()` × 1–2** — `HasStagedChanges` calls it once, and `HasUnstagedChanges` calls it again in the interactive prompt path. Checkout no longer adds an extra go-git status walk. Each remaining status check is a full working-tree walk; on a big repo this is the dominant non-hook cost.
4. **`TakeSnapshot`** (`internal/engine/undo.go:108`). Iterates `e.state.branches` and calls `branch.GetRevision()` per branch, hitting `r.revisionCache` one entry at a time. Also calls `git.ListMetadata` (separate ref iter) and an `os.ReadDir` for stack-depth enforcement. On a 50-branch repo this is ~50 cached lookups + 1 metadata scan + snapshot write — usually 5–15ms but it runs on every mutation.
5. **Bootstrap (`rebuildInternal`)** — same fixed cost as `co.md` describes.
6. **`TrackBranch` re-fetches `GetCurrentBranch`** inside its critical section (`internal/engine/engine_writer.go:102`) even though we just created the branch. Small but pointless.
7. **`getCommitMessageForBranch`** when no `-m` is provided opens an editor or reads from stdin — user-blocking, not stackit's fault.
8. **`SetScope` does its own metadata transaction** instead of bundling with the parent-set in `TrackBranch`. Two ref writes where one would do.

## Proposed wins (ranked)

### 1. Coalesce staging `worktree.Status()` calls *(medium impact, low risk)*

`HasStagedChanges` and `HasUnstagedChanges` both walk the working tree. Add a single `RepoStatus` (`{HasStaged, HasUnstaged, HasUntracked}`) call early in `create.Action`, cache it on the engine for the duration of the request, and reuse it for prompts/validation. Same fix benefits `modify`, `absorb`, and `submit`.

### 2. Snapshot should use batched revisions and skip on opt-out *(medium impact, low risk)*

`TakeSnapshot` should call `BatchGetRevisions` (or use the revision cache after `LoadAllBranchRevisions`) instead of iterating per-branch. Also: snapshots are only consumed by `undo`. For users that never undo, the work and disk write are pure overhead. Add a config flag (`undo.enabled=false`) and short-circuit `TakeBestEffortSnapshot` when it's off.

Also: `enforceMaxStackDepth` does `os.ReadDir + sort + os.Remove`. Cheap individually, but on every mutating command it adds up. Lazy enforcement (only when the directory size exceeds N × max-depth) is fine.

### 3. Drop or scope `ReloadRepository` after commit *(medium impact, medium risk)*

`ReloadRepository` throws away the entire go-git in-memory state to pick up a single new commit. Two cheaper options:
- Just invalidate the affected branch in `revisionCache` (`revisionCache.Invalidate(branchName)`) and let go-git re-read its packed refs lazily.
- Re-open only the refs subsystem, not the whole repo.

Risk: go-git caches loose-object packs; if we don't reload we may miss the new commit object on subsequent lookups. Easiest first step is `revisionCache.Invalidate(branchName)` plus a targeted ref refresh.

### 4. Bundle parent-set + scope into one metadata transaction *(small, low risk)*

`TrackBranch` opens a transaction to write the parent ref. `SetScope` opens another. They could be one: extend `SetParent` to accept an optional scope, or expose a `TrackWithScope` helper that does both atomically. Saves one ref write + one git ref update.

### 5. Skip `TrackBranch`'s "validate branches exist" re-fetch *(trivial)*

`internal/engine/engine_writer.go:107–139` re-runs `GetAllBranchNames` if the just-created branch isn't in the cached list. After `CreateAndCheckoutBranch`, that cache is stale — but we know the answer (we just created it). Have `CreateAndCheckoutBranch` push the branch into `e.state.branches` (it already does, `engine_writer.go:307`), and have `TrackBranch` trust the cache.

### 6. Defer `validation.MustBeOnBranch` `GetCurrentBranch` to use cached value *(trivial)*

Bootstrap already populated `e.currentBranch`. The validator should consult `eng.CurrentBranch()` (cached) rather than re-shelling.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine --warmup 1 \
  'cd /tmp/scratch && git add . && stackit create -m "perf: noop"'
```

Run with a no-op pre-commit hook (or `--no-verify` if that flag is available) to isolate the stackit overhead from user hook cost. Instrument: each remaining `worktree.Status()` call, `TakeSnapshot`, `ReloadRepository`, and `git commit`.
