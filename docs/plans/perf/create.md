# `create` — Performance Analysis

**Tier:** deep (the second most-run hot-path command after `co`).

## Call graph

```
NewCreateCmd.RunE → common.Run                       (same bootstrap as co)
  └─ create.Action                                   internal/actions/create/create.go:42
       ├─ validation.MustBeOnBranch
       │     └─ eng.ValidateOnBranch → eng.CurrentBranch()  ← shells `git symbolic-ref` (NOT cached)
       ├─ eng.CurrentBranch().GetName()              ← shells GetCurrentBranch again (create.go:56)
       ├─ actions.TakeBestEffortSnapshot             internal/actions/common.go:45
       │     │   (no-op when config undo.enabled=false)
       │     └─ engine.TakeSnapshot                  internal/engine/undo.go:108
       │           ├─ git.BatchGetRevisions(branches)        one rev-parse for all branches
       │           ├─ git.ListMetadata                       one go-git ref iter
       │           ├─ json.MarshalIndent + os.WriteFile      snapshot file
       │           └─ enforceMaxStackDepth                   os.ReadDir + sort + maybe os.Remove
       │
       ├─ eng.HasStagedChanges                       internal/git/staging.go:55 (`git diff --cached --quiet`)
       ├─ if interactive + no staged:
       │     eng.HasUnstagedChanges                  internal/git/staging.go:62 (`git diff --quiet`)
       ├─ if non-interactive + no staged guard:
       │     eng.HasUnstagedChanges + eng.HasUntrackedFiles  two more git subprocesses
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
       ├─ eng.CreateAndCheckoutBranch                internal/engine/branch_mutations.go:365
       │     ├─ git.runner.CreateAndCheckoutBranch   internal/git/branches.go:45 (native `git checkout -b`)
       │     └─ addKnownBranchLocked                 pushes new branch into e.state.branches
       │
       ├─ if staged: eng.Commit                      internal/engine/commit_ops.go:10
       │     └─ git.runner.CommitWithOptions         internal/git/commit.go:20
       │           └─ shells `git commit ...`        ← user hooks dominate (no go-git reload anymore)
       │
       ├─ eng.TrackBranch                            internal/engine/branch_tracking.go:12
       │     ├─ git.GetCurrentBranch                 cheap (HEAD), but redundant re-shell
       │     ├─ GetAllBranchNames re-fetch           gated; no longer fires (branch already cached)
       │     └─ SetParent → metadata transaction     1 ref write
       │
       ├─ if --scope: eng.SetScope                   another metadata transaction
       └─ if --insert: handleInsert + extra checkout back to original branch
```

## Where time goes (largest → smallest, typical interactive create)

1. **`git commit` subprocess + user hooks** — the user's pre-commit hook usually dominates wall time when present. Stackit can't change that. (`ReloadRepository` after commit is gone — `Commit` now just shells `git commit` with no go-git reload or cache invalidation, see `internal/git/commit.go`.)
2. **`git diff` staging probes × 1–3** — `HasStagedChanges` runs `git diff --cached --quiet`; the interactive prompt adds `HasUnstagedChanges`; the non-interactive empty-tree guard adds `HasUnstagedChanges` + `HasUntrackedFiles`. These are cheap native git subprocesses (not full go-git working-tree walks), but they are separate processes that could be collapsed into one `git status --porcelain`.
3. **`GetCurrentBranch` shelled 2–3×** — `validation.MustBeOnBranch` → `CurrentBranch()` re-shells, `create.go:56` re-shells, and `TrackBranch` re-shells again. `CurrentBranch()` does not consult any cache; it always shells `git symbolic-ref`.
4. **`TakeSnapshot`** (`internal/engine/undo.go:108`). Now batches revisions in one `BatchGetRevisions` call and is skipped entirely when `undo.enabled=false`. Remaining per-call cost: `git.ListMetadata` (ref iter), the JSON write, and `enforceMaxStackDepth`'s `os.ReadDir + sort` on every mutation.
5. **Bootstrap (`rebuildInternal`)** — same fixed cost as `co.md` describes.
6. **`SetScope` does its own metadata transaction** instead of bundling with the parent-set in `TrackBranch`. Two ref writes where one would do.
7. **`getCommitMessageForBranch`** when no `-m` is provided opens an editor or reads from stdin — user-blocking, not stackit's fault.

## Proposed wins (ranked)

### 1. Coalesce staging probes into one `git status` *(low impact, low risk)*

> **Status:** Premise corrected. The probes are **not** go-git `worktree.Status()` walks — `HasStagedChanges` / `HasUnstagedChanges` run `git diff --cached --quiet` / `git diff --quiet` (`internal/git/staging.go:55,62`) and `HasUntrackedFiles` runs `git ls-files`. So the original "full working-tree walk" cost is overstated; this is now a smaller, optional win.

The non-interactive guard (`internal/actions/create/create.go:125–137`) can fire three separate git subprocesses (`HasStagedChanges`, then `HasUnstagedChanges` + `HasUntrackedFiles`). Add a single `RepoStatus` (`{HasStaged, HasUnstaged, HasUntracked}`) call backed by one `git status --porcelain`, cache it on the engine for the request, and reuse it for staging decisions and the guard. Same fix benefits `modify`, `absorb`, and `submit`.

### 2. Lazy `enforceMaxStackDepth` *(small, low risk)*

> **Status:** Partially done. Batched revisions are implemented — `TakeSnapshot` already calls `e.git.BatchGetRevisions(e.state.branches)` (`internal/engine/undo.go:121`). The opt-out is implemented — `TakeBestEffortSnapshot` short-circuits when `undo.enabled=false` (`internal/actions/common.go:45–48`, config key `stackit.undo.enabled` in `internal/config/keys.go:18`). Only the lazy-enforcement item below remains.

`enforceMaxStackDepth` (`internal/engine/undo.go:172`) does `os.ReadDir + sort + os.Remove` on every snapshot. Cheap individually but it runs on every mutating command. Enforce lazily (only when the directory entry count exceeds N × max-depth) so the common case skips the directory scan and sort.

### 3. Bundle parent-set + scope into one metadata transaction *(small, low risk)*

`TrackBranch` opens a transaction to write the parent ref (`SetParent`, `internal/engine/branch_tracking.go:66`). `create.Action` then calls `SetScope` (`internal/actions/create/create.go:264`), which opens another (`internal/engine/branch_tracking.go:371`). They could be one: add a `TrackWithScope` / `SetParentAndScope` helper that writes both in a single transaction. (Note: `SetScopeAndMarkForUpdate` at `branch_tracking.go:401` already shows the pattern for bundling two writes, but it does not cover the parent-set.) Saves one ref write + one git ref update on every `create --scope`.

### 4. Use a cached current branch instead of re-shelling *(trivial)*

`CurrentBranch()` (`internal/engine/engine_reader.go:48`) always shells `git symbolic-ref` and ignores `e.currentBranch` except to overwrite it. In a single `create` it is invoked at least three times: `validation.MustBeOnBranch` → `ValidateOnBranch`, then `create.go:56`, then again inside `TrackBranch` (`branch_tracking.go:23`). Bootstrap already knows the current branch; provide a cached read (e.g. `CurrentBranchName()` backed by `e.currentBranch`, refreshed only on mutation) and have the validator and `create.Action` consult it. The fix is more involved than "call `CurrentBranch()`" because that method itself re-shells.

## Already implemented (removed from the plan)

- **`ReloadRepository` after commit** — removed. `Commit` (`internal/engine/commit_ops.go:10` → `internal/git/commit.go:20`) shells `git commit` with no go-git repo reload or revision-cache invalidation. The go-git `*Repository` / `revisionCache` machinery this win targeted no longer exists.
- **Snapshot batched revisions + opt-out** — see win #2 status note.
- **Skip `TrackBranch`'s "validate branches exist" re-fetch** — done. `CreateAndCheckoutBranch` pushes the new branch into `e.state.branches` via `addKnownBranchLocked` (`internal/engine/branch_mutations.go:375`), so the `GetAllBranchNames` re-fetch in `TrackBranch` (`branch_tracking.go:28–42`) no longer fires for the create path.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine --warmup 1 \
  'cd /tmp/scratch && git add . && stackit create -m "perf: noop"'
```

Run with a no-op pre-commit hook (or `--no-verify` if that flag is available) to isolate the stackit overhead from user hook cost. Instrument: the `git diff`/`git status` staging probes, `GetCurrentBranch` shells, `TakeSnapshot`, and `git commit`.
