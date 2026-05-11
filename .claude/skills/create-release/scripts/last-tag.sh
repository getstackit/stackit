#!/usr/bin/env bash
# Print the tag of the latest GitHub release. Falls back to git describe if
# the repo has no releases yet.
set -euo pipefail

tag=$(gh release view --json tagName --jq .tagName 2>/dev/null || true)
if [[ -z "$tag" ]]; then
    tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
fi
if [[ -z "$tag" ]]; then
    echo "ERROR: no releases or tags found" >&2
    exit 1
fi
echo "$tag"
