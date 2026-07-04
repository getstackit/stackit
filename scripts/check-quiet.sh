#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

export GOCACHE="${GOCACHE:-$project_root/.gocache}"
export GOLANGCI_LINT_CACHE="${GOLANGCI_LINT_CACHE:-$project_root/.golangci-cache}"
mkdir -p "$GOCACHE" "$GOLANGCI_LINT_CACHE"

log_dir="${TMPDIR:-/tmp}/stackit-check-quiet-$(
  date +%Y%m%d-%H%M%S
)-$$"
mkdir -p "$log_dir"

max_failure_lines="${STACKIT_CHECK_FAILURE_LINES:-160}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/check-quiet.sh <target> [args]

targets:
  all              fmt:check, lint, Go tests, and web checks
  go               fmt:check, lint, and Go tests
  fmt              check Go formatting without modifying files
  lint             run golangci-lint with compact output
  test [tier]      run Go tests for all|fast|git|integration|unit
  test:fresh       run all Go tests without cache
  test:race        run all Go tests with the race detector
  web:test         run frontend tests
  web:typecheck    run frontend typecheck
  web:build        build the frontend
  web:check        web:test, web:typecheck, and web:build
EOF
}

print_failure_log() {
  local log_file="$1"

  if [ ! -s "$log_file" ]; then
    echo "(no output captured)"
    return
  fi

  local summary_file="$log_file.summary"
  awk '
    /^=== (Failed|Errors)/ { show = 1 }
    show { print }
  ' "$log_file" >"$summary_file"
  if [ -s "$summary_file" ]; then
    tail -n "$max_failure_lines" "$summary_file"
    return
  fi

  tail -n "$max_failure_lines" "$log_file"
}

run_phase() {
  local name="$1"
  local repro="$2"
  shift 2

  local log_file="$log_dir/${name//[^A-Za-z0-9_.-]/_}.log"

  if "$@" >"$log_file" 2>&1; then
    echo "ok $name"
    return 0
  fi

  echo "fail $name"
  echo "repro: $repro"
  echo "log: $log_file"
  echo
  print_failure_log "$log_file"
  return 1
}

fmt_check() {
  local log_file="$log_dir/fmt.log"

  goimports -l . >"$log_file" 2>&1
  if [ ! -s "$log_file" ]; then
    echo "ok fmt"
    return 0
  fi

  echo "fail fmt"
  echo "repro: mise run fmt"
  echo "log: $log_file"
  echo
  echo "Files need goimports formatting:"
  print_failure_log "$log_file"
  return 1
}

lint() {
  run_phase \
    lint \
    "mise run lint" \
    golangci-lint run \
      --allow-parallel-runners \
      --show-stats=false \
      --output.text.print-issued-lines=false \
      --output.text.colors=false
}

go_test() {
  local tier="${1:-all}"
  shift || true

  local package_file="$log_dir/packages-$tier.log"
  local -a packages
  if ! bash scripts/go-test-packages.sh "$tier" >"$package_file" 2>&1; then
    echo "fail test:$tier packages"
    echo "repro: bash scripts/go-test-packages.sh $tier"
    echo "log: $package_file"
    echo
    print_failure_log "$package_file"
    return 1
  fi

  while IFS= read -r package; do
    packages+=("$package")
  done <"$package_file"

  if [ "${#packages[@]}" -eq 0 ]; then
    echo "fail test:$tier packages"
    echo "repro: bash scripts/go-test-packages.sh $tier"
    echo "log: $package_file"
    echo
    echo "No packages matched test tier '$tier'."
    return 1
  fi

  local repro="${STACKIT_CHECK_REPRO:-}"
  if [ -z "$repro" ]; then
    repro="mise run test"
  fi
  if [ "$tier" != "all" ]; then
    repro="mise run test:$tier"
  fi

  run_phase \
    "test:$tier" \
    "$repro" \
    gotestsum \
      --format standard-quiet \
      --hide-summary=skipped,output \
      -- \
      "$@" \
      "${packages[@]}"
}

web_test() {
  run_phase \
    "web:test" \
    "mise run web:test" \
    pnpm --filter @stackit/web test -- --reporter=agent --silent=passed-only
}

web_typecheck() {
  run_phase \
    "web:typecheck" \
    "mise run check:web" \
    pnpm --dir apps/web exec tsc --noEmit --pretty false
}

web_build() {
  run_phase \
    "web:build" \
    "mise run web:build" \
    pnpm --filter @stackit/web build
}

web_check() {
  web_test
  web_typecheck
  web_build
}

target="${1:-}"
if [ -z "$target" ]; then
  usage
  exit 2
fi
shift || true

case "$target" in
  all)
    fmt_check
    lint
    go_test all
    web_check
    ;;
  go)
    fmt_check
    lint
    go_test all
    ;;
  fmt)
    fmt_check
    ;;
  lint)
    lint
    ;;
  test)
    go_test "${1:-all}"
    ;;
  test:fresh)
    STACKIT_CHECK_REPRO="mise run test:fresh" go_test all -count=1
    ;;
  test:race)
    STACKIT_CHECK_REPRO="mise run test:race" go_test all -race
    ;;
  web:test)
    web_test
    ;;
  web:typecheck)
    web_typecheck
    ;;
  web:build)
    web_build
    ;;
  web:check)
    web_check
    ;;
  *)
    usage
    exit 2
    ;;
esac
