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
| `MarkNeedsPRBodyUpdate` in a loop | `BatchMarkNeedsPRBodyUpdate(branchNames)` |
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
for _, name := range branchNames {
    _ = eng.MarkNeedsPRBodyUpdate(name)
}

// GOOD - parallel reads + single atomic ref update
_ = eng.BatchMarkNeedsPRBodyUpdate(branchNames)
```

When adding new operations that touch multiple branches or refs, prefer designing batch APIs from the start. Use `UpdateRefsBatch` for atomic multi-ref writes and `BatchReadLocalMetadata` / `BatchReadMetadata` for parallel reads.

### Branch-state reads return values, not a global cache

Multi-branch reads of revisions, commits, diff stats, and divergence points go through the `Batch*` readers above, which return value maps you resolve once and pass around (often composed at the call site, e.g. `commits := eng.BatchCommits(...)`). Do **not** reintroduce an ambient/global revision cache or a `PreloadBranchData`-style warm-up. That pattern was removed deliberately: a globally cached revision could go stale between a read and a later read within the same operation (a correctness hazard, not just a perf concern), and explicit passed-around values are far easier to reason about. The revision-keyed diff-stat/commit-count memoization that remains is safe precisely because it is keyed by content, not warmed globally.

Forge status (CI, checks, review state) is a **separate concern** from branch state. Fetch it once via the GitHub batchers and join it to branch data at the consumer/render layer; never fold live forge status into the branch-state readers.

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
