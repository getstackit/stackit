# Branch Management Commands

Commands for creating, modifying, and managing individual branches.

> **CRITICAL:** Always run these commands with `command stackit ... --no-interactive`. For commands that require confirmation, include `--force` (for absorb) or `--yes` (for undo/merge).

## FORBIDDEN Commands

| FORBIDDEN | USE INSTEAD |
|-----------|-------------|
| `git commit` (new branches) | `command stackit create` |
| `git checkout -b` | `command stackit create` |
| `gh pr create` | `command stackit submit` |

**Required workflow for new stacked branches:**
```bash
git add -A                                                # 1. Stage FIRST
echo "message" | command stackit create --no-interactive  # 2. Then create
```

## Creating and Modifying

| Command | Description |
|---------|-------------|
| `command stackit create [name]` | Create a new stacked branch (name optional, auto-generated) |
| `command stackit modify` | Amend current commit (like git commit --amend) |
| `command stackit absorb` | Auto-amend changes to correct commits in stack |

## Branch Lifecycle

| Command | Description |
|---------|-------------|
| `command stackit split` | Split current branch into multiple branches |
| `command stackit squash` | Squash all commits on current branch |
| `command stackit fold` | Merge current branch into its parent |
| `command stackit pop` | Delete branch but keep changes in working tree |
| `command stackit delete` | Delete current branch and metadata |
| `command stackit rename [name]` | Rename current branch |

## Metadata

| Command | Description |
|---------|-------------|
| `command stackit scope [name]` | Manage logical scope (Jira/Linear ID) |
| `command stackit track` | Start tracking a branch |
| `command stackit untrack` | Stop tracking a branch |

## Common Flag Patterns

### command stackit create
- `--all` - Stage all changes first
- `--insert` - Insert between current and child
- `-w` - Create with a dedicated worktree

**Preferred usage (pipe format):**
```bash
# Branch name auto-generated from commit message
echo "feat: add user authentication" | command stackit create --no-interactive

# With explicit branch name
echo "feat: add user authentication" | command stackit create my-branch --no-interactive

# Stage all and create
echo "feat: add user authentication" | command stackit create --all --no-interactive
```

### Multiple commits per branch
**A stacked branch can have multiple commits.** You don't need a new branch for every commit:
- Add another commit: `git add . && git commit -m "message"`
- Amend current commit: `command stackit modify --no-interactive`
- Absorb fixes to correct commits: `command stackit absorb --no-interactive`

### command stackit modify
- Similar to `git commit --amend` but updates stack metadata
- Auto-restacks children if needed

### command stackit absorb
- Automatically determines which commit each change belongs to
- **Warning:** May cause compilation errors if dependencies split across commits
- Always validate with build/test after absorb
