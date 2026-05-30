---
description: Verify stack health by running checks on all branches
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), AskUserQuestion, Skill, Task
argument-hint: [check-command]
---

# Stack Verify

Run verification checks on all branches in the stack and report results.

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`

## Arguments
$ARGUMENTS

## Instructions

### 1. Determine the check command

If provided in arguments, use that. Otherwise:
1. Check README.md or CONTRIBUTING.md for build/test instructions
2. Look for common build files (Makefile, package.json scripts, etc.)
3. If not found, use `AskUserQuestion`:
   - Header: "Check command"
   - Question: "What command should I use to verify the code?"
   - Options:
     - "Skip verification" - Don't run checks
     - "Let me specify" - I'll provide the command

### 2. Run verification

**Quick mode (default)** - run branches at each depth in parallel and stop after the first failing depth:
```bash
stackit foreach --stack --json --find-first-failure --jobs 0 "<check-command>"
```

**Full diagnostic mode** - check all branches and collect every failure:
```bash
stackit foreach --stack --json --parallel --jobs 0 --no-fail-fast "<check-command>"
```

**Anchored mode** - if a branch or independent stack root is known, avoid checkout navigation and verify that upstack only:
```bash
stackit foreach --branch <branch-or-root> --upstack --json --find-first-failure --jobs 0 "<check-command>"
```

If uncertain which mode to use, prompt with `AskUserQuestion`:
- Header: "Verify mode"
- Question: "How should I run verification?"
- Options:
  - "Quick (Recommended)" - Stop at first failure
  - "Full diagnostic" - Check all branches, report all failures

### 3. Parse foreach output

Foreach JSON output looks like:
```json
{
  "status": "failure",
  "total_count": 3,
  "success_count": 2,
  "failure_count": 1,
  "results": [
    {"branch": "branch-1", "status": "done", "exit_code": 0},
    {"branch": "branch-2", "status": "done", "exit_code": 0},
    {"branch": "branch-3", "status": "error", "exit_code": 1, "output": "internal/foo.go:42: undefined: someVar"}
  ]
}
```

Look for results whose `status` is not `"done"` or whose `exit_code` is non-zero. In quick mode, `--find-first-failure` already stopped before descendant depths, so the failed results are the earliest failing branches.

### 4. Present results clearly

**Quick mode format** (stops at first failure):
```
Stack Verification Results
==========================
✓ branch-name-1 - passed
✓ branch-name-2 - passed
✗ branch-name-3 - FAILED

Summary: 2 passed, 1 failed (stopped at first failing depth)
First failing branch: branch-name-3
```

**Full diagnostic format** (checks all):
```
Stack Verification Results
==========================
✓ branch-name-1 - passed
✓ branch-name-2 - passed
✗ branch-name-3 - FAILED (first failure)
✗ branch-name-4 - FAILED

Summary: 2 passed, 2 failed
First failure: branch-name-3
```

### 5. Recommend next steps

- **All pass**: "Stack is healthy and ready for submit"
- **Failures found**: "Run /stack-fix to fix issues. Start by fixing branch-name-3, then restack to propagate the fix to dependent branches."

## Edge Cases

**Uncommitted changes**: Warn user - foreach may fail to checkout branches.

**Not on tracked branch**: Report that verification requires a tracked branch.

**All branches pass**: Clearly report success, suggest `/stack-submit`.

**Check command fails immediately**: Suggest verifying command works manually first.

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Do NOT
- Attempt to fix any issues (that's /stack-fix's job)
- Skip identifying the first-failure branch
- Modify any files or make commits
- Use git checkout directly

## Follow-up

After verification completes, use `AskUserQuestion` based on results:

**If all branches pass:**
- Header: "Next step"
- Question: "All branches pass verification. What would you like to do next?"
- Options:
  - label: "Submit PRs (Recommended)"
    description: "Push branches and create/update pull requests"
  - label: "View stack"
    description: "Show current stack state"
  - label: "Done for now"
    description: "No follow-up action needed"

**If failures found:**
- Header: "Next step"
- Question: "Verification found failures. What would you like to do next?"
- Options:
  - label: "Fix issues (Recommended)"
    description: "Diagnose and fix the failing branches"
  - label: "View details"
    description: "Show more information about failures"
  - label: "Done for now"
    description: "I'll fix manually"

Based on response:
- **"Submit PRs"**: Invoke `/stack-submit` skill using the `Skill` tool
- **"Fix issues"**: Invoke `/stack-fix` skill using the `Skill` tool
- **"View stack"** / **"View details"**: Run `stackit log --no-interactive`
- **"Done for now"**: End with summary of verification results
