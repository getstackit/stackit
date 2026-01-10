---
description: Create a new stacked branch with intelligent naming
allowed-tools: Bash(stackit:*), Bash(git:*), Read
argument-hint: [optional-branch-name]
---

# Stack Create

Create a new stacked branch on top of the current branch.

## CRITICAL: Required Workflow

**You MUST stage changes BEFORE running `command stackit create`.** The command creates a branch AND commits staged changes in one atomic operation.

```bash
# CORRECT workflow:
git add -A                                    # 1. Stage changes FIRST
echo "message" | command stackit create --no-interactive  # 2. Then create

# WRONG - NEVER do this:
command stackit create -m "..."   # Creates empty branch!
git commit -m "..."       # BYPASSES stackit - FORBIDDEN!
```

## FORBIDDEN Commands

When creating new stacked branches, NEVER use:
- `git commit` - Always use `command stackit create` instead
- `git checkout -b` - Use `command stackit create` which handles both

## Context
- Current branch: !`git branch --show-current`
- Staged changes: !`git diff --cached --stat`
- Staged diff: !`git diff --cached | head -200`
- Recent commits: !`git log --oneline -3 2>&1`
- Stack state: !`command stackit log --no-interactive 2>&1`

## Arguments
$ARGUMENTS

## Instructions

1. **Check for staged changes first** - If no staged changes, ask what to stage or run `git add --all`
2. If on trunk (main/master), confirm user wants to proceed
3. Generate a commit message:
   - Analyze the staged diff to understand what changed
   - Check README.md or CONTRIBUTING.md for commit conventions
   - Consider matching the style of recent commits if present
4. **Ensure changes are staged**, then run: `echo "commit message" | command stackit create [branch-name] --no-interactive`
   - Branch name is optional; stackit auto-generates from commit message
5. Show new stack state

## Do NOT
- Run `command stackit create` without staged changes (creates empty branch)
- Use `git commit` after creating a branch - this bypasses stackit
- Skip reading project conventions if they exist
