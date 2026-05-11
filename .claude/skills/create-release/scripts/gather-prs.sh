#!/usr/bin/env bash
# Print PRs merged into main since the given tag, as a JSON array suitable
# for jq processing. Each entry has: number, title, author, mergedAt,
# labels, url.
# Usage: gather-prs.sh <tag>
set -euo pipefail

tag="${1:?missing tag}"

published_at=$(gh release view "$tag" --json publishedAt --jq .publishedAt 2>/dev/null || true)
if [[ -z "$published_at" ]]; then
    published_at=$(git log -1 --format=%cI "$tag")
fi

gh pr list \
    --state merged \
    --base main \
    --search "merged:>$published_at" \
    --limit 200 \
    --json number,title,author,mergedAt,labels,url
