# Stackit

Go-based CLI tool for managing stacked changes in Git repositories.

**Tech stack:** Go 1.25, Cobra, Just (justfile)

## Architecture

- `cmd/stackit`: CLI entry point and command definitions.
- `internal/actions`: High-level business logic for CLI commands (create, submit, sync, undo, etc.).
- `internal/engine`: Core logic for managing stacked branches, metadata state, and branch relationships.
- `internal/git`: Low-level Git operations (executing git commands, reading config, managing refs).
- `internal/utils`: Shared utilities for branch naming, sanitization, and UI helpers.

## Requirements

**All changes must pass tests and lint before committing:**

```bash
just check             # Runs fmt, lint, and fast tests (recommended)
just test-fast         # Run fast unit tests (~30s)
just test-integration  # Run integration tests (~90s)
just test              # Run all tests
just test-pkg ./pkg    # Run tests for a specific package (e.g. ./internal/git)
just lint              # Run linter
```

**Workflow:** Run `just check` during development for quick feedback. Run `just test` before submitting PRs to ensure all tests pass.

## Build

```bash
just build   # Builds ./stackit binary
just deps    # Install dependencies
```

## Commit Messages

Use **Conventional Commits**:

```
<type>[optional scope]: <description>
```

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`

**Examples:**
- `feat: add branch traversal functionality`
- `fix: resolve merge conflict detection issue`
- `refactor: simplify merge plan logic`

## Using Stackit for Commits and PRs

This repo uses stackit for managing stacked changes. **Always use stackit commands instead of raw git for branch operations:**

| Instead of | Use |
|------------|-----|
| `git checkout -b` + `git commit` | `stackit create` |
| `gh pr create` | `stackit submit` |
| `git rebase` | `stackit restack` |

**Workflow:**
1. Run `stackit log` to understand current stack state before making changes
2. Use `stackit create` to create new branches with commits (auto-generates branch names)
3. Use `stackit submit` to push and create/update PRs
4. Use `stackit sync` to pull latest from trunk and cleanup merged branches

**Available skills:** Run `/stackit` for the full guide, or use specific commands:
- `/stack-create` - Create a stacked branch
- `/stack-submit` - Submit PRs
- `/stack-status` - View stack health
- `/stack-fix` - Diagnose and fix issues

## Implementation Details

### Metadata Handling
Stackit manages branch relationships and PR state using custom Git references and notes. 
- **Branch Metadata**: Stored in `refs/stackit/metadata/` for each branch.
- **PR Information**: Managed through the `Engine` which abstracts the storage of PR titles, bodies, and status.
- **State Management**: The `internal/engine` package is the source of truth for the stack structure. Always use the `Engine` to query or modify branch relationships.

