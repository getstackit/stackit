---
description: Absorb working changes into correct commits with intelligent fix sourcing
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Edit, Glob, Grep, AskUserQuestion, Skill
---

# Stack Absorb

Absorb staged changes into the correct commits, then intelligently fix any broken branches by drawing from the right sources.

## Context
- Current branch: !`git branch --show-current`
- Git status: !`git status --short`
- Stack state: !`stackit log --no-interactive`

## How Absorb Works

Absorb assigns each change to the commit that last modified those lines. This may break branches when absorbed changes depend on code that couldn't be absorbed (e.g., new functions defined in unabsorbable hunks). The `--json` flag tells us WHERE to find fixes:

- **Unabsorbable hunks**: Staged changes that commuted with everything
- **New files**: Files that weren't in any commit (can't be absorbed)
- **Absorbed upstack**: Code that got absorbed to a child branch but is needed by a parent

## Instructions

### Phase 1: Absorb with JSON Output

```bash
stackit absorb --json --force --no-interactive
```

Parse the JSON output to understand:
- What got absorbed and to which branches (`absorbed` array)
- What couldn't be absorbed and why (`unabsorbable` array)
- New files that exist (`new_files` array)
- The stack structure (`stack` array)

Save this information - you'll need it to find fixes.

### Phase 2: Find Broken Branches

**Determine the project's build command:**
1. Check README.md or CONTRIBUTING.md for build/test instructions
2. Look for common build files (Makefile, package.json scripts, etc.)
3. If not found, use `AskUserQuestion`:
   - Header: "Build command"
   - Question: "What command verifies the build?"
   - Options:
     - "Skip verification" - Don't verify after absorb
     - "Let me specify" - I'll provide the command

```bash
stackit foreach --stack --json --find-first-failure --jobs 0 "<build-command>"
```

Parse `.results` in the foreach JSON output and find entries whose `status` is not `"done"` or whose `exit_code` is non-zero. `--find-first-failure` stops before descendant depths, so the failed results are the earliest failing branches. The failing branch is where to fix.

**If all branches pass**: Done! Absorb succeeded.

### Phase 3: Fix from the Right Sources

For each broken branch, identify what's missing by reading the error.

**Source 1: Unabsorbable hunks**
- Check the JSON's `unabsorbable` array for the missing code
- If found, apply it to the failing branch:
  ```bash
  stackit checkout <failing-branch> --no-interactive
  # Edit the file to add the missing code from the unabsorbable hunk content
  git add <files>
  printf 'fix: add <missing-item> dependency' | git commit -F -
  stackit restack --branch <failing-branch> --upstack --no-interactive
  ```

**Source 2: New files**
- Check the JSON's `new_files` array
- If the missing code is in a new file, copy relevant parts:
  ```bash
  stackit checkout <failing-branch> --no-interactive
  # Copy the relevant code from the new file
  git add <files>
  printf 'fix: add <missing-item> from new file' | git commit -F -
  stackit restack --branch <failing-branch> --upstack --no-interactive
  ```

**Source 3: Absorbed upstack (bring down)**
- Check the JSON's `absorbed` array for hunks that went to child branches
- If the missing code was absorbed to a child, it needs to come DOWN:
  ```bash
  stackit checkout <failing-branch> --no-interactive
  # Apply the code from the absorbed hunk content
  git add <files>
  printf 'fix: bring down <missing-item> from upstack' | git commit -F -
  stackit restack --branch <failing-branch> --upstack --no-interactive
  ```

### Phase 4: Verify Fix

```bash
stackit foreach --branch <failing-branch> --upstack --json --find-first-failure --jobs 0 "<build-command>"
```

If another branch fails, repeat Phase 3 for that branch.

### Phase 5: Cleanup

- Remaining staged changes that are now redundant can be unstaged
- New files that were distributed should be committed on the appropriate branch
- Run final verification

## Fix Sourcing Logic

When branch X fails with "undefined: foo":
1. Search `unabsorbable` hunks for "foo" definition
2. Search `new_files` for "foo" definition
3. Search `absorbed` hunks (especially those targeting X's children) for "foo" definition
4. Apply the relevant code to branch X

## Example

```
$ stackit absorb --json --force --no-interactive

JSON shows:
- absorbed: validateUser() call -> add-login branch
- unabsorbable: hashPassword() definition (commutes_with_all)

$ stackit foreach --stack --json --find-first-failure --jobs 0 "<build-command>"

JSON shows:
- add-auth: PASS
- add-login: FAIL - undefined: hashPassword

Looking for hashPassword in unabsorbable hunks... Found!

$ stackit checkout add-login --no-interactive
# Edit utils/crypto.go to add hashPassword from unabsorbable content
$ git add utils/crypto.go
$ printf 'fix: add hashPassword dependency' | git commit -F -
$ stackit restack --branch add-login --upstack --no-interactive

$ stackit foreach --branch add-login --upstack --json --find-first-failure --jobs 0 "<build-command>"
All branches pass!
```

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Confidence Threshold

Only apply fixes you're 90%+ confident about. When sourcing fixes from unabsorbable hunks or absorbed code, verify the fix matches the error before applying. Better to ask than to introduce new bugs.

## Do NOT
- Skip the JSON analysis (it tells you where to find fixes)
- Apply fixes to multiple branches manually (fix at source, restack propagates)
- Leave broken builds
- Loop indefinitely on fixes (max 2 attempts per branch)

## Max Attempts Recovery

If max attempts (2) are reached for a branch, use `AskUserQuestion`:
- Header: "Fix attempts"
- Question: "Unable to fix after 2 attempts on this branch. How should I proceed?"
- Options:
  - "Undo absorb" - Run `stackit undo` to rollback
  - "Stop here" - Keep state, I'll fix manually
  - "Skip this branch" - Continue fixing other branches

## Follow-up

After successful absorb (all branches pass), use `AskUserQuestion`:
- Header: "Next step"
- Question: "Changes absorbed successfully. What would you like to do next?"
- Options:
  - label: "Restack to propagate (Recommended)"
    description: "Rebase affected branches to ensure consistency"
  - label: "Submit changes"
    description: "Push absorbed changes to update PRs"
  - label: "Done for now"
    description: "No follow-up action needed"

Based on response:
- **"Restack to propagate"**: Invoke `/stack-restack` skill using the `Skill` tool
- **"Submit changes"**: Invoke `/stack-submit` skill using the `Skill` tool
- **"Done for now"**: End with summary of what was absorbed
