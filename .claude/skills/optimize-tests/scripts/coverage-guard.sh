#!/usr/bin/env bash
# Coverage-preservation guardrail — the safety net for this whole skill.
#
# Fails (exit 1) if any statement block that was covered in BEFORE is no longer
# covered in ANY of the AFTER runs. Equal coverage PERCENTAGE is not enough: two
# suites can share a number while covering different lines. This compares the
# actual covered-block SETS, so deleting the only test that hit a line is caught
# even if the percent barely moves.
#
# Usage: coverage-guard.sh <before.cover> <after.cover> [after2.cover ...]
#
# Pass MULTIPLE after-runs to defuse flaky coverage: a block counts as still
# covered if ANY after-run hits it (the union). Some blocks cover
# nondeterministically across runs — e.g. a sort comparator fed by Go's
# randomized map iteration — and a single after-run will intermittently flag
# them. Two or three after-runs make the guard robust; the regression set is
# then only blocks that NO after-run covered, which is a real loss.
#
# Only edit *_test.go files between the runs. Coverage blocks are keyed by source
# position; editing non-test source shifts the keys and makes the comparison
# meaningless.
set -euo pipefail

before="${1:?usage: coverage-guard.sh <before.cover> <after.cover> [after2.cover ...]}"
shift
[ "$#" -ge 1 ] || { echo "usage: coverage-guard.sh <before.cover> <after.cover> [after2.cover ...]" >&2; exit 2; }

covered() {
  # Block key = field 1 = "import/path/file.go:startLine.col,endLine.col".
  # Covered when the trailing hit count (last field) > 0. Skip the mode header.
  awk 'NR>1 && $NF+0 > 0 {print $1}' "$@" | LC_ALL=C sort -u
}

# Union of covered blocks across every after-run (each "$@" is one after file).
after_union="$(covered "$@")"

# Lines covered BEFORE but in NONE of the after-runs = real regressions.
regressions="$(comm -23 <(covered "$before") <(printf '%s\n' "$after_union") || true)"

if [ -n "$regressions" ]; then
  count="$(printf '%s\n' "$regressions" | grep -c . || true)"
  echo "❌ COVERAGE REGRESSION: ${count} block(s) covered before are NOT covered in any after-run:" >&2
  printf '%s\n' "$regressions" | head -50 >&2
  [ "$count" -gt 50 ] && echo "  … and $((count - 50)) more" >&2
  echo >&2
  echo "Revert the edit that dropped these, or restore an assertion that exercises them." >&2
  echo "(If you passed a single after-run and the block looks nondeterministic, re-run" >&2
  echo " coverage and pass both files: coverage-guard.sh before.cover after1.cover after2.cover)" >&2
  exit 1
fi

echo "✅ no coverage regression — $(covered "$before" | grep -c .) covered blocks preserved across $# after-run(s)"
