#!/usr/bin/env bash
# Append GitHub's auto-generated "What's Changed" / "Full Changelog" footer
# to the notes file. Use when the user wants the auto-footer preserved
# below the curated notes.
# Usage: append-github-footer.sh <new-tag> <previous-tag> <notes-file>
set -euo pipefail

new_tag="${1:?missing new tag}"
prev_tag="${2:?missing previous tag}"
notes="${3:?missing notes file}"

if [[ ! -f "$notes" ]]; then
    echo "ERROR: notes file not found: $notes" >&2
    exit 1
fi

repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)

printf '\n' >> "$notes"
gh api "repos/${repo}/releases/generate-notes" \
    -f tag_name="$new_tag" \
    -f previous_tag_name="$prev_tag" \
    --jq .body >> "$notes"
