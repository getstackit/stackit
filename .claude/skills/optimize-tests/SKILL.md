---
name: optimize-tests
description: Make the test suite faster without losing coverage. A periodic, coverage-guarded cleanup pass that finds slow/redundant tests, applies free structural speedups (parallel, in-process, drop remote), and consolidates similar tests (table-driven merges, combined asserts) — verifying after every change that no covered line was lost. Keywords test speed, slow tests, test runtime, test cleanup, consolidate tests, parallel tests, combine asserts, coverage-safe, maximize test value.
allowed-tools: Bash(bash:*), Bash(go:*), Bash(git:*), Bash(mise:*), Bash(rg:*), Bash(jq:*), Bash(fd:*), Read, Edit, Write, Grep, Glob, Task
---

# /optimize-tests

Make the suite run faster while keeping every line of coverage. This is a
**periodic cleanup pass**, not a one-off: each run targets the current hotspots,
banks the wins, and reports what's left for next time.

The goal is to **maximize the value of each test** — fewer tests, each asserting
more, running in less wall-clock — without ever dropping a covered line or a
real assertion.

## The safety contract (read this first)

Two guarantees must hold for every change. The first is automated; the second is
your judgement. Both are mandatory.

1. **Coverage floor — automated.** `coverage-guard.sh` compares the *set of
   covered blocks* before and after, not the percentage. Two suites can share a
   percentage while covering different lines, so the percentage is a lie; the
   block set is the truth. If any block covered before is uncovered after, the
   guard fails and names the lines. This is what makes "delete / merge tests"
   safe: you may remove anything as long as the covered-block set never shrinks.

2. **Assertion value — judgement.** Coverage means a line *ran*, not that its
   result was *checked*. A line can execute with nothing asserting its behavior.
   So when you collapse or remove a test, **every distinct assertion and every
   distinct input in the originals must survive in the result.** Dedupe *setup*,
   never *assertions*. The guard cannot see this — you must.

If you can't satisfy both, don't make the change.

## Usage

```
/optimize-tests [scope] [--dry-run]
```

- `scope` (optional) — a test tier (`fast` | `git` | `integration` | `unit` |
  `all`) **or** a Go package pattern (`./internal/engine/...`). Default:
  `integration` (usually the slowest tier and the biggest win). Pick a hotspot;
  don't boil the ocean every run.
- `--dry-run` — measure and produce the worklist, but make no edits.

Examples:

```
/optimize-tests                       # incremental pass over the integration tier
/optimize-tests ./internal/git/...    # focus one package
/optimize-tests fast --dry-run        # just show me the opportunities
```

## Scripts

Deterministic steps live in `.claude/skills/optimize-tests/scripts/`. Use them
instead of inlining `go test` / `rg` — they encode the right flags (cache off,
`-coverpkg=./...`, `-covermode=set`) and keep invocations identical across runs.

| Script | Purpose |
|--------|---------|
| `measure-time.sh <scope> <out.json>` | Time a run (no coverage — instrumentation distorts timing); capture per-test timings. Prints `wall_seconds=`. |
| `measure-cover.sh <scope> <out.cover>` | Capture a coverage profile (`-coverpkg=./...`, `set` mode). Prints `total_pct=`. **Use the same scope before & after.** |
| `slowest.sh <out.json> [N]` | Rank the slowest tests and packages — your hotspot list. |
| `coverage-guard.sh <before.cover> <after.cover>` | **The gate.** Exit 1 + named blocks if any covered line was lost. |
| `scan-antipatterns.sh` | List coverage-safe structural speedups (the "free wins"). |

Work in a scratch dir, e.g. `/tmp/stackit-test-opt/`.

## The loop

### Step 0 — Scope

Resolve `scope` (default `integration`). State it up front: "Optimizing the
**integration** tier…". A periodic pass is incremental — target where the time
is now, not the whole tree.

### Step 1 — Baseline

```bash
D=/tmp/stackit-test-opt && mkdir -p "$D"
bash .claude/skills/optimize-tests/scripts/measure-time.sh  "$SCOPE" "$D/before.json"
bash .claude/skills/optimize-tests/scripts/measure-cover.sh "$SCOPE" "$D/before.cover"
bash .claude/skills/optimize-tests/scripts/slowest.sh "$D/before.json" 25
bash .claude/skills/optimize-tests/scripts/scan-antipatterns.sh
```

Record the numbers: `wall_seconds`, `total_pct`, the slowest tests/packages.
These are the before/after yardstick. **Do not paste full test files into
context** — work from the hotspot list and open files as needed.

If `--dry-run`, skip to Step 6 and report the worklist without editing.

### Step 2 — Free structural wins first (Tier A — zero coverage risk)

Apply everything `scan-antipatterns.sh` surfaced. These change *how* a test runs,
not *what* it asserts, so they cannot move coverage — knock them out first, they
are usually the biggest wall-time win:

- Missing `t.Parallel()` (top-level **and** each subtest) → add it.
- `NewTestShell(` (spawns a process) → `NewTestShellInProcess(` (~8ms/cmd).
- `WithRemote()` where the test never pushes/pulls/syncs → drop it.
- 3+ hand-rolled `CreateAndCheckoutBranch` calls → a fixture
  (`CreateLinearStack3()`, `CreateDiamondStack()`).

After this batch, run the affected packages green (`mise run test:pkg ./pkg`).
No coverage re-check is needed for Tier A.

> Watch for shared mutable state when adding `t.Parallel()` — package-level vars,
> `t.Setenv`, shared temp dirs. If a test mutates global state it can't be
> parallel as written; leave a note rather than introduce a flake.

### Step 3 — Analyze hotspots for consolidation (Tier B/C)

For each hotspot from `slowest.sh`, read the tests and build a **worklist**. For
a large scope, fan out **one `Task` agent per hotspot package** (read-only) to
produce worklists in parallel. Each item must carry:

- the concrete edit (file, what merges into what),
- the estimated time saving (why it's slow: per-case setup? remote? serial?),
- a **coverage-safety note**: which inputs and which assertions are preserved.

What to look for (see the catalog below). An agent prompt skeleton:

```
You are optimizing tests for the stackit Go CLI. Read <FILE> (a hotspot from the
timing report). Propose coverage-PRESERVING consolidations only. For each, list:
edit, est. saving, and EXACTLY which inputs + assertions survive. Never propose
weakening an assertion or dropping a distinct input. Output JSON: {file, items:[
{kind, where, savingHint, inputsPreserved, assertionsPreserved, rationale}]}.
```

### Step 4 — Apply consolidations (sequential, guarded)

Apply the worklist **one package at a time** so failures are isolated. After
each package:

```bash
mise run test:pkg ./internal/<pkg>     # must be green before moving on
```

Keep edits mechanical and reviewable: prefer table-driven merges that preserve
every row over clever rewrites. Don't interleave production-code changes (see
"What not to do").

### Step 5 — Coverage guardrail (the gate)

Re-measure coverage with the **same scope** and run the guard:

```bash
bash .claude/skills/optimize-tests/scripts/measure-cover.sh "$SCOPE" "$D/after.cover"
bash .claude/skills/optimize-tests/scripts/coverage-guard.sh "$D/before.cover" "$D/after.cover"
```

- **Pass** → coverage floor held. Proceed.
- **Fail** → the guard names the lost blocks. Revert the edit that dropped them,
  or restore the assertion that exercised them, then re-run. Loop until green.

> Flaky-coverage caveat: a 1–2 block "regression" on a timing-dependent
> integration path can be nondeterministic. Re-run `after.cover` once; if it
> clears, it wasn't a real loss. The guard fails closed (errs toward flagging) —
> that's the safe direction; always look before dismissing.

### Step 6 — Re-measure and report

```bash
bash .claude/skills/optimize-tests/scripts/measure-time.sh "$SCOPE" "$D/after.json"
```

Report:

```
## /optimize-tests — <scope>
Wall time:   <before>s → <after>s  (−X%, −Ys)
Coverage:    <before>% → <after>%  (guard: ✅ no blocks lost)
Tests:       <n> removed, <m> merged into table-driven, <k> parallelized
Free wins:   +parallel ×A, in-process ×B, dropped remote ×C, fixtures ×D
Top changes: <file: what happened, time saved>
Left for next pass: <remaining hotspots from slowest.sh>
```

### Step 7 — Land it (project workflow)

Only after **tests green and guard ✅**, stack the change (this repo dogfoods
stackit — never raw `git commit`/`gh pr create`):

```bash
git add -A
echo "perf(test): speed up <scope> suite, coverage preserved" | stackit create -F -
```

If the pass touched several unrelated subsystems, split into stacked branches per
`AGENTS.md`.

## Optimization catalog (by risk tier)

### Tier A — Free (no coverage risk; do first)
| Pattern | Fix | Why safe |
|---|---|---|
| No `t.Parallel()` | Add to func + every subtest | Same asserts, run concurrently |
| `NewTestShell(` | `NewTestShellInProcess(` | Same commands, no process spawn |
| Needless `WithRemote()` | Drop it | Remote setup the test never uses |
| Hand-rolled stack setup | `CreateLinearStack3()` etc. | Optimized shared fixture |
| Per-case expensive setup | Hoist read-only setup above the loop | Setup isn't the thing under test |

### Tier B — Consolidation (low risk + guard)
| Pattern | Fix | Guard |
|---|---|---|
| N near-identical tests differing only by input | One table-driven test, one row per input | Every row preserved → every input still run |
| Several asserts describing one scenario across many tiny tests | One test, multiple asserts | Combine *asserts*, keep all of them |
| Repeated subtests with copy-pasted bodies | Shared helper + table | Bodies identical → behavior identical |

### Tier C — Removal (needs proof)
| Pattern | Fix | Guard |
|---|---|---|
| A case that exercises the same path another case already covers | Delete it | `coverage-guard.sh` must stay ✅ **and** its asserted properties must be a strict subset of what remains |
| Redundant "smoke" test fully subsumed by a richer one | Delete it | Same — prove both floor and assertions |

Tier C is the **only** place coverage can silently drop. The guard is mandatory
and you must also eyeball the assertions — a removed test that hit the same lines
but checked a *different property* is a real loss the guard can't see.

## What not to do

- **Don't delete a test just because it's slow.** First make it fast (Tier A).
  Only remove it if you can *prove* redundancy (Tier C).
- **Don't weaken or drop assertions** to make something pass faster. Dedupe
  setup, never assertions.
- **Don't merge genuinely different setups** into one mega-test that's painful to
  debug. Smaller-but-fast beats big-and-opaque.
- **Don't chase the coverage percentage.** Chase the covered-block set +
  assertion strength. The percent can hold while real coverage rots.
- **Don't edit non-test source during a pass.** It shifts coverage block keys and
  invalidates the guard's before/after comparison. If a test reveals a
  production bug, that's a separate, non-stacked-on-top change.
- **Don't introduce flakiness** for speed — a parallel test sharing mutable
  state, or a dropped `WithRemote()` a later assertion needs, is a regression
  even if it's faster.

## Running it periodically

This is meant to be run on a cadence. Each pass is incremental: target the
current top entries from `slowest.sh`, bank the wins, report the rest.

- Ad-hoc: `/optimize-tests <hotspot-tier>` whenever the suite feels slow.
- Scheduled: drive it with `/loop` or `/schedule` against a rotating scope
  (e.g. integration one week, git the next). The "Left for next pass" section of
  each report seeds the following run.
