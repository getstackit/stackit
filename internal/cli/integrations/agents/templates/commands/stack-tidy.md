---
description: Clean up commits across the stack by squashing fixup/WIP commits
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), AskUserQuestion, Skill
---

# Stack Tidy

Clean up commits across the stack by squashing fixup/WIP commits into meaningful history.

## Context
- Current branch: !`git branch --show-current`
- Uncommitted changes: !`git status --short`
- Stack info (branches, parents, commit messages, diff stats): !`stackit info --stack --json --no-interactive`

## Task

**Precondition:** if the Context shows uncommitted changes, stop and ask the user
to commit (via `stackit create`/`stackit modify`), stash, or abort before tidying.
Squashing checks out and rewrites branches, which fails on a dirty working tree.

### Step 1: Gather Commit Data

For each non-trunk branch in the stack (from the JSON info), get the commit messages:

```bash
git log --format="%H %s" <parent>..<branch>
```

### Step 2: Classify Commits

For each commit, classify as **cleanup** or **meaningful**:

**Cleanup/noise** — message matches any of:
- Starts with `fixup!` or `squash!`
- Exact or near-exact match (case-insensitive): `wip`, `tmp`, `temp`, `fix`, `oops`, `typo`, `lint`, `fmt`, `cleanup`, `clean up`, `nit`, `tweak`
- Contains (case-insensitive): `fix typo`, `address review`, `review feedback`, `lint fix`, `formatting`, `minor fix`, `small fix`
- Single-word message under 10 characters

**Meaningful** — message that:
- Follows conventional commit format (`type(scope): desc` or `type: desc`)
- Descriptive message >15 characters not matching cleanup patterns
- First/oldest commit on the branch (gets benefit of the doubt)

### Step 3: Assign Per-Branch Strategy

| Condition | Strategy | Action |
|-----------|----------|--------|
| 0 or 1 commits | **SKIP** | Nothing to tidy |
| 1 meaningful + rest noise | **SQUASH** | `printf '%s\n' "<meaningful msg>" \| stackit squash -F - --no-interactive` |
| 0 meaningful (all noise) | **SQUASH** | `stackit squash --no-edit --no-interactive` (keeps oldest msg) |
| 2+ meaningful commits | **REVIEW** | Show commits, suggest manual action |

### Step 4: Present Plan

Show the full tidy plan to the user. For each branch:
- Branch name and commit count
- List of commits with classification (cleanup/meaningful)
- Proposed strategy and reasoning
- Proposed squash message (if applicable)

### Step 5: Confirm

Use `AskUserQuestion`:
- Header: "Stack Tidy Plan"
- Question: "Ready to execute the tidy plan?"
- Options:
  - label: "Execute all"
    description: "Squash all SQUASH branches (REVIEW branches will be skipped)"
  - label: "Review one by one"
    description: "Confirm each branch individually before squashing"
  - label: "Cancel"
    description: "Abort without changes"

### Step 6: Execute Bottom-Up

Process branches **closest to trunk first** (bottom-up). For each SQUASH branch:

1. `stackit checkout <branch> --no-interactive`
2. Run the squash command from Step 3
3. Show result

Skip REVIEW and SKIP branches.

### Step 7: Show Results

Show final stack state with `stackit tree --no-interactive`.

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Do NOT
- Squash branches the user hasn't approved
- Squash branches with only 1 commit
- Process branches top-down (children before parents) — always bottom-up
- Modify REVIEW branches without explicit user instruction
- Use `git rebase -i` — use `stackit squash` instead

## Follow-up

After successful tidy, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Stack tidied successfully. What would you like to do next?"
- Options:
  - label: "Submit changes"
    description: "Push tidied changes to update PRs"
  - label: "View final stack"
    description: "Show the updated stack state"
  - label: "Done for now"
    description: "No follow-up action needed"

Based on response:
- **"Submit changes"**: Invoke `/stack-submit` skill using the `Skill` tool
- **"View final stack"**: Run `stackit tree --no-interactive` and display
- **"Done for now"**: End with summary of what was tidied
