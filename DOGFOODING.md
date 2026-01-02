# Dogfooding Stackit

This guide explains how to use Stackit to manage the Stackit repository itself.

## Terminology

Stackit uses specific terminology to describe branch relationships:

- **Trunk**: The main development branch (usually `main`). All stacks eventually merge into trunk.
- **Stack**: A linear sequence of dependent branches. Each branch in the stack is based on the one below it.
- **Parent**: The branch directly below the current branch in the stack.
- **Child**: A branch based on the current branch.
- **Upstack**: Moving away from trunk, toward children/descendants.
- **Downstack**: Moving toward trunk, toward parents/ancestors.
- **Restack**: Updating a branch to be based on its updated parent (rebasing).
- **Submit**: Pushing branches to GitHub and creating/updating PRs.
- **Sync**: Updating local trunk and restacking local branches.
- **Absorb**: Folding staged changes into the relevant commits in the stack automatically.

## Quick Start

### 1. Build the Binary

```bash
just build
```

This creates a `stackit` binary and a convenient `st` symlink in the current directory.

### 2. Initialize Stackit

```bash
./st init
```

This will:
- Detect your trunk branch (defaults to `main`)
- Create `.git/.stackit_config` with trunk configuration
- Set up Stackit for the repository

### 3. Basic Usage

Use the `st` symlink for a better experience:

```bash
# View your branch tree
./st log

# View only the current branch's stack (ancestors and descendants)
./st log --stack

# View with interactive scrolling and collapsing
./st log -i
```

## Recommended Workflow

Stackit is designed for a "micro-branching" workflow where each logical change is its own branch.

### 1. Create a new branch

Start from `main` (trunk) or any existing branch:

```bash
# Create a new branch stacked on top of current
./st create feature-part-1 -m "feat: first part of my change"
```

### 2. Stack another change

Work on the next part without waiting for review:

```bash
# Create another branch on top of feature-part-1
./st create feature-part-2 -m "feat: second part of my change"
```

### 3. Make changes and "Absorb"

If you find a bug in `feature-part-1` while working on `feature-part-2`:

1.  Make the fix.
2.  Stage the changes: `git add .`
3.  Run `./st absorb`. Stackit will automatically figure out which branch the changes belong to and fold them in.

### 4. Restack after changes

If you manually rebase or modify a parent branch, restack the children:

```bash
./st restack
```

### 5. Submit for Review

Submit your entire stack to GitHub:

```bash
./st submit --stack
# or use the short alias:
./st ss
```

This will:
- Force-push each branch to your fork.
- Create or update a Pull Request for each branch.
- Maintain the PR chain on GitHub by setting each PR's base to its parent branch.

### 6. Keep in Sync

Update your local repository and stacks:

```bash
./st sync
```

This pulls from the remote trunk and restacks your local branches. It also prompts to delete branches that have been merged.

## Navigation

Quickly move around your stack:

- `./st up`: Move to the child branch.
- `./st down`: Move to the parent branch.
- `./st top`: Move to the very top of the stack.
- `./st bottom`: Move to the branch just above trunk.
- `./st trunk`: Checkout the trunk branch.

## Useful Commands

- `./st log`: Visualize the stack. Use `-i` for interactive mode.
- `./st info`: Show detailed info about the current branch and its place in the stack.
- `./st checkout <branch>`: Smart checkout that understands stack relationships.
- `./st absorb`: Automatically fold staged changes into the correct branches.
- `./st sync`: Keep your stack in sync with trunk and clean up merged branches.
- `./st delete`: Safely delete a branch and its metadata.
- `./st undo`: Undo the last Stackit operation.

## Tips

1. **Use the `st` alias**: It's faster to type and recommended for all commands.
2. **Interactive Log**: Run `./st log -i` to explore your stacks with a terminal UI.
3. **Rebuild often**: After making changes to Stackit's code, run `just build` to update your `st` binary.
4. **Test in isolation**: Use the `testhelpers` package to test commands without affecting your main repo.

## Troubleshooting

### "not a git repository"
Make sure you are in the root of the stackit repository (where `.git` is located).

### "out of sync with trunk"
If your stack is far behind `main`, run `./st sync`.

### Resetting Metadata
If Stackit's internal state gets confused, you can re-initialize:
```bash
./st init --reset
```
