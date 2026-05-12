# Package Dependencies

The codebase follows a layered architecture. The point is not only to avoid import cycles, but to keep stack behavior reusable across the CLI, API server, TUI, and future library surfaces.

See `docs/architecture.md` for the full model.

## Target Layering

```
bootstrap/runtime wiring
    ↓
adapters
    ↓
actions
    ↓
core
```

Dependencies should point inward. Inner layers must not depend on outer ones.

## Package Rules

```
internal/git
    Lowest layer. Keep dependencies minimal.

internal/engine
    CAN import: git and small domain/support packages
    CANNOT import: actions, tui, cli, api, app, output

internal/actions/*
    Canonical home for reusable business operations
    CAN import: engine, git, small domain/support packages
    CANNOT import: cli, api, tui, output, app/bootstrap packages

internal/tui
    Adapter layer for Bubble Tea models and interactive views
    CAN import: actions handler interfaces, engine, git, tui/components/*, tui/style
    CANNOT import: cli/*

internal/cli/*
    Adapter layer for Cobra commands, flag parsing, and terminal rendering
    CAN import: actions, tui, engine, app/bootstrap packages, output

internal/api/*
    Adapter layer for HTTP handlers, SSE, and transport mapping
    CAN import: actions, engine, app/bootstrap packages, internal/contracts/http

internal/app
internal/config
    Bootstrap/runtime wiring
    Responsible for repo discovery, config loading, engine construction, and client construction
    Do not let business orchestration accumulate here

internal/github
    Concrete integration adapter
    Keep GitHub client construction and transport details here, not in actions

internal/contracts/http
    Source of truth for API response/request shapes shared with the web app

apps/web/
    Consumes: internal/contracts/http via the API contract
    CANNOT import: any Go packages directly
```

## Action Boundary Rules

Action code should expose:

- request structs
- result structs
- narrow dependency interfaces or dependency bundles
- event/prompt interfaces when interaction is required

Action code should not:

- load config from disk
- discover the repo root
- construct GitHub clients
- call Cobra APIs
- render terminal output or styling directly

If an action needs config values, resolve them in bootstrap or the adapter layer and pass the final values into the request.

## Common Pitfalls

1. **TUI importing action data types directly**: If `internal/tui` needs a preview/result shape from `internal/actions/X`, define a local struct in `tui` with the same fields and have the caller convert.

2. **Action taking `*app.Context`**: This couples orchestration to bootstrap concerns. Prefer explicit dependency structs and request structs.

3. **Action rendering output**: Return structured results or emit events. Let CLI/TUI/API adapters decide how to present them.

4. **Action loading config or constructing GitHub clients**: Resolve configuration and integrations before calling the action.

5. **Circular handler dependencies**: Prompt/progress interfaces may live next to the action, but implementations belong in `internal/cli/*` or `internal/tui/*`.

## Example: Avoiding Cycles

```go
// BAD - creates an adapter/action dependency in the wrong direction
// internal/tui/preview.go
import "stackit.dev/stackit/internal/actions/move"
func RenderPreview(p move.Preview) string { ... }

// GOOD - define local adapter data
// internal/tui/preview.go
type PreviewData struct {
    SourceBranch string
    // ... same fields
}
func RenderPreview(p PreviewData) string { ... }

// Caller in cli/ converts:
previewData := tui.PreviewData{
    SourceBranch: preview.SourceBranch,
    // ...
}
```
