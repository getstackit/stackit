#!/usr/bin/env bash
# Build one CLI for all test packages, then run the supplied test command.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: bash scripts/with-test-binary.sh <command> [args...]" >&2
  exit 2
fi

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache_root="$(go env GOCACHE)/stackit-test-binaries"
mkdir -p "$cache_root"
build_dir="$(mktemp -d "$cache_root/build.XXXXXX")"
trap 'rm -rf "$build_dir"' EXIT

(
  cd "$project_root"
  go build -o "$build_dir/stackit" ./apps/cli
)

# A content-addressed path changes when the CLI changes, but stays stable on
# unchanged runs so Go can reuse test results. Never replace a binary being
# used by a concurrent suite running against a different revision.
binary_id="$(git hash-object "$build_dir/stackit")"
binary_dir="$cache_root/$binary_id"
mkdir -p "$binary_dir"
if [ ! -x "$binary_dir/stackit" ]; then
  mv "$build_dir/stackit" "$binary_dir/stackit"
fi

export STACKIT_TEST_BINARY="$binary_dir/stackit"
"$@"
