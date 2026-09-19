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
```

By default, historical module downloads and Go build artifacts are cached in
`/tmp/stackit-benchmark-go-cache`; pass `--cache-dir` to use another writable location.

Each revision is built from a `git archive`, then used to create its own ten-branch linear
fixture. Read-only cases reuse that fixture; mutating cases receive a fresh copy prepared
outside the timed interval. This makes results comparable even when metadata implementation
details differ between releases.

## Cases and interpretation

The suite covers the local, high-frequency workflow: `tree short`, `info`, `parent`,
`children`, exact `co`, `create`, `modify`, and no-op `restack`. It reports median, mean,
p95, and every millisecond sample in JSON. Treat the median as the release comparison and
the p95 as a warning for intermittent local contention.

The fixture setup, Go compilation, fixture copying, hook execution, and network operations
are excluded from samples. Benchmarks should run on an otherwise idle machine; retain the
JSON artifact with the commit hashes whenever recording a baseline.
