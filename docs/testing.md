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

## Organizing New Tests

Prefer the cheapest test shape that proves the behavior:

1. Pure functions and formatting belong in normal package unit tests.
2. Engine or action behavior should use fake collaborators when possible.
3. Git-backed scenario tests should cover important repository interactions, not every branch of pure logic.
4. Remote, worktree, sync, merge, and spawned CLI flows belong in integration-style tests.

When a package mixes cheap unit tests with expensive scenario tests, split the expensive cases behind an integration build tag or move them to an integration package. That lets `test:unit` and `test:fast` remain meaningful without losing coverage in `mise run test`.
