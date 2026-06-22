#!/usr/bin/env bash
# Emit the stack-aware changelog between <tag> and main as JSON: trunk commits
# with consolidated stacks collapsed, so each shipped stack appears as one entry
# whose constituent PRs (and their titles/scope) are listed together.
#
# Builds stackit from the current source first so `log --json` exists and matches
# the code being released — the installed stackit may predate this command.
# Build chatter is sent to stderr to keep stdout pure JSON.
#
# Usage: gather-changelog.sh <tag>
set -euo pipefail

tag="${1:?missing tag}"

mise run build 1>&2

./bin/stackit log --json "$tag..main"
