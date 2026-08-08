# Documentation Rules

## When to Update Docs

- New CLI commands → Add to README.md Command Reference
- New workflows → Add examples to Common Workflows in README.md
- Architecture or layering changes → Update `docs/architecture.md`
- Configuration changes → Update `docs/config.md`
- TUI changes → Update `docs/tui.md`
- Merge/shipping changes → Update `docs/shipping.md`
- Worktree changes → Update `docs/worktree.md`; if ownership, hold, or
  reconciliation behavior changes, also `.claude/rules/worktree-safety.md` and
  the hold section of `docs/multiplayer.md`
- Metadata storage or ref-write changes → Update `docs/metadata.md`
- Hook phases or when hooks run → Update `docs/hooks.md` and README
- Web component changes → Update `docs/web.md`
- API endpoint changes → Update `api/openapi/stackit.yaml` and `docs/web.md`
- Web build/config changes → Update `docs/web.md`
- New reusable operation patterns or package moves → Update `docs/recipes.md` and `.claude/rules/package-dependencies.md`
- Changes to `--json` output shape → Update the Automation & CI section in
  README.md; scripts key on these fields

## Command Help Text

The `Long` description in Cobra commands should include concrete examples:

```go
Long: `Syncs your stack with the remote repository.

Examples:
  stackit sync              # Sync current stack
  stackit sync --all        # Sync all branches`,
```

## Technical Docs (`docs/`)

- `docs/absorb.md` - Absorb command: target selection, stash/restore safety model, restack modes
- `docs/architecture.md` - Runtime layering, action boundaries, adapters, bootstrap
- `docs/config.md` - Configuration keys, layered config, adding new keys
- `docs/hooks.md` - Lifecycle hook configuration, env vars, approval flow, recipes
- `docs/metadata.md` - Ref namespaces, branch/stack metadata, transactions, CAS writes
- `docs/multiplayer.md` - Landed-work detection, the un-pushed trunk guard, the worktree hold, reparent invariants
- `docs/performance.md` - Remote-operation tuning, diagnosing slow commands
- `docs/recipes.md` - Step-by-step file lists for cross-cutting changes
- `docs/tui.md` - TUI patterns, styling, components
- `docs/testing.md` - Test tiers and layer-specific testing guidance
- `docs/shipping.md` - Merge strategies, commands, flags, flow diagrams
- `docs/web.md` - Web app architecture, components, data flow, styling
- `docs/worktree.md` - Worktree management, create vs attach, ownership, warm starts, workflows

Keep these up-to-date when modifying related systems.

### What to check in `docs/shipping.md`

When changing merge commands (`internal/cli/stack/merge/` or `internal/actions/merge/`):

- **Adding/removing flags** → Update the Command Reference flag tables
- **Adding/removing subcommands** → Update Quick Reference and add a Command Reference section
- **Changing default flag values** (e.g. `--wait`) → Update the Wait Behavior section and flag tables
- **Changing types or constants** → Update the Key Types section
- **Adding/removing files** → Update the Core Files listing
- **Changing function names or flow** → Update the Flow diagrams
