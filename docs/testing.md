# Testing

Stackit has several kinds of Go tests. Keep the tier explicit so local checks stay fast and CI still covers the full behavior.

## Tiers

| Tier | Command | Scope |
| --- | --- | --- |
| Unit | `mise run test:unit` | Low-IO packages that do not need real git repositories, remotes, or CLI scenario flows. |
| Fast | `mise run test:fast` | Cheap packages for everyday development. Excludes git-scenario and integration packages. |
| Git scenarios | `mise run test:git` | Git-backed action, engine, CLI, PR, worktree, and testhelper packages outside the integration tree. |
| Integration | `mise run test:integration` | End-to-end CLI and repository behavior under `internal/integration` and integration package paths. |
| All | `mise run test` | Every Go package except vendored or frontend dependency packages. |

Package selection is centralized in `scripts/go-test-packages.sh`. Update that script when adding or reclassifying package-level tiers.

## Shared CLI binary

The all, git, and integration runners build the CLI once with
`scripts/with-test-binary.sh` and pass its path through `STACKIT_TEST_BINARY`.
The binary lives at a content-addressed path in the Go build cache, so unchanged
runs retain Go's test-result caching and concurrent revisions use separate files.
Fast and unit tiers do not pay for this build.

Direct `go test` and `test:pkg` invocations still build lazily per package when
they need a subprocess. To share a binary for a custom multi-package run:

```bash
bash scripts/with-test-binary.sh go test ./internal/cli/...
```

An explicitly supplied `STACKIT_TEST_BINARY` must be an absolute path to an
executable file. Test packages validate it and never delete a caller-owned binary.

## Organizing New Tests

Prefer the cheapest test shape that proves the behavior:

1. Pure functions and formatting belong in normal package unit tests.
2. Engine or action behavior should use fake collaborators when possible.
3. Git-backed scenario tests should cover important repository interactions, not every branch of pure logic.
4. Remote, worktree, sync, merge, and spawned CLI flows belong in integration-style tests.

When a package mixes cheap unit tests with expensive scenario tests, split the expensive cases behind an integration build tag or move them to an integration package. That lets `test:unit` and `test:fast` remain meaningful without losing coverage in `mise run test`.

## Testing by Layer

Keep the test shape aligned with the architecture in `docs/architecture.md`.

### Actions

Action tests should verify orchestration with fake collaborators whenever possible.

- inject fake engines, Git ports, GitHub ports, or prompt/event handlers
- assert on returned results, emitted events, and dependency calls
- avoid real terminal rendering and config loading in these tests

### Adapters

Adapter tests should stay focused on translation concerns.

- CLI tests: flag parsing, request construction, handler wiring, and terminal output
- API tests: request decoding, response mapping, status codes, and SSE payloads
- TUI tests: model updates, message flow, and rendering behavior

Do not use adapter tests as the primary place to prove business rules that belong in action tests.

### Bootstrap / Runtime Wiring

Bootstrap tests should cover repo discovery, config loading, and concrete dependency wiring.

- verify config values are resolved once and passed through correctly
- verify runtime defaults and error paths
- keep business behavior assertions in action tests

### Core

Core package tests should focus on engine, metadata, and Git-backed domain behavior.

- prefer package unit tests for pure logic
- use git-backed scenario tests only for repository behaviors that need a real repo

## Golden Output Transcripts

Golden transcripts let you review and iterate on a command's user-facing output as a
checked-in artifact, without running the real command against a git repository. They run in
microseconds and capture the full streamed output **including interactive prompts and the
user's scripted answer**.

The first harness covers `sync` (`internal/cli/stack/sync_golden_test.go`, transcripts in
`internal/cli/stack/testdata/sync/*.golden`). A scenario is a declarative list of steps —
phase starts, progress events, prompts + the user's answer — plus a final summary. Each
scenario is played through sync's **real** renderer (`SimpleSyncHandler` for events/summary;
the shared `describe*` helpers for prompt descriptions) and the output is ANSI-stripped and
compared to its golden file.

This tests **presentation**, not which events the action decides to emit (that is covered by
the action/integration tests). Because the real renderer is exercised, the transcripts also
make latent output formatting issues visible in review.

Regenerate after intentionally changing output:

```bash
go test ./internal/cli/stack -run Golden -update   # rewrite .golden files
go test ./internal/cli/stack -run Golden            # CI-style check (no -update)
```

To extend the pattern to another command, reuse the generic `testhelpers/golden` helper
(the `-update` flag, ANSI stripping, file compare with a line diff) and add a thin
transcript handler plus step constructors for that command's `Handler`. Use
`internal/cli/stack/sync_golden_test.go` as the template.
