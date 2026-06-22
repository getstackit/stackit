# Navigation Commands

Commands for moving through your stack and viewing stack state.

> **CRITICAL:** Always run these commands with `stackit ... --no-interactive`.

## Core Navigation

| Command | Description |
|---------|-------------|
| `stackit tree --no-interactive` | Display the branch tree visualization |
| `stackit t --no-interactive` | Display the short branch tree |
| `stackit tree full --no-interactive` | Show tree with GitHub PR status and CI checks |
| `stackit checkout [branch] --no-interactive` | Switch to a specific branch |

## Movement Commands

| Command | Description |
|---------|-------------|
| `stackit up --no-interactive` | Move to the child branch |
| `stackit down --no-interactive` | Move to the parent branch |
| `stackit top --no-interactive` | Move to the top of the stack |
| `stackit bottom --no-interactive` | Move to the bottom of the stack |
| `stackit trunk --no-interactive` | Return to the main/trunk branch |

## Information Commands

| Command | Description |
|---------|-------------|
| `stackit children` | Show children of current branch |
| `stackit parent` | Show parent of current branch |
| `stackit info --no-interactive` | Show detailed branch info |

## Quick Tips

- Always run `stackit tree --no-interactive` first to understand your position
- Use `stackit checkout <branch> --no-interactive` to switch to a specific branch
- `stackit tree full --no-interactive` shows PR status - useful before submitting
