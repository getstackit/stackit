# Absorb Conflict Resolution

Guide for resolving conflicts during `stackit absorb --no-interactive --force` operations. Unlike regular git conflicts, absorb conflicts occur when staged changes cannot be cleanly applied to their target commits in the stack.

> **CRITICAL:** Always run stackit commands with `stackit ... --no-interactive`. For commands that require confirmation, also include the `--yes` or `-y` flag.

## Understanding Absorb Conflicts

Absorb conflicts happen when:
- The staged change modifies lines that have been changed differently in later commits
- The patch context doesn't match the target commit's file state
- Multiple commits have touched the same region of code

**Key insight:** Absorb conflicts are often not "real" semantic conflicts - they're usually the tool not being smart enough to see that the change can be applied with adjustments.

## Quick Resolution Checklist

```
Absorb Conflict Resolution:
- [ ] Step 1: Understand the staged change intent
- [ ] Step 2: Identify the target commit
- [ ] Step 3: View the conflict
- [ ] Step 4: Apply semantic resolution
- [ ] Step 5: Verify and complete
```

## Step 1: Understand the Staged Change Intent

First, understand what the staged change is trying to do:

```bash
# View the staged diff
git diff --cached

# View staged changes for specific file
git diff --cached -- path/to/file.go
```

**Ask yourself:**
- What is this change trying to accomplish semantically?
- Is it adding, removing, or modifying functionality?
- What is the "before" and "after" state?

## Step 2: Identify the Target Commit

When absorb fails with a conflict, identify where the change should go:

```bash
# Show absorb plan (dry run)
stackit absorb --dry-run --no-interactive

# View commits in current branch's stack
stackit tree --no-interactive

# See what each commit changed
git log --oneline --stat HEAD~5..HEAD
```

**The target commit is the one that:**
- Originally introduced the lines being modified
- Is the logical place for this change to "belong"

## Step 3: View the Conflict

Understand what's conflicting:

```bash
# If absorb left conflicts, show them
git diff --name-only --diff-filter=U

# View conflict markers in file
cat path/to/conflicted/file.go

# View the file at the target commit
git show <target-commit>:path/to/file.go

# View the file at HEAD (current state)
git show HEAD:path/to/file.go
```

**Conflict anatomy for absorb:**
- The staged change was created against HEAD's version
- The target commit has an older version of the file
- Intervening commits modified the same region

## Step 4: Apply Semantic Resolution

Instead of mechanically resolving conflict markers, apply the change semantically:

### Strategy A: Direct Application (Most Common)

The change can be directly applied to the target commit's version:

```bash
# 1. Checkout the target branch
stackit checkout <target-branch> --no-interactive

# 2. Manually apply the semantic change
# Edit the file to make the same logical change,
# but against this version of the code

# 3. Amend the commit
git add path/to/file.go
stackit modify --no-interactive

# 4. Restack to propagate — scope to the amended branch and its descendants
stackit restack --branch <target-branch> --upstack --no-interactive
```

Pass `--json` when you want a machine-readable report (which branches were restacked, skipped, or conflicted) instead of doing a second broad restack to "check".

### Strategy B: Split the Change

If the change touches code from multiple commits, split it:

```bash
# 1. Unstage everything
git reset HEAD

# 2. Stage only the part for commit A
git add -p  # Use patch mode to select hunks

# 3. Absorb the first part
stackit absorb --no-interactive --force

# 4. Stage the remaining part
git add -p

# 5. Absorb (will go to different commit)
stackit absorb --no-interactive --force
```

### Strategy C: Create New Commit

If the change doesn't belong in any existing commit:

```bash
# The change is already staged from the absorb attempt, so create a new branch
# with a commit on top. Pipe the message via -F - — a bare positional arg is
# treated as the BRANCH NAME, not the commit message.
echo "<type>: description of change" | stackit create -F - --no-interactive
```

### Strategy D: Interactive Resolution

For complex cases, use the absorb conflict workflow:

```bash
# 1. Show what absorb wants to do
stackit absorb --dry-run --no-interactive

# 2. If conflict occurs, check state
stackit absorb --show-conflict --no-interactive

# 3. Resolve the conflict manually
# Edit the file, removing conflict markers

# 4. Stage the resolution
git add path/to/file.go

# 5. Re-run absorb after resolution
stackit absorb --no-interactive --force

# Or abort and try a different approach
stackit abort --no-interactive
```

## Step 5: Verify and Complete

After resolution:

```bash
# 1. Verify the stack structure
stackit tree --no-interactive

# 2. Build/test each affected branch (use project's commands from README.md)
stackit foreach --no-interactive "<build-command>"
stackit foreach --no-interactive "<test-command>"

# 3. Check for any remaining issues
git status
```

## Common Absorb Conflict Patterns

### Pattern 1: Context Mismatch

**Scenario:** Staged change modifies a function, but a later commit added parameters to that function.

```go
// At target commit:
func process(data string) error {
    return validate(data)
}

// At HEAD (after later commits added parameter):
func process(data string, options Options) error {
    return validate(data, options)
}

// Your staged change (against HEAD):
func process(data string, options Options) error {
    return validateAndLog(data, options)  // Changed this line
}
```

**Resolution:** Apply the semantic change to the target commit version:
```go
// At target commit, apply the same semantic change:
func process(data string) error {
    return validateAndLog(data)  // Same semantic change, different signature
}
```

### Pattern 2: Code Moved/Refactored

**Scenario:** The code you're modifying was moved to a different location by a later commit.

**Resolution:**
1. Find where the code is now: `git log -S "functionName" --oneline`
2. Apply your change to the current location
3. Create a new commit instead of absorbing

### Pattern 3: Overlapping Changes

**Scenario:** Your change and an intervening commit both modified the same lines.

**Resolution:**
1. Understand both changes
2. Determine if they can coexist
3. Either:
   - Combine both changes in the earlier commit
   - Apply your change as a new commit on top

### Pattern 4: Deleted Code

**Scenario:** The code you're modifying was deleted by a later commit.

**Resolution:**
1. Understand why it was deleted
2. Either:
   - Don't absorb (the change is no longer needed)
   - Restore the code in the earlier commit and apply your change

## Claude-Assisted Resolution

When using `/stackit-absorb`, Claude can help by:

1. **Understanding intent:** Reading your staged changes and explaining what you're trying to do
2. **Finding the right target:** Determining which commit should receive the change
3. **Semantic application:** Applying the logical change to the correct version of the file
4. **Verification:** Ensuring the resolution compiles and tests pass

**Example interaction:**
```
You: /stackit-absorb
Claude: I see you have staged changes to `auth.go` that replace the GitHub
username resolution with a simpler git user.name check.

The target commit (abc123) doesn't have the GitHub username code yet - that
was added in a later commit (def456).

I'll apply your semantic change to the target commit's version:
- Before: `if err := eng.SetLastModifiedBy(info.BranchName); err != nil`
- After: Add the user.name validation before this call

Applying change... Done. Running tests... Passed.
Stack has been updated successfully.
```

## Troubleshooting

### "patch does not apply"

The patch context doesn't match. Use `--3way` (automatic) or apply semantically.

### "merge conflict while applying hunks"

Three-way merge couldn't auto-resolve. Manually resolve or use Claude assistance.

### Left in detached HEAD state

Absorb failed midway. Run `stackit abort --no-interactive` to recover.

### Changes lost after conflict

Check `git stash list` - absorb stashes changes before starting. Use `git stash pop` if needed.

## Prevention Tips

1. **Absorb frequently:** Smaller changesets have fewer conflicts
2. **Keep changes focused:** One logical change at a time
3. **Use `--dry-run` first:** Preview where changes will go
4. **Split unrelated changes:** Use `git add -p` to stage selectively

## Success Criteria

- All staged changes applied to appropriate commits
- No conflict markers in any files
- Stack builds and tests pass
- `stackit tree --no-interactive` shows clean structure
