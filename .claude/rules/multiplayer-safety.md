# Multiplayer Safety Invariants

Guarantees that protect users who share a repository — especially when some
collaborators do **not** use stackit. These are non-negotiable and extend
[safety-invariants](safety-invariants.md) ("GitHub Merge Methods Are
First-Class"). Full reference: [docs/multiplayer.md](../../docs/multiplayer.md).

## Detection Must Work With Zero Stackit Metadata

**A collaborator's merged branch can carry no stackit metadata at all. Landed-work
detection must still succeed from Git history alone.**

This applies to: restack, sync, branch cleanup, merged-branch detection, and any
"did this branch's changes land on the target" decision.

### Why This Matters

A teammate who does not use stackit pushes a plain GitHub branch; you stack your
work on top of it; they squash- or rebase-merge it through the GitHub UI. In your
local view their branch has **no recorded PR number and no `MERGED` state** —
only Git history reflects the merge. If detection requires PR metadata, it misses
the merge and replays their pre-merge commits into your branch (issue #1345's
data-loss shape).

### Required Behavior

- `branchLanded` must treat the Git fallback as valid **without** PR metadata
  when the target is trunk. Do not re-gate the trunk Git fallback behind a
  recorded PR number.
- Cover every merge method for a non-stackit collaborator, not just the stackit
  user. The detection signal differs by case:

  | Merge method | Commits | Non-stackit detection signal |
  |--------------|---------|------------------------------|
  | Merge commit | any | ancestry (`IsMerged`) |
  | Rebase | any | per-commit patch-id / `git cherry` (`IsMerged`) |
  | Squash | single | `git cherry` matches the lone commit (`IsMerged`) |
  | Squash | multi | **aggregate patch-id only (`IsSquashMerged`)** |

- A single-commit squash test proves nothing about squash support — `git cherry`
  already patch-matches it. **Multi-commit squash is the required coverage.**
- The Git fallback stays limited to trunk. For a non-trunk (stacked) parent, rely
  on `MERGED` PR metadata: patch-equivalence there can mean a local stack op made
  a child empty, not that a PR landed.

## Restack Only Builds On origin/<trunk>

**`restack` must refuse when a remote exists and local trunk is ahead of /
diverged from `origin/<trunk>`.**

This applies to: the `restack` command path (`PlanRestack` / `guardUnpushedTrunk`).

### Why This Matters

Restack rebases trunk-anchored branches onto the **local** trunk tip. In a shared
repo that is only safe when local trunk matches the remote — you must build your
stack only on commits everyone else also has (`origin/<trunk>`). If local trunk
has un-pushed or divergent commits, restack bakes machine-local work into the
stack that may never land as-is.

### Required Behavior

- Resolve trunk-vs-remote state **locally, no network**: read
  `refs/remotes/<remote>/<trunk>` and compare with
  `IsAncestor(localTrunk, remoteTrunk)`.
- **Ahead or diverged** (local trunk is NOT an ancestor of the remote trunk) →
  reject with a message pointing at `stackit sync` / pushing trunk.
- **Equal or behind** (ancestor is true) → allow. Behind is safe: the stack still
  builds only on commits present on the remote.
- **No remote-tracking ref** (local-only repo, fresh clone, never fetched) →
  never guard. Solo users with no remote restack freely.
- Only fire when the restack actually has branches to move; an empty restack is
  never an error.
- Do **not** extend this guard to `sync` / `modify` / `squash` / `reorder`
  (they go through `RestackBranches`, not `PlanRestack`). `sync` is the
  recommended way to reconcile trunk, so guarding it would deadlock the fix.

## Reparent Past Landed Work, Never Replay It

**After restack/sync past a merged parent, a branch must contain only its own
commits — never the landed parent's pre-merge commits.**

### Required Behavior

- When a parent has landed, reparent its descendants onto the parent's base
  without replaying the parent's commits. Invariant: `<base>..<branch>` equals
  exactly the branch's own commits.
- Never move an unrelated branch ref to a merged **sibling's** stale pre-merge
  tip.
- Never delete or reparent branch metadata based solely on SHA equality. Deletion
  stays gated on submitted-PR metadata (`metaHasSubmittedPR`); reparenting is the
  non-destructive path and does not require that gate.

## Performance: Keep The Squash Scan Cold

**`IsSquashMerged` (aggregate patch-id scan) must stay out of the hot
`IsMerged` primitive and out of broad detection loops.**

It runs a bounded log plus a patch-id per scanned commit. Call it only from
explicit "did this branch land into trunk" decisions, after cheap `IsMerged` has
already returned false. Folding it into `IsMerged` would put an expensive
per-commit walk on every merge check.

## Test Requirements

Any change to detection, reparenting, or the guard must cover:

- **All three GitHub merge methods** (merge commit, squash, rebase).
- **Both user types** (stackit user with `MERGED` metadata; non-stackit
  collaborator with zero metadata).
- **A multi-commit squash** case specifically.
- **Guard boundaries**: ahead/diverged rejected; equal/behind/no-remote/no-branches
  allowed.

See `docs/multiplayer.md` for the test-file map.
