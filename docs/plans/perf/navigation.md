# Navigation commands — Performance Analysis

Covers: `up`, `down`, `parent`, `children`, `top`, `bottom`, `trunk`, `main`. **Tier:** medium (high frequency, but the per-command logic is microseconds — bootstrap usually dominates).

These commands are bundled because they share one performance story: once the engine state is available, the actual navigation logic is mostly in-memory graph walks.

## Per-command call summary

| Command | After bootstrap | Ends with checkout? |
|---|---|---|
| `parent` (`internal/cli/navigation/parent.go`) | branches-only bootstrap + one current-branch parent lookup | no |
| `children` (`children.go`) | one `graph.ChildBranches()` cache hit | no |
| `down` (`down.go`) | walks parent chain in cache | yes (`common.Checkout`) |
| `up` (`up.go`) | builds `graph`, walks children; with `--to` builds `Range(RecursiveChildren)` per child | yes |
| `top` (`top.go`) → `actions/navigation.SwitchBranchAction` | walks children to leaf, may prompt | yes |
| `bottom` (`bottom.go`) → same | walks parent chain to trunk-adjacent | yes |
| `trunk` (default) (`trunk.go`) | branches-only bootstrap + walks parent chain + `config.LoadConfig` | no |
| `trunk --add` | `GetAllBranchNames` + `LoadConfig` + `Save` | no |
| `main` / `m` (`main.go`) | nothing | yes (`actions.CheckoutOptions{CheckoutTrunk: true}`) |

## Where time goes

1. **Bootstrap / metadata promotion** — see `co.md`. `parent` and default `trunk` already use `LoadModeBranchesOnly` and skip the managed-worktree check. `children` and graph-heavy commands still need broader metadata.
2. **Native `git checkout`** for commands that end with a branch switch. The previous go-git status/checkout tax is gone; remaining cost is Git's checkout work plus Stackit command overhead.
3. **`up --to <branch>`** specifically: for each child of the current branch, calls `graph.Range(child, {RecursiveChildren: true})` (`internal/cli/navigation/up.go:79`), then `slices.Contains` over the result. If the current branch has K children and a total of N descendants, this is O(K x N). On a deep tree with many siblings, this is the only non-bootstrap cost worth noting. Cheap fix: do one downstack DFS rooted at the current branch, build a child-to-leaves map, and look up `toBranch` in O(1).
4. **`trunk --add`** does a redundant `GetAllBranchNames` to verify the branch exists. Engine state already has this list. Substitute `eng.GetBranch(trunkName).Exists()` or check membership in `e.state.branches`.
5. **`findTrunkForBranch`** (`trunk.go:138`) calls `config.LoadConfig` from scratch even though `app.Context.Config` is already populated. Use `ctx.Config`.

## Proposed wins (ranked)

### 1. Expand lightweight bootstrap for graph-based navigation *(high impact, medium risk)*

`parent` and default `trunk` already use branches-only mode. The remaining read-only and graph-adjacent commands need:
- current branch (one HEAD read)
- the metadata ref for the current branch (to find its parent)
- optionally the metadata refs for its direct children

This is at most 3-10 metadata reads versus the N branch reads that a full shared-metadata bootstrap performs. A lazy engine mode where metadata is fetched per branch/stack on first access would make `children` and the early validate steps for `up`/`down`/`top`/`bottom` cheaper.

### 2. `up --to` should DFS once, not K x N *(small impact, low risk)*

`internal/cli/navigation/up.go:75–88`: build `descendantsOf := map[branchName]bool` by walking from each child once. Then `descendantsOf[toBranch]` is O(1). Saves real work only on wide stacks with `--to`, but the current code is unnecessarily quadratic.

### 3. `trunk` should reuse `ctx.Config` *(trivial)*

`handleAddTrunk` (`trunk.go:71`) and `findTrunkForBranch` (`trunk.go:140`) both call `config.LoadConfig` again. Bootstrap already loaded it into `ctx.Config`. Substitute.

### 4. `trunk --add` should skip `GetAllBranchNames` *(trivial)*

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

`parent` and default `trunk` measure the branches-only path. `children` measures graph metadata promotion. The delta to `down`/`main` is the checkout cost.
