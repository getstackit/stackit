# Navigation commands — Performance Analysis

Covers: `up`, `down`, `parent`, `children`, `top`, `bottom`, `trunk`, `main`. **Tier:** medium (high frequency, but the per-command logic is microseconds — bootstrap dominates).

These commands are bundled because they share one performance story: **bootstrap is the entire cost**. The actual navigation logic is in-memory graph walks over data already loaded by `rebuildInternal`.

## Per-command call summary

| Command | After bootstrap | Ends with checkout? |
|---|---|---|
| `parent` (`internal/cli/navigation/parent.go`) | one `GetParent()` cache hit | no |
| `children` (`children.go`) | one `graph.ChildBranches()` cache hit | no |
| `down` (`down.go`) | walks parent chain in cache | yes (`common.Checkout`) |
| `up` (`up.go`) | builds `graph`, walks children; with `--to` builds `Range(RecursiveChildren)` per child | yes |
| `top` (`top.go`) → `actions/navigation.SwitchBranchAction` | walks children to leaf, may prompt | yes |
| `bottom` (`bottom.go`) → same | walks parent chain to trunk-adjacent | yes |
| `trunk` (default) (`trunk.go`) | walks parent chain + `config.LoadConfig` | no |
| `trunk --add` | `GetAllBranchNames` + `LoadConfig` + `Save` | no |
| `main` / `m` (`main.go`) | nothing | yes (`actions.CheckoutOptions{CheckoutTrunk: true}`) |

## Where time goes

1. **`common.Run` bootstrap** (~`rebuildInternal` + `IsInManagedWorktree`) — see `co.md`. For `parent`, `children`, `trunk` (default), this is **100% of runtime**. The graph walk afterwards is microseconds.
2. **`worktree.Status()` + `worktree.Checkout()`** for the commands that end with a checkout. Same story as `co.md` items #1 + #3.
3. **`up --to <branch>`** specifically: for each child of the current branch, calls `graph.Range(child, {RecursiveChildren: true})` (`internal/cli/navigation/up.go:79`), then `slices.Contains` over the result. If the current branch has K children and a total of N descendants, this is O(K × N). On a deep tree with many siblings, this is the only non-bootstrap cost worth noting. Cheap fix: do one downstack DFS rooted at the current branch, build a child-to-leaves map, and look up `toBranch` in O(1).
4. **`trunk --add`** does a redundant `GetAllBranchNames` to verify the branch exists — engine state already has this list. Substitute `eng.GetBranch(trunkName).Exists()` or check membership in `e.state.branches`.
5. **`findTrunkForBranch`** (`trunk.go:138`) calls `config.LoadConfig` from scratch even though `app.Context.Config` is already populated. Use `ctx.Config`.

## Proposed wins (ranked)

### 1. Lite bootstrap for read-only navigation *(high impact, medium risk)*

`parent`, `children`, `trunk` (no flag), and the early "validate current branch" step of `up`/`down`/`top`/`bottom` only need:
- current branch (one HEAD read)
- the metadata ref for the current branch (to find its parent)
- optionally the metadata refs for its direct children

This is at most 3–10 metadata reads versus the N branch reads that `rebuildInternal` performs. A "lazy" engine mode where metadata is fetched per-branch on first access would make every navigation command (and `co <exact>`) essentially instant.

This is the same win listed as #2 in `co.md` — they share the fix.

### 2. Single checkout path optimization benefits all of `up`, `down`, `top`, `bottom`, `main` *(see co.md #1)*

Fix `git.runner.CheckoutBranch` (skip `worktree.Status()`) → every navigate-and-checkout command gets faster proportionally to working-tree size.

### 3. `up --to` should DFS once, not K × N *(small impact, low risk)*

`internal/cli/navigation/up.go:75–88`: build `descendantsOf := map[branchName]bool` by walking from each child once. Then `descendantsOf[toBranch]` is O(1). Saves real work only on wide stacks with `--to`, but the current code is unnecessarily quadratic.

### 4. `trunk` should reuse `ctx.Config` *(trivial)*

`handleAddTrunk` (`trunk.go:71`) and `findTrunkForBranch` (`trunk.go:140`) both call `config.LoadConfig` again. Bootstrap already loaded it into `ctx.Config`. Substitute.

### 5. `trunk --add` should skip `GetAllBranchNames` *(trivial)*

`trunk.go:60`. The engine already has the branch list. `slices.Contains(eng.AllBranchNames(), name)` (or a `Engine.HasBranch` helper) avoids spawning a git command.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit parent' \
  'stackit children' \
  'stackit trunk' \
  'stackit down' \
  'stackit main'
```

`parent`/`children`/`trunk` should be ~equal — they all just pay bootstrap. The delta to `down`/`main` is the checkout cost. The delta from `stackit parent` to a shell-level `pwd` is your bootstrap budget.
