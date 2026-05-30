---
description: Diagnose and fix common stack issues
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Edit, Glob, Grep, AskUserQuestion, Skill, Task
---

# Stack Fix

Diagnose and fix stack problems, including build/lint/test failures.

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`
- Diagnostics: !`stackit doctor --no-interactive`

## Instructions

### 1. Identify the Issue Type

Check the context and look for these indicators:

**Rebase in progress** (git status shows "rebase in progress" or "interactive rebase"):
- Follow the Rebase Conflict Resolution Workflow below

**Build/lint/test failures** (user reports build errors, or you see compiler errors):
- Follow the Build Failure Workflow below

**Branches need restack** (stackit log shows "needs restack" or branches are out of sync):
- Restack only the affected independent stack:
  - If the affected branch is known, run `stackit restack --branch <affected-branch> --upstack --no-interactive`.
  - If only the stack root is known, run `stackit restack --branch <stack-root> --upstack --no-interactive`.
  - If several independent stack roots are known, run `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`.
  - If every independent stack should be processed, run `stackit restack --all-stacks --continue-on-conflict --no-interactive`.

**Orphaned branches** (stackit log shows branch with no parent, or parent was merged):
- Run `stackit sync --no-interactive`

**Uncommitted changes** (git status shows modified/untracked files):
- Use `AskUserQuestion`:
  - Header: "Uncommitted"
  - Question: "You have uncommitted changes. How should I proceed?"
  - Options:
    - "Stash them" - Stash and continue, restore after
    - "I'll commit them" - Wait for user to commit
    - "Discard them" - Reset working tree (destructive)

**Not on tracked branch** (current branch not shown in stackit log with ◉):
- Guide user to checkout a tracked branch first

### 2. Rebase Conflict Resolution Workflow

When a rebase is in progress with conflicts:

**Note:** Being in a detached HEAD state during rebase is normal and expected. This is not an error.

#### Step 1: Identify conflicting files
Check `git status` to see which files have conflicts (marked as "both modified" or "UU").

#### Step 2: Resolve conflicts
- Read the conflicting files
- Look for conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`)
- Edit to resolve, keeping the correct version of the code
- Understanding the conflict context helps: HEAD is typically the newer refactored code, the incoming commit is the older version being rebased

#### Step 3: Run checks BEFORE continuing
This is critical - run the project's check command to catch any issues early:
```bash
<build-command>  # Check README.md or CONTRIBUTING.md for the project's build/test command
```

Fix any lint errors, unused variables, or build failures NOW. This prevents having to abort and redo the rebase.

#### Step 4: Stage and continue
```bash
git add -A
stackit continue --no-interactive
```

#### Step 5: If you need to amend a commit, use stackit modify
If you made additional fixes that belong in the current branch's commit, amend with
`stackit modify` (never `git commit --amend`) — it rewrites the commit AND restacks
descendants in one step:
```bash
git add -A
stackit modify --no-edit --no-interactive
```

`stackit modify` auto-restacks children after the amend, so no separate restack is needed.

#### Step 6: Verify
```bash
stackit log  # Should show clean tree, no "needs restack"
<build-command>      # All checks should pass
```

### 3. Build Failure Workflow

#### Step 0: Pre-checks
Before running foreach, verify:
- No uncommitted changes (check git status from context)
- No rebase in progress
- Currently on a tracked branch

If uncommitted changes exist, ask user to commit or stash first.

#### Step 1: Determine the check command
**Find the project's build/test command:**
1. Check README.md or CONTRIBUTING.md for build/test instructions
2. Look for common build files (Makefile, package.json scripts, etc.)
3. If not found, use `AskUserQuestion`:
   - Header: "Check command"
   - Question: "What command should I use to verify the code builds correctly?"
   - Options:
     - "Skip checks" - Don't verify (not recommended)
     - "Let me specify" - I'll provide the command

Optionally, verify the command works on current branch first:
```bash
<check-command>
```

#### Step 2: Run a depth-aware first-failure search
Prefer JSON output so branch, status, exit code, and command output are machine-readable. Use `--find-first-failure` so independent branches at the same depth can run in parallel, and descendants after the first failing depth are skipped.

For the current stack:
```bash
stackit foreach --stack --json --find-first-failure --jobs 0 "<check-command>"
```

For a known stack root or affected branch, avoid checking out first and anchor traversal explicitly:
```bash
stackit foreach --branch <branch-or-root> --upstack --json --find-first-failure --jobs 0 "<check-command>"
```

This starts at the selected root/branch and walks toward leaves. A failing branch at the earliest failing depth is where the bug was introduced. Multiple branches can fail at the same depth; inspect each failed result and fix the branch that matches the reported problem.

**Example JSON output:**
```json
{
  "status": "failure",
  "results": [
    {"branch": "branch-1", "status": "done", "exit_code": 0},
    {"branch": "branch-2", "status": "error", "exit_code": 1, "error": "exit status 1", "output": "undefined: someVariable"}
  ]
}
```

Parse `.results` and find entries whose `status` is not `"done"` or whose `exit_code` is non-zero. The first failing depth is already isolated by `--find-first-failure`.

**If `.status` is `"success"`**: Report success! Stack is healthy. Skip to "After Fixes".

#### Step 3: Checkout the failing branch
```bash
stackit checkout <failing-branch> --no-interactive
```

#### Step 4: Fix the issue
- Read the error output to understand the problem
- Make the necessary code changes
- Stage and commit (pipe the message via stdin so permission rules stay stable):
  ```bash
  git add -A
  printf 'fix: <description>' | git commit -F -
  ```

#### Step 5: Propagate the fix via restack
```bash
stackit restack --branch <failing-branch> --upstack --no-interactive
```

This rebases all child branches onto the fixed branch, propagating your fix.

**If restack has conflicts**:
- Follow the Rebase Conflict Resolution Workflow (Section 2) above

#### Step 6: Verify all branches now pass
```bash
stackit foreach --branch <failing-branch> --upstack --json --find-first-failure --jobs 0 "<check-command>"
```

If it stops at another failure, repeat from Step 2 (there may be multiple independent issues).

### 4. After Fixes

Verify stack is healthy:
- `stackit log` shows clean tree
- All branches pass checks

## Key Insight

**Fix at the SOURCE branch (first failure), then restack to propagate.**

Never fix the same issue on multiple branches - that's painful and error-prone!
The restack command automatically propagates your fix to all child branches.

## Edge Cases

**Uncommitted changes**: Check git status first, ask user to commit/stash.

**Rebase already in progress**: Follow the Rebase Conflict Resolution Workflow (Section 2) before using foreach.

**Not on tracked branch**: Guide user to checkout a tracked branch first.

**Branching stack (multiple children)**: foreach handles this - all descendants are checked.

**Multiple independent stacks**: Independent stack roots are direct children of trunk. If more than one independent stack needs restack, prefer `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`. If all independent stacks need restack, use `stackit restack --all-stacks --continue-on-conflict --no-interactive`.

**Multiple independent failures**: After fixing one, re-run JSON `foreach --find-first-failure` to find the next failing depth. For multiple known independent stack roots, run one anchored `foreach --branch <root> --upstack --json --find-first-failure --jobs 0 "<check-command>"` per root and compare failed JSON results.

**After amending a commit**: Always run `stackit restack --branch <amended-branch> --upstack --no-interactive` because child branches reference the old commit SHA.

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior or check if commands exist.

## Confidence Threshold

Only apply fixes you're 90%+ confident about. If unsure whether a fix is correct, ask the user rather than guessing. False fixes erode trust—better to ask than to introduce new bugs.

## Do NOT
- Fix the same bug on multiple branches manually
- Use `git checkout` directly - use `stackit checkout` instead
- Use `git rebase --continue` - use `stackit continue` instead (it handles metadata properly)
- Use `git rebase --abort` - use `stackit abort` instead
- Make destructive changes without user confirmation
- Loop indefinitely on fixes (max 2 attempts per issue type)
- Skip asking what check command to use if unclear

## Max Attempts Recovery

If max attempts (2) are reached for an issue, use `AskUserQuestion`:
- Header: "Fix attempts"
- Question: "I've tried fixing this issue twice. How should I proceed?"
- Options:
  - "Try again" - One more attempt
  - "Undo changes" - Rollback to before fix attempts
  - "Stop here" - Keep current state, I'll fix manually

## Follow-up

After all issues are fixed and verification passes, use `AskUserQuestion`:
- Header: "Next step"
- Question: "All issues fixed and verified. What would you like to do next?"
- Options:
  - label: "Submit updates (Recommended)"
    description: "Push fixed branches to update PRs"
  - label: "Restack branches"
    description: "Rebase affected branches to ensure consistency"
  - label: "Done for now"
    description: "No follow-up action needed"

Based on response:
- **"Submit updates"**: Invoke `/stack-submit` skill using the `Skill` tool
- **"Restack branches"**: Invoke `/stack-restack` skill using the `Skill` tool
- **"Done for now"**: End with summary of what was fixed
