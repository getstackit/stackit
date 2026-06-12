#!/usr/bin/env bash
# Resolve the baseline ref for a pre-release review.
#
#   - With no argument: print the latest GitHub release tag.
#     Falls back to the latest annotated/lightweight git tag if the repo has
#     no GitHub releases yet.
#   - With an argument: verify it resolves to a commit and echo it back.
#     Lets the caller pin the review to an explicit baseline
#     (e.g. `pre-release-check v0.18.0`).
#
# Usage: last-release.sh [ref]
set -euo pipefail

ref="${1:-}"

if [[ -n "$ref" ]]; then
    if ! git rev-parse --verify --quiet "${ref}^{commit}" >/dev/null; then
        echo "ERROR: baseline ref does not resolve to a commit: $ref" >&2
        exit 1
    fi
    echo "$ref"
    exit 0
fi

tag=$(gh release view --json tagName --jq .tagName 2>/dev/null || true)
if [[ -z "$tag" ]]; then
    tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
fi
if [[ -z "$tag" ]]; then
    echo "ERROR: no GitHub releases or git tags found; pass a baseline ref explicitly" >&2
    exit 1
fi
echo "$tag"
