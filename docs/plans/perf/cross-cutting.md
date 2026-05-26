# Cross-Cutting Performance Wins

This document collects the wins that remain open across multiple per-command analyses. Each entry has a high impact-to-effort ratio because it benefits many commands at once.

**How to read this:** the items are ordered by **how many of the analyzed commands benefit**, not by per-command magnitude. The "Affects" column lists which command pages discuss the same fix.

---

## Tier 1: Touch one place, many commands get faster

### 1. Expand lightweight engine load modes to the remaining safe commands

**Files:** `internal/app/context.go`, `internal/engine/engine_impl.go`, command wrappers in `internal/cli`

`engine.LoadModeShared` and `engine.LoadModeBranchesOnly` already exist. Default context creation uses `LoadModeShared`, and exact quiet checkout plus several read-only views now opt into `LoadModeBranchesOnly`.

The remaining win is broader adoption and a true lazy-by-branch path:

- `children`, `info`, and stack graph views still need more metadata than branch-list mode but often not the full repo.
- Non-quiet/fuzzy `co`, `up`, `down`, `top`, `bottom`, and `main` still pay broader bootstrap because they need graph/worktree/checkout context.
- `track --parent` only needs the parent + child branch.

**Proposal:** keep `LoadModeBranchesOnly` for exact branch-list cases, and add/expand lazy metadata loading for graph commands that only need the current stack. Metadata should be promoted per branch or per stack root on first access instead of forcing every repo branch into memory at startup.

**Affects:** co.md, navigation.md, info.md, scope.md, describe.md.

---

### 2. Batch branch status reads at stack-iteration call sites

**Files:** `internal/engine/engine_branch_status.go`, call sites in `submit`, `info`, and stack renderers

`ReadBranchStatuses` exists and `checkout` branch info uses it. The remaining issue is call sites that iterate a stack and call `IsBranchUpToDate`/revision lookups branch-by-branch.

**Proposal:** route stack-wide status checks through `ReadBranchStatuses` or a similar batched parent-revision helper. The engine already keeps metadata in memory after bootstrap; the expensive part to avoid is repeated live parent SHA lookups while walking a stack.

**Affects:** info.md #1, submit.md #1, any stack-wide status renderer.

---

### 3. Single batched git call for stats / diffs / revisions, never per branch

**Files:** `internal/engine/engine_branch_info.go`, `internal/git/commit_info.go`

Per-branch git invocations are the most pervasive N+1 pattern in the codebase. `log` and `submit` already preload revisions via `LoadAllBranchRevisions`. The same pattern needs to extend to:

- **diff stats**: currently one `git diff --numstat` per branch in `log`.
- **commit counts/ranges**: normal/full `log` no longer loads commit messages, but still counts commits per branch.
- **diff content** for `info --diff` / `info --patch`: only on demand, but should be cached once computed.

**Proposal:** introduce `eng.PreloadStackStats(branches)` that does one combined git pass and populates a per-branch cache. `log` calls it once; `info`, `submit`, etc., consult the same cache when set.

**Affects:** log.md #1, info.md #3, absorb.md #4, modify.md #6.

---

### 4. Snapshot taking should batch revisions and be opt-out

**Files:** `internal/engine/undo.go`

Every mutating command (`create`, `modify`, `absorb`, `restack`) calls `TakeBestEffortSnapshot`, which iterates `state.branches` and calls `branch.GetRevision()` per branch. Two issues:

- Per-branch loop instead of `BatchGetRevisions` (already exists).
- The snapshot is only ever read by `stackit undo`; for users who never undo, it is pure overhead.

**Proposal:**

- Use `BatchGetRevisions` or read from `revisionCache` after `LoadAllBranchRevisions`.
- Add `undo.enabled` config flag (default `true`); when `false`, `TakeBestEffortSnapshot` is a no-op.
- Move snapshot capture to just before mutation, not at the start of an action. Many actions short-circuit without ever needing a snapshot.

**Affects:** create.md #2, restack.md #5 + #6, absorb.md.

---

## Tier 2: Reusable plumbing for expensive operations

### 5. Coalesce `worktree.Status()` across the action's request

**Files:** `internal/git/staging.go`, all reused by multiple actions

Checkout no longer calls go-git's `worktree.Status()`, but staging helpers still can run the same full working-tree walk multiple times in one action. `create` can call `HasStagedChanges` and then `HasUnstagedChanges`; `absorb` calls `HasStagedChanges` before and after staging; `modify` checks staged changes after optional staging.

**Proposal:** an action-scoped `RepoStatus` value (`{HasStaged, HasUnstaged, HasUntracked, modified files}`) computed once per request and cached on `app.Context`. Pass it through to staging/validation helpers where needed.

**Affects:** create.md #1, modify.md #3, absorb.md #2.

---

### 6. Conflict-impossible validation: avoid per-spec worktrees when safe

**Files:** `internal/engine/rebase_validator.go`

The single biggest cost on `modify` (mid-stack), `restack`, and `--restack`-mode `submit` is creating a worktree per descendant spec to dry-run the rebase.

**Proposal:** add a safe fast path before scheduling worktree validation. The validator currently produces the rewritten SHAs that later apply steps consume, so this is not just "skip validation"; the fast path must either directly compute/apply the safe replay result or share a cheap replay path that produces equivalent output.

A first classifier can compare:

- `git diff --name-only <old-parent>..<new-parent>` (the change being rebased onto)
- `git diff --name-only <old-parent>..<branch>` (the change being rebased)

If the file sets are disjoint, no content conflict is possible and the direct replay path can avoid the temporary worktree.

**Affects:** modify.md #1, restack.md #1, absorb.md #1, submit.md #3 indirectly.

---

### 7. Reuse a single worktree per depth level for validation

**Files:** `internal/engine/rebase_validator.go`

If #6 is not enough because the stack genuinely has overlapping changes, validation could still amortize worktree creation across siblings at the same depth: one worktree per concurrency lane, reset between specs via `git rebase --abort` + `git reset --hard`.

**Affects:** modify.md #2, restack.md #3, absorb.md #1.

---

### 8. Scoped engine rebuild: `RebuildBranches([]string)`

**Files:** `internal/engine/engine_internal.go`, `internal/engine/engine_writer.go`

`engine.rebuild()` re-reads metadata for every branch in the repo even when only a handful changed. The worst offender is `UntrackBranch`, which calls `rebuild()` per branch in the loop.

**Proposal:** a `RebuildBranches([]string)` engine method that re-reads only the listed branches' metadata + revisions, then merges into `state.branchState`. Use from `untrack` (batched), `absorb` post-apply, `modify` post-commit, etc.

**Affects:** absorb.md #3, track-untrack.md #1 + #2.

---

### 9. Combine commit + metadata writes into single transactions

**Files:** `internal/engine/transaction.go`, callers in `engine_writer.go`

Commands that mutate both the parent ref and an adjacent metadata field (scope, lock, PR-update flag, description) issue two separate transactions today. Each transaction is a ref-update round trip. Combining them is straightforward via `withMetadataTx` once the engine exposes the right helpers.

**Affects:** create.md #4, scope.md #1, describe.md #2.

---

## Tier 3: Smaller universal hygiene

### 10. Continue gating `IsInManagedWorktree` to commands that need it

**File:** `internal/cli/common/common.go`

`SkipManagedWorktreeCheck` exists and several read-only commands use it. Continue moving commands that do not branch on managed-worktree state onto this option.

**Affects:** co.md and navigation.md indirectly.

---

### 11. Stop calling `ReloadRepository` after every commit

**File:** `internal/git/commit.go`

Closes the entire go-git repo handle to pick up one new commit. Cheaper: invalidate just the affected branch in `revisionCache` and let go-git re-read the new packed ref lazily.

**Affects:** create.md #3, modify.md #4, absorb.md post-apply, anywhere a commit is created.

---

### 12. Replace per-call `config.LoadConfig` with `ctx.Config`

**Files:** `internal/cli/navigation/trunk.go`, others

Bootstrap already loads config into `ctx.Config`. Several commands re-load it. Trivial to fix; affects perf only marginally but is a clean-up alongside the bigger items.

**Affects:** navigation.md #4.

---

## Recommended attack order

For maximum impact per engineering hour:

1. **#1 (expand lightweight load modes)** — biggest perceived speedup on read-only/common commands.
2. **#6 (safe validation fast path)** — biggest single win for `modify`, the second most-run hot-path mutating command.
3. **#3 (preload stack stats)** — turns `log` from O(N git processes) into O(1) on the diff-stats axis.
4. **#8 (`RebuildBranches`)** — fixes the `untrack` N+1 and improves `absorb` / `modify` cleanup.
5. **#4 (snapshot batching / opt-out)** — small but hits every mutating command.
6. **#5 (coalesce status checks)** — removes duplicate working-tree scans that still remain outside checkout.
7. The remaining items are mostly hygiene that compound once the above are in.

## Order-of-magnitude estimates

These are back-of-envelope guesses to size effort vs reward, not measured:

| Win | Affected commands | Typical saving per affected run |
|---|---|---|
| #1 expand load modes | read-only/common commands | remaining bootstrap cost (50-500ms depending on branch count) |
| #6 safe validation fast path | modify, restack, absorb | N x ~300ms worktree creation |
| #3 preload stack stats | log, info | N x ~5ms `git diff --numstat` processes |
| #8 RebuildBranches | untrack, absorb, modify, sync | K x full rebuild on multi-branch operations |
| #4 snapshot batching | every mutating command | 5-15ms |
| #5 coalesce Status | create, modify, absorb | duplicate Status walks |
| #9 combine txs | create, scope, describe | 1 ref write per command |
| #11 scoped reload | create, modify, absorb | 1 go-git reopen |

## What not to touch

- **`git commit` itself and pre-commit hooks.** The user owns hook cost.
- **GitHub API latency.** Batching is already in place; further wins are caching with TTL (`log.md`, `submit.md`) which is bounded by correctness.
- **go-git's internals.** Checkout is already a native-Git exception because the go-git checkout/status path was materially slower on large working trees. Keep the broader default as go-git where it is correct and fast enough; do not rewrite go-git internals.
