# Performance Analysis

Per-command analysis of where `stackit` spends time, with proposed wins. All claims are sourced from static reading of the code; **none of the numbers are measured**. Validate via instrumentation before committing to a refactor.

## Start here

- **[cross-cutting.md](cross-cutting.md)** — the wins that touch one place and benefit many commands. Read this first; it's the ROI ranking.

## Per-command pages

| Command | Tier | Page |
|---|---|---|
| `co` / `checkout` | deep | [co.md](co.md) |
| `log` (+ `full`, `short`) | deep | [log.md](log.md) |
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

1. Lite bootstrap path
2. Kill pre-checkout `worktree.Status()`
3. Conflict-impossible validation for restack
4. Cached `IsUpToDate`
5. Preload stack stats for `log`
6. `RebuildBranches([]string)` for `untrack` + `absorb` cleanup
