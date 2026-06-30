# `scope` — Performance Analysis

**Tier:** medium (low frequency; one branch at a time; network push at the end).

## Call graph

```
newScopeCmd → common.Run → scope.Action               internal/actions/scope/action.go:22
  ├─ --show: read explicit + resolved scope; print (no network)
  ├─ --unset:
  │    ├─ eng.SetScopeAndMarkForUpdate(Empty)         single metadata transaction (scope + PR-update flag)
  │    └─ actions.PushMetadataOnly                    `git push refs/stackit/metadata/<branch>`
  │
  └─ set scope:
       ├─ eng.SetScopeAndMarkForUpdate(newScope)      single metadata transaction (scope + PR-update flag)
       ├─ if scope-in-branch-name and renamed:
       │    handler.PromptConfirmRename
       │    eng.RenameBranch                          ref rename + checkout
       └─ actions.PushMetadataOnly                    `git push`
```

## Where time goes

1. **`actions.PushMetadataOnly`** — one `git push` (network round trip, 200–500ms typical). Dominates everything else when remote-sync is on. (`internal/actions/metadata.go:18`)
2. **`PrepareRemoteMetadataPush`** on first run — another network round trip. Only runs once per repo (caches in the remote-sync gate).
3. **One metadata transaction** per mutating call: `SetScopeAndMarkForUpdate` writes the scope and the `NeedsPRBodyUpdate` flag in a single ref update (`internal/engine/branch_tracking.go:401`).
4. **Bootstrap** — the mutating scope paths use the normal engine context.
5. **`--show`** is the fast path: current-branch metadata read after bootstrap, no network.

`RenameBranch` is rare — only triggered when the user explicitly confirms — but it does a real branch checkout + ref rename and is more expensive than the rest combined when it runs.

## Wins (ranked)

### 1. Make the metadata push async-friendly *(medium impact, medium risk)*

For non-interactive `--unset` and `--set`, the user is blocked on `git push` of metadata before the command returns (`scope/action.go:72` and `:117` → `actions.PushMetadataOnly`). Stackit's [`safety-invariants.md`](../../.claude/rules/safety-invariants.md) explicitly notes that GitHub writes should defer to `sync`, but the *metadata ref push* itself is the heavy bit here. Options:

- Background the push (fork a `git push` in the background, return immediately, log failures). Risk: user runs `submit` before metadata sync — but `submit` would re-push anyway, so the failure mode is benign.
- Batch metadata pushes across consecutive `scope`/`describe`/`lock` commands during a single shell session via a debounce. Complex; only worth it if these commands run in bursts.

Default to keeping the sync push but document that it's the dominant cost.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit scope --show' \
  'stackit scope ABC-123' \
  'stackit scope --unset'
```

The `--show` baseline measures bootstrap-only cost. The delta to set/unset is dominated by the metadata push.
