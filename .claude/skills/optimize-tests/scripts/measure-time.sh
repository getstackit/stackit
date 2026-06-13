#!/usr/bin/env bash
# Time a test run and capture per-test timings as JSON.
#
# Usage: measure-time.sh <scope> <out.json>
#   scope    a tier (fast|git|integration|unit|all) OR a go package pattern
#            (e.g. ./internal/engine/...)
#   out.json where to write the `go test -json` event stream
#
# Prints a short summary; the last line is `wall_seconds=<n>` for easy parsing.
# No coverage instrumentation here — it distorts timing. This run answers the
# single question "how fast is the suite?". Use measure-cover.sh for safety.
set -euo pipefail

scope="${1:?usage: measure-time.sh <scope> <out.json>}"
out="${2:?usage: measure-time.sh <scope> <out.json>}"

cd "$(git rev-parse --show-toplevel)"

resolve_scope() {
  case "$1" in
    fast | git | integration | unit | all) bash scripts/go-test-packages.sh "$1" ;;
    *) printf '%s\n' "$1" ;;
  esac
}

packages="$(resolve_scope "$scope")"

start=$SECONDS
# -count=1 disables the test cache so timings reflect real work, not cache hits.
# shellcheck disable=SC2086
if go test -json -count=1 $packages >"$out" 2>"$out.stderr"; then
  status="pass"
else
  status="fail"
fi
elapsed=$((SECONDS - start))

echo "scope:        $scope"
echo "packages:     $(printf '%s\n' "$packages" | grep -c . || true)"
echo "status:       $status"
echo "timing json:  $out"
echo "wall_seconds=$elapsed"
