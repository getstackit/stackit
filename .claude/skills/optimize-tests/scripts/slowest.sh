#!/usr/bin/env bash
# Rank the slowest tests and packages from a `go test -json` timing file.
# These are the hotspots worth a human/agent read — optimize where the time is.
#
# Usage: slowest.sh <timing.json> [N]   (N defaults to 25)
set -euo pipefail

timing="${1:?usage: slowest.sh <timing.json> [N]}"
n="${2:-25}"

echo "== Slowest $n tests =="
jq -r 'select(.Action=="pass" or .Action=="fail") | select(.Test != null)
       | [.Elapsed, .Package, .Test] | @tsv' "$timing" \
  | LC_ALL=C sort -rn | head -n "$n" \
  | awk -F'\t' '{printf "%8.3fs  %-55s %s\n", $1, $2, $3}'

echo
echo "== Slowest $n packages (wall time) =="
jq -r 'select(.Action=="pass" or .Action=="fail") | select(.Test == null)
       | [.Elapsed, .Package] | @tsv' "$timing" \
  | LC_ALL=C sort -rn | head -n "$n" \
  | awk -F'\t' '{printf "%8.3fs  %s\n", $1, $2}'
