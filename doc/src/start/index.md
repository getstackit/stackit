---
icon: material/rocket-launch
title: Get Started with Stackit
description: Learn how to install stackit and create your first stack of Git branches in under 5 minutes. Quick walkthrough with examples.
---

# Get started

Welcome to stackit! This guide will help you get up and running with stacked changes in just a few minutes.

## What you'll learn

- [Installing stackit](install.md) on your system
- [Creating your first stack](stack.md) of branches
- [Submitting PRs](submit.md) to GitHub

## Before you begin

Make sure you have:

- **Git 2.36+** installed (`git worktree list -z`, used by every stack operation to see which branches are checked out where)
- **GitHub CLI (`gh`)** for PR operations
- A GitHub repository to work with

## Quick walkthrough

### 1. Install stackit

```bash
brew install getstackit/tap/stackit
```

[Full installation guide →](install.md)

### 2. Initialize in your repository

```bash
cd your-repository
stackit init
```

This detects your trunk branch (usually `main`) and prepares the repo for stacking. You'll be prompted to install optional integrations (GitHub Actions, pre-commit hooks, AI agent files).

### 3. Create your first branch

```bash
git add internal/api.go
stackit create add-api -m "feat: add base api"
```

### 4. Stack another branch on top

```bash
git add internal/logic.go
stackit create add-logic -m "feat: implement logic"
```

### 5. Visualize the stack

```bash
stackit tree
```

```
● add-logic ← you are here
│
◯ add-api
│
main
```

### 6. Submit your PRs

```bash
stackit submit
```

This pushes both branches and creates two PRs on GitHub, with `add-logic` correctly pointing its base to `add-api`.

## Next steps

Once you're comfortable with the basics, explore:

- [Core concepts](../guide/concepts.md) - Understanding stacks, parents, and children
- [Workflows](../workflows/index.md) - Daily, advanced, and team collaboration patterns
- [Worktrees](../worktrees/index.md) - Work on multiple stacks in parallel
- [Integrations](../integrations/index.md) - Claude, GitHub, git hooks, and more
