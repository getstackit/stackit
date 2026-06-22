---
icon: material/play-circle
title: Getting Started with Git Worktrees
description: Create and use Git worktrees for parallel development with stackit. Work on multiple features simultaneously in isolated directories.
---

# Getting Started with Worktrees

Learn how to create and use worktrees for parallel development.

## Creating a Worktree

Create a new worktree from trunk:

```bash
stackit worktree create my-feature
```

This creates:

- A new anchor branch `my-feature` tracked by stackit
- A worktree directory at `../<repo-name>-stacks/my-feature/`

## Opening After Creation

Use `--open` to automatically change to the new worktree directory:

```bash
stackit worktree create my-feature --open
```

!!! note "Shell Integration Required"
    The `--open` flag requires [shell integration](shell-integration.md) to change your working directory. Without it, stackit prints the path instead.

## Setting a Scope

Associate a Jira ticket or Linear issue with all branches in the worktree:

```bash
stackit worktree create my-feature --scope PROJ-123
```

All branches created in this worktree will include the scope in their names.

## Alternative: Create During Branch Creation

You can also create a worktree while creating your first branch:

```bash
git add .
stackit create my-feature -m "feat: start new feature" -w
```

This stages your changes, creates a branch with a commit, and sets up a worktree in one command.

## Working in a Worktree

Once inside a worktree, all stackit commands work as expected:

```bash
# Navigate to your worktree
stackit worktree open my-feature

# Make changes
git add feature.go

# Create stacked branches
stackit create add-feature -m "feat: add new feature"

# View the stack
stackit tree

# Submit PRs
stackit submit
```

## Creating Worktrees from Worktrees

You can create new worktrees from inside an existing worktree:

```bash
# Inside ../repo-stacks/feature-a/
stackit worktree create feature-b --open
```

Stackit detects the context and creates the new worktree from the main repository. The new worktree is a sibling, not nested.

## Returning to Main Repository

```bash
# From inside a worktree
cd $(git rev-parse --path-format=absolute --git-common-dir)/..

# Or simply navigate to your original repo path
cd ~/projects/my-repo
```

## Next Steps

- [Shell Integration →](shell-integration.md) — Enable automatic directory changes
- [Management →](management.md) — List, remove, and configure worktrees
