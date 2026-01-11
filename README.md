# Stackit

[![Tests](https://github.com/getstackit/stackit/actions/workflows/test.yml/badge.svg)](https://github.com/getstackit/stackit/actions/workflows/test.yml)

**Stackit** is a command-line tool that makes working with stacked changes fast and intuitive.

## What is Stacking?

Stacked changes (or "stacked diffs") is a development workflow where you break a large feature into small, focused branches that build on top of each other. Instead of one massive Pull Request, you have a "stack" of smaller PRs. Stacks can be linear (a simple chain of branches) or they can branch out into a tree structure when you need to work on multiple parallel features that share a common base.

### How it helps engineers:

- **Faster Reviews**: Reviewers can process small, 50-line PRs much faster than a single 500-line PR.
- **Parallel Work**: You don't have to wait for a PR to be merged before starting the next part of your feature. Just stack a new branch on top.
- **Incremental Shipping**: Parts of a feature can be merged and deployed as they are approved, reducing the risk of large, complex merges.
- **Cleaner History**: Each PR represents a logical step in your feature's development, making the Git history easier to follow.

### The Stacked Workflow

```mermaid
graph BT
    main[main branch] --- B1[PR 1: API Changes]
    B1 --- B2[PR 2: Implementation]
    B2 --- B3[PR 3: UI Components]
    B2 --- B4[PR 4: CLI Support]
    B3 --- B5[PR 5: Integration Tests]

    style main stroke-dasharray: 5 5
```

Stacks naturally form a tree structure—a single branch can have multiple children when you need to work on parallel features. Stackit manages the complexity of this workflow—automatically handling rebases, keeping track of parent-child relationships, and submitting the entire stack to GitHub with a single command.

---

## Features

- 🌳 **Visual branch tree** — See your entire stack at a glance with `stackit log`
- 🔄 **Automatic restacking** — Keep all branches up to date when you rebase or modify a parent
- 📤 **Submit entire stacks** — Push all branches and create/update PRs in one command
- 🔀 **Smart merging** — Merge stacks bottom-up or squash top-down
- 🔧 **Absorb changes** — Automatically amend changes to the right commit in your stack
- 🧭 **Easy navigation** — Move `up`, `down`, `top`, or `bottom` of your stack
- 🧹 **Auto cleanup** — Detect and delete merged branches during `sync`
- 🎯 **Smart scoping** — Associate branches with Jira tickets, Linear IDs, or other logical scopes
- 🔒 **Branch protection** — `lock` or `freeze` branches to prevent accidental modifications
- 🔍 **Branch inspection** — Easily see parent/child relationships with `children` and `parent` commands
- ⚙️ **Advanced configuration** — Customize branch naming patterns and submit behavior
- 🤖 **AI assistant integration** — Generate integration files for Cursor and Claude Code
- 🐙 **GitHub Integration** — Install CI checks to prevent merging locked PRs
- ⚓ **Git Hooks** — Automatically validate branch state before committing with `precommit`
- 📂 **Worktrees** — Work on multiple stacks in parallel with dedicated directories

---

## Installation

### Homebrew (macOS and Linux)

```bash
brew install getstackit/tap/stackit
```

After installation, you can use either `stackit` or `st` (short alias).

### Shell Integration (Recommended)

Enable shell integration to automatically change directories when creating worktrees with `stackit create -w`. Add one of the following to your shell configuration:

```bash
# For zsh (~/.zshrc):
eval "$(stackit shell zsh)"

# For bash (~/.bashrc):
eval "$(stackit shell bash)"

# For fish (~/.config/fish/config.fish):
stackit shell fish | source
```

This is separate from shell completions. You likely want both:

```bash
# zsh example:
eval "$(stackit completion zsh)"
eval "$(stackit shell zsh)"
```

---

## Getting Started

### 1. Initialize Stackit
In your repository, run:
```bash
stackit init
```
This detects your trunk branch (usually `main`) and prepares the repo for stacking.

### 2. Create your first branch
Stage some changes, then create a branch:
```bash
git add internal/api.go
stackit create add-api -m "feat: add base api"
```

### 3. Stack another branch on top
Make more changes and create another branch:
```bash
git add internal/logic.go
stackit create add-logic -m "feat: implement logic"
```

### 4. Visualize the stack
See your current position in the stack:
```bash
stackit log
```
```
● add-logic ← you are here
│
◯ add-api
│
main
```

Stacks can also branch when you have parallel work:
```
◯ add-tests
│
│ ● add-ui ← you are here
├─┘
◯ add-logic
│
◯ add-api
│
main
```

### 5. Submit your PRs
Submit the entire stack to GitHub:
```bash
stackit submit
```
This pushes both branches and creates two PRs on GitHub, with `add-logic` correctly pointing its base to `add-api`.

### 6. Merge your stack
Once your PRs are approved, merge the entire stack:
```bash
stackit merge
```
This merges all approved PRs in your stack, bottom-up, and cleans up the merged branches.

---

## Claude Code Integration

Stackit includes specialized commands designed for Claude Code, providing intelligent automation for common stacking workflows. These commands understand stack context and can perform complex operations with minimal user input.

### Available Claude Commands

| Command | Description | When to Use |
|:---|:---|:---|
| `stack-status` | View current stack state, branch position, and health status | Getting oriented in a complex stack |
| `stack-create [branch-name]` | Create a new stacked branch with intelligent naming and commit messages | Adding a new feature branch to your stack |
| `stack-submit [--stack \| --draft]` | Submit branches as PRs with auto-generated descriptions | Creating or updating pull requests |
| `stack-sync` | Sync with trunk, cleanup merged branches, and restack | Keeping your stack up-to-date with main |
| `stack-restack` | Rebase all branches to ensure proper ancestry | Fixing branch relationships after changes |
| `stack-absorb` | Intelligently absorb working changes into correct commits | Applying fixes across multiple stack branches, with conflict resolution guidance |
| `stack-fix` | Diagnose and fix common stack issues | Resolving compilation errors or structural problems |

### Setting Up Claude Integration

```bash
stackit agent install
```

This creates the necessary integration files for Claude Code to use these specialized commands. The commands are designed to:

- **Understand Context**: Each command analyzes your current stack state and git status
- **Provide Validation**: Commands include quality checks and error handling
- **Guide Through Issues**: When conflicts or errors occur, commands provide step-by-step resolution guidance
- **Ensure Safety**: All commands prioritize data safety and provide undo capabilities

### Example Claude Workflow

```bash
# Claude can help with complex stacking operations
stack-create add-user-auth    # Creates branch with proper commit message
# Make changes...
stack-absorb                 # Intelligently distributes changes across commits
stack-fix                    # Diagnoses and fixes any issues
stack-submit --stack         # Creates/updates all PRs in the stack
```

---

## Command Reference

### Navigation
| Command | Description |
|:---|:---|
| `stackit log` | Display the branch tree |
| `stackit checkout` | Interactive branch switcher |
| `stackit up` / `down` | Move to the child or parent branch |
| `stackit top` / `bottom` | Move to the top or bottom of the stack |
| `stackit trunk` | Return to the main/trunk branch |
| `stackit children` | Show the children of the current branch |
| `stackit parent` | Show the parent of the current branch |

### Branch Management
| Command | Description |
|:---|:---|
| `stackit create [name]` | Create a new branch on top of current (use `-w` to create with worktree) |
| `stackit modify` | Amend the current commit (like `git commit --amend`) |
| `stackit absorb` | Intelligently amend changes to the correct commits in the stack |
| `stackit split` | Split the current branch's commits into multiple branches |
| `stackit squash` | Squash all commits on the current branch |
| `stackit fold` | Merge the current branch into its parent |
| `stackit pop` | Delete current branch but keep its changes in working tree |
| `stackit delete` | Delete the current branch and its metadata |
| `stackit rename [name]` | Rename the current branch and update metadata |
| `stackit scope [name]` | Manage logical scope (Jira ticket, Linear ID) for current branch |
| `stackit lock [branch]` | Lock a branch and its downstack (prevent local changes) |
| `stackit unlock [branch]` | Unlock a branch and its upstack (allow local changes) |
| `stackit freeze [branch]` | Freeze a branch (prevent local changes, local only) |
| `stackit unfreeze [branch]` | Unfreeze a branch |

### Worktree Management
| Command | Description |
|:---|:---|
| `stackit worktree list` | List all managed worktrees |
| `stackit worktree remove <stack>` | Remove a worktree and unregister it |
| `stackit worktree open <stack>` | Print path to worktree (for `cd $(st worktree open foo)`) |

### Stack Operations
| Command | Description |
|:---|:---|
| `stackit restack` | Rebase all branches in the stack to ensure proper ancestry |
| `stackit get [branch|PR]` | Sync a stack or specific PR from remote |
| `stackit foreach` | Run a shell command on each branch in the stack (default: upstack) |
| `stackit submit` | Push branches and create/update GitHub PRs (alias: `ss` for `--stack`) |
| `stackit sync` | Pull trunk, delete merged branches, and restack |
| `stackit merge` | Merge approved PRs and clean up merged branches |
| `stackit reorder` | Interactively reorder branches in your stack |
| `stackit move` | Rebase a branch (and its children) onto a new parent |
| `stackit pluck` | Extract a single branch from a stack (reparents children to grandparent) |

### Integrations
| Command | Description |
|:---|:---|
| `stackit agent install` | Setup integration files for Cursor and Claude Code |
| `stackit github install` | Install GitHub Action CI checks for branch locking |
| `stackit precommit install` | Install git pre-commit hook for branch state validation |
| `stackit precommit uninstall` | Remove the git pre-commit hook |

### Utilities & System
| Command | Description |
|:---|:---|
| `stackit undo` | Restore the repository to a state before a command |
| `stackit doctor` | Diagnose and fix issues with your stackit setup |
| `stackit info` | Show detailed info about the current branch |
| `stackit track` / `untrack` | Manually start/stop tracking a branch with stackit |
| `stackit config` | Manage stackit configuration |
| `stackit debug` | Dump debugging information about recent commands and stack state |
| `stackit continue` / `abort` | Continue or abort an interrupted operation (like a rebase) |

### Global Flags

These flags are available on all `stackit` commands:

| Flag | Description |
|:---|:---|
| `--cwd <path>` | Working directory in which to perform operations. |
| `--debug` | Write debug output to the terminal. |
| `--interactive` | Enable interactive features like prompts, pagers, and editors. (Default: true) |
| `--no-interactive` | Disable all interactive features. |
| `--verify` | Enable git hooks (pre-commit, etc.). (Default: true) |
| `--no-verify` | Disable git hooks. |
| `--quiet`, `-q` | Minimize output to the terminal. Implies `--no-interactive`. |

---

## Common Workflows

### Updating after Code Review
If you receive feedback on a branch in the middle of your stack:
1. `stackit checkout <branch>` to move to that branch.
2. Make your changes and run `stackit modify`.
3. Run `stackit restack` to update all child branches.
4. Run `stackit submit` to update the PRs on GitHub.

### Using `stackit absorb`
`absorb` is like magic for stacked PRs. If you have small fixes for multiple branches in your stack, just stage them all and run `stackit absorb`. Stackit will figure out which changes belong to which branch and amend them automatically.

### Syncing with the Main Branch
To keep your stack up-to-date with `main`:
```bash
stackit sync
```
This pulls the latest changes from `main`, deletes branches that have already been merged, and restacks your remaining branches on top of the new `main`.

### Working on Multiple Stacks in Parallel
To work on separate features simultaneously, each in their own directory:
```bash
# Create a new stack with its own worktree
stackit create my-feature -m "feat: start new feature" -w

# This creates:
# - A new branch 'my-feature' tracked by stackit
# - A worktree at ../your-repo-stacks/my-feature/
```
Navigate to the worktree with:
```bash
cd $(stackit worktree open my-feature)
```
Worktrees are automatically cleaned up during `stackit sync` when their stack is merged.

### Collaborating on Stacks
To work on a stack created by someone else or on another machine:
```bash
# Sync an entire stack by providing a PR number or branch name
stackit get 123
```
By default, `get` **freezes** the fetched branches locally. This prevents accidental local modifications while you build on top of them, without affecting the original author's metadata. Use `stackit unfreeze` if you need to modify them.

---

## Frozen & Locked Branches

Stackit provides two ways to protect branches from accidental modification.

### Frozen Branches (Local)
**Frozen** status is strictly **local** to your machine. It's the recommended way to protect branches you've fetched from others.
- **Use Case**: You want to stack your own work on top of someone else's PRs without accidentally changing their commits.
- **Behavior**: Prevents `modify`, `squash`, `absorb`, and `restack`. `st sync` will update frozen branches by hard-resetting them to their remote tracking branch instead of rebasing.
- **Commands**: `st freeze`, `st unfreeze`

### Locked Branches (Shared)
**Locked** status is **shared** with everyone collaborating on the stack via remote metadata.
- **Use Case**: You want to signal to your team that a set of branches are stable and should not be modified by anyone.
- **Behavior**: Same restrictions as frozen branches, but visible to all users who `st get` or `st sync` the stack.
- **Commands**: `st lock`, `st unlock`

---

### Automation & CI
Stackit is designed to be easily scriptable. Use global flags to control behavior in non-interactive environments:

```bash
# Run stackit on a specific repository from a script
stackit sync --cwd /path/to/repo --no-interactive --no-verify
```

---

## Configuration

Stackit supports several configuration options that can be managed via `stackit config`:

| Option | Description | Example |
|:---|:---|:---|
| `branch.pattern` | Customize how branch names are generated when not explicitly specified | `stackit config set branch.pattern "{username}/{date}/{message}"` |
| `submit.footer` | Control whether PRs include a footer linking back to the stack | `stackit config set submit.footer true` |
| `worktree.basePath` | Customize where worktrees are created | `stackit config set worktree.basePath "../my-stacks"` |
| `worktree.autoClean` | Auto-remove worktrees for merged stacks during sync (default: true) | `stackit config set worktree.autoClean false` |

### Interactive Configuration
Use the interactive TUI to manage all settings:
```bash
stackit config
```

### List Current Configuration
View all current configuration values:
```bash
stackit config --list
```

---

## Requirements

- **Git 2.25+**
- **GitHub CLI (`gh`)** for PR operations
- **Go 1.25+** (if building from source)

## Development

```bash
# Run tests and linter
just check

# Build locally
just build
```

## Philosophy

1. **Safety First**: Operations are non-destructive and can be undone with `stackit undo`.
2. **Speed**: Common operations should be fast and require minimal context switching.
3. **Visibility**: You should always know exactly where you are in your stack.
4. **Git Native**: Stackit uses standard Git refs and metadata under the hood.

## License

MIT

---

## Hack on Stackit

### Building from Source

Requires Go 1.25+:

```bash
git clone https://github.com/getstackit/stackit
cd stackit
go build -o stackit ./cmd/stackit
# Move to your PATH
mv stackit /usr/local/bin/
```

### Using Just

If you have [just](https://github.com/casey/just) installed:

```bash
just build
just install
```

The `just build` command will also create a local `st` symlink for convenience.

### Development

```bash
# Run tests and linter
just check

# Build locally
just build
```
