# `scope` — Performance Analysis

**Tier:** medium (low frequency; one branch at a time; network push at the end).

## Call graph

```
newScopeCmd → common.Run → scope.Action               internal/actions/scope/action.go:22
  ├─ --show: read explicit + resolved scope from cache; print            (no network)
  ├─ --unset:
  │    ├─ eng.SetScope(Empty)                        metadata transaction (1 ref)
  │    ├─ eng.BatchMarkNeedsPRBodyUpdate              metadata transaction (1 ref)
  │    └─ actions.PushMetadataOnly                    `git push refs/stackit/metadata/<branch>`
  │
  └─ set scope:
       ├─ eng.SetScope(newScope)                     metadata transaction
       ├─ if scope-in-branch-name and renamed:
       │    handler.PromptConfirmRename
       │    eng.RenameBranch                          ref rename + checkout
       ├─ eng.BatchMarkNeedsPRBodyUpdate              another metadata transaction
       └─ actions.PushMetadataOnly                    `git push`
```

## Where time goes

1. **`actions.PushMetadataOnly`** — one `git push` (network round trip, 200–500ms typical). Dominates everything else when remote-sync is on.
2. **`TestRemoteRefCompatibility`** on first run — another network round trip. Only runs once per repo (caches in `IsRemoteSyncEnabled`).
3. **Two metadata transactions** per call: `SetScope` writes one ref, `BatchMarkNeedsPRBodyUpdate` writes another. Both go through the metadata tx machinery; each is ~2–5ms.
4. **Bootstrap** — `rebuildInternal` as always.
5. **`--show`** is pure cache reads after bootstrap. The fast path.

`RenameBranch` is rare — only triggered when the user explicitly confirms — but it does a real branch checkout + ref rename and is more expensive than the rest combined when it runs.

## Wins (ranked)

### 1. Combine the two metadata writes into one transaction *(small impact, low risk)*

`SetScope` then `BatchMarkNeedsPRBodyUpdate` are two separate transactions touching the same branch's metadata. The engine's `withMetadataTx` could absorb both writes into one ref update — saves one git ref write. Fix touches `scope/action.go:73` and `:118`. Same pattern affects `describe`, `lock`, anywhere a command both mutates metadata and marks the branch for PR sync.

### 2. Make the metadata push async-friendly *(medium impact, medium risk)*

For non-interactive `--unset` and `--set`, the user is blocked on `git push` of metadata before the command returns. Stackit's [`safety-invariants.md`](../../.claude/rules/safety-invariants.md) explicitly notes that GitHub writes should defer to `sync`, but the *metadata ref push* itself is the heavy bit here. Options:

- Background the push (fork a `git push` in the background, return immediately, log failures). Risk: user runs `submit` before metadata sync — but `submit` would re-push anyway, so the failure mode is benign.
- Batch metadata pushes across consecutive `scope`/`describe`/`lock` commands during a single shell session via a debounce. Complex; only worth it if these commands run in bursts.

Default to keeping the sync push but document that it's the dominant cost.

### 3. `--show` should skip remote-shas / IsInManagedWorktree *(small impact, free)*

`common.Run` does `IsInManagedWorktree()` unconditionally (`internal/cli/common/common.go:42`). `scope --show` doesn't need that. Same fix listed in `co.md` #5 — gate per command.

### 4. Bootstrap "lite" mode benefits `--show` *(shared with co.md #2)*

`--show` only needs the current branch and its scope chain. Doesn't need a full `rebuildInternal`.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit scope --show' \
  'stackit scope ABC-123' \
  'stackit scope --unset'
```

The `--show` baseline measures bootstrap-only cost. The delta to set/unset is dominated by the metadata push.
