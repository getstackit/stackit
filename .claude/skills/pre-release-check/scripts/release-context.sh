#!/usr/bin/env bash
# Print a situational-awareness bundle for everything that has landed since
# <baseline>. Summaries only — diffstat, changed-file list, commit subjects,
# and merged PRs — NOT the full diff, which can be enormous across a whole
# release. Review agents run targeted `git diff <baseline>..<head> -- <path>`
# themselves against the range printed here.
#
# Usage: release-context.sh <baseline> [head]
set -euo pipefail

baseline="${1:?missing baseline ref}"
head="${2:-HEAD}"
range="${baseline}..${head}"

if ! git rev-parse --verify --quiet "${baseline}^{commit}" >/dev/null; then
    echo "ERROR: baseline does not resolve to a commit: $baseline" >&2
    exit 1
fi

commit_count=$(git rev-list --count "$range")

echo "# Pre-release context"
echo
echo "Baseline:  $baseline"
echo "Head:      $head ($(git rev-parse --short "$head"))"
echo "Range:     $range"
echo "Commits:   $commit_count"
echo

if [[ "$commit_count" == "0" ]]; then
    echo "No commits since baseline — nothing to release."
    exit 0
fi

echo "## Diffstat"
git diff --stat "$range"
echo

echo "## Changed files (status)"
git diff --name-status "$range"
echo

echo "## Commits (newest first, excluding merges)"
git log "$range" --no-merges --pretty=format:'%h %an %s'
echo
echo

echo "## Merged PRs (best-effort via gh)"
published_at=$(gh release view "$baseline" --json publishedAt --jq .publishedAt 2>/dev/null || true)
if [[ -z "$published_at" ]]; then
    published_at=$(git log -1 --format=%cI "$baseline" 2>/dev/null || true)
fi
if [[ -n "$published_at" ]]; then
    gh pr list --state merged --base main --search "merged:>$published_at" --limit 200 \
        --json number,title,author,url \
        --jq '.[] | "#\(.number) \(.title) — @\(.author.login)"' 2>/dev/null \
        || echo "(gh unavailable — PR list skipped, review the commit log above instead)"
else
    echo "(could not determine baseline date — PR list skipped)"
fi
