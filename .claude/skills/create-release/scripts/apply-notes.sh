#!/usr/bin/env bash
# Overwrite the release body with the contents of the given notes file.
# Prints the release URL on success.
# Usage: apply-notes.sh <tag> <notes-file>
set -euo pipefail

tag="${1:?missing tag}"
notes="${2:?missing notes file}"

if [[ ! -s "$notes" ]]; then
    echo "ERROR: notes file is missing or empty: $notes" >&2
    exit 1
fi

gh release edit "$tag" --notes-file "$notes" >/dev/null
gh release view "$tag" --json url --jq .url
