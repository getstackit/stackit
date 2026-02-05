# Safety Invariants

Critical guarantees that all operations must maintain. These are non-negotiable.

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

This applies to: describe, scope, lock, and any command that changes metadata affecting PR display.

### Why This Matters

- **Performance**: GitHub API calls are slow (~200-500ms each). Batching in `sync` is faster.
- **Predictability**: Users expect `sync` to be the network-heavy command, not `describe` or `lock`.
- **Offline support**: Users can work offline, then sync when connected.
- **Consistency**: One place to handle GitHub rate limits, retries, and errors.

### Exceptions (Acceptable GitHub Calls)

1. **Read-only for display**: `log` showing CI status, `get` showing PR info
2. **Primary purpose is GitHub**: `submit` creating/updating PRs, `merge` creating consolidation PRs
3. **During sync**: All PR body/title updates should happen here

### Implementation Pattern

```go
// WRONG - directly updates GitHub
if err := actions.PushMetadataAndSyncPRs(ctx, branches); err != nil {
    out.Debug("Failed: %v", err)
}

// CORRECT - mark for update, sync handles GitHub
for _, branch := range branches {
    _ = eng.MarkNeedsPRBodyUpdate(branch.GetName())
}
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
| `lock` | Lock section in PR body | ❌ TODO |
