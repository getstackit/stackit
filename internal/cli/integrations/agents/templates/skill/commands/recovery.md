# Recovery & Utilities Commands

Commands for troubleshooting, recovery, and diagnostics.

> **CRITICAL:** Always run these commands with `stackit ... --no-interactive`. For commands that require confirmation, include `--force` (for absorb) or `--yes` (for undo/merge).

## Recovery Commands

| Command | Description |
|---------|-------------|
| `stackit undo --no-interactive --yes` | Restore repo to state before a command |
| `stackit continue --no-interactive` | Continue interrupted operation |
| `stackit abort --no-interactive` | Abort interrupted operation |

## Diagnostic Commands

| Command | Description |
|---------|-------------|
| `stackit doctor --no-interactive` | Diagnose and fix setup issues |
| `stackit info --no-interactive` | Show detailed branch info |
| `stackit debug --no-interactive` | Dump debugging info |

## Recovery Workflows

### Rebase in Progress

If you see "rebase in progress":
```bash
# Check status
git status

# If conflicts exist:
# 1. Resolve conflicts in files
# 2. Stage resolved files
git add <resolved-files>

# 3. Continue
stackit continue --no-interactive

# Or abort if needed
stackit abort --no-interactive
```

### Undo Last Operation

```bash
# Restore to previous state
stackit undo --no-interactive --yes

# Check if it worked
stackit tree --no-interactive
```

### Branch Needs Restack

Prefer the narrowest scope that covers the affected branches:

```bash
# Most common: restack the flagged branch and its descendants
stackit restack --branch <branch> --upstack --no-interactive

# Multiple independent stacks need attention (e.g. after sync reparented roots)
stackit restack --all-stacks --continue-on-conflict --no-interactive

# Handle conflicts if they occur, then continue
stackit continue --no-interactive
```

Use `--json` to see which branches were restacked vs skipped and avoid a redundant second pass.

### Orphaned Branch (Parent Merged)

```bash
# Sync will reparent automatically
stackit sync --no-interactive

# If restack is still needed, scope it — avoid broad restacks
stackit restack --branch <reparented-branch> --upstack --no-interactive
# Or, if sync reparented roots across multiple stacks:
stackit restack --all-stacks --continue-on-conflict --no-interactive
```

### PR Base Mismatch

```bash
# Submit will update PR base branches
stackit submit --no-interactive
```

### Uncommitted Changes Blocking Operation

```bash
# Option 1 (safe default): Stash them, do the operation, then restore
git stash
# ...run the blocked operation...
git stash pop
```

```bash
# Option 2: Commit them — ONLY if the changes belong to the current branch's
# commit. `stackit modify` amends whatever commit is checked out, so do not use
# it to park unrelated changes (that silently corrupts an unrelated commit).
git add -A
stackit modify --no-interactive
```

## Diagnostic Workflows

### Check Stack Health

```bash
# View structure
stackit tree --no-interactive

# Get detailed info
stackit info --no-interactive

# Check for issues
stackit doctor --no-interactive
```

### Debug Issues

```bash
# Dump all diagnostic info
stackit debug --no-interactive

# This shows:
# - Git refs
# - Stackit metadata
# - Branch relationships
# - Configuration
```

## Troubleshooting Checklist

When things go wrong:
1. Check status: `git status` and `stackit tree --no-interactive`
2. Look for interrupted operations (rebase, merge)
3. Try `stackit doctor --no-interactive` for automatic fixes
4. If stuck, `stackit abort --no-interactive` then `stackit undo --no-interactive --yes`
5. For persistent issues, check `stackit debug --no-interactive` output
