# Absorb Command

## Overview

`stackit absorb` takes staged hunks from your working tree and amends them into the most relevant commits downstack.

At a high level, absorb:

1. Reads staged hunks.
2. Finds the target commit for each absorbable hunk.
3. Rewrites affected branch commits.
4. Restacks upstack branches (configurable via `--restack`).
5. Restores any hunks that could not be absorbed.

Entry points:

- CLI: `internal/cli/branch/absorb.go`
- Action: `internal/actions/absorb/absorb.go`

## Usage

Common flow:

```bash
git add -A
stackit absorb
```

Useful flags:

- `--dry-run`: show what would be absorbed; no history rewrite.
- `--force`: skip confirmation prompt.
- `--patch`: interactively choose hunks to stage first (`git add -p` behavior).
- `--all`: stage unstaged tracked changes (`git add -u`) before absorb.
- `--restack`: `all` (default), `current`, `scope`, or `none`.
- `--json`: print machine-readable absorb plan/summary.
- `--show-conflict`: show absorb conflict diagnostics.

## Preconditions

Absorb validates:

- you are on a branch (not detached HEAD),
- current branch is not trunk,
- current branch is tracked,
- current branch is modifiable (not locked/frozen),
- no rebase in progress.

Validation chain: `internal/actions/validation/chains.go`

## Target Selection

For each staged hunk:

1. Build candidate commit list from current branch plus downstack ancestors.
2. Respect scope boundaries for downstack search.
3. Skip hunks that are unsupported (`binary`, `new_file`, `deleted_file`).
4. For remaining hunks, run commutation checks against candidate commits.
5. First commit that does not commute is the target.
6. Resolve target commit -> target branch.

If no target is found, the hunk is unabsorbable (`commutes_with_all`).

Unabsorbable types are in `internal/actions/absorb/unabsorbable.go`.

## Apply Phase

Absorb groups hunks by target branch and commit, then applies branch-by-branch in topological order.

History rewrite happens in `engine.ApplyHunksToBranch`:

- checkout parent base in detached HEAD,
- cherry-pick branch commits oldest->newest,
- apply selected hunks and amend when needed,
- move branch ref to rewritten tip.

Implementation: `internal/engine/engine_absorb.go`

## Stash and Restore Safety Model

Absorb protects user state by separating staged vs unstaged/untracked changes:

- staged stash marker: `stackit-absorb-temp-staged`
- unstaged stash marker: `stackit-absorb-temp-unstaged`

Primary path:

1. Stash staged changes with `git stash push --staged`.
2. Stash unstaged/untracked changes separately (`git stash push -u`).
3. Perform absorb rewrite.
4. Restore the unstaged stash by marker.
5. Restore unabsorbed hunks to the working tree **and** index.
6. Drop the staged absorb stash only after every restore verifiably succeeds.

Fallback path (when `--staged` stash errors, e.g. a file with both staged and
unstaged changes — `MM` status):

1. Detect whether the staged stash was still created despite the non-zero exit
   (some Git versions create it anyway); keep it as the recovery net.
2. Capture the unstaged (`index -> worktree`) delta with `git diff --binary`
   **before** the reset, holding it in memory as a patch. The `--binary` form is
   required: a tracked binary file with an unstaged edit would otherwise diff to
   only a `Binary files ... differ` placeholder that `git apply` refuses
   (`cannot apply binary patch ... without full index line`), silently losing
   the edit after `reset --hard` — and, because `git apply` is atomic per
   invocation, poisoning any coexisting text edits captured in the same patch.
   `git diff --binary` embeds a `GIT binary patch` that reapplies cleanly.
3. `reset --hard HEAD` to clear staged state before rewriting. This leaves
   untracked files in place, so they need no separate stash.
4. After the rewrite, reapply the captured patch with `git apply` (working tree
   only). Unlike a `--keep-index` stash pop, `git apply` never three-way merges
   against the now-absorbed content, so it cannot write conflict markers.

Restoring is owned by `restoreStashedState` (in `restore.go`), which keeps the
cleanup logic testable in isolation from the `Action` orchestration.

Important details:

- Stash refs are resolved by marker at cleanup time to avoid `stash@{n}` drift.
  If the marker cannot be found, absorb **warns** rather than blindly popping
  `stash@{0}`, which could be an unrelated user stash.
- Non-binary unabsorbable hunks are restored to both the index (`git apply
  --cached` via `StageHunks`, which also writes new files to disk) and the
  working tree (`git apply`, working-tree only, per file). The per-file working
  tree apply means an unappliable file (e.g. overlapping unstaged edits) warns
  and does not block the rest; the content still lives in the index and the
  kept staged stash.
- Binary unabsorbable hunks are restored from the staged stash by whole-file
  path checkout (worktree + index).
- The staged safety stash is dropped only after **all** unabsorbable-hunk
  restores succeed. If any restore falls short (binary, index, or working tree),
  absorb keeps the staged stash and warns instead of silently dropping data.

Helpers:

- `internal/actions/absorb/stash.go`
- `internal/actions/absorb/restore.go`

## Restack Modes

After rewriting, absorb can restack different branch sets:

- `all`: restack descendants from oldest modified branch.
- `current`: restack current branch descendants.
- `scope`: restack within current scope only (falls back to `current` when scope is empty).
- `none`: skip restack.

Mode values are case-insensitive. For any mode narrower than `all`, absorb
warns when descendants of the rewritten commits are left un-restacked
("Skipped restacking N branches...") and points at `stackit restack --upstack`
to finish the job.

The follow-up restack runs in continue-on-conflict mode, never the interactive
conflict workflow: a conflicted stack is held back and absorb finishes on a
clean worktree, pointing at `stackit restack` to resolve. This is deliberate —
the absorbed hunks are already committed by that point, and ending mid-rebase
would make the deferred stash restore pop stashes onto a conflicted worktree
(and its failure path would re-stage already-absorbed hunks, duplicating
them).

Implementation: `internal/actions/absorb/restack.go`

## Conflict and Recovery

When absorb conflicts or is interrupted:

- `stackit absorb --show-conflict` provides context.
- `stackit abort` recovers absorb state.

Absorb-specific abort behavior:

- detects absorb stashes by marker,
- pops all absorb-related stash entries (staged + unstaged markers),
- leaves unrelated stash entries untouched.

Implementation: `internal/actions/absorb/conflict.go`

## JSON Output

`--json` emits a plan/summary with:

- `current_branch`
- `absorbed[]` (file, lines, target branch/commit, content)
- `unabsorbable[]` (file, lines, reason, content)
- `new_files[]`
- `stack[]`

Implementation: `internal/actions/absorb/plan.go`

## Tests

Primary absorb tests: `internal/actions/absorb/absorb_test.go`

Notable coverage includes:

- scope boundary targeting,
- three-way/intervening commit behavior,
- binary unabsorbable restoration,
- stash fallback behavior when `stash push --staged` returns non-zero,
- abort restoring multiple absorb stashes while preserving unrelated stashes.
