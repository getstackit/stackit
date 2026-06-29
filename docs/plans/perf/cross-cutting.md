# Cross-Cutting Performance Wins

This document collects the wins that remain open across multiple per-command analyses. Each entry has a high impact-to-effort ratio because it benefits many commands at once.

**How to read this:** the items are ordered by **how many of the analyzed commands benefit**, not by per-command magnitude. The "Affects" column lists which command pages discuss the same fix.

**Status legend:** items that have fully landed since this plan was written are listed under [Completed](#completed-since-last-revision) at the bottom rather than deleted outright, so their numbering doesn't shift the references in the per-command pages.

---

## Tier 1: Touch one place, many commands get faster

### 1. Expand lightweight engine load modes to the remaining read-only commands

> **Status:** Partially done. The infrastructure is complete — three load modes (`LoadModeFull`, `LoadModeShared`, `LoadModeBranchesOnly`) plus **per-branch lazy promotion** (`ensureBranchSharedLoaded`, `internal/engine/engine_impl.go:271`, wired into `GetParent` and the branch-status readers). What remains is purely per-command *adoption*, not new plumbing.

**Files:** command wrappers in `internal/cli` (the engine side is done)

Default context creation uses `LoadModeShared`. Commands already opted into the lighter `LoadModeBranchesOnly` path via `common.ApplyReadOnlyCurrentBranch`: exact quiet `co`, `parent`, default `trunk`.

Remaining adoption:

- `children`, `up`, `top`, `bottom` still use the full `common.Run` bootstrap; `down` only walks the parent chain and is the best remaining candidate for the lighter mode.
- `info` and stack-graph views need more than branch-list mode but rely on the per-branch promotion path rather than a full load — confirm they don't force `LoadModeFull`.
- Non-quiet/fuzzy `co` still pays broader bootstrap because it needs graph/worktree/checkout context.

**Affects:** co.md, navigation.md, info.md.

---

### 2. Batch branch status reads at stack-iteration call sites

> **Status:** Partially done. The batched reader `engine.ReadBranchStatuses` (`internal/engine/engine_branch_status.go:187`) exists and several actions use the `Batch*` readers. The remaining issue is specific call sites that still walk a stack calling per-branch up-to-date/restack checks.

**Files:** call sites in `submit`, `info`

Open call sites:

- `submit` calls per-branch `branch.IsBranchUpToDate()` (`internal/cli/.../submit.go:153`, `submit_validation.go:121,127`), each hitting the uncached `IsUpToDate` with a fresh `git rev-parse` per parent.
- `info`'s stack render loops `branch.NeedsRestack()` per branch (`internal/actions/stack_info.go:148-151`) → `IsUpToDate` → live parent SHA lookup, even though the heavy reads around it are already batched.

**Proposal:** route these stack-wide status checks through `ReadBranchStatuses` (or a batched parent-revision helper) so the live per-parent SHA lookups happen once.

**Affects:** info.md #1, submit.md #1.

---

### 3. Single batched git call for stats / diffs / revisions, never per branch

> **Status:** Partially done. `tree` is fully on the batched path (`BatchDiffStats` / `BatchBranchStats` / `BatchCommits`, `internal/engine/branch_view.go`); `info` batches its stack stats too. Remaining N+1s are on the on-demand diff paths.

**Files:** `internal/engine/engine_branch_info.go`, `internal/actions/absorb.go`, `internal/actions/info.go`

Remaining per-branch git invocations:

- **`info --diff` / `info --patch`**: `GetAllCommits` + `GetParentCommitSHA` per render (`info.go:190-193`, `:226-229`); compute once and reuse.
- **`absorb`**: per-branch `GetAllCommits` loop (`absorb.go:121-130`), plus the batched-scan path also calls `GetAllCommits` per branch — fold both into a single `git rev-list`.

**Proposal:** extend the batched stat/commit readers to these on-demand paths; the revision-keyed memoization already in `engine_branch_info.go` makes it safe.

**Affects:** info.md #3, absorb.md #4.

---

### 4. Snapshot taking: skip when there's no work

> **Status:** Mostly done. `TakeSnapshot` uses `BatchGetRevisions` (`internal/engine/undo.go:121`) — no per-branch revision loop. The `undo.enabled` opt-out is implemented: config key `stackit.undo.enabled` (`internal/config/keys.go:18`), honored by `TakeBestEffortSnapshot` (`internal/actions/common.go`), which no-ops when disabled.

**Files:** `internal/actions/common.go`, `internal/actions/restack.go`, `internal/engine/undo.go`

Remaining:

- **Skip the snapshot on no-op operations.** `RestackAction` (`internal/actions/restack.go:153`) takes a snapshot before discovering there's no work to do; gate it on `plan.HasWork()`. The same "snapshot only just before a real mutation" principle applies to other actions that frequently short-circuit.
- **Lazy `enforceMaxStackDepth`** (`undo.go:172`) still does an `os.ReadDir` + sort on every snapshot.

**Affects:** restack.md, create.md.

---

## Tier 2: Reusable plumbing for expensive operations

### 5. Coalesce staging status probes within an action

> **Status:** Premise obsolete. go-git has been removed; the staging helpers no longer do a full working-tree walk. `HasStagedChanges` / `HasUnstagedChanges` now shell `git diff --cached --quiet` / `git diff --quiet` (`internal/git/staging.go:55,62`). This is a much smaller win than originally described — coalescing redundant subprocesses, not eliminating tree walks.

**Files:** `internal/git/staging.go` and callers

Remaining redundant probes:

- `create`'s non-interactive guard can fire up to three separate status subprocesses (`create.go:125-137`).
- `absorb` calls `HasStagedChanges` twice and now *discards* the first result entirely (`absorb.go:63`, `:78`) — that first call can simply be deleted.
- `modify`'s angle is **closed** — its `HasStagedChanges` is just a `git diff --cached --quiet`, no tree walk.

**Proposal:** an action-scoped `RepoStatus` value (one `git status --porcelain`) computed once per request where multiple probes remain.

**Affects:** create.md #1, absorb.md #2.

---

### 7. Reuse a single worktree per depth level for validation

When #6's fast path can't apply (genuinely overlapping changes, multi-commit), validation still creates one worktree per spec (`rebase_validator.go:367`). It could amortize worktree creation across siblings at the same depth: one worktree per concurrency lane, reset between specs via `git rebase --abort` + `git reset --hard`. Note the tradeoff with the existing per-spec parallelism.

**Affects:** modify.md #2, restack.md #3, absorb.md #1.

---

### 8. Scoped engine rebuild: `RebuildBranches([]string)`

> **Status:** Open (method does not exist), but the motivating N+1 is already fixed. `untrack` no longer rebuilds per branch — `UntrackBranches` (`internal/engine/branch_tracking.go:82-98`) does a single `DeleteRefsBatch` + one `rebuild()`.

**Files:** `internal/engine/engine_internal.go`

`engine.rebuild()` still re-reads metadata for every branch in the repo even when only a handful changed. A `RebuildBranches([]string)` method would re-read only the listed branches' metadata + revisions and merge into `state.branchState`.

Remaining beneficiaries (each still does a full rebuild):

- `untrack`'s single post-batch rebuild (`internal/actions/untrack/action.go`).
- `absorb` post-apply (`absorb.go:297` calls `Rebuild("")`).

**Affects:** absorb.md #3, track-untrack.md #2.

---

### 9. Combine commit + metadata writes into single transactions

> **Status:** Partially done. `scope` is complete — `SetScopeAndMarkForUpdate` (`internal/engine/branch_tracking.go:401`) writes the scope ref and the PR-update flag in one transaction.

**Files:** `internal/engine/transaction.go`, `internal/engine/branch_tracking.go`, `internal/git/stack_metadata.go`

Remaining commands that issue two separate ref-update round trips:

- **`create`**: `TrackBranch`→`SetParent` and `SetScope` are separate transactions; no `SetParentAndScope` helper exists.
- **`describe`**: `SetStackDescription` (stack-meta ref) and `MarkBranchesForPRBodyUpdate` (per-branch local-meta refs) are separate write phases. `WriteStackMetaBlob` (`internal/git/stack_metadata.go:122`) already returns a blob SHA without a ref write, so both can fold into one `UpdateRefsBatch`.

**Affects:** create.md #4, describe.md #1.

---

## Tier 3: Smaller universal hygiene

### 12. Replace per-call `config.LoadConfig` with `ctx.Config`

> **Status:** Partially done. `trunk.go`'s `findTrunkForBranch` now reads `ctx.Config.AllTrunks()`; `hookmiddleware.go` updated.

**Files:** `internal/cli/navigation/trunk.go`, others

Remaining: `handleAddTrunk` (`trunk.go:75`) still calls `config.LoadConfig` because it needs a writable `*GitConfig` (`ctx.Config` is the read-only `Configurer` interface, which lacks `AddTrunk`/`Save`). This is not a trivial swap and may not be worth it. Audit `internal/cli` for any remaining read-only `config.LoadConfig` callers that can use `ctx.Config` directly.

**Affects:** navigation.md.

---

## Completed since last revision

These wins have fully landed. Numbering is preserved so per-command page references stay valid.

- **#10 — Gate `IsInManagedWorktree` to commands that need it.** `SkipManagedWorktreeCheck` exists, is applied to read-only commands via `ApplyReadOnlyCurrentBranch`, and call sites are minimal (`common.go` + tests). Keep applying it to new read-only commands.
- **#11 — Stop calling `ReloadRepository` after every commit.** go-git was removed entirely. There is no repo handle to close and no worktree re-scan; the expensive reopen this targeted no longer exists. The global revision cache it relied on is also gone (and a global revision cache is now an explicit anti-pattern — see `.claude/rules/code-style.md`).
- **#6 — Conflict-impossible validation fast path.** `validateSingleSpec` calls `tryConflictFreeReplay` (`internal/engine/rebase_validator.go`): when the branch's changed files are disjoint from the parent's, it replays the branch onto the new base via `merge-tree --write-tree` + `commit-tree` with **no worktree**. Now covers **multi-commit branches** too — each commit is replayed oldest-first via `replayCommitConflictFree`, chained onto the previous result, preserving per-commit author identity and message. The remaining worktree-amortization idea lives on as #7.

---

## Recommended attack order

For maximum impact per engineering hour:

1. **#1 remaining load-mode adoption** — pure call-site changes (`down`, `bottom`) on top of finished engine infrastructure.
2. **#2 batch stack-wide status checks** — removes the per-parent `git rev-parse` walks in `submit` and `info`.
3. **#8 `RebuildBranches`** — scopes the remaining full rebuilds in `absorb`/`untrack`.
4. **#3 on-demand diff batching** — `info --diff`/`--patch` and `absorb`'s commit scan.
5. **#7 per-level worktree reuse** — amortizes the worktree cost that remains for genuinely overlapping multi-branch validation (the disjoint-file fast path #6 already eliminated the common case).
6. **#9 combine txs** for `create` and `describe`; **#4 no-op snapshot skip**; **#5 / #12** hygiene.

## Order-of-magnitude estimates

Back-of-envelope guesses to size effort vs reward, not measured. Completed items omitted.

| Win | Affected commands | Typical saving per affected run |
|---|---|---|
| #1 remaining load-mode adoption | read-only/common commands | remaining bootstrap cost (50-500ms depending on branch count) |
| #2 batch status checks | submit, info | N x `git rev-parse` per stack walk |
| #3 on-demand diff batching | info, absorb | N x ~5ms `git` processes |
| #8 RebuildBranches | absorb, untrack | full rebuild → scoped read |
| #4 no-op snapshot skip | restack (no-work path) | 5-15ms |
| #9 combine txs | create, describe | 1 ref write per command |

## What not to touch

- **`git commit` itself and pre-commit hooks.** The user owns hook cost.
- **GitHub API latency.** Batching is already in place; further wins are caching with TTL (`tree.md`, `submit.md`) which is bounded by correctness — and, for the CLI's per-process model, would have to be on-disk to persist across invocations.
- **A global/ambient revision cache or `PreloadBranchData`-style warm-up.** Deliberately removed; reintroducing it is an anti-pattern (`.claude/rules/code-style.md`). Batch readers return values you pass around instead.
