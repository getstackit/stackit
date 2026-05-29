# Conflict Resolution Patterns

Guide for resolving conflicts during stackit operations (restack, sync, merge, etc.).

> **CRITICAL:** Always run stackit commands with `stackit ... --no-interactive`. For commands that require confirmation, also include the `--yes` or `-y` flag.

## Understanding Conflicts

Conflicts occur when:
- Same file modified in multiple branches
- Changes overlap or contradict
- Git can't automatically merge

Stackit operations (restack, sync) involve rebasing, which can create conflicts.

## Quick Resolution Checklist

```
Conflict Resolution Progress:
- [ ] Step 1: Identify conflict type and scope
- [ ] Step 2: Locate conflict markers in files
- [ ] Step 3: Understand both changes
- [ ] Step 4: Resolve conflicts
- [ ] Step 5: Verify resolution
- [ ] Step 6: Continue operation
```

## Step 1: Identify Conflict Type

When you see a conflict, first understand what operation was running:

```bash
# Check git status
git status
```

**Output examples:**

```
# During restack
rebase in progress; onto abc123
You are currently rebasing branch 'feature-x' on 'abc123'.

# During sync
rebase in progress
You are currently rebasing.

# During merge
You have unmerged paths.
```

## Step 2: Locate Conflict Markers

Find which files have conflicts:

```bash
# List conflicted files
git diff --name-only --diff-filter=U

# Or use git status
git status --short | grep "^UU"
```

**Open each conflicted file** and look for conflict markers:

```
<<<<<<< HEAD (Current Change)
your changes
=======
incoming changes
>>>>>>> branch-name (Incoming Change)
```

## Step 3: Understand Both Changes

Before resolving, understand what each side is trying to do:

### View Current Change (HEAD)
```bash
# This is your current branch's version
git show HEAD:path/to/file.go
```

### View Incoming Change
```bash
# This is what's trying to be applied
git show REBASE_HEAD:path/to/file.go

# Or for merge
git show MERGE_HEAD:path/to/file.go
```

### View Common Ancestor
```bash
# The version before both changes
git show :1:path/to/file.go
```

## Step 4: Resolve Conflicts

Choose the appropriate resolution strategy:

### Strategy A: Accept Current (Keep Yours)

```bash
# Keep your version entirely
git checkout --ours path/to/file.go
git add path/to/file.go
```

### Strategy B: Accept Incoming (Take Theirs)

```bash
# Accept incoming version entirely
git checkout --theirs path/to/file.go
git add path/to/file.go
```

### Strategy C: Manual Merge (Combine Both)

Edit the file to combine both changes:

```go
// Before (with conflict markers):
<<<<<<< HEAD
func processUser(user User) error {
    return validateEmail(user.Email)
=======
func processUser(user User) error {
    return validateUser(user)
>>>>>>> feature-validation

// After (manual merge):
func processUser(user User) error {
    if err := validateEmail(user.Email); err != nil {
        return err
    }
    return validateUser(user)
}
```

**Then stage:**
```bash
git add path/to/file.go
```

### Strategy D: Use Merge Tool

```bash
# Launch configured merge tool
git mergetool path/to/file.go

# Common tools: vimdiff, meld, kdiff3, vscode
```

## Step 5: Verify Resolution

Before continuing, verify your resolution is correct:

### For Code Files

Run the project's build and test commands (check README.md or CONTRIBUTING.md):

```bash
# Build the project
<build-command>

# Run tests
<test-command>
```

### For Config Files

```bash
# Validate syntax
# JSON
jq . < config.json

# YAML
yamllint config.yaml

# TOML
toml-test config.toml
```

**Only proceed if verification passes.**

## Step 6: Continue Operation

Once all conflicts are resolved and verified:

```bash
# Stage all resolved files
git add -A

# Continue the operation — always use stackit continue, NOT raw git.
# It finishes the rebase AND resumes restacking the remaining branches,
# keeping stackit metadata consistent.
stackit continue --no-interactive
```

## Common Conflict Patterns

### Pattern 1: Import/Dependency Conflicts

**Scenario:** Both branches added different imports

```go
<<<<<<< HEAD
import (
    "fmt"
    "encoding/json"
=======
import (
    "fmt"
    "time"
>>>>>>> feature-x
```

**Resolution:** Combine both
```go
import (
    "fmt"
    "encoding/json"
    "time"
)
```

### Pattern 2: Function Addition Conflicts

**Scenario:** Both branches added functions in same location

```go
<<<<<<< HEAD
func validateEmail(email string) error {
    // implementation
}
=======
func validatePhone(phone string) error {
    // implementation
}
>>>>>>> feature-x
```

**Resolution:** Keep both
```go
func validateEmail(email string) error {
    // implementation
}

func validatePhone(phone string) error {
    // implementation
}
```

### Pattern 3: Modification Conflicts

**Scenario:** Both modified same function differently

```go
<<<<<<< HEAD
func process(data string) error {
    return validateAndSave(data)
=======
func process(data string) error {
    return validateAndLog(data)
>>>>>>> feature-x
```

**Resolution:** Depends on intent - often need both:
```go
func process(data string) error {
    if err := validateAndLog(data); err != nil {
        return err
    }
    return save(data)
}
```

### Pattern 4: Deletion vs Modification

**Scenario:** One branch deleted, other modified

```
<<<<<<< HEAD
// File deleted in current branch
=======
func newFunction() {
    // code
}
>>>>>>> feature-x
```

**Resolution:** Decide based on why it was deleted:
- If deleted because feature removed: keep deleted
- If deleted by mistake: keep modification
- If refactored elsewhere: move modification to new location

## Aborting if Stuck

If resolution is too complex or you want to reconsider:

```bash
# Abort the operation — use stackit abort, NOT raw git rebase --abort, so the
# whole halted stackit operation (restack/sync/merge/absorb) is cancelled cleanly.
stackit abort --no-interactive

# Then, if you also want to revert the stackit command's metadata changes:
stackit undo --no-interactive --yes
```

## Prevention Strategies

### 1. Keep Changes Focused

Smaller, focused branches = fewer conflicts. A branch can have multiple related commits.

```bash
# Good: Small, focused changes (one branch per logical unit)
echo "feat: add email validation" | stackit create -F - --no-interactive
echo "feat: add phone validation" | stackit create -F - --no-interactive

# Also good: Multiple commits in one branch for related work
echo "feat: add validation helpers" | stackit create -F - --no-interactive
git add -A
printf 'test: add validation tests' | git commit -F -

# Risky: Large, sweeping changes
echo "feat: refactor entire validation system" | stackit create -F - --no-interactive
```

### 2. Sync Frequently

```bash
# Sync often to stay current with main
stackit sync --no-interactive --restack
```

### 3. Restack After Changes

```bash
# After modifying a branch, stackit modify automatically restacks its descendants.
stackit modify --no-interactive

# Only restack manually if modifications affected multiple independent stacks:
# stackit restack --all-stacks --continue-on-conflict --no-interactive
```

Use `--json` to confirm which branches were touched and avoid running a second broad restack.

### 4. Use `stackit foreach --no-interactive` to Check

```bash
# Verify entire stack builds before operations (use project's build command from README.md)
stackit foreach --no-interactive "<build-command>"
```

## Advanced: Complex Multi-Branch Conflicts

When restacking affects multiple branches:

1. **Resolve bottom-up:** Start with lowest branch in stack
2. **Verify each level:** Build/test before continuing
3. **Track changes:** Keep notes on what you resolved
4. **Use checkpoints:** After each branch resolves, verify stack state

```bash
# Example workflow
stackit log --no-interactive  # Note branch order

# Resolve first conflict
git status  # See conflicted files
# ... resolve conflicts ...
git add -A
stackit continue --no-interactive

# Before moving to next conflict, verify
<build-command>
stackit log --no-interactive  # Confirm structure still correct

# Continue to next conflict
# ... resolve conflicts ...
git add -A
stackit continue --no-interactive

# Repeat until complete
```

## Troubleshooting

### "Cannot continue - you have unstaged changes"

```bash
# Stage all resolved files
git add -A
stackit continue --no-interactive
```

### "No rebase in progress"

```bash
# Operation already completed or aborted
stackit log --no-interactive  # Check current state
```

### "Conflict resolution broke the build"

```bash
# Abort and try again
stackit abort --no-interactive
stackit undo --no-interactive --yes

# Or fix the build issue
<build-command>  # See error (check README.md for project's build command)
# Fix the issue
git add -A
stackit continue --no-interactive
```

## Example Walkthrough

**Scenario:** Restack causes conflict in `auth.go`

```bash
# 1. Restack started (scoped to the affected branch and its descendants)
stackit restack --branch feature-x --upstack --no-interactive
# → Conflict in auth.go

# 2. Check status
git status
# → auth.go has conflicts

# 3. View conflict
cat auth.go
# → See conflict markers

# 4. Understand changes
git show HEAD:auth.go       # Your version
git show REBASE_HEAD:auth.go  # Incoming version

# 5. Resolve manually
vim auth.go
# → Combine both changes, remove markers

# 6. Verify
<build-command>
# → Build succeeds

# 7. Continue
git add auth.go
stackit continue --no-interactive
# → Restack completes

# 8. Verify final state
stackit log --no-interactive
# → Stack structure correct
```

## Success Criteria

- ✓ All conflict markers removed from files
- ✓ Project builds successfully
- ✓ Tests pass
- ✓ Operation completed (not still in progress)
- ✓ Stack structure is correct (`stackit log --no-interactive`)
