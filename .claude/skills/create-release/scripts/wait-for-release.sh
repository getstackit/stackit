#!/usr/bin/env bash
# Poll until the GitHub release for the given tag exists. Goreleaser
# typically materializes the release entry 2-4 minutes after tag push.
# Usage: wait-for-release.sh <tag> [timeout-seconds]
set -euo pipefail

tag="${1:?missing tag}"
timeout="${2:-600}"
interval=20
elapsed=0

while (( elapsed < timeout )); do
    if gh release view "$tag" --json tagName --jq .tagName >/dev/null 2>&1; then
        echo "OK: release $tag exists"
        exit 0
    fi
    sleep "$interval"
    elapsed=$((elapsed + interval))
    echo "waiting for release $tag... (${elapsed}s/${timeout}s)" >&2
done

echo "ERROR: timed out waiting for release $tag after ${timeout}s" >&2
echo "Check workflow status: gh run list --workflow=release.yml --limit 1" >&2
exit 1
