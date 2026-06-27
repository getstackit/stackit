# Multiplayer (Collaboration)

How stackit behaves when more than one person works on the same repository, and
specifically when some collaborators **do not use stackit at all**. This is the
"multiplayer" surface: detecting other people's landed work, reparenting your
stack past it, and never building your stack on commits that only exist on your
machine.

For the lower-level merge-method guarantees this builds on, see
[safety-invariants](../.claude/rules/safety-invariants.md) ("GitHub Merge
Methods Are First-Class") and the companion rule
[multiplayer-safety](../.claude/rules/multiplayer-safety.md).

## The two kinds of collaborator

Stackit has to be correct for both, and the difference is **how much metadata
exists in your local view** of the other person's branch.

| | Stackit user | Non-stackit collaborator |
|---|---|---|
| Branch metadata (`refs/stackit/metadata/*`) | Yes — recorded PR number, base, and state | None, or at most a local parent pointer you created |
| When their PR merges | Their PR state becomes `MERGED` in metadata | Nothing changes in metadata; only Git history reflects it |
| How you learn it landed | PR metadata (authoritative) **or** Git history | **Git history only** |

The hard case is the non-stackit collaborator: a teammate pushes a plain GitHub
branch, you stack your own work on top of it, and they squash- or rebase-merge
it through the GitHub UI. Their merged work carries **zero stackit metadata**.
Detection must fall back entirely to Git, with no PR number to lean on.

This is the data-loss shape from issue #1345: if detection misses their merge,
restack replays their pre-merge commits into *your* branch.

## Landed-work detection

"Did this branch's changes land on the target?" is answered by
`engineImpl.branchLanded` (`internal/engine/engine_internal.go`), which combines
metadata and Git signals. It deliberately does **not** require PR metadata for
the trunk case, so the non-stackit collaborator works.

Signals, in order:

1. **Recorded PR state is `MERGED`** (`prStateIsMerged`) — authoritative across
   all three GitHub merge methods. Only the stackit user has this.
2. **Cheap Git merge check** (`git.IsMerged`) — merge-base equality, then
   `git cherry` per-commit patch matching, then ancestry fallback. Covers merge
   commits, rebase merges, and **single-commit** squash merges.
3. **Aggregate patch-id scan** (`git.IsSquashMerged`) — the only signal that
   catches a **multi-commit** squash with no PR metadata. Expensive, so it is
   reached only as a fallback for explicit "did this land on trunk" decisions
   (see Performance below).

The Git fallback (steps 2–3) is **limited to trunk** as the target. For a
non-trunk parent, patch-equivalence can also mean a local stack operation made a
child empty, not that a GitHub PR landed — so for stacked parents stackit relies
on `MERGED` PR metadata instead.

### Why one Git predicate is never enough

GitHub's three merge methods produce different histories for the same PR:

| Method | Lands on base as | PR tip reachable from base? | Detected by |
|--------|------------------|------------------------------|-------------|
| Merge commit | A merge commit (parents include base + PR head) | Yes | ancestry / `IsMerged` |
| Rebase merge | PR commits replayed with new SHAs, same patch-ids | No | `git cherry` / `IsMerged` |
| Squash (single commit) | One combined commit | No | `git cherry` matches the lone commit / `IsMerged` |
| Squash (multi commit) | One combined commit | No | aggregate patch-id only (`IsSquashMerged`) or `MERGED` metadata |

`git cherry` can patch-match a *single*-commit squash, which is why
single-commit squash tests prove nothing about multi-commit support. The
multi-commit squash case is the real safety coverage.

### Detection matrix (who detects what)

For a parent whose changes landed on trunk, restacking the child past it:

| Merge method | Commits | Stackit user (MERGED metadata) | Non-stackit (zero metadata) |
|--------------|---------|--------------------------------|-----------------------------|
| Merge commit | single | metadata or ancestry | ancestry (`IsMerged`) |
| Merge commit | multi | metadata or ancestry | ancestry (`IsMerged`) |
| Rebase | single | metadata or cherry | cherry (`IsMerged`) |
| Rebase | multi | metadata or cherry | cherry (`IsMerged`) |
| Squash | single | metadata or cherry | cherry (`IsMerged`) |
| Squash | multi | metadata | **aggregate patch-id (`IsSquashMerged`)** |

The bolded cell is the one that has no cheap signal and no metadata — it is why
`IsSquashMerged` exists.

## The reparent invariant

When restack (or sync) sees a **merged parent**, it reparents descendants past
the landed branch **without replaying that parent's commits**. The user-visible
guarantee:

> After restack, your branch contains **only your own commits** — never the
> landed parent's pre-merge commits.

Tested as `main..mine == 1` (your single commit) across the full detection
matrix. If detection misses the merge, the count is higher because the parent's
commits replayed into your branch.

Stackit must also never move an unrelated branch ref to a merged **sibling's**
stale pre-merge tip, and must never delete or reparent metadata based solely on
SHA equality.

## The un-pushed trunk guard

Restack rebases trunk-anchored branches onto the **local trunk tip**. In a
multiplayer repo that is only safe when local trunk matches the remote: you
should only build your stack on commits that exist on `origin/<trunk>`, because
those are the commits everyone else will also build on.

If local trunk is **ahead of** or **diverged from** `origin/<trunk>` — e.g. you
committed directly on `main` and never pushed — restacking would bake those
un-pushed commits into your stack. That work exists only on your machine and may
never land as-is. `stackit restack` refuses in that state:

```
local trunk "main" has commits that are not on "origin/main"; restack would
build the stack on those un-pushed commits.
Reconcile trunk with the remote first (run `stackit sync`, or push trunk) so
restack only builds on origin/main
```

### How the guard decides

`engine.TrunkRemoteState` (`internal/engine/engine_branch_status.go`) resolves
the state **locally, with no network**:

- Reads `refs/remotes/<remote>/<trunk>` via `GetRemoteSha` (a local `rev-parse`).
- Compares local trunk to that ref with `IsAncestor(localSha, remoteSha)`.
- Local trunk is **ahead or diverged** when it is **not** an ancestor of the
  remote-tracking trunk. Equal and behind both report `ancestor=true` (a commit
  is its own ancestor), so neither trips the guard.

`guardUnpushedTrunk` (`internal/actions/restack.go`) consumes this inside
`PlanRestack` and rejects before any rebase runs.

| Local trunk vs `origin/<trunk>` | `AheadOrDiverged` | Restack |
|---------------------------------|-------------------|---------|
| Equal (synced) | false | allowed |
| Behind (remote has more) | false | allowed — builds only on remote commits |
| Ahead (un-pushed local commits) | true | **rejected** |
| Diverged (each has unique commits) | true | **rejected** |
| No remote-tracking ref (local-only/fresh clone) | false (`HasRemoteRef=false`) | allowed — nothing to guard against |

Scope notes:

- The guard only fires when the restack actually has branches to move (an empty
  restack is never rejected).
- It is wired into `PlanRestack` (the `restack` command path). Operations that
  go through `RestackBranches` directly — `sync`, `modify`, `squash`, `reorder`
  — are unaffected; `sync` is in fact the recommended way to reconcile trunk.
- Local-only repos (no remote) are never guarded.

## Performance: keep the squash scan cold

`IsSquashMerged` runs a bounded log (`-200`) plus a patch-id per scanned target
commit. It is intentionally **not** part of the hot `IsMerged` primitive and is
not called in broad merge-detection loops — only from explicit "did this branch
land into trunk" decisions, after the cheap `IsMerged` has already returned
false. Keep it that way: folding the aggregate scan into `IsMerged` would put an
expensive per-commit patch-id walk on every merge check.

## Test coverage

| Area | Test |
|------|------|
| Detection matrix (all methods × single/multi × stackit/non-stackit) | `internal/integration/restack_synced_trunk_matrix_test.go` |
| Non-stackit collaborator squash/rebase, including fully untracked parent | `internal/integration/restack_untracked_collaborator_test.go` |
| Multi-commit squash with stale/absent PR state, conflict-abort, reflog | `internal/integration/restack_squash_merge_test.go`, `internal/integration/restack_squash_conflict_abort_test.go` |
| Un-pushed/diverged trunk guard (integration) | `internal/integration/restack_trunk_guard_test.go` |
| `TrunkRemoteState` predicate (engine unit) | `internal/engine/engine_impl_test.go` (`TestTrunkRemoteState`) |

When changing any of detection, reparenting, or the guard, cover **all three
merge methods** and **both user types**, and include a **multi-commit** squash
case — a single-commit squash proves nothing because `git cherry` already
matches it.

## Key code locations

| Concern | Location |
|---------|----------|
| Landed-work detection | `internal/engine/engine_internal.go` (`branchLanded`, `prStateIsMerged`) |
| Cheap merge check | `internal/git/merge_detection.go` (`IsMerged`) |
| Aggregate squash scan | `internal/git/merge_detection.go` (`IsSquashMerged` / `isSquashMerged`) |
| Reparent target selection | `internal/engine/engine_internal.go` (`shouldReparentBranch`, `findNearestValidAncestor`) |
| Trunk-remote state | `internal/engine/engine_branch_status.go` (`TrunkRemoteState`) |
| Restack guard | `internal/actions/restack.go` (`guardUnpushedTrunk`) |
