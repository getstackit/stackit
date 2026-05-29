---
description: View current stack state and health
model: haiku
allowed-tools: Bash(stackit:*), Bash(git:*)
---

# Stack Status

Show the current stack state, identify issues, and provide actionable recommendations.
## Context
- Current branch: !`git branch --show-current`
- Git status: !`git status --short`
- Stack state (json — PR/CI status, structure): !`stackit log --json --no-interactive`
- Stack info (json — per-branch needs_restack, locked/frozen, scope): !`stackit info --stack --json --no-interactive`

## Task

### Step 1: Display Stack Overview

Summarize from the JSON context:
- Current position in the stack (the branch with `is_current: true`)
- Parent/child relationships
- Any branches that need attention

If the user wants the visual tree, run `stackit log --no-interactive` and show it.

### Step 2: Health Analysis

Parse the stack state JSON and branch info JSON, then highlight issues:

**High Priority Issues (act now):**
- CI failing on any branch
- Merge conflicts detected

**Medium Priority Issues (address soon):**
- Branches needing restack
- Locked/frozen branches blocking expected changes

**Low Priority (when convenient):**
- Branches ready to merge (approved + CI passing)
- Branches without PRs

### Step 3: Recommendations

Based on the stack state, provide actionable next steps:

| Issue | Recommendation |
|-------|----------------|
| One branch/stack needs restack | `stackit restack --branch <branch-or-root> --upstack --no-interactive` |
| Multiple independent stacks need restack | `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive` |
| Branches have no PR | `stackit submit --no-interactive` |
| CI failing | Check CI logs and fix issues |
| Branch ready to merge | `stackit merge <branch> --no-interactive` |
| Branches need trunk updates | `stackit sync --no-interactive` |

### Step 4: Summary

End with a brief summary:
- Total branches in stack
- Overall health status (healthy/needs attention/issues found)
- Primary recommended action (if any)

## Example Output

```
Stack Status
============

Current: feature-api (3 commits, +45/-12)
  ↑ feature-models (2 commits, +30/-5) - needs restack
  ↑ main

Health:
  ⚠️  feature-models: needs restack
  ✓  feature-api: CI passing, ready for review

Recommendations:
  1. Run `stackit restack --branch feature-models --upstack --no-interactive` to update feature-models and descendants
  2. Run `stackit submit` to create PR for feature-api

Summary: 2 branches, 1 needs attention
```
