#!/usr/bin/env bash
# Create an annotated tag at current HEAD and push it to origin, which
# triggers .github/workflows/release.yml -> goreleaser.
# Usage: tag-and-push.sh <new-tag>
set -euo pipefail

tag="${1:?missing tag}"

if git rev-parse "$tag" >/dev/null 2>&1; then
    echo "ERROR: tag $tag already exists locally" >&2
    exit 1
fi

if git ls-remote --tags origin "refs/tags/$tag" | grep -q "$tag"; then
    echo "ERROR: tag $tag already exists on origin" >&2
    exit 1
fi

git tag -a "$tag" -m "$tag"
git push origin "$tag"
echo "OK: pushed $tag"
