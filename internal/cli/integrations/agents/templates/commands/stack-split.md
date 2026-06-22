---
description: Split committed changes between current branch and a new branch (parent or child)
model: opus
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Write, Glob, Grep, AskUserQuestion
argument-hint: [check-command]
---

# Stack Split

Split the committed changes on the current branch between this branch and a new branch. Supports both **file-level** and **hunk-level** splitting.

**Split methods:**
- `--by-file <files>`: Extract entire files (simplest, recommended for file-level splits)
- `--patch <file>`: Extract specific hunks using a patch file (for fine-grained control)
- `--by-hunk`: Interactive TUI for selecting hunks

**Split directions (works with all methods):**
- `--above` (upstack): Extract to a NEW CHILD branch above current
- `--below` (downstack, default): Extract to a NEW PARENT branch below current

**Preview mode:**
- `--dry-run`: Show what will happen without executing

**Key difference from related skills:**
- `/stack-plan` - Creates N branches from **uncommitted** changes (from scratch)
- `/stack-extract` - Extracts commits/files to sibling or parent branches
- `/stack-split` - Binary split of **committed** changes at file or hunk level

**Primary objective: Never lose someone's work.**

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`

## Arguments
$ARGUMENTS

## Preconditions

**This skill requires a clean working directory.** Before proceeding, verify there are no uncommitted changes.

## Safety Model

This workflow uses **stackit's undo system** to ensure commits are never lost:

1. **Before execution**: Stackit automatically takes a snapshot of the current state
2. **During execution**: The split command handles all git operations safely
3. **On failure**: Use `stackit undo` to restore the previous state

Recovery is always: `stackit undo`

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
- Stash changes: git stash
- Commit changes: git add -A, then stackit modify (amend current commit) or stackit create (new branch)
- Discard changes: git checkout -- .

Then re-run /stack-split
```

**Stop and exit.** Do not proceed with uncommitted changes.

**If output is empty**: Continue to Phase 1.

### Phase 1: Gather Committed Changes

Get the commits on the current branch that will be analyzed for splitting:

```bash
# Get parent branch from stackit metadata
stackit tree --no-interactive

# Survey size first (bounded), then read what you need
git diff --stat <parent-branch>..HEAD
# Full diff of all changes that could be split. For a large diff, avoid dumping
# everything into context — read per-file instead:
#   git diff <parent-branch>..HEAD -- <path>
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

**If branch has commits**: Continue to Phase 1.5.

### Phase 1.5: Dependency Analysis

Before proposing a split, analyze relationships between changed files to avoid breaking dependencies.

**Step 1: Check for project documentation**

Look for dependency/build guidance:
```bash
# Check for project-specific guidance
ls README.md CONTRIBUTING.md DEVELOPMENT.md docs/CONTRIBUTING.md 2>/dev/null
```

If found, read them to understand:
- How the project is structured
- Import/module conventions
- Test file conventions (e.g., `*_test.go`, `*.test.ts`, `__tests__/`)

**Step 2: Identify potential dependencies between changed files**

For each file being considered for extraction, check if kept files reference it:

```bash
# Get list of changed files
git diff --name-only <parent-branch>..HEAD

# For each file that might be extracted, search for references in other changed files
# Extract identifiers (function names, exports, class names, type definitions)
# Search kept files for those identifiers
rg -l "<identifier>" <other-changed-files>
```

**Language-agnostic patterns to check:**
- **Import statements**: `import`, `require`, `from ... import`, `#include`, `use`
- **Function/class names**: grep for identifiers defined in one file, used in another
- **Type definitions**: structs, interfaces, classes, enums defined in extracted files
- **Constants/variables**: exported values used across files

**Step 3: Flag potential dependency conflicts**

If a kept file appears to use symbols from an extracted file:

```
⚠️ Potential Dependency Conflict
================================
File to extract: internal/tui/style/formatter.go
  - Defines: IconInfo, IconWarning (functions)

File to keep: internal/actions/health.go
  - Uses: style.IconInfo (line 488)

Options:
1. Keep formatter.go with health.go (recommended)
2. Extract both files together
3. Proceed anyway (may break build)
```

Use `AskUserQuestion` to resolve:
- Header: "Dependency conflict"
- Question: "<kept-file> uses <symbol> from <extract-file>. How should we handle this?"
- Options:
  - "Keep together (Recommended)" - Move extract-file to keep list
  - "Extract both" - Move kept-file to extract list
  - "Proceed anyway" - I'll handle the dependency manually

**Continue to Phase 2** after resolving any conflicts.

### Phase 2: Analyze and Propose Split (Hunk-Level)

Analyze the changes at the **hunk level**. A hunk is a contiguous block of changes within a file.

**Related file detection** (check before proposing split):

Common patterns where files should stay together:
- **Implementation + Test**: `foo.go` and `foo_test.go`, `bar.ts` and `bar.test.ts`
- **Interface + Implementation**: Files defining types used by other changed files
- **Helper + Consumer**: Utility files and files that import them
- **Config + User**: Configuration files and code that reads them

Before proposing a split, verify:
1. Test files go with their implementation files
2. Files that define symbols stay with files that use those symbols
3. Helper/utility files stay with their consumers

If unsure, check the project's README or CONTRIBUTING file for guidance on code organization.

**Split Criteria:**

**Keep on current branch:**
- Hunks implementing the core feature/bug fix that motivated the work
- Hunks for directly related tests
- Changes essential to the branch's stated purpose
- Related hunks (e.g., if a function definition stays, its usages should too)

**Extract to new branch:**
- Tangential improvements discovered while working
- Refactoring not strictly necessary for the core feature
- Documentation changes unrelated to the core feature
- Infrastructure/config changes
- New functionality that grew out of scope
- Cleanup/formatting changes unrelated to the core work

**Choosing direction:**
- Use `--above` when the extracted changes logically come AFTER the core work (e.g., follow-up refactoring)
- Use `--below` (default) when the extracted changes are prerequisites (e.g., infrastructure changes needed before the feature)

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

**Otherwise, determine the build command:**

**Step 1: Check project documentation first**

```bash
# Look for build/test instructions
cat README.md CONTRIBUTING.md 2>/dev/null | head -100
```

Look for sections like:
- "Development", "Building", "Testing", "Getting Started"
- Commands mentioned: `make`, `npm`, `cargo`, `go`, `mise`

**Step 2: Auto-detect if not documented**

1. Check for `mise.toml` → use `mise run check` or `mise run test`
2. Check for `Makefile` → use `make test` or `make check`
3. Check for `package.json` → use `npm test` or `npm run check`
4. Check for `Cargo.toml` → use `cargo check` or `cargo test`
5. Check for `go.mod` → use `go build ./...` or `go test ./...`

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
New branch to create: <new-branch>
Direction: above (child) / below (parent)

Commits being split: N commits

Keep on [current-branch] (X hunks, Y files):
  - src/auth.go:42-58 (validateUser function)
  - src/auth.go:102-110 (error handling)

Extract to [new-branch] (A hunks, B files):
  - src/auth.go:15-20 (import cleanup)
  - src/utils.go:5-30 (new helper)

Each branch will be verified with: <check-command>
```

Use `AskUserQuestion` to get approval:
- Header: "Split plan"
- Question: "Ready to split these changes?"
- Options:
  - "Execute" - Create the split with verification
  - "Dry run" - Preview what will happen without executing
  - "Swap" - Exchange keep/extract categories
  - "Modify" - Change the split classification
  - "Cancel" - Exit without making changes

**If user selects "Dry run"**, use the `--dry-run` flag to preview:

For file-level splits:
```bash
stackit split --by-file <files-to-extract> --above --dry-run \
    --name "<new-branch-name>" \
    --message "<commit-message>"
```

For hunk-level splits (write patch first, then):
```bash
stackit split --patch /tmp/extract.patch --above --dry-run \
    --name "<new-branch-name>" \
    --message "<commit-message>"
```

The `--dry-run` output shows:
- Current branch and new branch names
- Direction (above/below/sibling)
- Files that will be extracted
- No actual changes are made

After showing the preview, ask again whether to execute or cancel.

**If user selects "Swap"**, exchange the keep/extract classifications and present the updated plan for approval.

**If user selects "Modify"**, let them describe changes, update the plan, and ask for approval again.

### Phase 5: Execute Split

Choose the appropriate method based on your split type:

#### Option A: File-Level Split (Recommended for extracting entire files)

Use `--by-file` when extracting complete files (not individual hunks within files):

```bash
# Extract files to a child branch (upstack):
stackit split --by-file path/to/file1.go path/to/file2.go --above \
    --name "<child-branch-name>" \
    --message "<commit-message>"

# Extract files to a parent branch (downstack, default):
stackit split --by-file path/to/file1.go path/to/file2.go \
    --name "<parent-branch-name>" \
    --message "<commit-message>"
```

**Advantages of `--by-file`:**
- No patch file generation needed
- Simpler command
- Works with new files without special handling
- Supports `--dry-run` for preview

#### Option B: Hunk-Level Split (For fine-grained splitting)

Use `--patch` when you need to split changes within files at the hunk level:

**Step 1: Generate Extract Patch**

Write the extract patch to a temporary file using the Write tool. This patch contains the hunks to move to the new branch.

**The extract patch must be in valid unified diff format:**
- Include proper `diff --git` headers
- Include `--- a/file` and `+++ b/file` lines
- Include `@@ ... @@` hunk headers

Example:
```diff
diff --git a/src/utils.go b/src/utils.go
--- a/src/utils.go
+++ b/src/utils.go
@@ -8,3 +8,8 @@ func existingFunc() {
 }

+func newHelper() {
+    // This hunk is being extracted
+}
```

**Handling new files**: A new file is a single hunk. Include the entire file in extract.patch.

**Handling deleted files**: A deleted file is a single hunk. Include the deletion if it should be in the new branch.

Write the patch to `/tmp/extract.patch` using the Write tool.

**Step 2: Run Split Command**

```bash
# For --above (extract to child branch):
stackit split --patch /tmp/extract.patch --above \
    --name "<child-branch-name>" \
    --message "<commit-message>"

# For --below (extract to parent branch, default):
stackit split --patch /tmp/extract.patch \
    --name "<parent-branch-name>" \
    --message "<commit-message>"
```

#### What the split command does internally:

1. Takes a snapshot for undo support
2. Resets the branch to expose all changes
3. Stages the specified files/hunks
4. Keeps remaining changes on the current branch
5. Creates the new branch with extracted changes
6. Reparents any existing children appropriately

**Note**: Stackit handles all git operations safely. No manual git reset or backup branch is needed.

#### Step 3: Verify Build

After the split completes:

```bash
# Verify the child branch builds
<check-command>

# Switch to parent and verify it also builds
stackit checkout <current-branch> --no-interactive
<check-command>
```

**On build failure**: Report and offer options (see Error Handling).

### Phase 6: Report Completion

After both branches are verified successfully:

```bash
# Show the final stack
stackit tree --no-interactive
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

Stack structure:
<stackit tree output>

Recovery: If anything is wrong, run `stackit undo` to restore.

Next steps:
- Run /stack-submit to create/update PRs
- Run /stack-verify to re-verify all branches
```

## Error Handling Reference

| Phase | Scenario | Recovery |
|-------|----------|----------|
| Phase 0 | Uncommitted changes | Inform and exit - user must commit/stash first |
| Phase 1 | No commits on branch | Inform and exit - nothing to split |
| Phase 1.5 | Dependency conflict detected | Ask user: keep together / extract both / proceed |
| Phase 2 | All hunks same category | Inform user - splitting not needed |
| Phase 4 | User cancels | Exit - no changes made |
| Phase 5 | Split command fails | `stackit undo` |
| Phase 5 | Build fails | Offer: Continue/Rollback |

**On any execution failure**, use `AskUserQuestion`:
- Header: "Build failed"
- Question: "The build failed on <branch>. How would you like to proceed?"
- Options:
  - "Continue anyway" - I've fixed it or will fix later
  - "Rollback" - Restore previous state and exit
  - "Stop here" - Keep current state, I'll handle manually

**If user selects "Rollback"**:
```bash
stackit undo
```

Then report: "Rolled back to original state. Your commits are restored."

## Patch File Format (for hunk-level splits only)

**Note:** If you're extracting entire files, use `--by-file` instead - no patch file needed.

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

## Mode Selection Guide

| Scenario | Use Mode |
|----------|----------|
| Extract entire files to child (upstack) | `--by-file <files> --above` |
| Extract entire files to parent (downstack) | `--by-file <files>` (or `--below`) |
| Split hunks within files | `--patch` or `--by-hunk` |
| Extract specific commits | `--by-commit` |
| Interactive hunk selection | `--by-hunk` (default) |
| Preview without executing | Add `--dry-run` to any mode |

**Key differences:**
- `--by-file`: Extracts entire files. Supports `--above` (child) and `--below` (parent) directions. **Recommended for file-level splits.**
- `--patch`: Non-interactive hunk-level split using a patch file you provide
- `--by-hunk`: Interactive TUI for selecting hunks
- `--by-commit`: Splits at commit boundaries
- `--dry-run`: Preview what will happen without executing (works with all modes)

**Direction options (for `--by-file` and `--by-hunk`/`--patch`):**
- `--above`: Extract to a new CHILD branch (upstack)
- `--below` (default): Extract to a new PARENT branch (downstack)
- `--as-sibling`: Extract to an independent branch on the same parent

**Examples** (pipe the message via `-F -`, the `--message-file` shorthand —
consistent with create/modify/squash):
```bash
# Extract files to child branch (upstack)
printf 'Extract utilities' | stackit split --by-file internal/utils.go --above -n "refactor-utils" -F -

# Extract files to parent branch (downstack, default)
printf 'Extract config' | stackit split --by-file internal/config.go -n "config-changes" -F -

# Preview a file-level split without executing
stackit split --by-file internal/utils.go --above --dry-run -n "refactor-utils"

# Hunk-level split using a patch file
printf 'Part 2' | stackit split --patch /tmp/extract.patch --above -n "feature-part-2" -F -
```

**Note:** `--by-file` extracts entire files to the new branch, not just the changes to those files. Use `--patch` or `--by-hunk` for hunk-level splitting within files.

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Confidence Threshold

Only classify hunks you're 90%+ confident about. For ambiguous hunks, ask the user rather than guessing. Incorrect splits require manual cleanup.

## Do NOT
- Proceed if there are uncommitted changes in the working directory
- Create branches without user approval of the plan
- Continue past a build failure without user consent
- Put the same hunk in both patches
- Use `git commit` directly for the child branch - use `stackit create`
- Skip build verification (unless user explicitly says to)
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
