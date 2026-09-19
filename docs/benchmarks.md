# CLI benchmarks

`scripts/benchmark.py` measures common, local CLI workflows across Git refs.
It intentionally avoids commands that contact GitHub: network latency and SSH connection
reuse are important operational concerns, but do not produce a stable version-to-version
regression signal. Use the structured subprocess trace in [performance.md](performance.md)
when diagnosing those commands.

## Run

Run the default release comparison (v0.25.0, v0.26.1, v0.27.1, and `main`):

```bash
mise run bench
```

The runner writes `benchmark-results.json` (ignored by Git). To use a lower-cost smoke run
or choose specific revisions:

```bash
python3 scripts/benchmark.py --runs 3 --warmup 1 --output /tmp/bench.json
python3 scripts/benchmark.py --refs v0.27.1 main --runs 20
python3 scripts/benchmark.py --refs v0.27.1 HEAD --branches 50 --runs 10
```

By default, historical module downloads and Go build artifacts are cached in
`/tmp/stackit-benchmark-go-cache`; pass `--cache-dir` to use another writable location.

Each revision is built from a `git archive`, then used to create its own ten-branch linear
fixture. Read-only cases reuse that fixture; mutating cases receive a fresh copy prepared
outside the timed interval. This makes results comparable even when metadata implementation
details differ between releases.

## Cases and interpretation

The suite covers `tree short`, normal `tree` (including stats), `info`, `parent`,
`children`, exact `co`, staged `create`, staged leaf `modify`, staged `modify` at the
bottom of a stack, no-op `restack`, and `restack --upstack` after amending the bottom
branch. The last two mutating scenarios replay all descendants with disjoint files.
Use `--branches 5`, `10`, and `50` to measure scaling. Each fixture branch has one
commit touching one distinct file; these cases measure CLI overhead and stack depth,
not large-repository or overlapping-file rebase performance.

The JSON includes median, mean, p95, all samples, source commits, and tool/platform
versions. With only ten samples, p95 is descriptive, not a reliable tail estimate.
Treat the median as the release comparison and inspect the samples for contention.

Fixture setup, Go compilation, fixture copying/deletion, staging, and the preparatory
amend for restack are outside the timer. Git user/system config is isolated, hooks and
signing are disabled, and logging is off. Results are saved after each revision so an
interrupted run retains completed versions. Run benchmarks on an otherwise idle machine.

Schema version 2 fixes the original timer including fixture deletion and changes create
and modify to commit staged content. Do not directly compare its mutating-case timings
with schema version 1. Both revisions must run through the same harness.

Check timing boundaries and error handling with:

```bash
python3 -B scripts/test_benchmark.py
```
