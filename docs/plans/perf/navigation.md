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
| `trunk` (default) (`trunk.go`) | branches-only bootstrap + walks parent chain (trunks list from `ctx.Config`) | no |
| `trunk --add` | `GetAllBranchNames` + `LoadConfig` + `Save` | no |
| `main` / `m` (`main.go`) | nothing | yes (`actions.CheckoutOptions{CheckoutTrunk: true}`) |

## Where time goes

1. **Bootstrap / metadata promotion** — see `co.md`. `parent` and default `trunk` already use `LoadModeBranchesOnly` and skip the managed-worktree check. `children` and graph-heavy commands still need broader metadata.
2. **Native `git checkout`** for commands that end with a branch switch. The previous go-git status/checkout tax is gone; remaining cost is Git's checkout work plus Stackit command overhead.
3. **`up --to <branch>`** specifically: for each child of the current branch, calls `graph.Range(child, {RecursiveChildren: true})` (`internal/cli/navigation/up.go:79`), then `slices.Contains` over the result. If the current branch has K children and a total of N descendants, this is O(K x N). On a deep tree with many siblings, this is the only non-bootstrap cost worth noting. Cheap fix: do one downstack DFS rooted at the current branch, build a child-to-leaves map, and look up `toBranch` in O(1).
4. **`trunk --add`** does a redundant `GetAllBranchNames` (a git subprocess) to verify the branch exists, even though the engine already loaded the full local branch list into `state.branches` at construction (`loadBranchList` / `GetAllBranchNames`). Substitute `ctx.Engine.BranchNames().Contains(trunkName)` (the `*BranchSet` from `engine.BranchNames()`).
5. **`findTrunkForBranch`** already reuses `ctx.Config` — it takes a `trunks []string` parameter sourced from `ctx.Config.AllTrunks()` (`trunk.go:131`) and no longer calls `config.LoadConfig`. The only remaining `config.LoadConfig` call is in `handleAddTrunk` (`trunk.go:75`), where it loads a *writable* `*GitConfig` for `AddTrunk`/`Save`; `ctx.Config` is the read-only `Configurer` interface and lacks those methods, so it is not a drop-in substitute.

## Proposed wins (ranked)

### 1. Expand lightweight bootstrap for graph-based navigation *(medium impact, medium risk)*

> **Status:** Partially done. The lazy engine infrastructure exists and is applied to the single-branch commands; the graph-based commands still bootstrap full.
>
> Already in place:
> - Lazy `LoadMode`s with on-demand promotion: `LoadModeBranchesOnly`, `LoadModeShared`, `LoadModeFull` (`internal/engine/engine.go:167-184`), promoted via `ensureSharedLoaded`/`ensureLocalLoaded` (`engine_impl.go:249`, `:310`).
> - **Per-branch** lazy shared-metadata load: `ensureBranchSharedLoaded` (`engine_impl.go:271`), wired into the branch-status accessors used by parent/down/trunk (`engine_branch_status.go`, `engine_reader.go:101`).
> - `parent` uses `common.RunReadOnlyCurrentBranch` (`parent.go:24`); default `trunk` applies `common.ApplyReadOnlyCurrentBranch` (`trunk.go:34`). Both resolve to `LoadModeBranchesOnly` + skip the managed-worktree check (`internal/cli/common/common.go:72-82`).

Remaining work — the graph-based commands still use the full `common.Run` path (`LoadModeFull`):
- `children` (`children.go:25`), `up` (`up.go:40`), `top` (`top.go:27`), `bottom` (`bottom.go:25`) build the whole `Graph()` over `AllBranches()`, so they need shared metadata for the stack — but none of them read frozen / NeedsPRBodyUpdate / PR state, so they could drop to `LoadModeShared` and skip the `BatchReadLocalMetadata` pass.
- `down` (`down.go:30`) only walks the parent chain via `GetParent()`, which already triggers per-branch promotion — it is the strongest candidate for `LoadModeBranchesOnly`.

Measure first: for these commands the win is skipping the local-metadata batch (and, for `down`, deferring shared reads to the visited branches), not the per-branch reads the infrastructure already provides.

### 2. `up --to` should DFS once, not K x N *(small impact, low risk)*

`internal/cli/navigation/up.go:75–99` (the `graph.Range` call is at `up.go:79`): build `descendantsOf := map[branchName]bool` by walking from each child once. Then `descendantsOf[toBranch]` is O(1). Saves real work only on wide stacks with `--to`, but the current code is unnecessarily quadratic.

### 3. `trunk` should reuse `ctx.Config` *(trivial)*

> **Status:** Mostly done. `findTrunkForBranch` no longer loads config — it takes a `trunks []string` parameter from `ctx.Config.AllTrunks()` (`trunk.go:131`, `:137`), and `handleShowTrunk` reads `ctx.Config` directly.

The only remaining `config.LoadConfig` call is in `handleAddTrunk` (`trunk.go:75`). It is **not** a trivial substitution: it needs a writable `*GitConfig` to call `AddTrunk` + `Save`, but `ctx.Config` is the read-only `Configurer` interface (`internal/config/interface.go`), which exposes neither method. Closing this would require either type-asserting `ctx.Config.(*config.GitConfig)` or widening the interface — weigh that against the fact that `--add` is a rare, write-path command where the extra read is negligible. Likely not worth doing.

### 4. `trunk --add` should skip `GetAllBranchNames` *(trivial)*

`trunk.go:64` calls `ctx.Engine.GetAllBranchNames(ctx)`, which spawns a git subprocess. The engine already loaded the full local branch list into `state.branches` at construction (every `LoadMode` runs `loadBranchList` / `rebuildInternal`, both backed by `GetAllBranchNames`). Substitute `ctx.Engine.BranchNames().Contains(trunkName)` — `BranchNames()` returns the cached `*BranchSet` (`internal/engine/branch_set.go:23`) for an O(1) in-memory check, no subprocess. (Note: there is no `AllBranchNames()` or `HasBranch` helper; `BranchNames()` is the right entry point.)

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
