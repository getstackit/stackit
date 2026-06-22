#!/usr/bin/env bash
# Direct-push backstop for gather-changelog.sh: prints commits on main since the
# given tag. Non-merge commits first, then merges. Use to spot direct
# pushes that bypass PR review.
# Usage: gather-commits.sh <tag>
set -euo pipefail

tag="${1:?missing tag}"

echo "## Non-merge commits"
git log "$tag"..main --pretty=format:'%h %s' --no-merges
echo
echo
echo "## Merge commits"
git log "$tag"..main --pretty=format:'%h %s' --merges
