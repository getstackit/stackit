#!/usr/bin/env bash
# Capture a coverage profile for a scope.
#
# Usage: measure-cover.sh <scope> <out.cover>
#   scope     a tier (fast|git|integration|unit|all) OR a go package pattern
#   out.cover where to write the coverage profile
#
# IMPORTANT: use the SAME scope for the before and after runs so
# coverage-guard.sh can compare block-for-block. Prints `total_pct=<n>` last.
set -euo pipefail

scope="${1:?usage: measure-cover.sh <scope> <out.cover>}"
out="${2:?usage: measure-cover.sh <scope> <out.cover>}"

cd "$(git rev-parse --show-toplevel)"

resolve_scope() {
  case "$1" in
    fast | git | integration | unit | all) bash scripts/go-test-packages.sh "$1" ;;
    *) printf '%s\n' "$1" ;;
  esac
}

packages="$(resolve_scope "$scope")"

# -coverpkg=./... attributes coverage to EVERY package, so an integration test
# that exercises code in another package still counts toward that package — this
# is what makes the guardrail honest about end-to-end coverage.
# -covermode=set => 0/1 per block (covered-or-not), which is exactly the signal
# coverage-guard.sh compares. -count=1 disables the cache.
# shellcheck disable=SC2086
if ! go test -count=1 -covermode=set -coverpkg=./... -coverprofile="$out" $packages >"$out.log" 2>&1; then
  echo "note: some tests failed during the coverage run; profile still written (see $out.log)" >&2
fi

pct="$(go tool cover -func="$out" 2>/dev/null | awk '/^total:/ {print $NF}')"
echo "scope:      $scope"
echo "coverage:   $out"
echo "total_pct=${pct:-n/a}"
