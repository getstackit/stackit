# Fixing Compilation Errors After Absorb

After `stackit absorb`, compilation errors may occur when absorbed changes depend on files/changes that didn't get cleanly absorbed into the same commit.

> **CRITICAL:** Always run stackit commands with `stackit ... --no-interactive`. For commands that require confirmation, also include the `--yes` or `-y` flag.

## Why This Happens

`stackit absorb` uses heuristics to assign each change to the "best matching" commit. Sometimes:
- A function definition goes to commit A
- The function usage goes to commit B
- Commit B now fails to build because the function isn't defined yet

This is normal and fixable by moving the dependency to the earlier commit.

## Workflow Checklist

Copy this checklist and track your progress:

```
Absorb Fix Progress:
- [ ] Step 1: Identify build/test commands
- [ ] Step 2: Build and test each branch
- [ ] Step 3: Identify failed branches
- [ ] Step 4: Find missing dependencies
- [ ] Step 5: Apply fixes
- [ ] Step 6: Verify entire stack
```

## Step 1: Identify Build/Test Commands

**Find the project's build and test commands:**

1. Check README.md or CONTRIBUTING.md for build/test instructions:
   ```bash
   grep -i "build\|compile\|test" README.md CONTRIBUTING.md
   ```

2. Look for common build configuration files (Makefile, package.json, etc.)

3. If not found, ask the user:
   - "What command should I use to build the project?"
   - "What command should I use to run tests?"

## Step 2: Build and Test the Whole Stack at Once

Don't check out and build each branch by hand — run the build across the whole
stack in one pass and let stackit stop at the first failing depth:

```bash
stackit foreach --stack --find-first-failure --no-interactive "<build-command>"
```

`--find-first-failure` runs branches depth-by-depth and stops at the first depth
that fails, so the reported branch is the earliest break — exactly the one to fix.
Only drop down to a single branch (Step 3) for that reported failure:

```bash
stackit checkout <failing-branch> --no-interactive
```

**Note the failing branch and error** - you'll need them for Step 3.

## Step 3: Identify Failed Branches

For each failed branch, analyze the error:

```bash
# Example: Build failed on branch "add-validation"
stackit checkout add-validation --no-interactive

# Run build and capture error
<build-command> 2>&1 | tee build-error.log

# Common error patterns:
# - "undefined: functionName" → function defined in later commit
# - "cannot find module" → import added in later commit
# - "type not defined" → type definition in later commit
```

**Write down:**
- Branch name
- What's missing (function, type, file, import, etc.)
- Error message

## Step 4: Find Missing Dependencies

For each missing item, search upstack branches for where it's defined:

```bash
# Get child branch name
stackit children --no-interactive

# Check what changes exist in child that aren't in current
git diff <current-branch>..<child-branch>

# Or search for specific item
git log <current-branch>..<child-branch> --all -S "functionName"
```

**Look for:**
- Function definitions
- Type definitions
- New files
- Import statements
- Configuration changes

## Step 5: Apply Fixes

For each missing dependency, move it to the failing branch:

### Option A: Cherry-pick specific commit

```bash
# If the needed change is in a single commit
git cherry-pick <commit-hash>

# Resolve conflicts if needed (cherry-pick has no stackit equivalent)
git add -A
git cherry-pick --continue
```

### Option B: Manual copy

```bash
# If change is spread across commits, manually apply:
# 1. View the change in child branch
git diff <current-branch>..<child-branch> -- path/to/file.go

# 2. Apply the specific parts needed
# Edit the file manually, or:
git show <child-branch>:path/to/file.go > path/to/file.go

# 3. Commit the fix
git add path/to/file.go
stackit modify --no-interactive  # Amends current branch's commit
```

### Option C: Reorder branches (advanced)

If the dependency lives on a *later branch* and needs to come earlier, reorder
the branches with stackit rather than a manual `git rebase -i` (which bypasses
stackit metadata and hangs a non-interactive agent):

```bash
# Opens an editor to reorder the branches between trunk and the current branch,
# then restacks all descendants. Editor-driven — run it where a human can edit.
stackit reorder
```

To move a single branch onto a different parent non-interactively:

```bash
stackit move --source <branch> --onto <new-parent> -y --no-interactive
```

## Step 6: Verify Entire Stack

After fixing all branches, verify the entire stack builds:

```bash
# Build all branches in order
stackit foreach --no-interactive "<build-command>"

# Test all branches
stackit foreach --no-interactive "<test-command>"
```

**Expected output:**
```
Branch: add-auth
✓ Build succeeded
✓ Tests passed

Branch: add-validation
✓ Build succeeded
✓ Tests passed

Branch: add-api-endpoints
✓ Build succeeded
✓ Tests passed
```

## Validation Loop

For each branch:
1. Run build command
2. **If fails:**
   - Analyze error message
   - Find missing dependency (Step 4)
   - Apply fix (Step 5)
   - Re-run build
   - Repeat until build succeeds
3. **If passes:**
   - Mark checklist item complete
   - Move to next branch

**Only consider fix complete when all branches build and test successfully.**

## Prevention Tips

To avoid this in the future:

1. **Keep changes focused**: Absorb works best when changes are closely related
2. **Absorb frequently**: Smaller sets of changes = fewer dependency issues
3. **Check as you go**: Run build after absorb to catch issues early
4. **Use modify for small fixes**: `stackit modify --no-interactive` is safer for targeted changes

## Example Walkthrough

**Scenario:** After absorb, `add-validation` branch fails to build.

**Error:** `undefined: validateUser`

**Steps:**
```bash
# 1. Find where validateUser is defined
git log add-validation..add-api-endpoints --all -S "validateUser"
# → Found in commit abc123 on add-api-endpoints

# 2. Check the change
git show abc123
# → Shows validateUser function definition

# 3. Cherry-pick it
stackit checkout add-validation --no-interactive
git cherry-pick abc123

# 4. Verify fix
<build-command>
# ✓ Build succeeded

# 5. Restack children (they're now based on old version) — scope to this subtree
stackit restack --branch add-validation --upstack --no-interactive
# (use --all-stacks only if the fix affected multiple independent stacks)

# 6. Verify entire stack
stackit foreach --no-interactive "<build-command>"
# ✓ All branches succeed
```

## Success Criteria

- ✓ All branches build without errors
- ✓ All branches pass tests
- ✓ Stack structure is clean (`stackit log --no-interactive` shows proper tree)
- ✓ No git conflicts or issues
