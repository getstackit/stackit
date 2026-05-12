# Architecture

This document defines the runtime layering for Stackit's Go codebase.

The goal is to keep core stack behavior reusable across the CLI, API server, TUI, tests, and future published Go packages.

## Layers

### Core

Core packages implement stack state and Git-backed domain behavior.

- `internal/git`
- `internal/engine`
- small domain/support packages that are independent of UI and transport

Core packages:

- own stack metadata, branch relationships, and Git operations
- do not know about Cobra, Bubble Tea, HTTP, terminal formatting, or config loading
- should remain safe to reuse from multiple entry points

### Use Cases

Use case packages orchestrate business operations such as create, track, restack, submit, and info.

Target location:

- `internal/usecase/<name>/`

Transitional location:

- `internal/actions/<name>/`

Use cases should expose:

- request structs
- result structs
- narrow dependency interfaces or dependency bundles
- optional event/prompt interfaces for progress and interaction

Use cases should not:

- load config from disk
- discover the repo root
- construct GitHub clients
- write directly to terminal output
- import Cobra, Bubble Tea models, or terminal styling packages

If a use case needs configuration, the caller should resolve it first and pass the final values in the request.

If a use case needs prompts or progress reporting, define an interface next to the use case and let adapters implement it.

### Adapters

Adapters translate between delivery mechanisms and use cases.

Examples:

- `internal/cli/*` - Cobra commands, flag parsing, terminal output
- `internal/api/*` - HTTP request/response mapping, SSE, OpenAPI-backed JSON shapes
- `internal/tui/*` - Bubble Tea models and interactive handlers
- `internal/github` - concrete GitHub integration

Adapters may:

- parse flags or HTTP input
- render terminal or JSON output
- implement prompt/progress interfaces
- map use case results into transport-specific response shapes

Adapters should not contain the core stack orchestration themselves.

### Bootstrap / Runtime Wiring

Bootstrap packages assemble concrete dependencies for a given runtime.

Examples:

- `internal/app`
- config loading in `internal/config`
- executable entry points in `apps/cli` and `apps/server`

Bootstrap is responsible for:

- repo discovery
- loading layered config
- constructing the engine
- constructing concrete GitHub clients
- wiring loggers, output sinks, and runtime flags

Bootstrap should call into use cases or adapters. It should not become the place where business logic accumulates.

## Dependency Direction

Dependencies should point inward:

`bootstrap -> adapters -> use cases -> core`

Not every path exists in every flow, but the important rule is that inner layers must not depend on outer ones.

In particular:

- core must not depend on use cases or adapters
- use cases must not depend on CLI, API, TUI, or concrete GitHub clients
- adapters may depend on use cases and core
- bootstrap may depend on everything needed to assemble the runtime

## Practical Rules

When adding or refactoring an operation:

1. Put the orchestration in a use case package.
2. Pass resolved options into the request struct instead of loading config inside the operation.
3. Pass dependencies explicitly instead of passing `*app.Context`.
4. Return structured results and typed events instead of writing output directly.
5. Keep terminal rendering, JSON mapping, and interactive prompts in adapters.

## Transitional Guidance

The repository still contains substantial logic under `internal/actions/*`.

Until the codebase finishes migrating to `internal/usecase/*`, treat `internal/actions/*` as use cases in transition:

- prefer explicit deps over `*app.Context`
- keep new logic free of direct output/styling
- move config loading to callers
- define interfaces for prompts and progress
- keep concrete GitHub wiring outside the action

New reusable business logic should follow this pattern even if it remains in `internal/actions/*` temporarily.
