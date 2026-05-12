# Documentation Rules

## When to Update Docs

- New CLI commands → Add to README.md Command Reference
- New workflows → Add examples to Common Workflows in README.md
- Architecture or layering changes → Update `docs/architecture.md`
- Configuration changes → Update `docs/config.md`
- TUI changes → Update `docs/tui.md`
- Merge/shipping changes → Update `docs/shipping.md`
- Worktree changes → Update `docs/worktree.md`
- Web component changes → Update `docs/web.md`
- API endpoint changes → Update `api/openapi/stackit.yaml` and `docs/web.md`
- Web build/config changes → Update `docs/web.md`
- New reusable operation patterns or package moves → Update `docs/recipes.md` and `.claude/rules/package-dependencies.md`

## Command Help Text

The `Long` description in Cobra commands should include concrete examples:

```go
Long: `Syncs your stack with the remote repository.

Examples:
  stackit sync              # Sync current stack
  stackit sync --all        # Sync all branches`,
```

## Technical Docs (`docs/`)

- `docs/architecture.md` - Runtime layering, use case boundaries, adapters, bootstrap
- `docs/config.md` - Configuration keys, layered config, adding new keys
- `docs/tui.md` - TUI patterns, styling, components
- `docs/testing.md` - Test tiers and layer-specific testing guidance
- `docs/shipping.md` - Merge strategies, commands, flags, flow diagrams
- `docs/web.md` - Web app architecture, components, data flow, styling
- `docs/worktree.md` - Worktree management, create vs attach, workflows

Keep these up-to-date when modifying related systems.

### What to check in `docs/shipping.md`

When changing merge commands (`internal/cli/stack/merge/` or `internal/actions/merge/`):

- **Adding/removing flags** → Update the Command Reference flag tables
- **Adding/removing subcommands** → Update Quick Reference and add a Command Reference section
- **Changing default flag values** (e.g. `--wait`) → Update the Wait Behavior section and flag tables
- **Changing types or constants** → Update the Key Types section
- **Adding/removing files** → Update the Core Files listing
- **Changing function names or flow** → Update the Flow diagrams
