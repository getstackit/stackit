---
icon: material/robot
title: AI Agent Integration
description: AI-assisted stacking with Claude Code and Codex. Intelligent skills for creating branches, syncing stacks, absorbing changes, and fixing issues.
---

# AI Agent Integration

Stackit includes specialized skills for Claude Code and Codex, providing intelligent automation for common stacking workflows.

## Overview

Agent integration enables AI-assisted stacking with skills that:

- **Understand Context**: Analyze your current stack state and git status
- **Provide Validation**: Include quality checks and error handling
- **Guide Through Issues**: Offer step-by-step resolution guidance
- **Ensure Safety**: Prioritize data safety with undo capabilities

## Setup

Install the agent integration files:

```bash
stackit agent install
```

This creates integration files for Claude Code, Codex, and Cursor.

| Agent | Installed files |
|:------|:----------------|
| Claude Code | `~/.claude/skills/stackit/` plus `~/.claude/skills/stack-*/` |
| Codex | `~/.codex/skills/stackit/` plus `~/.codex/skills/stack-*/` |
| Cursor and other agents | Repository guidance in `AGENTS.md` when selected |

Use `--format=claude`, `--format=codex`, or `--format=claude,codex` to install a specific skill format in non-interactive environments.

## Available skills

### stack-status

View current stack state, branch position, and health status.

```bash
stack-status
```

**When to use**: Getting oriented in a complex stack, checking for issues.

### stack-create

Create a new stacked branch with intelligent naming and commit messages.

```bash
stack-create [branch-name]
```

**When to use**: Adding a new feature branch to your stack.

### stack-submit

Submit branches as PRs with auto-generated descriptions.

```bash
stack-submit [--stack | --draft]
```

**When to use**: Creating or updating pull requests.

**Options**:
- `--stack`: Submit entire stack
- `--draft`: Submit as draft PRs

### stack-sync

Sync with trunk, cleanup merged branches, and restack.

```bash
stack-sync
```

**When to use**: Keeping your stack up-to-date with main.

### stack-restack

Rebase branches with scoped or multi-stack targeting to ensure proper ancestry.

```bash
stack-restack
```

**When to use**: Fixing branch relationships after changes.

### stack-absorb

Intelligently absorb working changes into correct commits.

```bash
stack-absorb
```

**When to use**: Applying fixes across multiple stack branches, with conflict resolution guidance.

### stack-fix

Diagnose and fix common stack issues.

```bash
stack-fix
```

**When to use**: Resolving compilation errors or structural problems.

### stack-describe

Generate or update an AI-powered description for the current stack.

```bash
stack-describe
```

**When to use**: Documenting what your stack does for PRs. Descriptions appear in `stackit info` output and are included in consolidated PR bodies.

## Example workflow

Here's a typical AI-assisted workflow:

```bash
# Claude Code or Codex helps create a branch with a proper commit message
stack-create add-user-auth

# Make your changes...

# Intelligently distribute changes across commits
stack-absorb

# Diagnose and fix any issues
stack-fix

# Create/update all PRs in the stack
stack-submit --stack
```

## How it works

Stackit agent workflows are executed through skills that:

1. **Analyze current state**: Check git status, stack structure, and branch relationships
2. **Execute operations**: Perform the requested action with appropriate flags
3. **Handle errors**: Provide context-aware error messages and recovery steps
4. **Validate results**: Ensure operations completed successfully

## Integration with regular commands

Agent skills complement regular stackit commands. You can mix and match:

```bash
# Use an agent skill for complex operations
stack-create my-feature

# Use regular commands for simple tasks
stackit tree
stackit checkout parent-branch

# Back to an agent skill for multi-branch operations
stack-absorb
```

## Best practices

1. **Let the agent handle complexity**: Use agent skills for operations involving multiple branches
2. **Use regular commands for navigation**: Simple operations like $$stackit tree$$ don't need AI assistance
3. **Review suggestions**: The agent will explain what it's doing—review before proceeding
4. **Leverage context awareness**: The skills understand your stack structure and can make intelligent decisions

## Troubleshooting

If agent skills aren't working:

1. Verify installation:
   ```bash
   stackit agent install
   ```

2. Check for updates:
   ```bash
   brew upgrade stackit
   ```

3. Consult the [troubleshooting guide](../guide/troubleshooting.md)

## Next steps

- [View all CLI commands →](../cli/reference.md)
- [Workflows →](../workflows/index.md)
- [Other integrations →](index.md)
