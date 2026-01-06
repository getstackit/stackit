---
description: Create a new stacked branch with intelligent naming
allowed-tools: Bash(stackit:*), Bash(git:*), Read
argument-hint: [optional-branch-name]
---

# Stack Create

Create a new stacked branch on top of the current branch. Branch name is optional—stackit will generate one from the commit message.

## Context
- Repo initialized: `stackit init` already run? If not, prompt to run it.
- Current branch: !`git branch --show-current` (warn if trunk: main/master)
- Staged changes: !`git diff --cached --stat 2>/dev/null || echo 'No staged changes'`
- Stack state: !`stackit log --no-interactive 2>/dev/null || echo 'Not initialized'`
- Project guidelines: check `README.md` / `CONTRIBUTING.md` for commit and branch rules

## Arguments
$ARGUMENTS

## Instructions

1. If no staged changes, ask what to stage or offer `git add --all`; abort if user declines.
2. If on trunk, confirm proceeding or suggest switching to a feature branch first.
3. **Generate commit message with validation loop**:
   - Read `README.md` / `CONTRIBUTING.md` for project-specific rules.
   - Follow project conventions if documented; otherwise write a clear, descriptive message.
   - Examples: "Add user authentication to login flow", "Fix timeout handling in API client"

   **Validation loop:**
   - Generate message
   - Verify: Clear and descriptive? Follows project conventions (if documented)?
   - If validation fails: revise and re-validate
   - Only proceed when message meets quality standards

4. **Branch name** (optional):
   - If user provided a branch name: use it
   - Otherwise: allow stackit to auto-generate from the commit message (respects `stackit config branch.pattern`)

5. **Run command using pipe** (preferred):
   - With name: `echo "commit message" | stackit create branch-name --no-interactive`
   - Auto-name: `echo "commit message" | stackit create --no-interactive`
   - Add `--no-verify` only if the user explicitly opts out of hooks.

6. Show new stack state with `stackit log --no-interactive`

## Error Handling
- If not initialized: prompt to run `stackit init`.
- If on trunk: warn and confirm or suggest switching to a feature branch.
- If unstaged changes remain: suggest staging first or abort.
- If create fails: show error and suggest fixes (stage changes, leave trunk, resolve conflicts, or run `stackit doctor`).
