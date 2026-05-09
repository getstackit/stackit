#!/usr/bin/env bash
set -euo pipefail

tier="${1:-all}"

integration_packages='/internal/integration($|/)|/internal/cli/integrations($|/)|/internal/actions/integrations($|/)'
git_scenario_packages='/internal/actions($|/absorb$|/create$|/delete$|/describe$|/flatten$|/fold$|/foreach$|/lock$|/merge$|/move$|/navigation$|/pluck$|/scope$|/submit$|/sync$|/track$|/undo$|/untrack$|/worktree$)|/internal/cli($|/branch$|/navigation$|/stack($|/merge$))|/internal/engine$|/internal/git$|/internal/pr$|/internal/worktree$|/testhelpers($|/inprocess$|/scenario$)'

list_all_packages() {
  go list ./... | grep -v '/node_modules/'
}

case "$tier" in
  all)
    list_all_packages
    ;;

  fast)
    list_all_packages | grep -Ev "${integration_packages}|${git_scenario_packages}"
    ;;

  git)
    list_all_packages | grep -Ev "$integration_packages" | grep -E "$git_scenario_packages"
    ;;

  integration)
    go list ./internal/integration/... ./internal/cli/integrations/... ./internal/actions/integrations/...
    ;;

  unit)
    list_all_packages | grep -Ev "${integration_packages}|${git_scenario_packages}|/internal/config$|/internal/github$|/internal/rerere$"
    ;;

  *)
    echo "usage: $0 [all|fast|git|integration|unit]" >&2
    exit 2
    ;;
esac
