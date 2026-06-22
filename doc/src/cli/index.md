---
icon: material/console
title: Stackit CLI Overview
description: Complete command-line interface reference for stackit. Browse navigation, branch management, stack operations, and worktree commands.
---

# CLI

Complete command-line reference for stackit.

## Quick links

<div class="grid cards" markdown>

-   :material-book-open:{ .lg .middle } **Command Reference**

    ---

    Complete documentation for all stackit commands.

    [View reference →](reference.md)

-   :material-wrench:{ .lg .middle } **Configuration**

    ---

    Customize stackit behavior with configuration options.

    [Configure stackit →](config.md)

</div>

## Command categories

### Navigation

Move around your stack efficiently.

- $$stackit tree$$ - Display the branch tree
- $$stackit t$$ - Display the short branch tree
- $$stackit log$$ - Show trunk commit history with consolidated stacks collapsed
- $$stackit checkout$$ - Interactive branch switcher
- $$stackit up$$ / $$stackit down$$ - Move between parent/child branches
- $$stackit top$$ / $$stackit bottom$$ - Jump to extremes
- $$stackit trunk$$ - Return to main branch

### Branch management

Create, modify, and organize branches.

- $$stackit create$$ - Create a new branch
- $$stackit modify$$ - Amend the current commit
- $$stackit absorb$$ - Intelligently amend changes
- $$stackit delete$$ - Delete a branch
- $$stackit rename$$ - Rename a branch

### Stack operations

Manage your entire stack.

- $$stackit submit$$ - Create/update PRs
- $$stackit sync$$ - Update from trunk
- $$stackit merge$$ - Merge your stack
- $$stackit restack$$ - Rebase branches (scoped with `--upstack`, `--all-stacks`, `--parallel`)
- $$stackit flatten$$ - Optimize stack structure

### Worktrees

Work on multiple stacks simultaneously.

- `stackit create -w` - Create branch with worktree
- `stackit worktree create` - Create a standalone worktree
- `stackit worktree list` - List all worktrees
- `stackit worktree open` - Open worktree (auto-cd with shell integration)

### Git hooks

Enforce branch restrictions locally.

- `stackit precommit install` - Prevent commits to locked branches
- `stackit prepush install` - Prevent pushes to locked branches

See [Git Hooks](../integrations/git-hooks.md) for details.

## Global flags

Available on all commands:

| Flag | Description |
|:---|:---|
| `--cwd <path>` | Working directory for operations |
| `--debug` | Write debug output to terminal |
| `--interactive` | Enable interactive features (default: true) |
| `--no-interactive` | Disable all interactive features |
| `--verify` | Enable git hooks (default: true) |
| `--no-verify` | Disable git hooks |
| `--quiet`, `-q` | Minimize output (implies `--no-interactive`) |

## Next steps

- [View full command reference →](reference.md)
- [Learn about configuration →](config.md)
