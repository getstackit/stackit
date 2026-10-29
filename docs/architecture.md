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

### Actions

Action packages orchestrate business operations such as create, track, restack, submit, and info.

Canonical location:

- `internal/actions/<name>/`

Actions should expose:

- request structs
- result structs
- narrow dependency interfaces or dependency bundles
- optional event/prompt interfaces for progress and interaction

Actions should not:

- load config from disk
- discover the repo root
- construct GitHub clients
- write directly to terminal output
- import Cobra, Bubble Tea models, or terminal styling packages

If an action needs configuration, the caller should resolve it first and pass the final values in the request. When the action must also write config (e.g. persisting a chosen merge method), accept a narrow interface defined next to the action and let the caller pass the loaded config. `.golangci.yml` enforces this with a `forbidigo` rule that rejects `config.Load*` calls under `internal/actions/`. See "Using a Config Value in an Action" in `docs/config.md`.

If an action needs prompts or progress reporting, define an interface next to the action and let adapters implement it.

For generic prompts (confirm, single select, text input, multi-select), depend
on the narrow interfaces in `internal/actions/handler` (`Confirmer`,
`Selector`, `TextInputPrompter`, `MultiSelector`). An action that prompts
takes a `handler.Prompter` in its `Deps`; the CLI passes `ctx.Prompts()`,
which falls back to a non-interactive prompter (every prompt returns
`term.ErrInteractiveDisabled`) when no adapter set `ctx.Prompter`. The CLI
sets `common.TUIPrompter`. Whole-screen interactive flows (e.g. the move
target picker, the reorder editor) live in the adapter and hand the action
its result or implement an action-defined interface such as
`actions.OrderEditor`. The `depguard` rule `actions-no-tui` in
`.golangci.yml` rejects `internal/tui` imports from `internal/actions`.

#### Shared restack pipeline

Commands that report restack progress (`restack`, `sync`, `get`, `submit`)
choose their branches, then hand them to `actions.RunRestack`
(`internal/actions/restack_run.go`). It enables rerere, runs the groups
sequentially or in parallel worktrees, applies a `RestackConflictPolicy`
(enter the conflict workflow, report, or prompt then resolve), and reports every
branch to an `actions.RestackHandler` as one `RestackBranchEvent` type. Adapters
render that one type regardless of which command produced it, through
`common.FormatRestackRow` and `common.FormatRestackSummary`
(`internal/cli/common/restack.go`). A command that brackets the restack in its
own phase (sync, submit) passes `actions.RestackBranchFunc` to receive only the
per-branch events. Pre-run worktree holds and snapshots stay
with the caller, because their scope differs per command. Mutating commands that
restack as a side effect (`modify`, `move`, `fold`, …) still call
`RestackBranches` directly.

### Adapters

Adapters translate between delivery mechanisms and actions.

Examples:

- `internal/cli/*` - Cobra commands, flag parsing, terminal output
- `internal/api/*` - HTTP request/response mapping, SSE, OpenAPI-backed JSON shapes
- `internal/tui/*` - Bubble Tea models and interactive handlers
- `internal/github` - concrete GitHub integration

Adapters may:

- parse flags or HTTP input
- render terminal or JSON output
- implement prompt/progress interfaces
- map action results into transport-specific response shapes

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
- wiring loggers, output sinks, and runtime flags (the engine's git runner and
  metadata store receive their logger through `engine.Options.Logger`)

The engine never hands out its raw git runner or metadata store. Code outside
the engine that genuinely needs git below the engine abstraction receives a
narrow dependency from bootstrap instead: the GitHub integration gets
`app.Context.GHRunner` (`github.GitCommandRunner`), set when the context is
built. Tests reach raw state through `scenario.Scenario.Git` / `.Metadata`.

Bootstrap should call into actions or adapters. It should not become the place where business logic accumulates.

## Dependency Direction

Dependencies should point inward:

`bootstrap -> adapters -> actions -> core`

Not every path exists in every flow, but the important rule is that inner layers must not depend on outer ones.

In particular:

- core must not depend on actions or adapters
- actions must not depend on CLI, API, TUI, or concrete GitHub clients
- adapters may depend on actions and core
- bootstrap may depend on everything needed to assemble the runtime

## Practical Rules

When adding or refactoring an operation:

1. Put the orchestration in an `internal/actions/<name>/` package.
2. Pass resolved options into the request struct instead of loading config inside the operation.
3. Pass dependencies explicitly. Actions never see `*app.Context`: the entry
   point is `func Action(ctx context.Context, deps Deps, opts Options, ...)`,
   where `Deps` holds the package's exported consumer-side `Engine` interface
   plus whatever else it needs (`output.Output`, a lazy
   `GitHub func() github.Client`, the gh runner, the repo root, resolved
   config values or a narrow `Config` interface).
   Branch-rewriting actions embed `actions.MutationDeps` (output, `Here`,
   `Snapshots`, a `restack.Runner`). Adapters build deps at the boundary with
   `internal/app/actiondeps` (shared pieces: `actiondeps.Mutation`,
   `actiondeps.Restack`, `actiondeps.PRSync`, ...) and, for commands that
   delegate to other commands, `common.SubmitDeps` / `SyncDeps` /
   `MergeDeps` / `CheckoutDeps`. The `actions-no-app-context` depguard rule
   rejects `internal/app` imports from non-test files under
   `internal/actions/`.
4. Return structured results and typed events instead of writing output directly.
5. Keep terminal rendering, JSON mapping, and interactive prompts in adapters.
6. Read-only consumers take `engine.StackView` (or a narrower local interface
   composed from it) instead of `engine.Engine` / `engine.BranchReader`, and get
   shared view data (stack discovery, annotations, batched branch reads) from
   `internal/actions/stackview`.
7. Each action's `Engine` interface lists exactly the engine methods the
   package calls. Helpers that only need the engine take that narrow type
   rather than `engine.Engine`. Compose shared pieces instead of repeating them:
   `engine.StackView` / `engine.BranchLookup` for reads,
   `validation.BranchValidationEngine` / `GitOperationEngine` when the package
   runs those validators, `actions.MetadataPushEngine` for `PushMetadataOnly`,
   and `actions.RecoveryEngine` for abort/undo-style rollback. Test fakes then
   implement only the narrow interface, so a missing method is a compile error
   rather than a nil-embedded panic.

### Restack core

Every command that rebases branches goes through `internal/actions/restack`,
split so each half depends only on what it uses:

- `restack.Planner` (read-only `PlanEngine`) validates ancestry, builds the
  engine plan, trial-rebases it in a scratch worktree, and returns a `Plan`
  classifying each branch as apply, conflict, or blocked.
- `restack.Executor` (`ApplyEngine`, a `ProgressFunc`, a `ContinuationStore`,
  a `ConflictReporter`) applies a `Plan` under a `ConflictMode`: report
  conflicts and hold back their stacks, or stop at the first conflict and
  leave the repository mid-rebase for `stackit continue`.

Neither writes output directly: progress goes to the `ProgressFunc`, and
`restack.TextReporter` renders it for commands without a progress UI.
Converted actions get a `restack.Runner` through `actions.MutationDeps`; the
command-level pipeline (`PlanRestack`, `RestackAction`, `RunRestack`) takes
`actions.RestackDeps`. The CLI builds both with `actiondeps.Mutation` and
`actiondeps.Restack`.

## Transitional Guidance

The repository already uses `internal/actions/*` as the home for reusable orchestration.

When extending or adding actions:

- take `(ctx, Deps, ...)`, never `*app.Context`
- keep new logic free of direct output/styling
- keep config loading in callers (already enforced by lint)
- define interfaces for prompts and progress
- keep concrete GitHub wiring outside the action

New reusable business logic should follow this pattern inside `internal/actions/*`.
