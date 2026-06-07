# Stackit

Go CLI for managing stacked changes in Git repositories.

**Tech stack:** Go 1.26, Cobra, bubbletea, mise | Next.js 16, React 19, TypeScript, Tailwind CSS 4, pnpm

## Stacking Workflow (CRITICAL)

**This project dogfoods stackit.** All changes must use stacked PRs.

### Why Stack?

**Small PRs get reviewed faster.** PRs under 400 lines get reviewed in hours, not days. Stacking lets you ship big features as small, focused PRs.

**Each branch = one reviewable unit.** Stack related changes so reviewers can approve phases independently. The refactor can land while tests are still in review.

**Easy to revert.** If one phase causes issues, revert just that PR without affecting the rest of the feature.

**Clear history.** Your git log tells the story of how a feature evolved, not a jumbled mess of "fix typo" commits.

### Decision Framework

**Stack when ANY of these apply:**
- Change has 2+ distinct logical phases
- Total diff would exceed ~400 lines
- Different reviewers needed for different parts
- You want early feedback on foundational work
- Changes touch unrelated subsystems

**Single commit/PR is fine when:**
- Small, focused change (<200 lines)
- Single logical unit
- Trivial fix that doesn't benefit from splitting

**Default to stacking.** When in doubt, create a new stacked branch. It's easy to fold branches together later; splitting is harder.

### Workflow

```bash
# Stage changes FIRST (required!)
git add -A

# Create stacked branch with commit. For agent workflows, prefer piping the
# message via `-F -` (or reading from a file with `-F <path>`) so the literal
# message text stays out of the command line — see "Keeping Permission Rules
# Stable" in .claude/rules/stackit-workflow.md.
echo "feat: description" | stackit create -F -

# Continue working, stage more changes, create more branches...
git add -A
echo "feat: next phase" | stackit create -F -

# Submit when ready
stackit submit
```

`-m "literal message"` still works for one-off use; switch to `-F -` once a
flow is repeated so permission rules cover every invocation.

### Handling PR Feedback

```bash
stackit checkout <branch>  # Go to branch with feedback
# Make changes...
stackit modify             # Amend commit
stackit restack --upstack   # Update children of this branch
stackit submit             # Update PRs
```

### Common Pitfalls

| Mistake | Fix |
|---------|-----|
| Forgetting to stage | Always `git add -A` before `create` |
| Using `git commit` for new branch | Use `stackit create` instead |
| Using `git checkout -b` | Use `stackit create` instead |
| Manual rebase | Use `stackit restack --upstack` (or `--all-stacks`) |
| Using `gh pr create` | Use `stackit submit` |

**Use `/stack-plan` when starting from scratch** to plan the stack structure before writing code.

## Architecture

```
apps/
├── cli/        CLI entry point
├── api/        HTTP API + embedded static web assets
├── st-tui/     TUI storyboard binary
└── web/        Next.js frontend (see docs/web.md)
    ├── src/app/         Pages and layouts
    ├── src/components/  UI components, providers
    ├── src/hooks/       Custom React hooks
    └── src/lib/         API client, SSE, utilities
internal/
├── actions/       Business logic for commands (create, submit, sync, etc.)
├── cli/           Command definitions, handlers
├── api/           HTTP handlers, middleware, watcher
├── engine/        Core stack logic, metadata, branch relationships
├── git/           Low-level git operations
├── tui/           Terminal UI components (see docs/tui.md)
├── config/        Configuration system (see docs/config.md)
└── utils/         Shared utilities
api/openapi/       API contract source of truth
```

## Documentation

- `docs/config.md` - Configuration system, keys, layered config
- `docs/hooks.md` - Lifecycle hook configuration, env vars, approval flow, recipes
- `docs/performance.md` - Remote-operation tuning (SSH reuse), diagnosing slow commands with the tracer
- `docs/recipes.md` - Step-by-step file lists for cross-cutting changes
- `docs/shipping.md` - Merge strategies, consolidation, multi-stack shipping
- `docs/tui.md` - TUI patterns, BaseModel, styling, components
- `docs/web.md` - Web app architecture, components, data flow, styling
- `docs/worktree.md` - Worktree management, create vs attach, workflows

## Common Development Tasks

| Task | Guide |
|------|-------|
| Add a new config option | See "Adding New Configuration" in `docs/config.md` |
| Add submit command config | See "Submit Command Config Flow" in `docs/config.md` |
| Add a new GitHub client method | See `docs/recipes.md` (5 files to update) |
| Add a new API response field | See `docs/recipes.md` (7 files backend-to-frontend) |
| Add a new CLI command | See `docs/recipes.md` |
| Add TUI component | See `docs/tui.md` |
| Add worktree functionality | See `docs/worktree.md` |
| Modify merge/shipping logic | See `docs/shipping.md` |
| Add web component | See `docs/web.md` |
| Add API consumer in web app | See `docs/web.md` |
| Modify web styling | See `docs/web.md` |

## CLI Tools

Use these tools instead of standard alternatives for better performance:

| Tool | Use Instead Of | Purpose |
|------|----------------|---------|
| `rg` (ripgrep) | `grep` | Fast text search, respects .gitignore |
| `fd` | `find` | Fast file search, intuitive syntax |
| `ast-grep` | `sed` for code | AST-based code search and refactoring |
| `jq` | - | JSON processing |
| `yq` | - | YAML processing |
| `tokei` | `wc -l` | Code statistics and language breakdown |

**Examples:**
```bash
rg "func.*Create" --type go          # Search for Create functions in Go files
fd "\.go$" internal/                  # Find all Go files in internal/
ast-grep -p 'fmt.Errorf($$$)' .       # Find all fmt.Errorf calls
tokei                                 # Get codebase statistics
jq '.dependencies' package.json       # Parse JSON
```

## Requirements

**All changes must pass tests and lint before committing:**

```bash
mise run check         # Runs fmt, lint, and fast tests
mise run test:fast     # Run fast unit tests (~30s)
mise run test:integration  # Run integration tests (~90s)
mise run test          # Run all tests
mise run test:pkg ./pkg    # Run tests for a specific package (e.g. ./internal/git)
mise run lint          # Run linter
mise run web:test      # Run web app tests
mise run check:web     # Run web tests + typecheck + build
```

**Workflow:** Run `mise run check` during development for quick feedback. Run `mise run test` before submitting PRs to ensure all tests pass.

## Validation Strategy

**Use the lightest validation that covers your change.** Running full test suites for every edit wastes time.

| Change Type | Command | Time |
|-------------|---------|------|
| Docs/comments only | `mise run compile` | ~2s |
| Refactoring/style | `mise run lint` | ~5s |
| Single package logic | `mise run test:pkg ./internal/foo` | ~10s |
| Multi-package logic | `mise run check` | ~30s |
| Engine/integration | `mise run test` | ~2min |
| Web component change | `mise run web:test` | ~10s |
| Web + API change | `mise run check:web` | ~30s |

**Decision guide:**
- `compile` - Quick "does it build?" check for trivial changes
- `lint` - Catches style issues without running tests
- `test:pkg` - Targeted testing for isolated changes
- `check` - Standard development workflow
- `test` - Full suite before PRs or for engine changes

See `.claude/rules/validation.md` for detailed guidance on when to use each level.

## Build

```bash
mise run build           # Builds ./bin/stackit and ./bin/stackit-server
mise run deps            # Install dependencies
mise run web:build       # Build web app static export
mise run web:sync-static # Copy web build into apps/server/static/ for embedding
```

## Commit Messages

Use **Conventional Commits**: `<type>: <description>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`

When the subject line is not enough to explain the change, add a body:

```text
<type>: <description>

Explain why the change exists and any important context or tradeoffs.
Prefer concise prose over diff-style detail.
```

Body guidance:
- Use a blank line between subject and body
- Focus on why and important context, not a line-by-line summary of what changed
- Omit the body for small, obvious changes; include it for broader changes where the intent is not clear from the subject alone

## Metadata

Stackit stores branch relationships in `refs/stackit/metadata/`. The `Engine` package is the source of truth for stack structure. Always use `Engine` to query/modify branch relationships.
