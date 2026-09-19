# Batched commit and planning reads: release comparison

Measured on 2026-09-19 with the schema-v3 CLI benchmark harness. Every case has
two warmups and ten measured samples. Tables show median milliseconds; negative
changes mean faster. Revisions ran sequentially on the same machine, without
concurrent builds/tests during timed samples.

| Revision | Commit |
|---|---|
| v0.27.1 | `634c094bf59a2ecf2259346c1672197b51846ee4` |
| perf/batch-diff-reads | `2bcbb3fe1066912279d6bd32c45b9c70466a7e3f` |
| perf/batch-commit-reads | `602ee9a56b3e3d137813c8a26d5ae97bfb84d608` |

Toolchain: Go 1.26.7, Git 2.47.3, Python 3.14.7; Linux x86-64, 6 available CPUs.
“Parent” below means `perf/batch-diff-reads`, separating this cleanup from earlier
post-release improvements.

## Findings

At 50 one-commit branches, the new branch improves tree by 15%, absorb dry-run by
27%, and no-op restack by 35% over its parent. Restacking changed descendants is
another 10% faster than the parent, or 45% faster than v0.27.1. No-op restack now
improves at every tested depth.

This is not a uniform speedup. At five branches absorb dry-run is 9% slower than
the parent (3.8 ms); plain info is 5% slower (1.3 ms). At 50 five-commit branches,
absorb is essentially unchanged versus the parent and 5% slower than the release,
while plain info is 6% slower than the parent. Tree's incremental gain shrinks to
2% on that fixture. Sequential generation reads trade fewer total subprocesses
for per-generation latency; small batches and longer branch histories benefit
less than wide stacks of short branches.

Navigation and single-branch mutations have small mixed changes. These are
descriptive measurements from one machine and fixed revision order, not
statistical significance claims. Ten samples are too few for a dependable tail
latency estimate.

## What changed

- Typed commit ranges, counts, and ancestry reads replace the format-string and
  one-off history methods. Linear ranges share a bounded walk: one resolution
  process plus one process per uncached generation. Fifty one-commit ranges take
  two processes; ten five-commit ranges take at most six.
- Merge, unrelated, unbounded, and deep histories retain native Git semantics.
  Speculative walking stops after eight generations. Count fallbacks use
  `rev-list --count`, not full-history materialization. Complex histories can
  still require one native query per range; this is not a constant-process claim
  for arbitrary graphs.
- Tree counts and absorb downstack reads use the actual batch reader. Absorb
  retains per-branch errors instead of rereading empty results.
- Info and commit splitting reuse typed commit data. Info reuses resolved range
  endpoints and now includes earlier commits in multi-commit diff/patch output.
- Restack batches ancestry and frozen remote-ref checks, and reuses landing
  decisions only within one read-only plan. Existing anchor, frozen, landed,
  locked, reparenting, and stale-metadata handling remains in place.
- Create, absorb, and split share working-tree status reads where multiple flags
  are needed. Status is reread after mutations, not cached globally; NUL-delimited
  parsing handles rename paths and unusual filenames. Untracked-file guards no
  longer depend on `status.showUntrackedFiles`.

## Results

### 5 branches × 1 commit per branch

| Case | v0.27.1 | Parent | New branch | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree-short | 20.02 | 20.76 | 20.47 | +2.3% | -1.4% |
| tree | 24.91 | 26.49 | 26.02 | +4.5% | -1.8% |
| info | 30.12 | 25.35 | 26.63 | -11.6% | +5.1% |
| info-diff | 36.62 | 32.28 | 28.07 | -23.4% | -13.0% |
| absorb-dry-run | 41.09 | 42.03 | 45.82 | +11.5% | +9.0% |
| parent | 17.98 | 17.49 | 17.99 | +0.1% | +2.9% |
| children | 17.49 | 17.84 | 17.60 | +0.6% | -1.4% |
| checkout-exact | 20.77 | 20.73 | 20.76 | -0.1% | +0.1% |
| create | 34.79 | 34.62 | 35.11 | +0.9% | +1.4% |
| modify | 31.19 | 29.93 | 30.55 | -2.0% | +2.1% |
| modify-midstack | 144.95 | 104.22 | 102.05 | -29.6% | -2.1% |
| restack-noop | 61.52 | 61.98 | 50.88 | -17.3% | -17.9% |
| restack-upstack | 154.52 | 116.37 | 104.20 | -32.6% | -10.5% |

### 10 branches × 1 commit per branch

| Case | v0.27.1 | Parent | New branch | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree-short | 21.68 | 22.23 | 21.98 | +1.4% | -1.1% |
| tree | 29.52 | 29.61 | 28.20 | -4.5% | -4.8% |
| info | 31.74 | 26.27 | 26.63 | -16.1% | +1.4% |
| info-diff | 37.62 | 32.36 | 27.49 | -26.9% | -15.1% |
| absorb-dry-run | 46.59 | 49.41 | 50.06 | +7.5% | +1.3% |
| parent | 17.96 | 17.29 | 17.51 | -2.5% | +1.2% |
| children | 18.55 | 18.11 | 18.30 | -1.3% | +1.0% |
| checkout-exact | 21.06 | 21.11 | 21.18 | +0.6% | +0.3% |
| create | 36.57 | 35.75 | 36.44 | -0.4% | +1.9% |
| modify | 32.85 | 30.70 | 31.17 | -5.1% | +1.5% |
| modify-midstack | 260.58 | 168.30 | 161.42 | -38.1% | -4.1% |
| restack-noop | 74.72 | 76.92 | 58.73 | -21.4% | -23.6% |
| restack-upstack | 271.74 | 183.07 | 165.92 | -38.9% | -9.4% |

### 50 branches × 1 commit per branch

| Case | v0.27.1 | Parent | New branch | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree-short | 28.01 | 31.86 | 30.58 | +9.2% | -4.0% |
| tree | 62.27 | 52.69 | 44.92 | -27.9% | -14.7% |
| info | 34.78 | 31.02 | 30.20 | -13.2% | -2.6% |
| info-diff | 41.48 | 37.44 | 32.63 | -21.3% | -12.8% |
| absorb-dry-run | 92.48 | 101.67 | 74.28 | -19.7% | -26.9% |
| parent | 18.73 | 18.20 | 18.26 | -2.5% | +0.4% |
| children | 21.28 | 21.06 | 21.60 | +1.5% | +2.6% |
| checkout-exact | 22.86 | 22.21 | 22.61 | -1.1% | +1.8% |
| create | 41.37 | 42.43 | 43.05 | +4.1% | +1.5% |
| modify | 38.88 | 38.92 | 39.91 | +2.7% | +2.6% |
| modify-midstack | 1204.09 | 714.51 | 652.00 | -45.9% | -8.7% |
| restack-noop | 185.63 | 193.94 | 126.03 | -32.1% | -35.0% |
| restack-upstack | 1215.58 | 734.21 | 664.01 | -45.4% | -9.6% |

### 50 branches × 5 commits per branch

This focused fixture measures read-heavy cases and no-op planning. The new
`info --diff` displays all five commits' changes; the older revisions display
only the newest commit's changes, so output work is not identical.

| Case | v0.27.1 | Parent | New branch | vs release | vs parent |
|---|---:|---:|---:|---:|---:|
| tree | 65.66 | 56.77 | 55.68 | -15.2% | -1.9% |
| info | 36.58 | 30.86 | 32.63 | -10.8% | +5.8% |
| info-diff | 43.84 | 38.06 | 34.26 | -21.9% | -10.0% |
| absorb-dry-run | 116.78 | 122.03 | 122.62 | +5.0% | +0.5% |
| restack-noop | 238.91 | 233.23 | 161.56 | -32.4% | -30.7% |

## Reproduce

```bash
for depth in 5 10 50; do
  python3 -B scripts/benchmark.py \
    --refs v0.27.1 perf/batch-diff-reads 602ee9a5 \
    --branches "$depth" --runs 10 --warmup 2 \
    --output "/tmp/stackit-commit-reads-$depth.json"
done

python3 -B scripts/benchmark.py \
  --refs v0.27.1 perf/batch-diff-reads 602ee9a5 \
  --branches 50 --commits-per-branch 5 \
  --cases tree info info-diff absorb-dry-run restack-noop \
  --runs 10 --warmup 2 --output /tmp/stackit-commit-reads-50x5.json
```

The measured source commit is pinned above because this report is a later
documentation-only commit. Raw samples and platform details are in those four
JSON files and appended to the ignored local `benchmark-results.json` matrix;
the earlier diff-read runs remain intact.

Fixture setup, copying, deletion, staging, and preparatory amendments are outside
the timer. No GitHub/network commands are measured. See [benchmarks.md](benchmarks.md)
for the harness contract and [the previous comparison](benchmarks-diff-reads.md)
for the earlier diff batching work.

## Validation

- Go formatting, lint, and full unit/integration suite: 3,720 tests, 4 skipped.
- Targeted race checks for typed reads, worktree status, branch stats, and restack
  planning.
- Native-Git oracle comparisons for overlapping, merged, unrelated, deep, empty,
  reverse, tagged, and invalid ranges; per-input errors and cancellation.
- Subprocess budgets for single-/multi-commit batches and operation-local restack
  caching, plus regression coverage for whole-branch info diffs and status refresh.
- Benchmark timing/failure tests, including absorb preparation outside the timer.
