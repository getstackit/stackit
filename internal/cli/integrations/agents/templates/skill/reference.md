# Stackit Command Reference

Quick reference for all stackit commands. For detailed documentation, see:
- **Navigation details:** [commands/navigation.md](commands/navigation.md)
- **Branch operation details:** [commands/branch.md](commands/branch.md)
- **Stack operation details:** [commands/stack.md](commands/stack.md)
- **Recovery details:** [commands/recovery.md](commands/recovery.md)

> **CRITICAL:** Always run stackit with `stackit ... --no-interactive`. For commands that require confirmation, include `--force` (for absorb) or `--yes` (for undo/merge).

## FORBIDDEN Commands

| FORBIDDEN | USE INSTEAD |
|-----------|-------------|
| `git commit` (new branches) | `stackit create` |
| `git checkout -b` | `stackit create` |
| `gh pr create` | `stackit submit` |
| `git rebase` (stack branches) | `stackit restack --branch <branch> --upstack` (or `--all-stacks`) |

**Required workflow for new stacked branches:**
```bash
git add -A                                        # 1. Stage FIRST
echo "message" | stackit create -F - --no-interactive  # 2. Then create
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
| `stackit log` | Display the branch tree visualization |
| `stackit log full` | Show tree with GitHub PR status and CI checks |
| `stackit checkout [branch]` | Switch to a specific branch |
| `stackit up` | Move to the child branch |
| `stackit down` | Move to the parent branch |
| `stackit top` | Move to the top of the stack |
| `stackit bottom` | Move to the bottom of the stack |
| `stackit trunk` | Return to the main/trunk branch |
| `stackit children` | Show children of current branch |
| `stackit parent` | Show parent of current branch |

## Branch Management

| Command | Description |
|---------|-------------|
| `stackit create --no-interactive [name]` | Create a new stacked branch |
| `stackit modify --no-interactive` | Amend current commit (like git commit --amend) |
| `stackit absorb` | Auto-amend changes to correct commits in stack |
| `stackit split` | Split current branch into multiple branches |
| `stackit squash` | Squash all commits on current branch |
| `stackit fold` | Merge current branch into its parent |
| `stackit pop` | Delete branch but keep changes in working tree |
| `stackit delete` | Delete current branch and metadata |
| `stackit rename [name]` | Rename current branch |
| `stackit scope [name]` | Manage logical scope (Jira/Linear ID) |

## Stack Operations

| Command | Description |
|---------|-------------|
| `stackit restack --branch <branch> --upstack --no-interactive` | Rebase a branch and its descendants (preferred) |
| `stackit restack --all-stacks --continue-on-conflict --no-interactive` | Rebase every independent stack rooted at trunk while letting unaffected stacks continue |
| `stackit restack --stacks <root1>,<root2> --continue-on-conflict --no-interactive` | Rebase specific independent stack roots while letting unaffected selected roots continue |
| `stackit foreach` | Run command on each branch in stack |
| `stackit submit --no-interactive` | Push branches and create/update PRs |
| `stackit sync --no-interactive` | Pull trunk, delete merged branches, restack |
| `stackit merge --yes --no-interactive` | Merge the next (bottom) ready PR non-interactively (bare `stackit merge` with no flags opens a TTY wizard); use `stackit merge ship --yes --no-interactive` to consolidate the whole stack into one PR |
| `stackit reorder` | Reorder branches (editor-driven — no agent-safe non-interactive form) |
| `stackit move -y` | Rebase branch onto new parent (`-y` skips the prompt) |

## Recovery & Utilities

| Command | Description |
|---------|-------------|
| `stackit undo` | Restore repo to state before a command |
| `stackit continue` | Continue interrupted operation |
| `stackit abort` | Abort interrupted operation |
| `stackit doctor` | Diagnose and fix setup issues |
| `stackit info` | Show detailed branch info |
| `stackit track` | Start tracking a branch |
| `stackit untrack` | Stop tracking a branch |
| `stackit debug` | Dump debugging info |

## Common Flag Patterns

### stackit create --no-interactive
- `-F, --message-file` - Read commit message from a file; use `-` for stdin (preferred — keeps permission rules stable across messages)
- `-m "message"` - Inline commit message (mutually exclusive with `-F`)
- `--all` - Stage all changes first
- `--insert` - Insert between current and child

### stackit submit --no-interactive
- `--stack` - Submit entire stack (alias: `ss`)
- `--draft` - Create as draft PRs
- `--edit` - Edit PR metadata interactively

### stackit sync --no-interactive
- `--restack` - Auto-restack after cleanup

## Workflow Examples

### Start a new feature
```bash
git add -A
echo "feat: add new feature" | stackit create -F - --no-interactive
```

### Stack another change
```bash
git add -A
echo "feat: extend feature" | stackit create -F - --no-interactive
```

### Add more commits to current branch
```bash
# A stacked branch can have multiple commits - no need to create a new branch.
# git commit is allowed for follow-up commits; pipe the message via stdin.
git add -A
printf 'test: add tests for feature' | git commit -F -
```

### Submit for review
```bash
stackit submit --no-interactive --stack
```

### After code review changes
```bash
git add -A
stackit modify --no-interactive
# stackit modify automatically restacks descendants — no manual restack needed.
stackit submit --no-interactive
```

### Sync with main
```bash
stackit sync --no-interactive --restack
```

## Troubleshooting

For detailed troubleshooting workflows, see:
- **Fixing absorb errors:** [workflows/fix-absorb.md](workflows/fix-absorb.md)
- **Conflict resolution:** [workflows/conflict-resolution.md](workflows/conflict-resolution.md)
- **Recovery commands:** [commands/recovery.md](commands/recovery.md)

### Quick Fixes

| Issue | Solution |
|-------|----------|
| "Branch needs restack" | `stackit restack --branch <branch> --upstack --no-interactive` (scope to the branch and its descendants) |
| "Rebase conflict" | Resolve conflicts, `git add <files>`, `stackit continue` |
| "Orphaned branch" | `stackit sync --no-interactive` to reparent |
| "PR base mismatch" | `stackit submit --no-interactive` to update PRs |
| Build breaks after absorb | See [workflows/fix-absorb.md](workflows/fix-absorb.md) |
