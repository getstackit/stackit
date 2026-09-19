# Batched diff reads: release comparison

Measured on 2026-09-19 using the corrected schema-v2 CLI benchmark harness.
Each case has two warmups and ten measured samples. All timings below are medians
in milliseconds; negative changes mean faster. Revisions ran sequentially on the
same machine, with no concurrent test/build workload during measured samples.

| Revision | Commit |
|---|---|
| v0.27.1 | `634c094bf59a2ecf2259346c1672197b51846ee4` |
| main | `55f3d860d15d97ccf434d6edd3c5d49c38dc1f87` |
| Batch diff branch | `2b8212588de473e6584b52c19dc4aa2982221039` |

Toolchain: Go 1.26.7, Git 2.47.3, Python 3.14.7; Linux x86-64, 6 available CPUs.

## Results

The largest improvement is in disjoint-file descendant replay. At 50 branches,
modify/restack are approximately 41% faster than v0.27.1 and 23% faster than the
unchanged main baseline. Normal tree rendering is 16% faster than the release
at that depth. Info improves by 11–14% across the three sizes.

Small-stack tree rendering does not improve consistently, and navigation, create,
and no-op restack show small mixed changes. Do not interpret every percentage as
a significant change: this is one machine, ten samples per case, and fixed revision
order. The main baseline separates earlier post-release changes from this cleanup.

### 5 branches

| Case | v0.27.1 | main | Batch diff branch | vs release | vs main |
|---|---:|---:|---:|---:|---:|
| tree-short | 20.08 | 20.78 | 20.82 | +3.7% | +0.2% |
| tree | 25.37 | 25.93 | 26.43 | +4.2% | +1.9% |
| info | 29.50 | 30.02 | 25.33 | -14.1% | -15.6% |
| parent | 17.19 | 18.12 | 17.37 | +1.0% | -4.2% |
| children | 17.98 | 18.14 | 18.16 | +1.0% | +0.1% |
| checkout-exact | 20.64 | 20.81 | 19.98 | -3.2% | -4.0% |
| create | 34.49 | 33.83 | 33.97 | -1.5% | +0.4% |
| modify | 31.84 | 31.60 | 30.64 | -3.8% | -3.0% |
| modify-midstack | 145.51 | 126.02 | 103.98 | -28.5% | -17.5% |
| restack-noop | 60.75 | 62.76 | 64.00 | +5.4% | +2.0% |
| restack-upstack | 154.15 | 134.81 | 117.90 | -23.5% | -12.5% |

### 10 branches

| Case | v0.27.1 | main | Batch diff branch | vs release | vs main |
|---|---:|---:|---:|---:|---:|
| tree-short | 21.95 | 21.80 | 22.27 | +1.5% | +2.2% |
| tree | 29.03 | 30.99 | 29.00 | -0.1% | -6.4% |
| info | 29.95 | 31.87 | 26.33 | -12.1% | -17.4% |
| parent | 17.33 | 17.97 | 17.87 | +3.1% | -0.6% |
| children | 18.06 | 18.42 | 18.40 | +1.9% | -0.1% |
| checkout-exact | 21.02 | 20.57 | 20.59 | -2.1% | +0.1% |
| create | 35.59 | 36.67 | 35.95 | +1.0% | -2.0% |
| modify | 31.96 | 33.72 | 30.49 | -4.6% | -9.6% |
| modify-midstack | 263.04 | 211.53 | 171.46 | -34.8% | -18.9% |
| restack-noop | 74.37 | 78.48 | 76.51 | +2.9% | -2.5% |
| restack-upstack | 271.57 | 224.30 | 182.63 | -32.8% | -18.6% |

### 50 branches

| Case | v0.27.1 | main | Batch diff branch | vs release | vs main |
|---|---:|---:|---:|---:|---:|
| tree-short | 28.92 | 31.20 | 30.93 | +6.9% | -0.9% |
| tree | 62.95 | 65.60 | 53.15 | -15.6% | -19.0% |
| info | 35.02 | 35.86 | 31.15 | -11.1% | -13.1% |
| parent | 18.47 | 19.00 | 18.44 | -0.1% | -2.9% |
| children | 21.66 | 21.93 | 21.25 | -1.9% | -3.1% |
| checkout-exact | 22.69 | 22.08 | 22.40 | -1.3% | +1.4% |
| create | 41.44 | 42.66 | 42.83 | +3.4% | +0.4% |
| modify | 39.63 | 41.30 | 38.59 | -2.6% | -6.6% |
| modify-midstack | 1212.84 | 929.43 | 716.32 | -40.9% | -22.9% |
| restack-noop | 187.61 | 192.00 | 192.68 | +2.7% | +0.4% |
| restack-upstack | 1230.49 | 950.15 | 729.82 | -40.7% | -23.2% |

## Scope and verification

The Git interface replaces `IsDiffEmpty`, `GetChangedFiles`, and `GetDiffNumstat`
with `ReadDiffs`. Many-input file/stat reads use one tree-resolution process plus
one diff-tree process; tree-only checks use one process and single file/stat reads
use one direct diff. Regression tests assert those process counts and compare
single-input and batched results against Git, including renames, unusual paths,
binary files, aliases, missing refs, equal trees, and cancellation.

Branch stats and file counts now use the batch reader; info reuses the file count
from its stats result. Rebase validation reads both file sets together. Independent
diff and commit-count reads overlap during tree rendering. Commit-range/count
batching and ancestry-planning cleanup remain future work.

Validation: `mise run check:go` passed (3,713 tests, four skipped). Targeted race
checks passed, including after overlapping diff/count reads; lint and the Python
benchmark regression tests passed. No remote GitHub benchmarks were run.

## Reproduce

```bash
for depth in 5 10 50; do
  python3 -B scripts/benchmark.py \
    --refs v0.27.1 55f3d860 2b821258 \
    --branches "$depth" --runs 10 --warmup 2 \
    --output "/tmp/stackit-diff-$depth.json"
done
```

Fixtures are linear stacks with one commit and one distinct file per branch.
Modify-midstack and restack-upstack start at the bottom branch and replay all
its descendants. These measurements do not cover large working trees, overlapping
file conflicts, or network latency. Setup, staging, and fixture cleanup are excluded
from the timer. Schema-v1 mutating-case timings included deletion and are not comparable.

The local ignored `benchmark-results.json` artifact contains the three complete
suite runs, including all raw samples and p95 values, under `suite_runs`.
