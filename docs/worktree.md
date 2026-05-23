# Worktrees

Worktrees allow you to work on multiple stacks in parallel, each in its own directory.

## Quick Reference

| Command | Description |
|:---|:---|
| `stackit worktree create <name>` | Create a new worktree with a fresh anchor branch |
| `stackit worktree attach <branch>` | Create a worktree for an existing stack |
| `stackit worktree list` | List all managed worktrees |
| `stackit worktree open <name>` | Open/cd to a worktree |
| `stackit worktree remove <name>` | Remove an empty worktree and delete its hidden anchor |
| `stackit worktree detach <name>` | Remove worktree but keep branches |
| `stackit worktree prune` | Clean up empty/stale worktrees |

**Alias:** `wt` is a short alias for `worktree` (e.g., `stackit wt list`).

---

## Overview

Git worktrees let you check out multiple branches simultaneously in separate directories. Stackit manages worktrees to give each stack its own isolated workspace.

**Key concepts:**

- **Anchor branch**: A hidden marker branch that owns a managed worktree. Real stack branches are parented under this anchor.
- **Main repo**: Your original repository checkout.
- **Worktree**: A secondary checkout in a separate directory.

**Default location:** Worktrees are created in a sibling directory named `{repo}-stacks/`. For example, if your repo is at `~/projects/myapp`, worktrees go to `~/projects/myapp-stacks/`.

---

## Commands

### `worktree create` - Start a new stack in a worktree

Creates a fresh worktree with an empty anchor branch. Use this when starting new work that you want isolated from your main checkout.

```bash
stackit worktree create my-feature
# Creates:
#   - Anchor branch: {pattern}-my-feature-wt (at trunk HEAD)
#   - Worktree at: ../myapp-stacks/my-feature/
```

**Options:**
- `--scope <name>`: Set a scope (Jira ticket, Linear ID) on the anchor branch
- `--no-open`: Don't auto-cd to the new worktree

**Workflow:**
```bash
stackit wt create auth-refactor
# Now in ../myapp-stacks/auth-refactor/
stackit create api-changes -m "refactor: update auth API"
stackit create ui-updates -m "feat: new login UI"
```

### `worktree attach` - Move existing stack to a worktree

Creates a worktree for a stack that already exists in your main repo. Stackit inserts a hidden anchor above the existing stack root, then checks the worktree out on the original stack root branch.

```bash
# You have a stack: main -> feature -> tests
stackit worktree attach feature
# Creates worktree at: ../myapp-stacks/feature/
# Main repo switches to trunk
```

**Options:**
- `--name <name>`: Custom worktree name (defaults to stack root name)
- `--no-open`: Don't auto-cd to the new worktree

**When to use:**
- You started work in the main repo but want to isolate it
- You want to work on a different stack without stashing/switching
- You fetched someone's stack with `stackit get` and want a dedicated workspace

### `worktree list` - Show all managed worktrees

```bash
stackit worktree list
```

Shows each worktree with:
- Name and anchor branch
- Path on disk
- Stack size (number of branches)
- Current branch in that worktree
- Dirty status (uncommitted changes)

### `worktree open` - Navigate to a worktree

```bash
stackit worktree open my-feature
```

With shell integration enabled, this changes your directory to the worktree. Without shell integration, it prints the path for use with `cd $(...)`.

### `worktree remove` - Delete a worktree

Removes the worktree directory and deletes its hidden anchor, but only when the worktree has no real child branches.

```bash
stackit worktree remove my-feature
```

**Options:**
- `--force`: Remove and discard uncommitted changes

**Use when:** The stack is fully merged or the worktree is otherwise empty. `--force` only discards dirty files; it does not delete real branches.

### `worktree detach` - Remove worktree, keep branches

Removes the worktree directory but preserves all stack branches in the main repo.

```bash
stackit worktree detach my-feature
```

Detach reparents children of the hidden anchor to the anchor's parent, unregisters the worktree, and deletes the anchor branch.

**Options:**
- `--force`: Detach even with uncommitted changes

**Use when:**
- You want to continue work in the main repo instead
- You need to free up disk space but keep your branches
- You're done with the isolated workspace but not the code

### `worktree prune` - Clean up stale worktrees

Removes worktrees that are empty (no stacked branches) or have missing directories.

```bash
stackit worktree prune
stackit worktree prune --dry-run  # Preview what would be removed
```

**Skips worktrees that:**
- Have stacked branches
- Have uncommitted changes
- Are currently checked out

---

## How Anchor Branches Work

When you create a worktree with `wt create`, Stackit creates an **anchor branch** — a special marker branch with no commits that connects your worktree's stack to trunk. Anchors are an implementation detail that you generally don't need to think about; Stackit handles them transparently.

### Anchors Are Hidden

Anchor branches are automatically hidden from most UI surfaces:

- **`stackit log`** — Anchors are filtered out of the tree. A stack like `main → wt-anchor → feature` renders as `main → feature`.
- **Navigation (`up`, `down`, `top`, `bottom`)** — Navigation commands skip over anchors. Moving "down" from a feature branch in a worktree goes straight to trunk, not to the anchor.
- **`stackit submit`** — PR parent resolution skips anchors. A branch parented to an anchor will have its PR base set to trunk (or the nearest real ancestor), not the anchor branch.

### Anchors Cannot Be Modified

Anchor branches are protected from modification. You cannot `modify`, `squash`, `absorb`, or `checkout` an anchor directly. If you try to check out an anchor, Stackit will suggest using `stackit worktree open` instead.

### Worktree-Aware Checkout

When you check out a branch that lives in a different worktree, Stackit detects this and switches you to the correct worktree automatically (with shell integration enabled). Without shell integration, it prints the path and a fallback command:

```bash
# From main repo, checking out a branch in a worktree:
stackit checkout feature
# → "Switching to worktree for stack payments."
# → cd ../myapp-stacks/payments && stackit co feature
```

Similarly, checking out trunk or an untracked branch from within a worktree switches you back to the main repository.

---

## Create vs Attach

| | `wt create` | `wt attach` |
|:---|:---|:---|
| **Starting point** | Fresh (from trunk) | Existing stack |
| **Anchor branch** | New empty marker branch | New empty marker branch above the stack root |
| **Main repo after** | Unchanged | Switches to trunk |
| **On detach** | Anchor deleted, children reparented | Anchor deleted, stack root reparented to the anchor's parent |

**Rule of thumb:**
- Use `create` when starting new work
- Use `attach` when moving existing work to a worktree

---

## Configuration

Configure worktree behavior in `.stackit.yaml` or via `stackit config`:

```yaml
# .stackit.yaml
worktree:
  basePath: "../my-stacks"  # Custom location (default: ../{repo}-stacks)
  autoClean: true           # Auto-remove merged worktrees during sync
```

```bash
stackit config set worktree.basePath "../my-stacks"
stackit config set worktree.autoClean false
```

---

## Shell Integration

Enable shell integration to auto-cd when creating/opening worktrees:

```bash
# zsh (~/.zshrc)
eval "$(stackit shell zsh)"

# bash (~/.bashrc)
eval "$(stackit shell bash)"

# fish (~/.config/fish/config.fish)
stackit shell fish | source
```

Without shell integration, use command substitution:
```bash
cd $(stackit worktree open my-feature)
```

---

## Post-Create Hooks

Run commands automatically after creating a worktree:

```yaml
# .stackit.yaml
hooks:
  post-worktree-create:
    - npm install
    - cp .env.example .env
```

**Security:** First-time hooks require approval (defaults to "No"). Approvals are stored in git config. Hooks have a 60-second timeout.

---

## Common Workflows

### Start isolated feature work

```bash
stackit wt create payments
# In worktree now
stackit create api -m "feat: payment API"
stackit create ui -m "feat: checkout UI"
stackit submit
```

### Move in-progress work to worktree

```bash
# In main repo with stack: main -> auth -> tests
stackit wt attach auth
# Now in worktree, main repo is on trunk
```

### Finish and clean up

```bash
# After PRs merged
stackit sync           # Cleans up merged worktrees if autoClean=true
# Or manually:
stackit wt remove my-feature
```

### Keep branches, remove worktree

```bash
stackit wt detach my-feature
# Worktree gone, branches still in main repo
stackit checkout my-feature  # Continue in main repo
```
