# Safety Invariants

Critical guarantees that all operations must maintain. These are non-negotiable.

## GitHub Merge Methods Are First-Class

**Stackit must correctly handle PRs merged through all GitHub UI merge methods:
merge commit, squash merge, and rebase merge.**

This applies to: sync, restack, branch cleanup, merged-branch detection, stack
navigation, merge orchestration, and any operation that decides whether branch
changes have landed on trunk or another parent branch.

### Why This Matters

GitHub's three merge methods produce different Git histories for the same PR:

| Method | What lands on the base branch | Is the PR branch tip reachable from base? | Can ancestry prove it merged? |
|--------|-------------------------------|-------------------------------------------|-------------------------------|
| Merge commit | A merge commit whose parents include the base and PR head | Yes | Yes |
| Squash merge | One new commit containing the combined PR diff | No | No |
| Rebase merge | New commits replaying the PR commits onto base | No | No |

For merge commits, `git merge-base --is-ancestor <branch> <base>` and
`git branch --merged <base>` are meaningful. For squash and rebase merges they
are not: the PR's original commits are not ancestors of the base branch even
though the PR landed.

Squash merges are especially risky for stacked changes because a multi-commit
PR lands as one combined commit. Per-commit patch matching (`git cherry`) can
fail even when the branch's final diff is present on trunk. Rebase merges keep
one commit per PR commit but rewrite SHAs, so ancestry still fails.

### Required Behavior

Code must not define "merged" using a single Git predicate. A merged PR can be
proven by a combination of signals, including:

- PR metadata that records the PR state as merged
- The GitHub-reported landed commit being reachable from the target branch
- Ancestry when the PR used a merge commit
- Patch/range equivalence when the PR used rebase merge
- Aggregate diff equivalence or recorded landed metadata when the PR used
  squash merge

When sync or restack sees a merged parent, it must reparent descendants past the
landed branch without replaying that parent's commits. When it sees a merged
sibling, it must never move an unrelated branch ref to the sibling's stale
pre-merge tip.

### Implementation Rules

- Do not assume `IsAncestor(branch, trunk)` means "not merged" when it returns
  false.
- Do not assume `git branch --merged` covers squash or rebase merges.
- Do not assume `git cherry` covers multi-commit squash merges.
- Do not delete or reparent branch metadata based solely on SHA equality.
- Preserve or fetch enough PR information to distinguish "closed unmerged" from
  "merged by any GitHub method".
- Any change to sync, restack, cleanup, or merge detection must consider all
  three GitHub merge methods explicitly.

## No Detached HEAD State

**Operations must NEVER leave the user in a detached HEAD state when cancelled or on failure.**

This applies to: split, sync, create, merge, absorb, fold, restack, and any other operation that modifies branch state.

### Implementation Pattern

1. Capture the current branch at the start of the operation
2. Take a snapshot before any mutations: `eng.TakeSnapshot(...)`
3. Perform the operation
4. On error or cancellation, restore the original branch before returning

```go
// Example pattern
currentBranch := eng.CurrentBranch()
if currentBranch == nil {
    return fmt.Errorf("not on a branch")
}

// ... operation logic ...

// On cancellation/error:
if err := git.CheckoutBranch(currentBranch.Name); err != nil {
    // Log but don't mask original error
}
return originalErr
```

### Why This Matters

- Users expect to be on a branch after any operation completes (success or failure)
- Detached HEAD is confusing and requires manual recovery
- Cancelled operations should have minimal side effects

## Worktree Operations Must Use Detached HEAD

**Temporary worktrees must NEVER check out shared branches (like trunk/main) directly.**

This applies to: combination analysis, merge validation, CI validation, compatibility checks, and any exploratory operation that creates merge commits.

### Why This Matters

Git worktrees share refs with the parent repository. If a worktree checks out `main` and creates commits (especially merges), those commits update `refs/heads/main` globally, affecting the user's main workspace.

### Implementation Pattern

```go
// WRONG - can modify shared branch refs
session, _ := wtExecutor.CreateSession(ctx, opts)
session.Engine.CheckoutBranch(ctx, trunk)  // Now on main!
session.Engine.MergeMultiple(...)          // Updates refs/heads/main!

// CORRECT - always stay detached
session, _ := wtExecutor.CreateSession(ctx, opts)
// Worktree is already at detached HEAD from CreateSession
// If you need the latest trunk, use ResetHard (keeps HEAD detached):
session.Engine.ResetHard(ctx, trunk.GetName())
session.Engine.MergeMultiple(...)  // Creates commits at detached HEAD only
```

### When Checking Out Branches in Worktrees

If you must checkout a branch in a worktree (e.g., for pushing), create a NEW branch first:

```go
// Create a new branch at current HEAD (safe - new ref)
session.Engine.CreateBranch(ctx, "my-temp-branch", "HEAD")
session.Engine.CheckoutBranch(ctx, tempBranch)
session.Engine.PushBranch(ctx, tempBranch, remote, opts)
```

### What Can Go Wrong

1. User is on a feature branch in main repo
2. Worktree is created (detached at trunk)
3. Worktree pulls trunk (updates refs/heads/main globally)
4. Worktree checks out main (succeeds because main not checked out elsewhere)
5. Worktree creates merge commits (on main!)
6. User switches to main - sees unexpected merge commits

## GitHub Writes Only During Sync

**Commands must NOT directly update GitHub PRs. Instead, mark branches for update and let `sync` handle it.**

This applies to: describe, scope, and any command that changes metadata affecting PR display.

**Exception:** `lock`/`unlock` deliberately do NOT follow this pattern — see the Exceptions section below.

### Why This Matters

- **Performance**: GitHub API calls are slow (~200-500ms each). Batching in `sync` is faster.
- **Predictability**: Users expect `sync` to be the network-heavy command, not `describe` or `lock`.
- **Offline support**: Users can work offline, then sync when connected.
- **Consistency**: One place to handle GitHub rate limits, retries, and errors.

### Exceptions (Acceptable GitHub Calls)

1. **Read-only for display**: `log` showing CI status, `get` showing PR info
2. **Primary purpose is GitHub**: `submit` creating/updating PRs, `merge` creating consolidation PRs
3. **During sync**: All PR body/title updates should happen here
4. **Lock/unlock**: `lock` and `unlock` call GitHub directly (`PushMetadataAndSyncPRs`), on purpose. A lock is a shared state change: it must reflect for other collaborators and re-evaluate PR CI **immediately**, not on the locking user's next `sync`. Deferring it would let others keep merging a branch the user just locked. The performance/offline tradeoffs above are outweighed by the need for an immediate, visible effect. Do not migrate `lock`/`unlock` to the mark-and-sync pattern.

### Implementation Pattern

```go
// WRONG - directly updates GitHub
if err := actions.PushMetadataAndSyncPRs(ctx, branches); err != nil {
    out.Debug("Failed: %v", err)
}

// CORRECT - batch mark for update, sync handles GitHub
branchNames := make([]string, len(branches))
for i, b := range branches {
    branchNames[i] = b.GetName()
}
_ = eng.MarkBranchesForPRBodyUpdate(ctx, branchNames)
if err := pushMetadataOnly(ctx, eng, branchName); err != nil {
    out.Debug("Failed: %v", err)
}
```

### How Sync Processes Flags

```go
// In sync/github_sync.go - only updates flagged branches
flaggedBranches := ctx.Engine.GetBranchesNeedingPRBodyUpdate()
if len(flaggedBranches) > 0 {
    actions.UpdateStackPRMetadata(ctx, flaggedBranches, owner, repo)
}
```

### Commands That Should Use This Pattern

| Command | What it changes | Should mark, not call GitHub |
|---------|-----------------|------------------------------|
| `describe` | Stack description in footer | ✅ Fixed |
| `scope` | PR title prefix | ❌ TODO |
| `lock` | Lock section in PR body | ⛔ Exception — syncs immediately (see Exceptions above) |
