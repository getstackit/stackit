---
icon: material/cog
title: Worktree Management
description: List, open, remove, and configure stackit worktrees. Set up auto-cleanup, custom paths, and post-create hooks for dependency installation.
---

# Worktree Management

List, configure, and maintain your worktrees.

## Listing Worktrees

View all stackit-managed worktrees:

```bash
stackit worktree list
```

Shows each worktree's anchor branch, path, and status.

## Opening an Existing Worktree

Switch to a worktree directory:

```bash
stackit worktree open my-feature
```

You can specify either the worktree name or the anchor branch name.

!!! note
    Requires [shell integration](shell-integration.md) for automatic directory change. Without it, use:
    ```bash
    cd $(stackit worktree open my-feature)
    ```

## Removing a Worktree

Clean up a worktree when you're done:

```bash
stackit worktree remove my-feature
```

This removes:

- The worktree directory
- The worktree registration in stackit

The stack's branches remain intact. Use `--force` to remove even if there are errors.
Use `--keep-branch` to preserve the anchor branch when removing the worktree.

!!! warning "Ignored files are deleted with the directory"
    Deleting the worktree directory deletes everything in it, including files
    Git ignores — a `.env` you edited there, dependency caches, and anything
    copied in by a [warm start](#warm-starting-new-worktrees). Git has no copy
    of those, so there is nothing to recover them from. Copy out anything you
    want to keep before removing.

## Attaching to an Existing Stack

Attach an existing branch/stack to a new worktree:

```bash
stackit worktree attach my-feature
```

This creates a worktree for an existing stack that wasn't originally created with `-w`. Useful when you want to work on an existing stack in isolation.

## Detaching a Worktree

Stop working in a worktree while keeping all of its branches:

```bash
stackit worktree detach my-feature
```

Unlike `remove`, which refuses when the worktree holds real branches, `detach`
preserves them: it reparents the stack onto the hidden anchor's parent, deletes
the anchor branch, and unregisters the worktree. Use `--force` if there are
uncommitted changes.

!!! warning "Detach also deletes the directory"
    Detach preserves your *branches*, not the worktree directory — that is
    removed, along with every ignored file in it. The same caution as
    [Removing a Worktree](#removing-a-worktree) applies: copy out any
    warm-started `.env` or cache you want to keep first.

## Pruning Stale Worktrees

Clean up worktrees that no longer exist on disk:

```bash
stackit worktree prune
```

Use `--dry-run` to preview what would be cleaned up without making changes.

Prune skips worktrees that still hold stacked branches or have uncommitted
changes. It does delete directories that are still on disk but empty of stack
branches — and ignored files do not make a worktree count as dirty, so a
worktree holding nothing but a warm-started `.env` is prunable and that file
goes with it.

## Automatic Cleanup

During $$stackit sync$$, worktrees for merged stacks are automatically cleaned up when `worktree.autoClean` is enabled (the default).

## Configuration Options

### worktree.basePath

Customize where worktrees are created:

```bash
stackit config set worktree.basePath "../my-stacks"
```

**Default**: `../<repo-name>-stacks`

### worktree.autoClean

Control automatic worktree cleanup during sync:

```bash
stackit config set worktree.autoClean false
```

**Default**: `true`

## Warm-Starting New Worktrees

A new worktree is a fresh checkout, so the ignored files your build needs —
`.env`, dependency caches — aren't there. Add a `.worktreeinclude` file at the
repository root to copy selected ignored files into every new managed worktree:

```gitignore
# .worktreeinclude — .gitignore syntax, selects from files Git already ignores
.env
.env.local
node_modules/
```

The copy runs before `post-worktree-create` hooks, so setup hooks can use the
warmed files. A pattern can only ever copy a file Git already ignores — never a
tracked file — and an existing file in the destination is never overwritten.

!!! warning "Treat `.worktreeinclude` as a secrets allowlist"
    Every managed worktree receives these files. Include secrets only when that
    is acceptable for all of them. And because warm-started files are ignored by
    Git, nothing else has a copy: `remove`, `prune`, and `detach` all delete
    them with the directory.

## Post-Create Hooks

Run commands automatically after worktree creation by adding a `.stackit.yaml` file:

```yaml
# .stackit.yaml
hooks:
  post-worktree-create:
    - npm install
    - cp .env.example .env
```

Common uses:

- Installing dependencies
- Setting up environment files
- Running initialization scripts

!!! warning "Security"
    The first time a hook is encountered, stackit prompts for approval. Approvals are stored locally and persist across sessions.

See [Configuration](../cli/config.md#worktree-hooks) for more examples.

## Working in Worktrees

### Run Stack Commands Where the Stack Lives

Commands that change branch content — `create`, `modify`, `absorb`, `fold`,
`squash`, `split`, `move`, `reorder`, `pluck`, `delete`, `track` — must run in
the worktree that owns the stack, so the edit lands in the working tree you are
actually looking at. From anywhere else stackit refuses and says where to go:

```
branch payments-api belongs to worktree payments; run this command from there: cd ../myapp-stacks/payments
```

$$stackit sync$$ and $$stackit restack$$ are the exception — they reconcile the
whole repository and can run from any worktree.

If the owning worktree's directory no longer exists, `cd` is not an option; the
refusal points at `stackit worktree detach` instead, which releases the branches.

### Creating Stacked Branches

Once inside the worktree that owns the stack, create branches as usual:

```bash
# Make changes
git add feature.go

# Create a stacked branch
stackit create add-feature -m "feat: add new feature"
```

### Creating Worktrees from Worktrees

You can create new worktrees from inside an existing worktree:

```bash
# Inside ../repo-stacks/feature-a/
stackit worktree create feature-b --open
```

Stackit detects the context and creates the new worktree from the main repository. The new worktree is a sibling, not nested.

### Returning to Main Repository

```bash
# From inside a worktree
cd $(git rev-parse --path-format=absolute --git-common-dir)/..

# Or simply navigate to your original repo path
cd ~/projects/my-repo
```

## Best Practices

- **One worktree per stack** — Keep features isolated for easier context switching
- **Clean up after merging** — Remove worktrees once PRs are merged, or let $$stackit sync$$ do it automatically
- **Use scopes** — Associate worktrees with tickets using `--scope` for better organization
- **Set up hooks** — Configure `post-worktree-create` hooks to automate dependency installation

## Next Steps

- [Getting Started →](getting-started.md)
- [Shell Integration →](shell-integration.md)
- [Configuration →](../cli/config.md)
