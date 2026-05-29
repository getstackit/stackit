# Stack Operations Commands

Commands for managing the entire stack or multiple branches.

> **CRITICAL:** Always run these commands with `stackit ... --no-interactive`.

## Stack Maintenance

| Command | Description |
|---------|-------------|
| `stackit restack --branch <branch> --upstack --no-interactive` | Rebase a branch and its descendants (preferred — minimizes churn) |
| `stackit restack --all-stacks --continue-on-conflict --no-interactive` | Rebase every independent stack rooted at trunk, skipping conflicted stacks so unrelated stacks still proceed |
| `stackit restack --stacks <root1>,<root2> --continue-on-conflict --no-interactive` | Rebase specific independent stack roots while letting unrelated selected roots continue past conflicts |
| `stackit sync --no-interactive` | Pull trunk, delete merged branches, restack |
| `stackit state --json` | Complete one-call snapshot: current branch, working tree, in-progress operation, and the full stack (PR/CI + needs_restack/locked/frozen/scope) |
| `stackit info --stack --json --no-interactive` | Export full per-branch metadata incl. commit messages and diff stats (use when you need commit messages, which `state` omits) |
| `stackit merge --yes --no-interactive` | Merge the next (bottom) ready PR non-interactively (bare `stackit merge` with no flags opens a TTY wizard). Use `stackit merge ship --yes --no-interactive` to consolidate the whole stack into one PR. |
| `stackit fold --no-interactive` | Fold current branch into its parent |

## Bulk Operations

| Command | Description |
|---------|-------------|
| `stackit foreach --no-interactive` | Run command on each branch in stack |
| `stackit submit --no-interactive` | Push branches and create/update PRs |
| `stackit reorder` | Reorder branches (editor-driven — opens an editor, so there is no agent-safe non-interactive form) |
| `stackit move -y` | Rebase branch onto new parent (`-y/--yes` to skip the prompt) |

## Common Flag Patterns

### stackit submit --no-interactive
- `--stack` - Submit entire stack (alias: `ss`)
- `--draft` - Create as draft PRs
- `--edit` - Edit PR metadata interactively

**Examples:**
```bash
# Submit current branch and ancestors
stackit submit --no-interactive

# Submit entire stack
stackit submit --no-interactive --stack

# Submit as drafts
stackit submit --no-interactive --draft --stack
```

### stackit sync --no-interactive
- `--restack` - Auto-restack after cleanup

**What it does:**
1. Pulls latest from trunk/main
2. Deletes branches whose PRs were merged
3. Deletes branches whose PRs were closed
4. Optionally restacks remaining branches

### stackit foreach --no-interactive
**Usage:** `stackit foreach --no-interactive "command to run"`

**Examples:**
```bash
# Build all branches (use project's build command from README.md)
stackit foreach --no-interactive "<build-command>"

# Test all branches (use project's test command from README.md)
stackit foreach --no-interactive "<test-command>"

# Show status on each
stackit foreach --no-interactive "git status --short"
```

## Workflow Examples

### Start a feature stack
```bash
git add -A
echo "feat: implement user authentication" | stackit create -F - --no-interactive

# Add tests to the same branch (branches can have multiple commits).
# git commit is allowed for follow-up commits on an existing branch; pipe the
# message via -F - so permission rules stay stable across messages.
git add -A
printf 'test: add auth tests' | git commit -F -

# Work on next part as a separate stacked branch
git add -A
echo "feat: add JWT token validation" | stackit create -F - --no-interactive
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

### Restack scope cheat sheet

Prefer the narrowest scope that covers what actually changed:

| Situation | Command |
|-----------|---------|
| Amended/modified one branch | `stackit restack --branch <branch> --upstack --no-interactive` |
| Uncertain which branches need restack (single stack) | `stackit restack --branch <stack-root> --upstack --no-interactive` |
| Multiple independent stacks need restack (post-sync, shared parent change) | `stackit restack --all-stacks --continue-on-conflict --no-interactive` |
| Specific set of independent roots | `stackit restack --stacks <root1>,<root2> --continue-on-conflict --no-interactive` |

Use `--json` for programmatic runs; it reports which branches were restacked, skipped, or conflicted so you can skip a redundant follow-up pass.

### Sync with main
```bash
stackit sync --no-interactive --restack
```
