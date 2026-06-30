# `co` / `checkout` — Performance Analysis

**Tier:** deep (hot path; runs many times per session).

## Call graph

```
NewCheckoutCmd.RunE
  ├─ if --quiet and exact branch/trunk:
  │    globalOpts.EngineLoadMode = &LoadModeBranchesOnly
  │
  └─ common.RunWithOptions                   internal/cli/common/common.go
       ├─ app.GetContextWithWriter
       │    ├─ git.NewRunner / DiscoverRepoRoot
       │    ├─ config.LoadConfig
       │    ├─ output.NewFileLogger
       │    └─ engine.NewEngine
       │         ├─ LoadModeBranchesOnly: GetAllBranchNames only
       │         └─ default LoadModeShared: shared metadata, local metadata lazy
       └─ Engine.IsInManagedWorktree() unless SkipManagedWorktreeCheck
  └─ common.Checkout → actions.CheckoutAction
       ├─ resolveBranchName
       │    ├─ eng.BranchNames().Contains(input)  (O(1) exact match, returns early)
       │    └─ eng.AllBranches() only for scope/fuzzy fallback
       ├─ getWorktreeSwitchInfo
       │    ├─ Engine.GetStackRootForBranch
       │    └─ Engine.GetWorktreeForStack
       ├─ Engine.CheckoutBranch
       │    └─ git.runner.CheckoutBranch
       │         └─ native `git checkout <branch>`
       └─ printBranchInfo                    (skipped if --quiet)
            └─ ReadBranchStatuses(target + up to 10 ancestors)
```

## Where time goes (largest -> smallest, typical case)

1. **Bootstrap**. Exact quiet checkout can use `LoadModeBranchesOnly`; interactive, fuzzy, and non-quiet checkout still need broader engine state for branch resolution, worktree switching, and branch info.
2. **Native `git checkout`**. The prior go-git `worktree.Status()` + `worktree.Checkout()` path has been removed. The remaining checkout cost is Git's own dirty-tree safety and ref/index update.
3. **`printBranchInfo`**. This now uses `ReadBranchStatuses` for the target branch and up to ten ancestors rather than issuing one status check per branch. It is still optional informational work and is skipped by `--quiet`.
4. **`IsInManagedWorktree()`**. Still runs for checkout because checkout needs worktree-switch behavior.

## Proposed wins (ranked by expected impact / risk)

### 1. Expand lightweight bootstrap beyond exact quiet checkout *(high impact, medium risk)*

> **Status:** Partially done. The opt-in and the per-branch promotion mechanism
> both exist. `NewCheckoutCmd.RunE` opts exact quiet `co <branch>` and
> `co --trunk --quiet` into `LoadModeBranchesOnly`
> (`internal/cli/navigation/checkout.go:37`). Lazy single-branch promotion is
> already implemented via `ensureBranchSharedLoaded`
> (`internal/engine/engine_impl.go:271`), which loads one branch's shared
> metadata without triggering the full `ensureSharedLoaded` batch — accessors
> like `GetParent` and `ReadBranchStatuses` already call it. The remaining work
> is to extend the CLI opt-in beyond the quiet path.

Exact quiet `co <branch>` and `co --trunk --quiet` already opt into `LoadModeBranchesOnly`. The remaining common cases are:

- exact non-quiet checkout, where branch info needs a small ancestor slice rather than full repo metadata;
- fuzzy checkout, where branch names are enough until a candidate is chosen;
- interactive checkout, where remote/worktree display data may need promotion but not always at startup.

Use branch-list mode first, then promote only the chosen branch's stack/worktree metadata when needed.

### 2. `printBranchInfo` short-circuit/config flag *(small impact, free)*

`printBranchInfo` is informational. It is already behind `--quiet` and uses batched status reads; add a config option to disable it by default for users who prioritize checkout latency over guidance.

### 3. Keep auditing managed-worktree checks *(small impact, command-specific)*

> **Status:** Partially done. The `SkipManagedWorktreeCheck` opt-out already exists
> (`app.GlobalOptions.SkipManagedWorktreeCheck`, honored in
> `RunWithOptions` at `internal/cli/common/common.go:47`, and set by
> `ApplyReadOnlyCurrentBranch` / `RunReadOnlyCurrentBranch`). Checkout itself
> still needs `IsInManagedWorktree` (`internal/engine/engine_worktree.go:315`).

Remaining work is an audit, not a change in `co`: find commands that reuse
checkout helpers or command wrappers but never switch worktrees, and route them
through `RunReadOnlyCurrentBranch` (or set `SkipManagedWorktreeCheck`) so they
skip the managed-worktree probe.

## How to validate

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit co <branch> --quiet' \
  'stackit co <branch>' \
  'git checkout <branch>'
```

The delta between `co --quiet` and `git checkout` is now mostly lightweight bootstrap plus Stackit command overhead. The delta between `co` and `co --quiet` is branch info and any metadata promotion needed for worktree switching.
