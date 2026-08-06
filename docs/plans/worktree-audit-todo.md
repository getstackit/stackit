# Worktree Audit — Open Items

Status after the verification pass of 2026-08-05. The fix stacks that landed on main
(reconciliation safety, the 18-branch checkout-safety stack, and the follow-ups through
`8f32c6a2`) closed the entire previous P0 list and nearly all of P1/P2: fold's
detached-HEAD loss, `create -w` branch deletion, absorb's unguarded rewrite, the rebase
ref CAS, `merge --force` trunk checkout, multi-stack leaks/locks, autoClean orphaning,
remove ordering, the refusal triangle, orphaned path-ref sweep, prune dry-run, batch
abort, delete/pluck guards, tree JSON anchors, split stash refs, gitdir resolution,
locked-worktree force removal, relative `basePath`, the anchor-base 422 loop, and the
`PullTrunk` swallow. Verified per commit; regressions and residuals below are what's
left. Line numbers as of the verification; re-check before fixing.

## High

- [ ] **A dirty trunk worktree silently holds every restack, engine-wide.**
  `restackBranches` puts *any* dirty worktree's branch into `heldBranches` with no trunk
  exclusion (`internal/engine/engine_sync.go:133-143`), and `branchHeldBack`
  (`internal/engine/restack_impl.go:68-103`) checks membership *before* its trunk
  termination — so a main worktree parked on `main` with one untracked scratch file
  propagates "held" to every trunk-rooted branch. `modify`, `sync --restack`, absorb's
  follow-up restack etc. all silently return `RestackUnneeded` for everything (no held
  reporting outside the `restack` command). Holding is wrong here: restack never moves
  trunk's ref. Empirically confirmed with a probe test. Fix: exclude trunk when building
  `heldBranches`, or terminate the ancestry walk on trunk before the `Contains` check.

- [ ] **`continue` after a conflicted restack of a branch checked out in a sibling
  worktree corrupts the sibling and strands the remaining restacks.**
  Despite `d06d105a`'s title, `ContinueRebase`
  (`internal/engine/engine_sync.go:265-313`) still has no
  `PathForBranch`/`ResetWorktreeWorkingDir` after its CAS ref move — the original
  finding, unimplemented; the `3cbbb4b0` test covers the *parent* in a sibling worktree,
  not the rebased branch itself. A clean sibling checkout keeps pre-rebase content under
  the moved ref (next `modify -a` there commits the revert). Compounding fallout from
  `569b6981`: `internal/actions/continue.go:101`'s `CheckoutBranch` now hard-fails for a
  foreign-checkout branch, abandoning `BranchesToRestack`, leaving continuation state
  behind, and leaving the user detached. Fix: reset the branch's worktree after the CAS
  move (mirror `restackBranch`), and skip/soften the final checkout when the branch
  lives elsewhere.

## Medium

- [ ] **`create -w` runs post-create hooks in the main repo when worktree creation fails.**
  Introduced by `076e6316` (the branch-preservation fix): on `CreateAnchoredWorktreeForBranch`
  failure, execution falls through to `RunPostCreateHooks(ctx, worktreePath)` with
  `worktreePath == ""` (`internal/actions/create/create.go:322-325`) → hooks (e.g.
  `pnpm install`) execute in the process CWD on trunk. Move the hooks call inside the
  success branch.

- [ ] **Metadata CAS gaps (in-flight commit `b7c00f2d` — fix before submit).**
  All in `internal/git/metadata.go`: (a) `WriteLocalMetadata` (~:419) lacks the
  `expected == sha` early return `WriteMetadata` has — a no-op local save from a stale
  reader silently reverts a concurrent write, the exact bug the commit targets; (b) CAS
  failure doesn't invalidate the in-process cache entry, so cache-first re-reads return
  superseded meta and retries fail forever in a long-lived engine (server); (c)
  `ReadLocalMetadata`'s not-found path (~:393) doesn't clear a stale cached SHA, so
  writes CAS against a deleted ref persistently fail in-process. Also note: batch-read →
  write flows carry no expectation (safe fallback, last-writer-wins) — document or
  extend later.

- [ ] **Mid-rebase worktree holds only cover the `restack` command.**
  `bca90c07` wired `WorktreeRebaseInProgress` into `skipDirtyWorktreeStacks`
  (`internal/actions/restack.go:506,534`) but the engine snapshot still keys
  `heldBranches` on `worktree.Branch != ""` (`internal/engine/engine_sync.go:133-143`) —
  a mid-rebase worktree reports `Branch == ""`, so modify/sync/squash/absorb-driven
  restacks can move the ref of a branch being rebased elsewhere (the user's later
  `continue` CAS-fails; resolved rebase stranded detached, reflog recovery). Sync's
  `SkipReasonForWorktree` (`internal/actions/sync/worktree_cleanup.go:29-43`) also
  doesn't check rebase-in-progress. Add rebase detection to the engine snapshot (hold
  the worktree's branch when determinable from the rebase state, or hold the whole
  anchor stack) and to sync's gate.

- [ ] **Sync never reports engine-level holds.**
  `3ec80337` fixed reporting for the `restack` command only. Branch-level engine holds
  surface as bare `RestackUnneeded` → `EventCompleted` in
  `internal/actions/sync/restack.go:100-110`, indistinguishable from up-to-date.
  `RestackBranchResult` carries no "held" marker, so no consumer can distinguish. Add a
  held marker + reason to the result and report it in sync (this also serves the
  dirty-trunk item above once trunk is excluded).

- [ ] **Absorb aborts mid-loop when a later target is held, then restores duplicated hunks.**
  When target k of n is held (`internal/actions/absorb/absorb.go:385-387`), targets
  1..k−1 keep their rewritten commits and the failure restore pops the *full* staged
  stash — re-staging hunks already committed (duplication on next absorb/restack). The
  recovery path is pre-existing, but the hold made it a common entry. Cheap fix:
  pre-check all target branches' worktrees before rewriting anything (also removes the
  per-target `ListWorktrees` N+1).

- [ ] **Multi-stack failure unlocks are local-only.**
  The consolidation lock is pushed to remote metadata + PR bodies at lock time
  (`internal/actions/merge/multistack.go:350`), but the failure/excluded-stack unlocks
  only run `eng.SetLocked` locally (`multistack.go:241-256`). Until the user's next
  sync, teammates see phantom `LockReasonConsolidating` locks and stale PR-body notices.
  Fail-safe direction, but violates the lock-immediacy rationale in reverse — push the
  unlock like `lock`/`unlock` do.

- [ ] **Split's stash pop still races: positional ref resolved at push time, popped later.**
  `60907466` made pops ref-qualified, but `splitStashRef`
  (`internal/actions/split/stash.go:17-29`) resolves `stash@{N}` once right after push;
  indices are shared across worktrees and shift on any stash traffic, so a concurrent
  stash during the split still pops the wrong entry. Re-resolve the marker immediately
  before each pop. Also: when `splitStashRef` errors, the pushed stash is left behind
  without mention.

## Low

- [ ] **Orphaned path-ref sweep can delete a live ref registered concurrently.**
  `internal/engine/engine_worktree.go:99-118` lists forward refs before path refs and
  deletes via unguarded `DeleteRefsBatch`; a `worktree create` committing its atomic
  pair between the two lists gets its fresh path ref classified as orphaned. Manual
  `repair` only, so low. Fix: list path refs first, and/or delete with OldSHA guards.
- [ ] **Sync-time divergence report nags unmanaged-worktree users.**
  In-flight `0eb35b3e` surfaces `OwnershipWarnings` on every sync, including "checked
  out in unmanaged worktree Y" rows that no guard acts on — a recurring warning for a
  legitimate workflow, with no dedup. Consider restricting the sync-time report to
  managed-owner mismatches.
- [ ] **`resetWorktreeIfClean` snapshot TOCTOU.** Uncommitted edits made in a sibling
  worktree after the pass's up-front dirty snapshot but before that branch's reset are
  destroyed (commits are now CAS-safe; working-tree edits in the window are not).
  Narrow; hard to close under the snapshot design — consider a re-probe just before
  reset.
- [ ] **`ResetTrunkToRemote` has zero test coverage** despite the rewrite
  (`internal/engine/pull.go:92-165`). Add main-checkout-clean / main-checkout-dirty /
  detached-engine cases.
- [ ] **Double ref-write in `restackBranch`.** `git.Rebase` CAS-moves the ref, then
  `restackBranch` writes it again (no-op) in `UpdateRefsBatchWithLog`
  (`internal/engine/restack_impl.go:565-570`), and `resetWorktreeIfClean` at :548 is
  correct only because the ref already moved inside `Rebase`. Consolidate or comment.
- [ ] **`foreach` capability change from `569b6981`.** Branches checked out in other
  worktrees previously ran detached; now they error per-branch. Honest but a
  regression for read-only foreach use — consider an explicit detached mode.
- [ ] **Cosmetics/info:** `sync/branch_cleaning.go:154` "checked out here" wording is
  wrong from a linked worktree; `worktreePathUsable` stranded `getWorktreeSwitchInfo`'s
  doc comment (`internal/actions/checkout.go:190-196`); `fc702272`'s message doesn't
  match its diff (content landed in `e6404bda`); registering over an orphaned path ref
  says "failed to register worktree" without pointing at `worktree repair`; `create -w`
  from *inside* a managed worktree resolves a relative basePath against the worktree
  root rather than the main repo; warm-start treats any mkdir errno as a per-file skip
  and repeats warnings per file under a broken prefix; `foreach.go:511` passes
  `appCtx.Context` where siblings use the local `ctx`.

## Suggested next steps

1. Fix the two Highs together (both in `engine_sync.go` / continue path): trunk
   exclusion for `heldBranches`, sibling reset in `ContinueRebase`, soften
   `continue.go`'s final checkout.
2. Patch the in-flight stack before submit: metadata CAS gaps (`b7c00f2d`), hooks
   fall-through (`076e6316` follow-up fits the same stack), sync divergence noise.
3. Fold the mid-rebase engine snapshot + sync held-reporting into one "engine hold
   completeness" branch.
4. Long tail as convenient.
