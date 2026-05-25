# Cross-Cutting Performance Wins

This document collects the wins that appear across multiple per-command analyses. Each entry has a high impact-to-effort ratio because it benefits many commands at once.

**How to read this:** the items are ordered by **how many of the analyzed commands benefit**, not by per-command magnitude. The "Affects" column lists which command pages discuss the same fix.

---

## Tier 1: Touch one place, every command gets faster

### 1. Lite bootstrap path — skip full `rebuildInternal` for read-only / single-branch commands

**Files:** `internal/app/context.go:306`, `internal/engine/engine_internal.go:13`

Today every command pays the cost of `engine.NewEngine` → `rebuildInternal(false)` which lists every branch and reads every branch's `refs/stackit/metadata/*` ref. For most commands the data is wasted:

- `parent`, `children`, `trunk` (default), `info`, `scope --show`, `describe --show` — only need the current branch and at most its parent/scope chain.
- `co <exact-branch>`, `down`, `parent`, `up`, `top`, `bottom`, `main` — only need to validate the target branch exists.
- `track --parent` — only needs the parent + child branch.

**Proposal:** introduce an `engine.LoadMode` ({`Full`, `LazyByBranch`, `BranchListOnly`}) on `NewEngine`. In Lazy mode, `state.branchState` is populated on first access per branch; in `BranchListOnly` mode, only `GetAllBranchNames` runs. The `app.Context` factory picks the mode based on the command (or commands opt in via a `LoadMode` field on a request struct).

This is the single biggest user-perceived win across the CLI. Especially for repos with hundreds of branches.

**Affects:** co.md #2, navigation.md #1, info.md (indirect), scope.md #4, describe.md #4 — basically every page.

---

### 2. Stop calling `worktree.Status()` before every checkout / branch create

**Files:** `internal/git/branches.go:45` (`CheckoutBranch`), `:91` (`CreateAndCheckoutBranch`)

Both functions call go-git's `worktree.Status()` purely to decide the `Keep: !status.IsClean()` argument to `Checkout`. `Status()` walks the entire working tree — easily the slowest single operation in `co` on a large repo.

**Proposal:** shell out to `git checkout <branch>` and let git handle dirty-tree behavior natively. This should be treated as a narrow exception to the default preference for go-git: use go-git where it is correct and fast enough, but prefer native Git for checkout because go-git's checkout/status path walks the whole working tree and is materially slower on large repos.

**Affects:** co.md #1, create.md #1, modify.md #4, absorb.md #1, navigation.md #2, and every other command that ends in a branch switch.

---

### 3. `IsBranchUpToDate` / `IsUpToDate` should read in-memory state, not re-read metadata

**Files:** `internal/engine/engine_branch_status.go:148`

The current implementation re-reads metadata + does a fresh `git.GetRevision` per call, even though `rebuildInternal` already populated `state.branchState` with everything needed.

**Proposal:** read the stored `ParentBranchRevision` from `state.branchState`, batch the parent SHA lookups (one `git for-each-ref` over the unique parent set), compare in-memory. Make per-branch lookups O(1) cache hits after bootstrap.

**Affects:** co.md #3, log.md (indirectly through annotation builds), info.md #1, submit.md #1. Anywhere a stack is iterated checking up-to-date-ness.

---

### 4. Single batched git call for stats / diffs / revisions, never per-branch

**Files:** `internal/engine/engine_branch_info.go:60` (`GetDiffStats`), `:159` (`GetAllCommits`), `internal/git/commit_info.go:150` (`LoadAllBranchRevisions`)

Per-branch git invocations are the most pervasive N+1 pattern in the codebase. `log` and `submit` already preload revisions via `LoadAllBranchRevisions`. The same pattern needs to extend to:

- **diff stats**: currently one `git diff --numstat` per branch in `log`. A single git invocation per stack (or per `(base, head)` pair) cached for the session.
- **commit ranges**: `GetAllCommits` does a per-branch go-git walk; could do one `git rev-list --boundary` and partition by parent metadata.
- **diff content** (for `--diff` / `--patch`): only on demand, but should also be cached.

**Proposal:** introduce `eng.PreloadStackStats(branches)` that does one combined git pass and populates a per-branch cache. `log` calls it once; `info`, `submit`, etc., consult the same cache when set.

**Affects:** log.md #1, info.md #3, absorb.md #5, modify.md #7.

---

### 5. Snapshot taking should batch revisions and be opt-out

**Files:** `internal/engine/undo.go:108` (`TakeSnapshot`)

Every mutating command (`create`, `modify`, `absorb`, `restack`) calls `TakeBestEffortSnapshot`, which iterates `state.branches` and calls `branch.GetRevision()` per branch. Two issues:

- Per-branch loop instead of `BatchGetRevisions` (already exists).
- The snapshot is only ever read by `stackit undo`; for users who never undo, it's pure overhead.

**Proposal:**
- Use `BatchGetRevisions` (or read from `revisionCache` after `LoadAllBranchRevisions`).
- Add `undo.enabled` config flag (default `true`); when `false`, `TakeBestEffortSnapshot` is a no-op.
- Move snapshot capture to *just before mutation*, not at the start of action — many actions short-circuit (e.g. `restack` when `!plan.HasWork()`) without ever needing one.

**Affects:** create.md #2, restack.md #5 + #6, absorb (snapshot capture).

---

## Tier 2: Reusable plumbing for the expensive operations

### 6. Coalesce `worktree.Status()` across the action's request

**Files:** `internal/git/staging.go:102` (`HasStagedChanges`), `:115` (`HasUnstagedChanges`), `:128` (`HasUntrackedFiles`), all reused by multiple actions

In `create`, `worktree.Status()` runs up to 3 times in a single command: once in `HasStagedChanges`, once in `HasUnstagedChanges` (if prompted), once in `CreateAndCheckoutBranch`. `absorb` does it twice (`absorb.go:63` and `:78`). `modify` does it after staging.

**Proposal:** an action-scoped `RepoStatus` value (`{HasStaged, HasUnstaged, HasUntracked, modified files}`) computed once per request and cached on `app.Context`. Pass it through to git layer where needed. Fix #2 (skipping the pre-checkout Status) further reduces calls — combine the fixes.

**Affects:** create.md #1, modify.md #5, absorb.md #3.

---

### 7. Conflict-impossible validation: skip per-spec worktrees when safe

**Files:** `internal/engine/rebase_validator.go:72` (`ValidateRebases`)

The single biggest cost on `modify` (mid-stack), `restack`, and `--restack`-mode `submit` is creating a worktree per descendant spec to dry-run the rebase.

**Proposal:** before scheduling worktree validation, do a cheap file-overlap check per spec:
- `git diff --name-only <old-parent>..<new-parent>` (the change being rebased *onto*)
- `git diff --name-only <old-parent>..<branch>` (the change being rebased)
- If the two file sets are disjoint, no conflict is possible — apply directly without a worktree.

A typical post-amend restack touches one file in the parent; descendants usually touch other files. This collapses N worktrees → N cheap diffs (~ms each).

Risk: rare false negatives (file-level granularity might miss hunk-level true non-conflicts that git would have rebased cleanly anyway — that's fine, fall back to worktree). False positives only happen if the diff sets *do* overlap; in that case we fall back to today's behavior. Safe.

**Affects:** modify.md #1, restack.md #1, absorb.md #2, submit.md (#3 indirectly).

---

### 8. Reuse a single worktree per depth level for validation

**Files:** `internal/engine/rebase_validator.go:184` (`ValidateRebasesParallel`)

If #7 isn't enough (stack genuinely has overlapping changes), validation could still amortize worktree creation across siblings at the same depth: one worktree per concurrency lane, reset between specs via `git rebase --abort` + `git reset --hard`. Cost goes from O(specs) worktrees to O(levels × concurrency).

**Affects:** modify.md #2, restack.md #3, absorb.md #2.

---

### 9. Scoped engine rebuild — `RebuildBranches([]string)`

**Files:** `internal/engine/engine_internal.go:47` (`rebuild`), `internal/engine/engine_writer.go:148` (`UntrackBranch`)

`engine.rebuild()` re-reads metadata for every branch in the repo even when only a handful changed. The worst offender is `UntrackBranch`, which calls `rebuild()` per branch in the loop — `untrack` of a 10-branch substack does 11 full rebuilds.

**Proposal:** a `RebuildBranches([]string)` engine method that re-reads only the listed branches' metadata + revisions, then merges into `state.branchState`. Use from `untrack` (batched), `absorb` post-apply, `modify` post-commit, etc.

**Affects:** absorb.md #4, track-untrack.md #1+#2.

---

### 10. Combine commit + metadata writes into single transactions

**Files:** `internal/engine/transaction.go` (`MetadataTx`), all callers in `engine_writer.go`

Commands that mutate both the parent ref and an adjacent metadata field (scope, lock, PR-update flag, description) issue two separate transactions today. Each transaction is a ref-update round trip. Combining them is straightforward via `withMetadataTx` once the engine exposes the right helpers.

**Affects:** create.md #4 (parent + scope), scope.md #1, describe.md #2.

---

## Tier 3: Smaller universal hygiene

### 11. Move `IsInManagedWorktree` out of the global `common.Run`

**File:** `internal/cli/common/common.go:42`

Runs unconditionally for every command. Only needed for `co`, `worktree open`, `worktree attach`, and similar.

**Affects:** co.md #5, navigation.md (indirect), scope.md #3.

---

### 12. Stop calling `ReloadRepository` after every commit

**File:** `internal/git/commit.go:57`, `:65`, `:82`

Closes the entire go-git repo handle to pick up one new commit. Cheaper: invalidate just the affected branch in `revisionCache` and let go-git re-read the new packed ref lazily.

**Affects:** create.md #3, modify.md #5, absorb.md (post-apply), anywhere a commit is created.

---

### 13. Replace per-call `config.LoadConfig` with `ctx.Config`

**Files:** `internal/cli/navigation/trunk.go:71` + `:140`, others

Bootstrap already loads config into `ctx.Config`. Several commands re-load it. Trivial to fix; affects perf only marginally but is a clean-up alongside the bigger items.

**Affects:** navigation.md #4.

---

## Recommended attack order

For maximum impact per engineering hour:

1. **#1 (lite bootstrap)** — single biggest perceived speedup. Touches `app.Context` + engine. Affects every command. Multi-day, but every workday afterward saves time across the team.
2. **#2 (kill pre-checkout Status)** — half-day change. Immediate huge win on large working trees for `co` / `create` / `modify` / `absorb` / navigation.
3. **#7 (skip validation when conflict-impossible)** — biggest single win for `modify`, the second most-run hot-path mutating command.
4. **#3 (`IsUpToDate` uses cached state)** — pre-req for cleaning up `log`, `submit`, `info` per-branch loops. Small standalone.
5. **#4 (preload stack stats)** — turns `log` from O(N git processes) into O(1) on the diff-stats axis.
6. **#9 (`RebuildBranches`)** — fixes the `untrack` N+1 quickly and unlocks better behavior in `absorb` / `modify`.
7. The remaining items are mostly hygiene that compound once the above are in.

## Order-of-magnitude estimates

These are back-of-envelope guesses to size effort vs reward, not measured:

| Win | Affected commands | Typical saving per affected run |
|---|---|---|
| #1 lite bootstrap | every command | bootstrap cost (50–500ms depending on branch count) |
| #2 kill pre-checkout Status | co, create, modify, absorb, navigation | working-tree Status walk (100ms–1s on large repos) |
| #7 skip safe validation | modify, restack, absorb | N × ~300ms worktree creation |
| #3 cached IsUpToDate | log, submit, info | N × metadata read (~5ms each) |
| #4 preload stack stats | log, info | N × ~5ms `git diff --numstat` processes |
| #9 RebuildBranches | untrack, absorb, modify, sync | K × full rebuild on multi-branch operations |
| #5 snapshot batching | every mutating command | 5–15ms |
| #6 coalesce Status | create, modify, absorb | 2 × Status walks |
| #10 combine txs | create, scope, describe | 1 ref write per command |
| #11 gate IsInManagedWorktree | every command | tiny but free |
| #12 scoped reload | create, modify, absorb | 1 go-git reopen |

## What not to touch

- **`git commit` itself and pre-commit hooks.** The user owns hook cost.
- **GitHub API latency.** Batching is already in place; further wins are caching with TTL (`log.md` #4, `submit.md` #2) which is bounded by correctness.
- **go-git's internals.** Checkout is the narrow native-Git exception in item #2 because the go-git checkout/status path is materially slower on large working trees. Keep the broader default as go-git where it is correct and fast enough; don't rewrite go-git internals.
