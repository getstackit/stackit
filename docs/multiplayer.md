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

## Fetching a branch whose parent has landed

`stackit get` faces the same landed work one step earlier: it fetches a branch
from the remote before there is any local history to reason about. The child's
pushed metadata still names the parent it was submitted against, and GitHub has
already deleted that branch as part of the merge.

Two things go wrong without care.

**The fetch is all-or-nothing.** `get` resolves the target's ancestry from the
fetched metadata and then asks for every ancestor head in one refspec list.
`git fetch` fails the whole command when a single explicit refspec names a ref
that no longer exists, so the branch the user actually asked for never arrives:

```
Error: failed to fetch branches from origin: git fetch failed:
fatal: couldn't find remote ref refs/heads/<merged-parent>
```

`get` recovers by listing the remote's refs (`MissingRemoteBranches`), dropping
the branches that are gone, and fetching the rest. That listing runs **only on
the failure path**, so a healthy stack still fetches in one round trip.

**The substitute parent needs the right divergence point.** A branch discovered
through metadata that has no ref on the remote was pushed once and has landed,
so `get` drops it from the sync set and tracks its children against the nearest
ancestor that still exists — usually trunk. Recording that parent alone is not
enough: the fetched branch still contains the landed parent's commits, and a
plain merge-base against trunk puts them all back into a later restack's replay
range for every merge method that rewrites SHAs.

`TrackBranchPastLandedParent` records the parent the branch was actually pushed
on top of, plus that parent's tip from the same metadata, before setting the new
parent. The tip is still reachable from the fetched branch even though its ref
is gone, so `SetParent`'s recompute path can check whether it landed and keep it
as the divergence anchor. The reparent invariant above then holds through `get`
as well: `<trunk>..<branch>` is exactly the branch's own commits.

A parent that is gone from the remote but **still checked out locally** is left
alone. It is a branch the user may still be working with, and restack's own
landed-parent handling reparents past it.

### Where the anchor comes from, and what happens without one

The anchor is a branch's own recorded `ParentBranchRevision`, so it survives the
landed parent's metadata being deleted — but only if `get` goes looking for it.

`crawlAncestorsViaMetadata` collects anchors as it walks, and abandons the whole
walk the moment one ancestor has no metadata. That is exactly the state a merged
parent is left in: branch cleanup deletes a merged branch's remote metadata ref
during the author's own `stackit sync`, so by the time a teammate runs
`stackit get`, the ancestry crawl gives up and the GitHub crawl takes over. PR
bases carry parents but no revisions, so that path knows no anchors at all.

`harvestAnchors` closes the gap: after whichever crawl ran, it reads
`ParentBranchRevision` out of the remote metadata cache for every branch in the
sync set that does not have one yet. The child's own metadata ref is still on the
remote, and it is the record that matters.

When nothing anywhere records the tip — a branch pushed by a collaborator who
does not use stackit, whose parent then landed and was deleted — there is no way
to tell the landed commits apart from the branch's own. Re-anchoring still
happens, because tracking a branch against a parent that no longer exists is
worse. Rebasing does not: `LandedAncestorReport.Unanchored` names those branches,
`Unfreezable` excludes them, and `get` reports them and leaves them frozen rather
than offering a rebase that would replay the landed work.

### Re-anchoring is reported, never silent

`get` checks branches out **frozen** by default: a frozen branch mirrors the
remote and restack resets it rather than rebasing it. So re-anchoring moves the
parent pointer while the branch keeps carrying the landed commits, and restack
will not fix that on its own.

`get` says so and offers the remedy — unfreeze the affected branches so this run
rebases them onto the new parent. Declining is a valid answer, not a failure:
the branch goes on mirroring the remote, correctly tracked, and `st unfreeze`
plus `st restack` finishes the job later.

The offer is only ever made for branches `get` can actually rebase: frozen (so
restack skips them as things stand) and anchored (so the rebase drops the landed
commits rather than replaying them). It is also only made when the run has a
restack phase to feed — unfreezing under `--no-restack` would drop the protection
and rebase nothing.

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

## The worktree hold

The trunk guard protects the stack from commits your teammates do not have. A
second guard protects *you* from the other direction: uncommitted work sitting in
a worktree that a restack is about to reset.

Restacking a branch moves its ref, and any worktree with that branch checked out
must be reset to match. Suppressing the reset while leaving the ref moved is the
hazard: the worktree keeps the old commit's content under a new ref, `git status`
reports the new commit's files as deleted, and the next `stackit modify -a`
commits that deletion into the stack. So Stackit does not restack the branch at
all. The branch is **held**, and it self-heals on the next restack once the
worktree is clean.

### What holds a branch

| Worktree state | Held? |
|----------------|-------|
| Uncommitted changes to tracked files | Yes — the reset would discard them |
| Rebase in progress | Yes on the `restack` command path; **not yet** in the engine snapshot (see below) |
| Cannot be inspected | Yes — nothing is known, so no reset is safe |
| Untracked files | Only when one occupies a path the incoming commit also writes |
| Clean | No — reset to the new ref |

Rebase-in-progress detection currently lives in `skipDirtyWorktreeStacks`, which
only the `restack` command runs. The engine snapshot keys its hold set on the
worktree's checked-out branch, and a mid-rebase worktree reports no branch — so
`modify`, `squash`, and absorb-driven restacks can still move the ref of a
branch being rebased elsewhere. Known gap.

Untracked files are the case worth being careful about. `git reset --hard`
overwrites an untracked file only when the incoming commit contains that same
path, and leaves every other one alone. Holding on *any* untracked file would
stop a restack for the ordinary state of having written a new file and not
staged it — so the collision is checked per branch, once the incoming tree is
known.

This applies to **every** physical checkout, not just Stackit-managed worktrees.
A plain `git worktree` holding one of your branches is inspected the same way.

### Blast radius

The unit held differs by worktree kind:

- A dirty **managed** worktree holds its whole stack — its anchor is the stack
  root, so the stack is dropped as a unit.
- Any **other** worktree, including the main one, holds a single branch. Its
  descendants are still correctly based on a branch that did not move, so they
  restack as a no-op rather than a hazard.

A held branch does propagate to descendants through the ancestry walk when the
hold sits mid-stack: a branch cannot be rebased onto a parent that did not move
to where it was supposed to.

### Trunk is never held

Restack never moves trunk's ref and never resets trunk's worktree, so a dirty
trunk checkout protects nothing. Trunk is excluded when the hold set is built.

This exclusion matters more than it looks: without it, a main worktree parked on
`main` with one untracked scratch file propagated "held" through the ancestry
walk to every trunk-rooted branch in the repository, and `modify`, `sync
--restack`, and absorb's follow-up restack all silently returned "nothing to do"
for everything.

### Held is not up-to-date

A held branch returns `RestackUnneeded` — the same status as a branch that
needed no work — so anything that could leave a user believing a restack
happened must say otherwise.

The engine carries the reason out on `RestackBranchResult.HeldBy`, phrased for
the user and naming the worktree, so every command that restacks reports it
rather than printing "up to date":

- `restack`, `modify`, `squash`, `absorb`, and the rest of the mutating commands
  warn with `Held <branch> back: <reason>`.
- `sync` renders the same reason on its restack row, in both the streaming and
  interactive handlers, and excludes held branches from the "already current"
  count (`isPlainUpToDate`).
- `restack --json` lists them under `held` as `{branch, reason}`. They also stay
  in `skipped`, so an empty `conflicts` list must not be read as "the whole
  stack moved".

A branch held because an *ancestor* is held reports the ancestor's reason
prefixed with which ancestor it is, since the remedy lives there.

`stackit restack` additionally reports the stack-level holds from its own gate
(`skipDirtyWorktreeStacks`); `sync` reports those through
`SkipReasonForWorktree`. Rebase-in-progress detection is still missing from the
engine snapshot and from sync's gate — see "What holds a branch" above.

**Source**: `internal/engine/engine_sync.go` (hold set construction),
`internal/engine/restack_impl.go` (`holdBranch`, `branchHeldBack`),
`internal/actions/worktree_holds.go` (restack command hold detection), and
`internal/actions/restack.go` (`skipDirtyWorktreeStacks`, `heldWorktreeReason`).

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
| `get` past a parent that landed and left the remote (all merge methods) | `internal/integration/get_landed_parent_test.go` |
| Same, with the landed parent's remote metadata already cleaned up | `internal/integration/get_landed_parent_test.go` (`TestGetPastLandedParentWithoutParentMetadata`) |
| Same, with no stackit metadata at all — reported, never rebased | `internal/integration/get_landed_parent_test.go` (`TestGetPastLandedParentWithNoMetadataAtAll`) |
| Re-anchor target selection, anchor harvesting, report views (actions unit) | `internal/actions/get_internal_test.go` |
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
| Fetch recovery and re-anchoring in `get` | `internal/actions/get.go` (`reanchorPastLanded`) |
| Remote branch existence | `internal/engine/engine_branch_status.go` (`MissingRemoteBranches`) |
| Divergence anchor for a fetched branch | `internal/engine/branch_tracking.go` (`TrackBranchPastLandedParent`) |
| Anchor recovery when the crawl gave up | `internal/actions/get.go` (`harvestAnchors`) |
