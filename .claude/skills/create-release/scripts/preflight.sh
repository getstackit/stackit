#!/usr/bin/env bash
# Verify preconditions for cutting a release.
# Exits non-zero with a clear message if any check fails.
set -euo pipefail

branch=$(git branch --show-current)
if [[ "$branch" != "main" ]]; then
    echo "ERROR: must be on main, currently on: $branch" >&2
    exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
    echo "ERROR: working tree is dirty" >&2
    git status --short >&2
    exit 1
fi

git fetch origin --tags --prune >/dev/null

counts=$(git rev-list --left-right --count main...origin/main)
ahead=$(echo "$counts" | cut -f1)
behind=$(echo "$counts" | cut -f2)
if [[ "$ahead" != "0" || "$behind" != "0" ]]; then
    echo "ERROR: main is not in sync with origin/main (ahead=$ahead behind=$behind)" >&2
    exit 1
fi

echo "OK: on main, clean, in sync with origin/main"
