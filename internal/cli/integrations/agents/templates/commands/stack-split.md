---
description: Split committed changes between current branch and a new child branch
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Write, Glob, Grep, AskUserQuestion
argument-hint: [check-command]
---

# Stack Split

Split the committed changes on the current branch between this branch (keep focused changes) and a new child branch (extract remainder). Supports **hunk-level** analysis for fine-grained splitting.

**Key difference from related skills:**
- `/stack-plan` - Creates N branches from **uncommitted** changes (from scratch)
- `/stack-extract` - Extracts commits/files to sibling or parent branches
- `/stack-split` - Binary split of **committed** changes: keep focused changes on current, extract rest to a NEW child branch above

**Primary objective: Never lose someone's work.**

## Context
- Current branch: !`git branch --show-current`
- Git status: !`git status --short`
- Stack state: !`command stackit log --no-interactive 2>&1`

## Arguments
$ARGUMENTS

## Preconditions

**This skill requires a clean working directory.** Before proceeding, verify there are no uncommitted changes.

## Safety Model

This workflow uses a **backup branch** strategy to ensure commits are never lost:

1. **Before execution**: Current branch state is saved to a backup branch
2. **During execution**: Branch is reset and patches are selectively applied
3. **On success**: Backup branch is deleted (changes now live in split branches)
4. **On failure**: Backup branch remains - user can recover original state

Recovery is always: `git checkout <backup-branch>`

## Instructions

### Phase 0: Check Preconditions

First, verify the working directory is clean:

```bash
git status --porcelain
```

**If output is NOT empty** (there are uncommitted changes):
```
Cannot Split: Working Directory Not Clean
=========================================
You have uncommitted changes. Please commit or stash them first.

Options:
- Commit changes: git add -A && git commit -m "WIP"
- Stash changes: git stash
- Discard changes: git checkout -- .

Then re-run /stack-split
```

**Stop and exit.** Do not proceed with uncommitted changes.

**If output is empty**: Continue to Phase 1.

### Phase 1: Gather Committed Changes

Get the commits on the current branch that will be analyzed for splitting:

```bash
# Get parent branch from stackit metadata
command stackit log --no-interactive

# Get the diff between parent and current branch HEAD
# This shows all changes that could be split
git diff <parent-branch>..HEAD

# Also get commit history for context
git log --oneline <parent-branch>..HEAD
```

**If no commits on branch** (branch is at same point as parent):
```
Nothing to Split
================
This branch has no commits ahead of its parent.
```
Stop and exit.

**If branch has commits**: Continue to Phase 2.

### Phase 2: Analyze and Propose Split (Hunk-Level)

Analyze the changes at the **hunk level**. A hunk is a contiguous block of changes within a file.

**Split Criteria:**

**Keep on current branch:**
- Hunks implementing the core feature/bug fix that motivated the work
- Hunks for directly related tests
- Changes essential to the branch's stated purpose
- Related hunks (e.g., if a function definition stays, its usages should too)

**Extract to child branch:**
- Tangential improvements discovered while working
- Refactoring not strictly necessary for the core feature
- Documentation changes unrelated to the core feature
- Infrastructure/config changes
- New functionality that grew out of scope
- Cleanup/formatting changes unrelated to the core work

**Present the proposal with hunk-level detail:**
```
Proposed Split (Hunk-Level)
===========================
Current branch: <branch-name>
New child branch: <child-name>

Keep on [current-branch]:
  src/auth.go:42-58 (validateUser function) - Core feature logic
  src/auth.go:102-110 (error handling) - Related to validateUser
  src/auth_test.go:15-45 (test for validateUser) - Feature tests

Extract to [child-branch]:
  src/auth.go:15-20 (import cleanup) - Tangential formatting
  src/utils.go:5-30 (new helper) - Utility discovered during development
  README.md:1-50 (doc update) - Documentation improvement
```

**For ambiguous hunks**, use `AskUserQuestion`:
- Header: "Hunk placement"
- Question: "This hunk in <file>:<lines> modifies <description>. Which category?"
- Options: ["Keep on current branch", "Extract to child branch"]

**Related hunk detection**: If a function definition goes to one category, its usages should follow. Ask the user if this requires splitting usages across branches:
- Header: "Related hunks"
- Question: "The <function> definition is being kept/extracted. Its usages appear in other hunks. Should they follow?"
- Options: ["Yes, keep together", "No, split separately"]

**Branch naming**: Generate a descriptive name for the child branch based on what's being extracted (e.g., "refactor-auth-imports", "add-helper-utils", "update-docs").

**If all changes belong to the same category**: Inform the user that splitting isn't needed:
- If all changes are core: "Nothing to extract - all changes belong to this branch's purpose"
- If all changes are tangential: "All changes are tangential - consider if this is the right branch for them"

### Phase 3: Determine Build/Check Command

**If provided in arguments**: Use that command.

**Otherwise, auto-detect**:
1. Check for `mise.toml` → use `mise run check` or `mise run test`
2. Check for `Makefile` → use `make test` or `make check`
3. Check for `package.json` → use `npm test` or `npm run check`
4. Check README.md or CONTRIBUTING.md for build/test instructions

**If no command found**, use `AskUserQuestion`:
- Header: "Check command"
- Question: "What command should I use to verify each branch builds correctly?"
- Options:
  - "Skip verification" - Create branches without running checks (not recommended)
  - "Let me specify" - User will provide the command

If user selects "Let me specify", wait for them to provide the command in chat.

### Phase 4: Get User Approval

Present the complete plan:

```
Stack Split Summary
===================
Current branch: <current-branch>
Parent branch: <parent-branch>
Child branch to create: <child-branch>

Commits being split: N commits

Keep on [current-branch] (X hunks, Y files):
  - src/auth.go:42-58 (validateUser function)
  - src/auth.go:102-110 (error handling)

Extract to [child-branch] (A hunks, B files):
  - src/auth.go:15-20 (import cleanup)
  - src/utils.go:5-30 (new helper)

Each branch will be verified with: <check-command>
```

Use `AskUserQuestion` to get approval:
- Header: "Split plan"
- Question: "Ready to split these changes?"
- Options:
  - "Execute" - Create the split with verification
  - "Dry run" - Show exact commands without executing
  - "Swap" - Exchange keep/extract categories
  - "Modify" - Change the split classification
  - "Cancel" - Exit without making changes

**If user selects "Dry run"**, show the exact commands:
```
Dry Run - Setup
===============
ORIGINAL_BRANCH=<current-branch>
PARENT_BRANCH=<parent-branch>
BACKUP_BRANCH="stack-split-backup-$(date +%s)"

# Create backup of current state
git branch "$BACKUP_BRANCH"

Dry Run - Generate Patches
==========================
git diff "$PARENT_BRANCH"..HEAD > /tmp/full.patch
# Parse and create:
#   /tmp/keep.patch - hunks to keep on current branch
#   /tmp/extract.patch - hunks to extract to child

Dry Run - Reset and Apply Keep Patch
====================================
git reset --hard "$PARENT_BRANCH"
git apply --index /tmp/keep.patch
git commit -m "<keep-message>"
<check-command>

Dry Run - Create Child with Extract Patch
=========================================
git apply --index /tmp/extract.patch
echo "<extract-message>" | command stackit create <child-name> --no-interactive
<check-command>

Dry Run - Cleanup (on success)
==============================
git branch -D "$BACKUP_BRANCH"
```

Then ask again whether to execute or cancel.

**If user selects "Swap"**, exchange the keep/extract classifications and present the updated plan for approval.

**If user selects "Modify"**, let them describe changes, update the plan, and ask for approval again.

### Phase 5: Execute Split

#### Step 0: Create Backup Branch

**This is the critical safety step.** Before modifying anything, save the current branch state:

```bash
# Record branch names
ORIGINAL_BRANCH=$(git branch --show-current)
PARENT_BRANCH=<parent-from-stackit-metadata>

# Create backup branch at current HEAD
BACKUP_BRANCH="stack-split-backup-$(date +%s)"
git branch "$BACKUP_BRANCH"

# Record the backup commit SHA
BACKUP_COMMIT=$(git rev-parse HEAD)
```

Display the recovery reference:
```
Starting Split
==============
Current branch: <current-branch>
Parent branch: <parent-branch>
Child branch: <child-branch>

Safety Backup Created
---------------------
Backup branch: <backup-branch-name>
Backup commit: <commit-sha>

RECOVERY: If anything goes wrong, run:
  git reset --hard <backup-branch-name>

Your original commits are safely preserved. Proceeding...
```

#### Step 1: Generate Patches

Generate the full diff and create separate patch files:

```bash
# Generate full diff from parent to current HEAD
git diff "$PARENT_BRANCH"..HEAD > /tmp/full.patch
```

**Parse the patch into hunks and create two patch files:**
- `/tmp/keep.patch` - hunks to keep on current branch
- `/tmp/extract.patch` - hunks to extract to child branch

Write these files using the Write tool. Each patch file must:
- Include proper `diff --git` headers
- Include `--- a/file` and `+++ b/file` lines
- Include `@@ ... @@` hunk headers
- Be a valid patch that `git apply` can consume

**Handling new files**: A new file is a single hunk. Include the entire file in either keep.patch or extract.patch.

**Handling deleted files**: A deleted file is a single hunk. Include the deletion in the appropriate patch.

#### Step 2: Reset and Apply Keep Patch

```bash
# Reset current branch to parent (removes all commits)
git reset --hard "$PARENT_BRANCH"

# Apply the keep patch
git apply --index /tmp/keep.patch

# Verify files are staged
git diff --cached --stat

# Commit the kept changes
git commit -m "<keep-message>"

# Run build verification
<check-command>
```

**On build failure**: Report and offer options (see Error Handling).

#### Step 3: Create Child Branch with Extract Patch

```bash
# Apply the extract patch
git apply --index /tmp/extract.patch

# Verify files are staged
git diff --cached --stat

# Create the child branch
echo "<extract-message>" | command stackit create <child-name> --no-interactive

# Run build verification
<check-command>
```

**On build failure**: Report and offer options (see Error Handling).

### Phase 6: Report Completion

After both branches are updated successfully:

```bash
# Delete the backup branch (no longer needed)
git branch -D "$BACKUP_BRANCH"

# Show the final stack
command stackit log --no-interactive
```

Present a summary:
```
Split Completed Successfully
============================

Current branch [<current-branch>]:
  - <keep-message>
  - X hunks, Y files

Child branch [<child-branch>]:
  - <extract-message>
  - A hunks, B files

Backup branch deleted (changes now live in split branches).

Stack structure:
<stackit log output>

Next steps:
- Run /stack-submit to create/update PRs
- Run /stack-verify to re-verify all branches
```

## Error Handling Reference

| Phase | Scenario | Recovery |
|-------|----------|----------|
| Phase 0 | Uncommitted changes | Inform and exit - user must commit/stash first |
| Phase 1 | No commits on branch | Inform and exit - nothing to split |
| Phase 2 | All hunks same category | Inform user - splitting not needed |
| Phase 4 | User cancels | Exit - no changes made |
| Phase 5 | Patch apply fails | `git reset --hard <backup-branch>` |
| Phase 5 | Keep build fails | Offer: Continue/Rollback |
| Phase 5 | Extract build fails | Offer: Continue/Rollback |

**On any execution failure**, use `AskUserQuestion`:
- Header: "Build failed"
- Question: "The build failed on <branch>. How would you like to proceed?"
- Options:
  - "Continue anyway" - I've fixed it or will fix later
  - "Rollback" - Restore from backup and exit
  - "Stop here" - Keep current state, I'll handle manually

**If user selects "Rollback"**:
```bash
# Reset to backup state
git reset --hard "$BACKUP_BRANCH"

# Delete the child branch if created
git branch -D <child-name> 2>/dev/null || true

# Delete backup branch
git branch -D "$BACKUP_BRANCH"
```

Then report: "Rolled back to original state. Your commits are restored."

## Patch File Format

Valid patch format for `git apply`:

```diff
diff --git a/src/auth.go b/src/auth.go
--- a/src/auth.go
+++ b/src/auth.go
@@ -40,6 +40,12 @@ func existingCode() {
 }

+func validateUser(u User) error {
+    if u.Name == "" {
+        return errors.New("name required")
+    }
+    return nil
+}
+
 func anotherFunction() {
```

For new files:
```diff
diff --git a/src/newfile.go b/src/newfile.go
new file mode 100644
--- /dev/null
+++ b/src/newfile.go
@@ -0,0 +1,10 @@
+package main
+
+func newHelper() {
+    // ...
+}
```

## Do NOT
- Proceed if there are uncommitted changes in the working directory
- Create branches without user approval of the plan
- Continue past a build failure without user consent
- Put the same hunk in both patches
- Use `git commit` directly for the child branch - use `command stackit create`
- Skip build verification (unless user explicitly says to)
- Delete the backup branch until BOTH branches are successfully created and verified
- Propose child branch names that already exist
- Split hunks that are interdependent (function + its usages) without asking

## Follow-up

After successful split, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Split completed successfully. What would you like to do next?"
- Options:
  - label: "Submit both as PRs (Recommended)"
    description: "Push branches and create/update pull requests"
  - label: "Verify both branches"
    description: "Run full build verification on both"
  - label: "Done for now"
    description: "No follow-up action needed"

Based on response:
- **"Submit both as PRs"**: Invoke `/stack-submit` skill using the `Skill` tool
- **"Verify both branches"**: Invoke `/stack-verify` skill using the `Skill` tool
- **"Done for now"**: End with summary of what was split
