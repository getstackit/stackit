---
description: Submit branches as PRs with auto-generated descriptions
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), Read, AskUserQuestion, Skill
argument-hint: [--stack | --draft]
---

# Stack Submit

Submit branches to GitHub and create/update PRs.

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`

## Arguments
$ARGUMENTS

## Instructions

### Step 1: Pre-flight Checks

Check the branch info JSON:
- If parent branch doesn't exist or was deleted, run `stackit sync --no-interactive` first
- If there are uncommitted changes, warn the user

### Step 2: Identify Branches Needing PRs

From the stack state, identify which branches need PR descriptions generated:
- Branches without existing PRs need full title + body generation
- Branches with PRs may just need updates

### Step 3: Submit PRs

Run submit command:

**Current branch and its ancestors (default):**
```bash
stackit submit --no-interactive
```

**Entire stack (also includes descendants):**
```bash
stackit submit --stack --no-interactive
```

**As drafts:** add `--draft`

Under `--no-interactive`, stackit does **not** prompt for title/body — the PR
title comes from each branch's commit subject and the stack footer is
auto-generated. The interactive `--edit-title`/`--edit-description` flags don't
apply here. To control content non-interactively:
- **Title**: write a clear commit subject (under 72 chars); it becomes the PR title.
- **Stack description/footer**: set it before submitting with
  `stackit describe -m "<title>" -d "<body>" --no-interactive` (see `/stack-describe`).

Check `.github/pull_request_template.md` and `CONTRIBUTING.md` for format requirements.

### Step 4: Report Results

Report the created/updated PR URLs to the user.

## Do NOT
- Submit if parent branch was deleted (run sync first)
- Create PRs with placeholder content ("TODO", "TBD", empty sections)

## Follow-up

After successful submit, use `AskUserQuestion`:
- Header: "Next step"
- Question: "PRs submitted successfully. What would you like to do next?"
- Options:
  - label: "Sync with trunk (Recommended)"
    description: "Pull latest changes from trunk and update stack"
  - label: "Check PR status"
    description: "View full stack status with PR links"
  - label: "Done for now"
    description: "No follow-up action needed"

Based on response:
- **"Sync with trunk"**: Invoke `/stack-sync` skill using the `Skill` tool
- **"Check PR status"**: Run `stackit tree full --no-interactive`
- **"Done for now"**: End with summary including PR URLs from the submit output
