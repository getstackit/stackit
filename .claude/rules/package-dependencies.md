# Package Dependencies

The codebase follows a layered architecture. The point is not only to avoid import cycles, but to keep stack behavior reusable across the CLI, API server, TUI, and future library surfaces.

See `docs/architecture.md` for the full model.

## Target Layering

```
bootstrap/runtime wiring
    ↓
adapters
    ↓
use cases
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
    CANNOT import: usecase/actions, tui, cli, api, app, output

internal/usecase/*
    Target home for reusable business operations
    CAN import: engine, git, small domain/support packages
    CANNOT import: cli, api, tui, output, app/bootstrap packages

internal/actions/*
    Transitional location for existing business operations
    Follow the same rules as internal/usecase/* for new code whenever practical
    In particular: avoid new direct imports of tui, output, and app in core action logic

internal/tui
    Adapter layer for Bubble Tea models and interactive views
    CAN import: usecase/actions handler interfaces, engine, git, tui/components/*, tui/style
    CANNOT import: cli/*

internal/cli/*
    Adapter layer for Cobra commands, flag parsing, and terminal rendering
    CAN import: usecase/actions, tui, engine, app/bootstrap packages, output

internal/api/*
    Adapter layer for HTTP handlers, SSE, and transport mapping
    CAN import: usecase/actions, engine, app/bootstrap packages, internal/contracts/http

internal/app
internal/config
    Bootstrap/runtime wiring
    Responsible for repo discovery, config loading, engine construction, and client construction
    Do not let business orchestration accumulate here

internal/github
    Concrete integration adapter
    Keep GitHub client construction and transport details here, not in use cases

internal/contracts/http
    Source of truth for API response/request shapes shared with the web app

apps/web/
    Consumes: internal/contracts/http via the API contract
    CANNOT import: any Go packages directly
```

## Use Case Boundary Rules

Use case code should expose:

- request structs
- result structs
- narrow dependency interfaces or dependency bundles
- event/prompt interfaces when interaction is required

Use case code should not:

- load config from disk
- discover the repo root
- construct GitHub clients
- call Cobra APIs
- render terminal output or styling directly

If a use case needs config values, resolve them in bootstrap or the adapter layer and pass the final values into the request.

## Common Pitfalls

1. **TUI importing use case data types directly**: If `internal/tui` needs a preview/result shape from `internal/actions/X` or `internal/usecase/X`, define a local struct in `tui` with the same fields and have the caller convert.

2. **Action/use case taking `*app.Context`**: This couples orchestration to bootstrap concerns. Prefer explicit dependency structs and request structs.

3. **Action/use case rendering output**: Return structured results or emit events. Let CLI/TUI/API adapters decide how to present them.

4. **Action/use case loading config or constructing GitHub clients**: Resolve configuration and integrations before calling the use case.

5. **Circular handler dependencies**: Prompt/progress interfaces may live next to the use case, but implementations belong in `internal/cli/*` or `internal/tui/*`.

## Example: Avoiding Cycles

```go
// BAD - creates an adapter/use case dependency in the wrong direction
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
