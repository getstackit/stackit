# Worktree Safety Invariants

Guarantees that protect uncommitted work and branch content when a repository
has more than one physical checkout. These are non-negotiable and extend
[safety-invariants](safety-invariants.md) ("Worktree Operations Must Use
Detached HEAD"). Full reference: [docs/worktree.md](../../docs/worktree.md),
[docs/multiplayer.md](../../docs/multiplayer.md) ("The worktree hold").

The core hazard: a branch ref and the working tree that has it checked out must
stay in agreement. Move one without the other and the divergence is committed by
the user's next `modify -a`, silently, as a deletion of the files the ref now
claims.

## Branch-Content Mutations Run Only In The Owning Worktree

**Any operation that rewrites a branch's commits or moves its ref must refuse to
run outside the worktree that owns the stack.**

This applies to: `create`, `modify`, `absorb`, `fold`, `squash`, `split`,
`move`, `reorder`, `pluck`, `delete`, `track`, and the pre-commit/pre-push
integrations.

### Why This Matters

The mutation would land under a ref whose worktree still holds the old content.
`git status` there reports the incoming commit's files as deleted, and the next
`stackit modify -a` commits that deletion into the stack. The user sees work
disappear from a branch they were not touching.

### Required Behavior

- Guard with `actions.EnsureCanModifyNamesHere(ctx, names...)` or
  `actions.EnsureCanModifyHere(ctx, branches...)`
  (`internal/actions/worktree_ownership.go`).
- Call it **before** taking a snapshot or mutating anything, so a refusal is a
  no-op.
- Guard every branch the operation touches, not just the current one — `create`
  guards both the current branch and its `--onto` target.
- The refusal must say where to go. When the owning worktree's directory no
  longer exists, `cd` is useless advice: point at `worktree detach` instead.
- Ownership checks do **not** belong on `engine.Branch`. `Branch` has no command
  context, and the same branch is valid to mutate in one worktree and invalid in
  another.

## Reconcilers Run Anywhere But Never Check Out A Foreign Branch

**`sync` and `restack` are whole-repository reconcilers. They are the deliberate
exception to the ownership guard — do not add it to them.**

### Why This Matters

Reconciling is the operation that *fixes* a repository whose worktrees have
drifted. Requiring it to run in a specific worktree would make the repair
unreachable from wherever the user noticed the problem.

### Required Behavior

- Never check out a branch owned by another worktree.
- Before moving any ref, inspect **every physical checkout** — including plain
  `git worktree` checkouts Stackit does not manage.
- Reset a clean holder to the new ref. Hold back a holder that is dirty, mid-
  rebase, or uninspectable, along with its descendants.
- Never move the ref while suppressing the reset. Not restacking at all is the
  safe outcome; it self-heals once the worktree is clean.

## Hold Trunk Never, Report Holds Always

**Trunk is excluded from the hold set, and a held branch must never be reported
as up-to-date.**

### Why This Matters

Restack never moves trunk's ref and never resets trunk's worktree, so a dirty
trunk checkout protects nothing — but holding it propagates through
`branchHeldBack`'s ancestry walk to **every trunk-rooted branch in the
repository**. A main worktree with one untracked scratch file silently turned
`modify`, `sync --restack`, and absorb's follow-up restack into no-ops.

Separately, a held branch returns `RestackUnneeded` — the same status as a
branch that needed no work. Without explicit reporting, "I protected your work"
and "there was nothing to do" are indistinguishable, and the remedy lives in
another directory the user is not looking at.

### Required Behavior

- Exclude trunk when building `heldBranches`, or terminate the ancestry walk on
  trunk before the membership check. Marking trunk's worktree dirty is still
  correct — that stops resets of it.
- Report every hold with the worktree and the reason. This applies to `sync` as
  well as `restack`; both can hold.
- Distinguish untracked from tracked. Tracked changes hold unconditionally.
  Untracked files hold only when one occupies a path the incoming commit also
  writes — that is the only case `git reset --hard` destroys. Holding on any
  untracked file at all stops a restack for the ordinary state of having written
  a new file and not staged it.

## Ref Writes Are Compare-And-Swap

**Metadata refs and rebased branch refs are written conditionally, against the
blob or SHA this process last read.**

### Required Behavior

- Do not short-circuit a write of unchanged content as a no-op. The expectation
  is what this process last read, not what the ref holds now; short-circuiting
  reports success for a ref another process may have moved.
- Record the expectation on **every** read path. `BatchReadMetadata` is the one
  that matters — engine graph loads go through it, so recording only in
  `ReadMetadata` silently degrades essentially every write to unconditional
  without failing anything.
- Drop the cache entry on a rejected write. Keeping it means a re-read answers
  from cache, recomputes the same stale expectation, and fails identically
  forever — harmless in the CLI, a wedge in the long-lived server.
- An unknown expectation falls back to an unconditional write. Newly tracked
  branches need this.

## Warm Starts Copy Only Ignored Files, Never Outside The Worktree

**`.worktreeinclude` is project-controlled input to a file-copying operation.
Treat it as untrusted for path purposes.**

### Required Behavior

- A pattern can never copy a tracked file; it must also be ignored by Git.
- Never overwrite an existing file in the destination.
- Skip symlinks and non-regular files.
- A symlinked parent directory in the destination aborts worktree creation and
  rolls it back — that is the path by which a warm start could be steered into
  writing outside the worktree.
- A file that cannot be placed is reported and skipped; the rest of the warm
  start still runs and the worktree is still created.
- Warm-started files are ignored by Git, so nothing else has a copy. Any command
  that deletes a worktree directory destroys them irrecoverably — say so in
  user-facing docs for `remove`, `prune`, and `detach`.

## Test Requirements

Any change to ownership guards, hold logic, or reconciliation must cover:

- **Both worktree kinds**: Stackit-managed and plain `git worktree`.
- **Both hold units**: a managed worktree holding its whole stack, another
  worktree holding a single branch.
- **Trunk exclusion**: a dirty trunk worktree must not hold trunk-rooted
  branches.
- **Untracked collision vs. coexistence**: an untracked file that the incoming
  commit writes (held) and one it does not (not held).
- **The sibling case**: a branch checked out in a worktree other than the one
  running the command, through both restack and `continue`.
