# Stackit Command Reference

Quick reference for all stackit commands. For detailed documentation, see:
- **Navigation details:** [commands/navigation.md](commands/navigation.md)
- **Branch operation details:** [commands/branch.md](commands/branch.md)
- **Stack operation details:** [commands/stack.md](commands/stack.md)
- **Recovery details:** [commands/recovery.md](commands/recovery.md)

> **CRITICAL:** Always run stackit with `command stackit ... --no-interactive`. For commands that require confirmation, include `--force` (for absorb) or `--yes` (for undo/merge).

## FORBIDDEN Commands

| FORBIDDEN | USE INSTEAD |
|-----------|-------------|
| `git commit` (new branches) | `command stackit create` |
| `git checkout -b` | `command stackit create` |
| `gh pr create` | `command stackit submit` |

**Required workflow for new stacked branches:**
```bash
git add -A                                        # 1. Stage FIRST
echo "message" | command stackit create --no-interactive  # 2. Then create
```

## Utility Scripts

Run these helper scripts for analysis:

```bash
# Analyze stack health and get suggestions
bash ~/.claude/skills/stackit/scripts/analyze_stack.sh
```

## Navigation Commands

| Command | Description |
|---------|-------------|
| `command stackit log` | Display the branch tree visualization |
| `command stackit log full` | Show tree with GitHub PR status and CI checks |
| `command stackit checkout [branch]` | Switch to a specific branch |
| `command stackit up` | Move to the child branch |
| `command stackit down` | Move to the parent branch |
| `command stackit top` | Move to the top of the stack |
| `command stackit bottom` | Move to the bottom of the stack |
| `command stackit trunk` | Return to the main/trunk branch |
| `command stackit children` | Show children of current branch |
| `command stackit parent` | Show parent of current branch |

## Branch Management

| Command | Description |
|---------|-------------|
| `command stackit create --no-interactive [name]` | Create a new stacked branch |
| `command stackit modify --no-interactive` | Amend current commit (like git commit --amend) |
| `command stackit absorb` | Auto-amend changes to correct commits in stack |
| `command stackit split` | Split current branch into multiple branches |
| `command stackit squash` | Squash all commits on current branch |
| `command stackit fold` | Merge current branch into its parent |
| `command stackit pop` | Delete branch but keep changes in working tree |
| `command stackit delete` | Delete current branch and metadata |
| `command stackit rename [name]` | Rename current branch |
| `command stackit scope [name]` | Manage logical scope (Jira/Linear ID) |

## Stack Operations

| Command | Description |
|---------|-------------|
| `command stackit restack --no-interactive` | Rebase all branches to ensure proper ancestry |
| `command stackit foreach` | Run command on each branch in stack |
| `command stackit submit --no-interactive` | Push branches and create/update PRs |
| `command stackit sync --no-interactive` | Pull trunk, delete merged branches, restack |
| `command stackit merge` | Merge approved PRs and cleanup |
| `command stackit reorder` | Interactively reorder branches |
| `command stackit move` | Rebase branch onto new parent |

## Recovery & Utilities

| Command | Description |
|---------|-------------|
| `command stackit undo` | Restore repo to state before a command |
| `command stackit continue` | Continue interrupted operation |
| `command stackit abort` | Abort interrupted operation |
| `command stackit doctor` | Diagnose and fix setup issues |
| `command stackit info` | Show detailed branch info |
| `command stackit track` | Start tracking a branch |
| `command stackit untrack` | Stop tracking a branch |
| `command stackit debug` | Dump debugging info |

## Common Flag Patterns

### command stackit create --no-interactive
- `-m "message"` - Commit message
- `--all` - Stage all changes first
- `--insert` - Insert between current and child

### command stackit submit --no-interactive
- `--stack` - Submit entire stack (alias: `ss`)
- `--draft` - Create as draft PRs
- `--edit` - Edit PR metadata interactively

### command stackit sync --no-interactive
- `--restack` - Auto-restack after cleanup

## Workflow Examples

### Start a new feature
```bash
git add .
echo "feat: add new feature" | command stackit create --no-interactive
```

### Stack another change
```bash
git add .
echo "feat: extend feature" | command stackit create --no-interactive
```

### Add more commits to current branch
```bash
# A stacked branch can have multiple commits - no need to create a new branch
git add .
git commit -m "test: add tests for feature"
```

### Submit for review
```bash
command stackit submit --no-interactive --stack
```

### After code review changes
```bash
git add .
command stackit modify --no-interactive
command stackit restack --no-interactive
command stackit submit --no-interactive
```

### Sync with main
```bash
command stackit sync --no-interactive --restack
```

## Troubleshooting

For detailed troubleshooting workflows, see:
- **Fixing absorb errors:** [workflows/fix-absorb.md](workflows/fix-absorb.md)
- **Conflict resolution:** [workflows/conflict-resolution.md](workflows/conflict-resolution.md)
- **Recovery commands:** [commands/recovery.md](commands/recovery.md)

### Quick Fixes

| Issue | Solution |
|-------|----------|
| "Branch needs restack" | `command stackit restack --no-interactive` |
| "Rebase conflict" | Resolve conflicts, `git add <files>`, `command stackit continue` |
| "Orphaned branch" | `command stackit sync --no-interactive` to reparent |
| "PR base mismatch" | `command stackit submit --no-interactive` to update PRs |
| Build breaks after absorb | See [workflows/fix-absorb.md](workflows/fix-absorb.md) |
