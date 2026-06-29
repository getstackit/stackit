# Performance Analysis

Per-command analysis of where `stackit` spends time, with proposed wins. All claims are sourced from static reading of the code; **none of the numbers are measured**. Validate via instrumentation before committing to a refactor.

## Start here

- **[cross-cutting.md](cross-cutting.md)** — the wins that touch one place and benefit many commands. Read this first; it's the ROI ranking.

## Per-command pages

| Command | Tier | Page |
|---|---|---|
| `co` / `checkout` | deep | [co.md](co.md) |
| `tree` (+ `full`, `short`) | deep | [tree.md](tree.md) |
| `create` | deep | [create.md](create.md) |
| `modify` | deep | [modify.md](modify.md) |
| `up`, `down`, `parent`, `children`, `top`, `bottom`, `trunk`, `main` | medium | [navigation.md](navigation.md) |
| `info` / `i` | medium | [info.md](info.md) |
| `absorb` | medium | [absorb.md](absorb.md) |
| `restack` | medium | [restack.md](restack.md) |
| `submit` / `ss` | medium | [submit.md](submit.md) |
| `track`, `untrack` | medium | [track-untrack.md](track-untrack.md) |
| `scope` | medium | [scope.md](scope.md) |
| `describe` | medium | [describe.md](describe.md) |

## Scope and caveats

- **Skipped (per request):** `sync`, `ship`, `merge` — the complex shipping path. Patterns covered in other pages apply.
- **Skipped (low frequency / trivial):** `init`, `doctor`, `debug`, `docs`, `shell`, `config get/set/show`, `passthrough` git commands, worktree subcommands, integration installers.
- **Method:** static code reading. File-line references throughout. No measurements; validation suggestions at the bottom of each page.
- **Tier:** "deep" pages walk the full call graph with citations; "medium" pages give the highlights and top wins.

## Suggested attack order

See [cross-cutting.md → Recommended attack order](cross-cutting.md#recommended-attack-order). TL;DR:

1. Extend the conflict-free validation fast path to multi-commit branches (single-commit case already ships)
2. Finish lightweight load-mode adoption on the remaining navigation commands
3. Batch stack-wide status checks in `submit` + `info`
4. `RebuildBranches([]string)` for `absorb` + `untrack` cleanup
5. On-demand diff batching for `info --diff`/`--patch` and `absorb`
6. Combine metadata transactions for `create` + `describe`; remaining hygiene

Several originally-planned wins have since landed — see [cross-cutting.md → Completed](cross-cutting.md#completed-since-last-revision) (go-git removal / `ReloadRepository`, snapshot batching + `undo.enabled` opt-out, `IsInManagedWorktree` gating, `scope`/`untrack` batching).
