#!/usr/bin/env bash
# Coverage-preservation guardrail — the safety net for this whole skill.
#
# Fails (exit 1) if any statement block that was covered in BEFORE is no longer
# covered in AFTER. Equal coverage PERCENTAGE is not enough: two suites can share
# a number while covering different lines. This compares the actual covered-block
# SETS, so deleting the only test that hit a line is caught even if the percent
# barely moves.
#
# Usage: coverage-guard.sh <before.cover> <after.cover>
#
# Only edit *_test.go files between the two runs. Coverage blocks are keyed by
# source position; editing non-test source shifts the keys and makes the
# comparison meaningless.
set -euo pipefail

before="${1:?usage: coverage-guard.sh <before.cover> <after.cover>}"
after="${2:?usage: coverage-guard.sh <before.cover> <after.cover>}"

covered() {
  # Block key = field 1 = "import/path/file.go:startLine.col,endLine.col".
  # Covered when the trailing hit count (last field) > 0. Skip the mode header.
  awk 'NR>1 && $NF+0 > 0 {print $1}' "$1" | LC_ALL=C sort -u
}

# Lines present in BEFORE-covered but absent from AFTER-covered = regressions.
regressions="$(comm -23 <(covered "$before") <(covered "$after") || true)"

if [ -n "$regressions" ]; then
  count="$(printf '%s\n' "$regressions" | grep -c . || true)"
  echo "❌ COVERAGE REGRESSION: ${count} block(s) covered before are NOT covered after:" >&2
  printf '%s\n' "$regressions" | head -50 >&2
  [ "$count" -gt 50 ] && echo "  … and $((count - 50)) more" >&2
  echo >&2
  echo "Revert the edit that dropped these, or restore an assertion that exercises them." >&2
  exit 1
fi

echo "✅ no coverage regression — $(covered "$before" | grep -c .) covered blocks preserved"
