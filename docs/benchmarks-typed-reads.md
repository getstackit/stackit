# Typed read models: refactor and benchmark comparison

Measured on 2026-09-19 with the schema-v3 CLI harness: two warmups and ten
measured samples per case. Medians below are milliseconds; negative percentages
mean faster. Revisions ran sequentially, without concurrent builds or tests
during timed samples.

| Revision | Commit |
|---|---|
| v0.27.1 | `634c094bf59a2ecf2259346c1672197b51846ee4` |
| perf/batch-commit-reads (parent) | `396970e8` |
| refactor/typed-read-models | `f5b30ea20083e11f75741f0b960092bcb6538282` |

Toolchain: Go 1.26.7, Git 2.47.3, Python 3.14.7; Linux x86-64, 6 available CPUs.
The parent includes only documentation changes after the previously measured
`602ee9a5` implementation.

## Findings

This is primarily a complexity reduction, not another performance optimization.
Measured medians are broadly level with the parent: most cases move by less than
3%, with the largest increases being 3.3% for multi-commit info-diff (1.1 ms) and
2.3% for 50-branch checkout (0.5 ms). These runs do not indicate a material
performance regression, but ten samples on one machine cannot establish
statistical equivalence or dependable tail latency.

The earlier batching gains remain: at 50 one-commit branches, tree is 28% faster
than v0.27.1, absorb dry-run 20% faster, no-op restack 33% faster, and restacking
changed descendants 46% faster. These are cumulative gains, not gains caused by
this refactor alone.

Existing tradeoffs remain too. Absorb is 13% slower than the release at five
branches, and 7% slower with 50 five-commit branches, while remaining essentially
unchanged from the parent. At 50 branches tree-short is 11% slower and create 5%
slower than the release. The data do not support claiming every operation is faster.

## Refactors

All six changes are on one stacked branch:

- One branch-range snapshot resolves heads, stored bases, and parent fallback.
  History and stats share it; pure per-branch derivation no longer uses a worker
  pool. Missing parents remain acceptable when a stored base exists, and errors
  cannot silently turn a bounded branch history into an unbounded walk.
- Commit records remain typed through stack-log and HTTP mapping. Removed
  `CommitFormat`, SHA/subject NUL records, and date/tab records between packages.
  Terminal projections are pure helpers. `CommitNode` separates topology-only
  reads from full display/replay metadata, preserving the lightweight read path.
  Empty subjects now remain attached to their identities in HTTP mapping too.
- The engine returns `git.WorktreeStatus` intact, with clean/unstaged predicates,
  instead of unpacking three booleans. Callers still reread after mutations.
- Batch results have private, single-outcome storage and centralized recording.
  Success, failure, and unrequested inputs remain distinct; projections cannot
  mutate the result maps. Metadata keeps observed versions even on decode error.
  Immutable diff/count caches use `RevRange` keys instead of concatenated strings.
- The bounded walker produces a complete linear history or an explicit native
  fallback. Typed readers interpret it without nil-map dispatch. Native counts
  still use `rev-list --count`, and ancestry uses `merge-base --is-ancestor`.
- An operation-local restack planner owns its snapshot and derived indexes.
  Explicit skip/anchor/frozen decisions replace piecemeal setup; skip is an action
  rather than an independent boolean. Metadata-only refresh remains distinct from
  ref movement and does not invalidate children.

Production Go changes total 613 added / 629 removed lines (net -16); tests add
coverage. The main reduction is in duplicated state, formatting protocols, and
branch-resolution/control-flow scaffolding, not a large decrease in total lines.

## Results

### 5 branches × 1 commit per branch

| Case | v0.27.1 | Parent | Refactor | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree-short | 19.98 | 21.18 | 20.79 | +4.1% | -1.8% |
| tree | 24.57 | 25.53 | 24.98 | +1.7% | -2.1% |
| info | 30.12 | 25.62 | 24.66 | -18.1% | -3.7% |
| info-diff | 35.55 | 27.33 | 26.48 | -25.5% | -3.1% |
| absorb-dry-run | 40.59 | 45.55 | 45.81 | +12.9% | +0.6% |
| parent | 17.17 | 17.04 | 17.41 | +1.4% | +2.2% |
| children | 17.81 | 17.68 | 18.03 | +1.3% | +2.0% |
| checkout-exact | 21.27 | 20.82 | 20.26 | -4.7% | -2.7% |
| create | 34.77 | 35.20 | 34.52 | -0.7% | -1.9% |
| modify | 31.46 | 29.69 | 29.45 | -6.4% | -0.8% |
| modify-midstack | 143.37 | 100.49 | 99.63 | -30.5% | -0.9% |
| restack-noop | 64.03 | 50.92 | 50.53 | -21.1% | -0.8% |
| restack-upstack | 153.56 | 105.42 | 104.41 | -32.0% | -1.0% |

### 50 branches × 1 commit per branch

| Case | v0.27.1 | Parent | Refactor | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree-short | 28.61 | 33.63 | 31.71 | +10.8% | -5.7% |
| tree | 61.71 | 44.34 | 44.63 | -27.7% | +0.6% |
| info | 35.63 | 30.97 | 30.80 | -13.6% | -0.5% |
| info-diff | 42.59 | 32.96 | 32.74 | -23.1% | -0.7% |
| absorb-dry-run | 92.50 | 74.44 | 73.61 | -20.4% | -1.1% |
| parent | 18.81 | 18.76 | 18.48 | -1.8% | -1.5% |
| children | 21.96 | 21.57 | 21.73 | -1.0% | +0.7% |
| checkout-exact | 22.53 | 22.28 | 22.80 | +1.2% | +2.3% |
| create | 40.73 | 43.05 | 42.77 | +5.0% | -0.7% |
| modify | 38.92 | 39.47 | 39.16 | +0.6% | -0.8% |
| modify-midstack | 1207.15 | 654.11 | 649.48 | -46.2% | -0.7% |
| restack-noop | 187.87 | 123.09 | 125.48 | -33.2% | +1.9% |
| restack-upstack | 1213.04 | 656.84 | 660.89 | -45.5% | +0.6% |

### 50 branches × 5 commits per branch

The older release's info-diff omits earlier commits in a multi-commit branch;
the parent and refactor include them, so that release comparison does not
represent identical output work.

| Case | v0.27.1 | Parent | Refactor | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree | 65.73 | 55.41 | 56.19 | -14.5% | +1.4% |
| info | 35.77 | 32.46 | 30.84 | -13.8% | -5.0% |
| info-diff | 42.75 | 32.43 | 33.49 | -21.7% | +3.3% |
| absorb-dry-run | 112.84 | 122.02 | 121.21 | +7.4% | -0.7% |
| restack-noop | 231.49 | 155.85 | 156.06 | -32.6% | +0.1% |

## Reproduce

```bash
for depth in 5 50; do
  python3 -B scripts/benchmark.py \
    --refs v0.27.1 396970e8 f5b30ea2 \
    --branches "$depth" --runs 10 --warmup 2 \
    --output "/tmp/stackit-typed-reads-$depth.json"
done

python3 -B scripts/benchmark.py \
  --refs v0.27.1 396970e8 f5b30ea2 \
  --branches 50 --commits-per-branch 5 \
  --cases tree info info-diff absorb-dry-run restack-noop \
  --runs 10 --warmup 2 --output /tmp/stackit-typed-reads-50x5.json
```

The measured implementation is pinned because this report is a later
documentation-only commit. Raw samples are in the three JSON files above and
appended to the ignored local `benchmark-results.json` matrix, retaining all
seven previous runs.

## Validation

- Full Go formatting, lint, and unit/integration suite: 3,725 tests, 4 skipped.
- Targeted race checks for batch reads, status, branch stats/snapshots, and
  operation-local restack planning.
- Existing native-Git oracle and subprocess-budget tests still pass: 50
  one-commit ranges need two processes; ten five-commit ranges need at most six.
- New tests cover result replacement/empty successes, map isolation, corrupt
  metadata versions, worktree predicates, typed commit projections and HTTP
  mapping, and stored-base fallback when a parent ref disappears.

See [the harness contract](benchmarks.md) and
[the preceding batching comparison](benchmarks-commit-reads.md).
