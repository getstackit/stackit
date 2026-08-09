---
icon: material/download
title: Install Stackit
description: Install stackit via Homebrew on macOS or Linux. Set up shell integration, completions, and initialize your repository for stacked changes.
---

# Installation

## Requirements

- **Git 2.36+** — required, not optional. Stack operations use
  `git worktree list -z` to see which branches are checked out where, and
  stackit refuses to run them on older Git rather than guess.
- **GitHub CLI (`gh`)** for PR operations

Check your version with `git --version`. Some still-supported distributions ship
older Git than this — Ubuntu 22.04 has 2.34 and Debian 11 has 2.30 — so you may
need to upgrade Git before stackit will work. Run $$stackit doctor$$ after
installing to confirm your environment.

## Homebrew (macOS and Linux)

The easiest way to install stackit is via Homebrew:

```bash
brew install getstackit/tap/stackit
```

After installation, you can use either `stackit` or `st` (short alias).

## Shell integration

!!! tip "Recommended"
    Enable shell integration to automatically change directories when creating worktrees with `stackit create -w`.

Add one of the following to your shell configuration:

=== "zsh"

    ```bash
    # ~/.zshrc
    eval "$(stackit shell zsh)"
    ```

=== "bash"

    ```bash
    # ~/.bashrc
    eval "$(stackit shell bash)"
    ```

=== "fish"

    ```fish
    # ~/.config/fish/config.fish
    stackit shell fish | source
    ```

This is separate from shell completions. You likely want both:

```bash
# zsh example:
eval "$(stackit completion zsh)"
eval "$(stackit shell zsh)"
```

## Shell completions

Enable tab completion for stackit commands:

=== "zsh"

    ```bash
    # ~/.zshrc
    eval "$(stackit completion zsh)"
    ```

=== "bash"

    ```bash
    # ~/.bashrc
    eval "$(stackit completion bash)"
    ```

=== "fish"

    ```fish
    # ~/.config/fish/config.fish
    stackit completion fish | source
    ```

## Verify installation

Check that stackit is installed correctly:

```bash
stackit --version
```

## Initialize your repository

In your Git repository, run:

```bash
stackit init
```

This command:

1. Detects your trunk branch (usually `main` or `master`)
2. Prepares the repository for stacking
3. Offers to install optional integrations:
   - **GitHub Actions** — CI checks for branch locking
   - **Git hooks** — Prevent commits and pushes to locked branches ([learn more](../integrations/git-hooks.md))
   - **AI agent files** — Integration files for Claude Code, Codex, and Cursor

You can skip the interactive prompts with `stackit init --skip-integrations` or install integrations later using the individual commands (`stackit github install`, `stackit precommit install`, `stackit prepush install`, `stackit agent install`).

## Next steps

- [Create your first stack →](stack.md)
- [Learn core concepts →](../guide/concepts.md)
