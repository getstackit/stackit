---
description: Plan and create a stack from uncommitted working tree changes
model: opus
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Glob, Grep, AskUserQuestion
argument-hint: [check-command]
---

# Stack Plan

Analyze uncommitted working tree changes, suggest a logical stacking structure, and create the stack with build verification at each step.

**Primary objective: Never lose someone's work.**

## Context
- Current branch: !`git branch --show-current`
- Git status: !`git status --short`
- Stack state: !`command stackit log --no-interactive 2>&1`

## Arguments
$ARGUMENTS

## Safety Model

This workflow uses a **backup commit** strategy to ensure changes are never lost:

1. **Before execution**: All changes are committed to a temporary backup branch
2. **During execution**: Files are selectively checked out from the backup commit
3. **On success**: Backup branch is deleted (changes now live in stack branches)
4. **On failure**: Backup branch remains - user can recover ALL changes

This is safer than stash because:
- Changes are in git history, not volatile stash storage
- Backup survives even if something corrupts the stash
- `git checkout <backup> -- file` naturally isolates files per branch
- Recovery is always `git checkout <backup-branch>` or `git cherry-pick <backup-commit>`

## Instructions

### Phase 1: Gather All Changes

First, check the current state of the working tree:

```bash
# Check current staging state
git diff --cached --stat

# Staged changes
git diff --cached

# Unstaged changes
git diff

# List untracked files
git ls-files --others --exclude-standard
```

For untracked files, read their contents to understand what they contain.

**For binary files**: Note the filename and extension. If the purpose is unclear, use `AskUserQuestion`:
- Header: "Binary file"
- Question: "What is the purpose of <filename>? Which feature does it belong to?"
- Options: List candidate branches or "Other"

**If no changes detected**: Inform the user and exit - there's nothing to stack.

**Note on partial staging**: If changes are partially staged, all changes will be re-staged during execution. The original staging state is not preserved.

### Phase 2: Analyze and Propose Stack Structure

Analyze the changes and determine a logical stacking order using semantic analysis.

#### Step 1: Semantic Analysis

Perform these analyses to understand relationships between changed files:

**1. File Clustering Analysis:**
- Group files by directory pattern (e.g., `internal/models/`, `internal/api/`)
- Identify naming conventions (e.g., `*_test.go` pairs with `*.go`)
- Look for related files (controller/service/model for same entity)

**2. Import/Dependency Analysis (Go projects):**
```bash
# For each changed Go file, extract imports
grep -h "^import" <file> -A 20 | grep -E '^\s*"' | head -20
```
- Files that import each other → should likely be in the same branch or adjacent
- Shared dependencies → may indicate foundational code (stack earlier)

**3. Test ↔ Implementation Pairing:**
- `*_test.go` should typically go with its corresponding `*.go` file
- Integration tests (`*_integration_test.go`) may form their own branch

**4. Semantic Type Detection:**
| Pattern | Type | Stack Position |
|---------|------|----------------|
| `migrations/*`, `schema/*` | Database schema | Early (foundational) |
| `internal/models/*`, `pkg/types/*` | Data models | Early |
| `internal/engine/*`, `pkg/core/*` | Core logic | After models |
| `internal/api/*`, `handlers/*` | API layer | After core |
| `internal/tui/*`, `ui/*`, `frontend/*` | UI layer | Late |
| `cmd/*`, `main.go` | Entry points | Last |

**5. Change Size Assessment:**
- Large files (>100 lines changed) → may warrant own branch
- Tiny changes (<10 lines) across many files → consider grouping by purpose

#### Step 2: Apply Grouping Strategy

Based on analysis, select the best grouping strategy:

**Grouping Strategies** (pick best fit for the codebase):
1. **By concern/feature** - Group related functionality together
2. **By layer** - Group by architectural layer (db → api → frontend)
3. **By dependency order** - Foundational changes first, dependent changes later
4. **By directory** - When directories cleanly map to concerns

**Ordering Principles**:
- Independent/foundational changes come first (bottom of stack)
- Changes that depend on others come later (top of stack)
- Smaller, focused branches are preferred over large ones
- Test files should accompany their implementation

**Limitation - File-level granularity**: This workflow splits changes at the file level. If a single file contains changes that should go to different branches (e.g., two unrelated functions modified), ask the user to either:
1. Accept the file going to one branch, OR
2. Manually split the file's changes first using `git add -p`, then re-run /stack-plan

**Present the proposal clearly**:
```
Proposed Stack Structure
========================
1. [branch-name-1] "commit message 1"
   - file1.go
   - file2.go
   Reason: <why these are grouped and why they're first>

2. [branch-name-2] "commit message 2"
   - file3.go
   - tests/file3_test.go
   Reason: <why these are grouped and depend on branch 1>

3. [branch-name-3] "commit message 3"
   - file4.go
   Reason: <why this comes last>
```

**Validate branch names** before proposing:
```bash
# Check if branch names already exist locally
git branch --list <branch-name>

# Check if branch names exist on remote
git ls-remote --heads origin <branch-name> 2>/dev/null
```

If a proposed branch name already exists, choose a different name or ask the user.

**If a file logically belongs in multiple branches**, use `AskUserQuestion`:
- Header: "File placement"
- Question: "Which branch should contain <filename>?"
- Options: List the candidate branch names

**If unsure about grouping**, use `AskUserQuestion`:
- Header: "Grouping"
- Question: "How should I group these related files?"
- Options: Present the reasonable grouping strategies

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

### Phase 4: Pre-validate Working Set

Before creating any branches, verify the combined changes build successfully:

```bash
# 1. Stage ALL changes
git add -A

# 2. Run the build/check command
<check-command>
```

**Note**: Pre-validation confirms the combined changes build together. Individual branches are still verified separately during execution, as splitting might expose missing dependencies between files.

**On pre-validation success**: Report success and continue to approval:
```
Pre-validation Passed
=====================
Your combined changes pass the build. Ready to split into branches.
```

**On pre-validation failure**: Report the error:
```
Pre-validation Failed
=====================
Your changes don't pass the build. Fix the issues first before stacking.

Error output:
<error-details>

Your changes are intact - nothing has been committed or branched.
```

Then use `AskUserQuestion` to determine next steps:
- Header: "Pre-validation failed"
- Question: "The build failed before creating any branches. How would you like to proceed?"
- Options:
  - "I'll fix it" - Exit so user can fix issues and re-run /stack-plan
  - "Skip validation" - Proceed without pre-validation (not recommended)
  - "Cancel" - Exit without creating branches

**Why pre-validate?**
- Catches issues before any branches are created
- If the combined changes don't build, splitting them won't help
- User can fix issues with all changes still in working tree (easiest state to work with)

### Phase 5: Get User Approval

Present the complete plan with validation status:

```
Stack Plan Summary
==================
Branches to create: N
Total files: Y
Pre-validation: PASSED ✓ (or SKIPPED if user chose to skip)

1. [branch-name-1] "commit message 1" (X files)
2. [branch-name-2] "commit message 2" (Y files)
3. [branch-name-3] "commit message 3" (Z files)

Each branch will be verified with: <check-command>
```

Use `AskUserQuestion` to get approval:
- Header: "Stack plan"
- Question: "Ready to create this stack?"
- Options:
  - "Execute" - Create all branches with verification
  - "Dry run" - Show exact commands without executing
  - "Modify" - Change groupings, order, or names
  - "Cancel" - Exit without creating branches

**If user selects "Dry run"**, show the exact commands that would run:
```
Dry Run - Setup
===============
BACKUP_BRANCH="stack-plan-backup-<timestamp>"
git checkout -b "$BACKUP_BRANCH"
git add -A
git commit -m "stack-plan: backup of all changes"
BACKUP_COMMIT=<commit-sha>
git checkout <original-branch>

Dry Run - Branch 1 of 3: <branch-name>
======================================
git checkout "$BACKUP_COMMIT" -- file1.go file2.go
echo "commit message" | command stackit create <branch-name> --no-interactive
<check-command>

[Continue for each branch...]

Dry Run - Cleanup (on success)
==============================
git branch -D "$BACKUP_BRANCH"
```

Then ask again whether to execute or cancel.

**If user selects "Modify"**, let them describe changes, update the plan, and ask for approval again.

### Phase 6: Execute Stack Creation

#### Step 0: Create Backup Commit

**This is the critical safety step.** Before touching any files, commit ALL changes to a backup branch:

```bash
# Record original branch for return
ORIGINAL_BRANCH=$(git branch --show-current)

# Create backup branch with timestamp
BACKUP_BRANCH="stack-plan-backup-$(date +%s)"
git checkout -b "$BACKUP_BRANCH"

# Stage and commit ALL changes (including untracked)
git add -A
git commit -m "stack-plan: backup of all changes"

# Record the backup commit SHA
BACKUP_COMMIT=$(git rev-parse HEAD)

# Return to original branch
git checkout "$ORIGINAL_BRANCH"
```

Display the recovery reference:

```
Starting Stack Creation
=======================
Total branches to create: N
Progress: [0/N]

Safety Backup Created
---------------------
Backup branch: <backup-branch-name>
Backup commit: <commit-sha>

RECOVERY: If anything goes wrong, run:
  git checkout <backup-branch-name>

All your changes are safely committed. Proceeding...
```

**For each branch**, follow these steps:

#### Step 1: Report Current State

```
[X/N] Creating branch: <branch-name>
=====================================
Backup commit: <commit-sha>
Files for this branch: <file-list>
Branches created so far: [branch-1, branch-2, ...] (or "none" for first)
```

#### Step 2: Execute Branch Creation

**For each branch** (same process for all):
```bash
# 1. Checkout ONLY the files for this branch from the backup commit
#    This stages them automatically and excludes all other files
git checkout "$BACKUP_COMMIT" -- <file1> <file2> ...

# 2. Verify files are staged (safety check)
git diff --cached --stat

# 3. Create the stacked branch
echo "<commit-message>" | command stackit create <branch-name> --no-interactive

# 4. Verify the branch was created with commits (not empty)
#    Check that we're on the new branch and it has the expected files
git log -1 --stat

# 5. Run build verification
<check-command>
```

**Why checkout from backup commit?**
- Only the specified files appear in the working tree
- Files are automatically staged
- Other files from the backup are NOT present (natural isolation)
- No stash management needed

**CRITICAL: Verify branch creation succeeded** before continuing. Check that:
- `stackit create` output shows "Created branch X with Y commits" (not "created a branch with no commit")
- `git log -1` shows the expected commit message and files

If the branch was created empty, STOP and report the error immediately.

#### Step 3: Handle Results

**On build success**: Update progress and continue to the next branch:
```
[X/N] ✓ Branch <branch-name> created and verified
```

**On build failure**: STOP immediately and report:
```
[X/N] ✗ Build Failed
====================
Branch: <branch-name>
Error output:
<error-details>

Recovery Options
----------------
Your backup branch is intact: <backup-branch-name>

To recover ALL original changes:
  git checkout <backup-branch-name>

To see what's in the backup:
  git show <backup-commit> --stat
```

Then use `AskUserQuestion`:
- Header: "Build failed"
- Question: "The build failed on branch <branch-name>. How would you like to proceed?"
- Options:
  - "Continue" - I've fixed the issue, resume from next branch
  - "Undo last" - Rollback <branch-name> with `command stackit undo`
  - "Rollback all" - Undo all created branches and restore from backup
  - "Stop here" - Keep created branches, I'll handle it manually

**If user selects "Rollback all"**:
```bash
# Undo each created branch in reverse order
command stackit undo --yes  # Undo branch X
command stackit undo --yes  # Undo branch X-1
# ... repeat for all created branches

# Restore all changes from backup
git checkout "$BACKUP_BRANCH" -- .
```
Then report: "All branches rolled back. Your original changes are restored to the working tree. Backup branch `<backup-branch-name>` still exists for reference."

**On branch creation failure** (e.g., empty branch created):
```
[X/N] ✗ Branch Creation Failed
==============================
Branch <branch-name> was created but appears to be empty.

This can happen if the files weren't properly staged.

Recovery: git checkout <backup-branch-name>
```

STOP and do not continue. The backup branch preserves all changes.

### Phase 7: Report Completion

After all branches are created successfully:

```bash
# Delete the backup branch (no longer needed)
git branch -D "$BACKUP_BRANCH"

# Show the final stack
command stackit log --no-interactive
```

Present a summary:
```
Stack Created Successfully
==========================
[N/N] Complete ✓

Created N branches from Y files:
1. ✓ branch-name-1 (X files)
2. ✓ branch-name-2 (Y files)
3. ✓ branch-name-3 (Z files)

Backup branch deleted (changes now live in stack).

Stack structure:
<stackit log output>

Next steps:
- Run /stack-submit to create PRs
- Run /stack-verify to re-verify all branches
```

## Error Handling Reference

| Phase | Scenario | Backup State | Recovery |
|-------|----------|--------------|----------|
| Gather | No changes detected | N/A | Inform user and exit |
| Analyze | Branch name exists | N/A | Choose different name or ask user |
| Analyze | File needs hunk-level split | N/A | Ask user to split manually |
| Pre-validate | Build fails | N/A | User fixes, changes in working tree |
| Approval | User cancels | N/A | Exit - changes in working tree |
| Backup creation | Commit fails | N/A | Report error, changes in working tree |
| Execution | Checkout from backup fails | Backup exists | `git checkout <backup-branch>` |
| Execution | Create produces empty branch | Backup exists | STOP, `git checkout <backup-branch>` |
| Execution | Build fails | Backup exists | Offer options, `git checkout <backup-branch>` |
| Execution | User selects "Rollback all" | Backup exists | Undo branches, `git checkout <backup> -- .` |
| Complete | Success | Deleted | All changes in stack branches |

**Key guarantees**:
- **Backup commit created BEFORE any file manipulation** - changes are in git history
- **Backup branch survives all failures** - only deleted on complete success
- **Recovery is always possible** via `git checkout <backup-branch>`
- **No stash usage** - avoids stash's confusing behavior with staged files
- **Empty branch detection** - stops immediately if `stackit create` produces empty branch

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior. Assume `stackit create` will succeed if staged changes exist.

## Do NOT
- Create branches without user approval of the plan
- Continue past a build failure
- Continue if a branch is created empty (verify each branch has commits)
- Put the same file in multiple branches
- Use `git commit` directly - always use `command stackit create`
- Make up branch names without showing the user first
- Skip build verification (unless user explicitly says to)
- Delete the backup branch until ALL branches are successfully created and verified
- Use `git stash` for isolating changes - use the backup commit approach instead
- Propose branch names that already exist
- Run `stackit undo` without first checking that the backup branch still exists
