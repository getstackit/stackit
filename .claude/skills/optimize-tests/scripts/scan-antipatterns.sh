#!/usr/bin/env bash
# Surface coverage-SAFE structural speedups — the "free wins". Every item below
# changes HOW a test runs, not WHAT it asserts, so it cannot reduce coverage.
# Knock these out first; they need judgement only about whether the test still
# needs the slower form, never a coverage re-check.
#
# Output is advisory — confirm each call site before editing. Usage:
#   scan-antipatterns.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

hr() { printf '\n== %s ==\n' "$1"; }

hr "Process-spawning shells — prefer NewTestShellInProcess (~8ms/cmd cheaper)"
rg -n 'NewTestShell\(' -g '*_test.go' | rg -v 'func |//' || echo "  none (good — all in-process)"

hr "WithRemote() call sites — drop where the test has no push/pull/sync"
rg -n '\bWithRemote\(\)' -g '*_test.go' | rg -v 'func |//' || echo "  none"

hr "Files with 3+ manual CreateAndCheckoutBranch calls — candidates for a fixture"
rg -c '\.CreateAndCheckoutBranch\(' -g '*_test.go' \
  | awk -F: '$2>=3 {printf "  %s (%d calls)\n", $1, $2}' || echo "  none"

hr "⚠ PARALLEL HAZARDS — test files that mutate process-global state"
# These mutate state shared by the whole process, so they CANNOT be made parallel
# (and a concurrent test inheriting that state can hang or flake). t.Setenv panics
# under t.Parallel; os.Stdin/out/err swaps let a concurrent test's child process
# inherit the wrong pipe and block forever; os.Chdir/os.Setenv race the cwd/env.
# EXCLUDE these files (or just the offending func) when parallelizing — same class
# as the t.Setenv rule. Leave them serial.
rg -ln 't\.Setenv\(|os\.Setenv\(|os\.Chdir\(|os\.Stdin\b|os\.Stdout\b|os\.Stderr\b' -g '*_test.go' \
  | sed 's/^/  exclude: /' || echo "  none"

hr "⚠ SHARED SCENARIO — one scenario built at func level, reused across subtests"
# A scenario declared at the function body (1-tab indent), not inside each t.Run,
# is shared mutable state: making the subtests parallel races on it. Either give
# each subtest its own scenario or keep the func serial. (Stackit-specific to the
# scenario.New* helpers; adapt the pattern to your fixture constructor.)
rg -n '^\t[a-zA-Z_]+ :?= .*scenario\.New' -g '*_test.go' | sed 's/^/  review: /' || echo "  none"

hr "Serial top-level Test funcs — no .Parallel() anywhere in the body"
# Heuristic brace-depth scan: flag func TestX(... that never calls .Parallel().
# Advisory only (braces inside strings can fool the depth counter) — the agent
# confirms before editing.
rg --files -g '*_test.go' | LC_ALL=C sort | tr '\n' '\0' | xargs -0 awk '
  /^func[ \t]+Test[A-Za-z0-9_]+[ \t]*\(/ {
    name = $2; sub(/\(.*/, "", name); infunc = 1; depth = 0; sawp = 0; startln = FNR
  }
  infunc {
    if ($0 ~ /\.Parallel\(\)/) sawp = 1
    o = gsub(/{/, "{"); c = gsub(/}/, "}"); depth += o - c
    if (FNR > startln && depth <= 0) {
      if (!sawp && name != "TestMain") printf "%s:%d\t%s\n", FILENAME, startln, name
      infunc = 0
    }
  }
' | sort | awk 'END{if(NR)print "  ("NR" candidates — list above)"} {print "  "$0}' || echo "  none"
