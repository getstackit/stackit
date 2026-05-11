#!/usr/bin/env bash
# Compute the next semver tag from the current tag and a bump kind.
# Usage: bump-version.sh <current-tag> <patch|minor|major>
set -euo pipefail

current="${1:?missing current tag}"
kind="${2:?missing bump kind (patch|minor|major)}"

ver="${current#v}"
IFS=. read -r major minor patch <<<"$ver"

if [[ -z "${major:-}" || -z "${minor:-}" || -z "${patch:-}" ]]; then
    echo "ERROR: could not parse '$current' as vMAJOR.MINOR.PATCH" >&2
    exit 1
fi

case "$kind" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
    *) echo "ERROR: bump kind must be patch|minor|major (got '$kind')" >&2; exit 1 ;;
esac

echo "v${major}.${minor}.${patch}"
