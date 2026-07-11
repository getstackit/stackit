# Code Style

## Go Patterns

- Early returns over deep nesting
- Meaningful names; single-letter only for loop indices
- Remove unused parameters entirely (don't use `_`)
- `switch` over if-else chains with 3+ conditions
- For boolean conditions: `switch { case cond1: ... case cond2: ... }`
- Use typed constants (enums) instead of boolean parameters for clarity at call sites

## Boolean Parameters

Avoid boolean parameters that are unclear at call sites. Use typed constants instead:

```go
// BAD - unclear what false, true means
CreateWorktree(ctx, branch, prefix, false, true)

// GOOD - self-documenting
CreateWorktree(ctx, branch, prefix, WorktreeCheckoutFull, WorktreePruneSkip)
```

Define typed constants:
```go
type WorktreeCheckoutMode int

const (
    WorktreeCheckoutFull WorktreeCheckoutMode = iota
    WorktreeCheckoutShallow
)
```

## Batch Operations (N+1 Prevention)

**Always use batch APIs for git and GitHub operations.** Calling individual operations in a loop creates N+1 performance problems — each call spawns a separate git process or HTTP request.

| Instead of | Use |
|------------|-----|
| per-branch PR-body-update marking in a loop | `MarkBranchesForPRBodyUpdate(ctx, branchNames)` |
| `ReadLocalMetadata` in a loop | `BatchReadLocalMetadata(branchNames)` |
| `UpdateRef` in a loop | `UpdateRefsBatch(ctx, updates)` |
| `DeleteRef` in a loop | `DeleteRefsBatch(ctx, refNames)` |
| `PushBranch` in a loop | `PushMetadataRefs(ctx, branches)` |
| `GetRevision` in a loop | `GetRevisions(branchNames)` |
| `GetDiffStats` / `GetCommitCount` in a loop | `BatchDiffStats(branches)` / `BatchBranchStats(branches)` |
| `GetAllCommits` in a loop | `BatchCommits(branches, format)` |
| `GetDivergencePoint` in a loop | `BatchDivergencePoints(branches)` |

**Why:** Each git command spawns a process (~2-5ms overhead). Each GitHub API call takes ~200-500ms. For N branches, a loop costs O(N × overhead) while a batch costs O(1) or O(N) with parallelism.

```go
// BAD - N git processes for N branches
for _, branch := range branches {
    div, _ := eng.GetDivergencePoint(branch.GetName())
    divPoints[branch.GetName()] = div
}

// GOOD - one batched, cache-backed pass
divPoints := eng.BatchDivergencePoints(branches)
```

When adding new operations that touch multiple branches or refs, prefer designing batch APIs from the start. Use `UpdateRefsBatch` for atomic multi-ref writes and `BatchReadLocalMetadata` / `BatchReadMetadata` for parallel reads.

### Branch-state reads return values, not a global cache

Multi-branch reads of revisions, commits, diff stats, and divergence points go through the `Batch*` readers above, which return value maps you resolve once and pass around (often composed at the call site, e.g. `commits := eng.BatchCommits(...)`). Do **not** reintroduce an ambient/global revision cache or a `PreloadBranchData`-style warm-up. That pattern was removed deliberately: a globally cached revision could go stale between a read and a later read within the same operation (a correctness hazard, not just a perf concern), and explicit passed-around values are far easier to reason about. The revision-keyed diff-stat/commit-count memoization that remains is safe precisely because it is keyed by content, not warmed globally.

Forge status (CI, checks, review state) is a **separate concern** from branch state. Fetch it once via the GitHub batchers and join it to branch data at the consumer/render layer; never fold live forge status into the branch-state readers.

### Bound parallel fan-out

**Never spawn one goroutine per item over a caller-controlled slice.** Branch sets, ref lists, and spec lists are unbounded in size — a stack can have dozens of branches, and each parallel task on a cold cache may spawn a git subprocess. `wg.Add(1); go func(){…}()` inside a `for range items` loop fans out to as many concurrent goroutines (and subprocesses) as there are items, which exhausts file descriptors and thrashes the scheduler on large inputs.

Use a **bounded worker pool** instead. The work still runs in parallel, but at most `GOMAXPROCS` tasks at once:

```go
// BAD - one goroutine (and possibly one git process) per branch, unbounded
var wg sync.WaitGroup
for i, b := range branches {
    wg.Add(1)
    go func(i int, b Branch) {
        defer wg.Done()
        results[i] = expensive(b) // may spawn git on a cold cache
    }(i, b)
}
wg.Wait()

// GOOD - bounded pool; each worker writes only its own index, so no locking
type indexed struct{ i int; b Branch }
items := make([]indexed, len(branches))
for i, b := range branches { items[i] = indexed{i, b} }
utils.Run(items, func(it indexed) { results[it.i] = expensive(it.b) })
```

- **`utils.Run`** — default `GOMAXPROCS` workers. Reach for this first; it's what `batchByBranch` and the log/annotation builders use.
- **`utils.RunWithWorkers(items, n, fn)`** — when you need a specific cap (e.g. to throttle network calls below the CPU count).
- **Semaphore** (`make(chan struct{}, maxConcurrency)`) — when goroutines must coordinate cancellation or collect results through a channel rather than write indexed slots. See `internal/engine/rebase_validator.go` for the canonical pattern (parent-context check before acquiring the semaphore so an outer cancel short-circuits queued siblings).

A single long-lived background goroutine running *alongside* the main work (one `go func` total, not one per item) is fine and does not need a pool.

## Error Handling

- Always handle errors explicitly (never `_`)
- Wrap with context: `fmt.Errorf("context: %w", err)`
- Return errors to callers; don't log and continue

## Testing

See `testing.md` for comprehensive testing guidelines. Key points:

- Table-driven tests for multiple cases
- Integration tests in `internal/integration/` using `NewTestShellInProcess(t)`
- Use `require` over `assert` for early failure
- Always use `t.Parallel()` for parallel test execution

## TUI

Use constants from `internal/tui/core/`:
```go
core.KeyCtrlC, core.KeyEsc, core.KeyQuit, core.KeyEnter
```

Never use string literals like `"ctrl+c"`.

Before creating types, check for existing ones:
```bash
rg "type.*KeyMap" internal/tui/
```

## General

- Clarity over cleverness
- Leave TODOs rather than unimplemented code
- No backwards compatibility unless specified
- Comments explain "why" not "what"
