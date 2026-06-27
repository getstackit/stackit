# Testing Best Practices

## Performance is Critical

**Fast tests are non-negotiable.** Slow tests kill developer productivity and discourage running tests frequently. Every millisecond matters when tests run hundreds of times per day.

### Why Performance Matters

- Developers skip slow tests, leading to broken commits
- CI feedback loops become painful bottlenecks
- Slow tests discourage TDD and iterative development
- Test suite runtime compounds: 100 tests × 100ms overhead = 10 seconds wasted

### Performance Guidelines

1. **Always use `t.Parallel()`** - Serial tests waste CPU cores
2. **Use `NewTestShellInProcess(t)`** - Saves ~8ms per command vs spawning processes
3. **Only use `WithRemote()` when necessary** - Remote setup adds overhead
4. **Use template repos** - The test infrastructure clones from cached templates, don't bypass this
5. **Minimize git operations** - Each git command has overhead; batch when possible
6. **Use stack fixtures** - `CreateLinearStack3()` is optimized; don't hand-roll setup

### Performance Anti-Patterns

```go
// BAD - spawns new process for each command
sh := NewTestShell(t, binaryPath)

// GOOD - runs CLI in-process
sh := NewTestShellInProcess(t)

// BAD - sets up remote when not needed
sh := NewTestShellInProcess(t, WithRemote())
sh.Run("create feature -m 'test'")  // No push/pull, didn't need remote

// GOOD - no remote for local-only operations
sh := NewTestShellInProcess(t)
sh.Run("create feature -m 'test'")

// BAD - forgetting parallel
func TestSomething(t *testing.T) {
    // runs serially, blocking other tests
}

// GOOD - parallel execution
func TestSomething(t *testing.T) {
    t.Parallel()
}
```

## Test Structure

### Where Tests Live

- **Unit tests**: Same package as the code (`*_test.go` alongside source)
- **Integration tests**: `internal/integration/` for CLI command testing
- **Test helpers**: `testhelpers/` package with shared utilities

### Running Tests

```bash
mise run check           # fmt, lint, and fast tests (use during development)
mise run test:fast       # Fast unit tests (~30s)
mise run test:integration  # Integration tests (~90s)
mise run test            # All tests
mise run test:pkg ./internal/git  # Tests for a specific package
```

### Choosing Validation Level

**Don't always run `mise run check`.** Match validation scope to change scope:

| Change | Validation | Why |
|--------|------------|-----|
| Comment/doc fix | `mise run compile` | Just verify it builds |
| Variable rename | `mise run lint` | Catches style issues, no behavior change |
| Bug fix in one pkg | `mise run test:pkg ./pkg` | Test only what changed |
| Multi-pkg change | `mise run check` | Full fast validation |
| Engine changes | `mise run test` | Need integration coverage |

See `.claude/rules/validation.md` for the full decision guide.

## Integration Tests with TestShell

Use `TestShell` for integration tests - it provides a fluent interface that reads like terminal sessions:

```go
func TestFeature(t *testing.T) {
    t.Parallel()
    sh := NewTestShellInProcess(t)

    sh.Write("feature.txt", "content").
        Run("create feature -m 'Add feature'").
        OutputContains("Created branch").
        OnBranch("feature")
}
```

### TestShell Creation Options

```go
// Basic setup - no remote (fastest)
sh := NewTestShellInProcess(t)

// With remote - for tests needing push/pull/sync
sh := NewTestShellInProcess(t, WithRemote())
```

Only use `WithRemote()` when the test actually needs remote operations. Tests without remotes are faster.

### TestShell Methods

| Method | Purpose |
|--------|---------|
| `Run("command")` | Execute stackit command |
| `RunExpectError("command")` | Execute command expecting failure |
| `Git("command")` | Execute raw git command (use sparingly) |
| `Write(file, content)` | Create/modify file and stage it |
| `Checkout(branch)` | Switch branches |
| `Modify(file, content)` | Modify file and run `stackit modify` |

### Assertions

```go
sh.OnBranch("expected-branch")           // Assert current branch
sh.HasBranches("a", "b", "main")         // Assert exact branch list
sh.OutputContains("expected text")       // Assert last command output
sh.OutputNotContains("unexpected")       // Assert absence in output
sh.CommitCount("main", "feature", 2)     // Assert commit count between refs
sh.ExpectBranchParent("child", "parent") // Assert stack structure
```

### Stack Fixtures

Use built-in fixtures for common stack patterns:

```go
// Linear stack: main -> a -> b -> c
sh.CreateLinearStack3()
// or with custom names:
sh.CreateLinearStack("feat", "test", "docs")

// Diamond stack: main -> parent -> [child1, child2]
sh.CreateDiamondStack()
```

## Scenario-Based Tests

Use `Scenario` for tests that need direct Engine access:

```go
func TestEngineOperation(t *testing.T) {
    s := scenario.NewScenario(t, nil)
    s.WithLinearStack3().
        Checkout("b").
        ExpectStackStructure(map[string]string{
            "a": "main",
            "b": "a",
            "c": "b",
        })
}
```

## Unit Tests

### Table-Driven Tests

Use table-driven tests for multiple cases:

```go
func TestSanitizeBranchName(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {
            name:     "spaces replaced with hyphens",
            input:    "my feature branch",
            expected: "my-feature-branch",
        },
        {
            name:     "special characters removed",
            input:    "feature!@#$",
            expected: "feature",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            result := SanitizeBranchName(tt.input)
            require.Equal(t, tt.expected, result)
        })
    }
}
```

### Scene-Based Unit Tests

For tests needing a Git repository without full CLI:

```go
func TestGitOperation(t *testing.T) {
    scene := testhelpers.NewSceneParallel(t, func(s *testhelpers.Scene) error {
        return s.Repo.CreateChangeAndCommit("initial", "init")
    })

    // Use scene.Repo for git operations
    err := scene.Repo.CreateAndCheckoutBranch("feature")
    require.NoError(t, err)
}
```

## Test Conventions

### Always Use Parallel Tests

```go
func TestFeature(t *testing.T) {
    t.Parallel()  // Top-level

    t.Run("subtest", func(t *testing.T) {
        t.Parallel()  // Each subtest too
        // ...
    })
}
```

### Use `require` Over `assert`

Prefer `require` for early failure - don't continue after assertion fails:

```go
// GOOD - stops on failure
require.NoError(t, err)
require.Equal(t, expected, actual)

// AVOID - continues after failure
assert.NoError(t, err)  // test continues even if err != nil
```

### Don't Add Code After Terminal Assertions

```go
// GOOD
func TestThing(t *testing.T) {
    result, err := doThing()
    require.NoError(t, err)
    require.Equal(t, expected, result)
}

// BAD - code after require is unreachable on failure
func TestThing(t *testing.T) {
    result, err := doThing()
    require.NoError(t, err)
    require.Equal(t, expected, result)
    cleanup()  // Never runs if assertion fails - use t.Cleanup instead
}
```

### Use `t.Helper()` in Helper Functions

```go
func assertBranchExists(t *testing.T, repo *GitRepo, branch string) {
    t.Helper()  // Makes failure point to caller, not this function
    branches, err := repo.ListBranches()
    require.NoError(t, err)
    require.Contains(t, branches, branch)
}
```

### Use `t.Cleanup()` for Cleanup

```go
func TestWithResource(t *testing.T) {
    resource := createResource()
    t.Cleanup(func() {
        resource.Close()  // Always runs, even on test failure
    })
    // ...
}
```

## In-Process vs Binary Execution

**Always use in-process execution** (`NewTestShellInProcess`) unless you have a specific reason not to.

The ~8ms savings per command adds up fast:
- A test with 10 commands saves 80ms
- 100 such tests save 8 seconds
- This compounds across the entire suite

Use binary execution (`NewTestShell(t, binaryPath)`) only when specifically testing:
- Process isolation behavior
- Signal handling
- Environment variable inheritance
- Exit codes from the actual binary

## Test Naming

Use descriptive names that explain the scenario:

```go
// GOOD
t.Run("squash parent restacks all children", func(t *testing.T) { ... })
t.Run("returns error when branch not found", func(t *testing.T) { ... })

// AVOID
t.Run("test1", func(t *testing.T) { ... })
t.Run("squash", func(t *testing.T) { ... })
```

## GitHub Merge Method Coverage

When testing sync, restack, branch cleanup, merged-branch detection, or merge
orchestration, cover the GitHub merge method that matters for the behavior:

| Method | Test shape |
|--------|------------|
| Merge commit | PR branch tip is reachable from trunk; ancestry-based detection should work. |
| Squash merge | PR changes land as one combined commit; original PR commits are not ancestors of trunk. Include a multi-commit PR when testing patch/diff equivalence. |
| Rebase merge | PR commits are replayed onto trunk with new SHAs; original PR commits are not ancestors of trunk. |

Do not rely on a single-commit squash test to prove squash support: `git cherry`
can already patch-match that case. Multi-commit squash tests are the important
safety coverage for replay/reparenting bugs.

## Debugging Failing Tests

Set `DEBUG=1` to preserve test directories:

```bash
DEBUG=1 go test -run TestFailingTest ./internal/integration/
```

The test directory path is printed and won't be cleaned up, allowing inspection.

## Common Pitfalls

1. **Forgetting `t.Parallel()`** - Tests run serially without it, wasting time and CPU cores
2. **Using `NewTestShell` instead of `NewTestShellInProcess`** - Process spawning adds ~8ms per command
3. **Using `WithRemote()` unnecessarily** - Remote setup adds overhead; only use for push/pull/sync tests
4. **Using `NewScene` instead of `NewSceneParallel`** - `NewScene` uses `os.Chdir()` which breaks parallel tests
5. **Using `assert` instead of `require`** - Tests continue past failures, causing confusing cascading errors
6. **Not using `WithRemote()` when needed** - Sync/push/pull tests fail without a remote
7. **Using raw git commands** - Prefer stackit commands via `sh.Run()` to test actual behavior
