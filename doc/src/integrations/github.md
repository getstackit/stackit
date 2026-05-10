---
icon: material/github
title: GitHub Actions Integration
description: Automated CI checks for stackit branches. Prevent merging locked PRs and enforce correct merge order with GitHub Actions.
---

# GitHub Integration

Stackit provides GitHub Actions integration to enforce stack discipline in CI/CD pipelines.

## Overview

The GitHub integration adds automated checks to your pull requests:

- **Lock check** — Prevents merging PRs for locked branches
- **Stack order check** — Ensures PRs are merged in the correct order (bottom of stack first)

## Installation

Install the GitHub Action workflow:

```bash
stackit github install
```

This creates `.github/workflows/stackit.yml` in your repository.

!!! note
    You'll need to commit and push this file for the checks to take effect.

### Force Overwrite

If the workflow file already exists:

```bash
stackit github install --force
```

## How It Works

### Lock Check

When a PR is opened or updated, the action checks if the branch is locked:

```
✓ Lock check passed - branch is not locked
```

If the branch is locked:

```
✗ Lock check failed - branch "feature-api" is locked
```

This prevents accidentally merging branches that are still being worked on or coordinated with teammates.

### Stack Order Check

The action verifies that the PR is at the bottom of its stack. A branch is considered ready to merge when its parent is the trunk (e.g. `main`).

If the PR is not at the bottom of the stack, the check fails with a message naming the parent branch that must merge first.

This ensures stacked PRs are merged in order, maintaining clean git history.

## Workflow File

The installed workflow reads the stackit metadata refs directly with `git` and `jq` — no separate stackit installation is required on the runner. The two jobs are:

- **check-lock** — fails if the branch's metadata has `lockReason` set
- **check-stack-order** — fails if the branch's parent is not the trunk

See `.github/workflows/stackit.yml` after running `stackit github install` for the full template.

## Customization

### Branch Protection Rules

For maximum effectiveness, configure GitHub branch protection:

1. Go to **Settings → Branches → Branch protection rules**
2. Add a rule for your main branch
3. Enable **Require status checks to pass before merging**
4. Select the stackit checks

### Required Checks

You can make either check required or optional:

- **Lock check as required** — Strictly prevents merging locked branches
- **Stack order as advisory** — Shows warning but allows merge (for flexibility)

## Troubleshooting

### Checks Not Running

1. Verify the workflow file exists:
   ```bash
   cat .github/workflows/stackit.yml
   ```

2. Check that it's committed and pushed:
   ```bash
   git status
   ```

3. View workflow runs in GitHub Actions tab

### Check Failing Unexpectedly

1. View the check details in the PR — the inline shell script logs which condition failed and why
2. Check branch metadata locally:
   ```bash
   stackit info
   ```
3. View the raw metadata ref:
   ```bash
   git show "refs/stackit/metadata/$(git branch --show-current)"
   ```

## Combining with Local Hooks

For complete protection, use both GitHub Actions and local git hooks:

```bash
# GitHub Actions (CI)
stackit github install

# Local hooks (development)
stackit precommit install
stackit prepush install
```

This provides:

- **Local protection** — Catch issues before pushing
- **CI protection** — Enforce rules for all contributors

## Next Steps

- [Git Hooks →](git-hooks.md) — Local pre-commit and pre-push hooks
- [Team Collaboration →](../workflows/collaboration.md) — Branch locking patterns
- [Other integrations →](index.md)
