---
description: Create a new stacked branch with commit
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), AskUserQuestion, Skill
argument-hint: [-m "message"] [--scope <scope>] [branch-name]
---

# Stack Create

## Context
- Working tree: !`git status --short --branch`

## Arguments
$ARGUMENTS

## Task

Create a new stacked branch with the current changes.

**Critical:** `stackit create` requires staged changes. It creates a branch AND commits in one atomic operation.

1. Start from the working-tree status above. If there are no changes at all, inform the user and stop.
2. Only if you need to decide whether this should be split, run exactly one targeted path query:
   - Prefer `git diff --cached --name-only`
   - If nothing is staged yet, use `git diff --name-only`
3. **If changed paths span multiple unrelated concerns** (different features, mixed refactoring + features, many directories with different purposes), use `AskUserQuestion`:
   - Header: "Large changes"
   - Question: "These changes span multiple areas. How would you like to proceed?"
   - Options:
     - "Use /stack-plan (Recommended)" → Stop and tell user to run `/stack-plan`
     - "Single commit" → Proceed with one commit
     - "Let me describe" → Wait for user to provide message
4. Stage the intended changes with `git add -A`.
5. If the user provided `-m "message"`, use it.
6. Otherwise, generate a non-empty commit message deterministically:
   - Read repo-local guidance first
   - Run `git log --oneline -5` only now, when style reference is needed
   - Choose the Conventional Commit type from the dominant intent of the staged changes
   - Prefer a one-line subject under 72 characters
7. If the user did not provide a branch name, preserve the repo's configured `branch.pattern` by omitting the branch-name argument. Only pass an explicit branch name when the user asked for one.
8. Run: `printf '%s\n' "<message>" | stackit create -F - [branch-name only if user provided one] [--scope <scope> if provided] --no-interactive`
9. **If create fails due to missing scope** (error mentions scope required):
   - Use `AskUserQuestion`:
     - Header: "Scope"
     - Question: "Branch pattern requires a scope (e.g., feature area, ticket ID). What scope should this branch use?"
     - Options: Generate 2-3 sensible suggestions based on the codebase + user can type custom
   - Retry with `--scope <value>`
10. If creation fails because Git cannot write under `.git/refs/heads` or create a `.lock` file, retry the exact same command with the required permission or approval. Do not change the branch name just to work around that failure.
11. After success, run `stackit tree --no-interactive` and report:
   - branch name
   - parent branch
   - commit subject
   - scope status: explicit, inherited, or none
   - worktree path if created
   - recommended next step

Only run the next shell command needed for the next decision. Avoid eager status gathering.

**Never use:** `git commit` or `git checkout -b` — always use `stackit create`.

## Follow-up

After successful creation, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Branch created. What would you like to do next?"
- Options:
  - "Submit as PR (Recommended)" → Invoke `/stack-submit` using Skill tool
  - "Stack another change" → Tell user to make changes and run `/stack-create`
  - "Done for now" → End with summary
