#!/usr/bin/env bash
# Analyze stack health and suggest actions
# Part of stackit Claude Code skill

set -e

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "=== Stack Health Analysis ==="
echo

# Check if stackit is installed
if ! command -v stackit &> /dev/null; then
    echo -e "${RED}❌ stackit not found${NC}"
    echo "→ Install from: https://github.com/getstackit/stackit"
    exit 1
fi

# Check if in git repo
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    echo -e "${RED}❌ Not a git repository${NC}"
    exit 1
fi

# Check if stackit is initialized
if ! stackit log > /dev/null 2>&1; then
    echo -e "${RED}❌ Stackit not initialized in this repository${NC}"
    echo "→ Run: stackit init"
    exit 1
fi

echo -e "${BLUE}Current Stack:${NC}"
stackit log
echo

# Fetch authoritative stack metadata once as JSON (the source of truth for
# branch relationships, sizes, and PR state) instead of scraping rendered output.
STACK_JSON=$(stackit log short --json --no-interactive 2>/dev/null || echo '{}')
github_available=$(echo "$STACK_JSON" | jq -r '.github_available // false')

echo -e "${BLUE}Health Checks:${NC}"

# Check for uncommitted changes
if ! git diff-index --quiet HEAD -- 2>/dev/null; then
    echo -e "${YELLOW}⚠️  Uncommitted changes detected${NC}"
    echo "→ Run: git status"
    echo "→ Consider: git add -A (then) stackit modify"
    echo
fi

# Check for branches without PRs (non-trunk branches with no "pr" field).
# Only meaningful when GitHub data is available.
branches_no_pr=0
if [ "$github_available" = "true" ]; then
    branches_no_pr=$(echo "$STACK_JSON" | jq '[.branches[]? | select((.is_trunk | not) and (has("pr") | not))] | length')
    if [ "$branches_no_pr" -gt 0 ]; then
        echo -e "${YELLOW}ℹ️  $branches_no_pr branch(es) without PRs${NC}"
        echo "→ Run: stackit submit --stack"
        echo
    fi
fi

# Check if sync needed (any PR merged or closed)
merged_or_closed=$(echo "$STACK_JSON" | jq '[.branches[]? | select(.pr.state == "MERGED" or .pr.state == "CLOSED")] | length')
if [ "$merged_or_closed" -gt 0 ]; then
    echo -e "${YELLOW}ℹ️  $merged_or_closed PR(s) have been merged/closed${NC}"
    echo "→ Run: stackit sync --restack"
    echo
fi

# Surface failing CI when GitHub data is available
failing_ci=$(echo "$STACK_JSON" | jq -r '[.branches[]? | select(.pr.ci_status == "failing") | .name] | join(", ")')
if [ -n "$failing_ci" ]; then
    echo -e "${RED}⚠️  Failing CI on: $failing_ci${NC}"
    echo
fi

# Check for rebase in progress
if [ -d "$(git rev-parse --git-dir)/rebase-merge" ] || [ -d "$(git rev-parse --git-dir)/rebase-apply" ]; then
    echo -e "${RED}⚠️  Rebase in progress${NC}"
    echo "→ Resolve conflicts and run: stackit continue"
    echo "→ Or abort with: stackit abort"
    echo
fi

# Get current branch
current_branch=$(git branch --show-current)

# Check if on trunk
trunk_branch=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@' || echo "main")
if [ "$current_branch" = "$trunk_branch" ]; then
    echo -e "${YELLOW}ℹ️  Currently on trunk branch ($trunk_branch)${NC}"
    echo "→ Consider: stackit checkout (to switch to a feature branch)"
    echo
fi

# Check for large branches that might need splitting, using the additions/deletions
# already reported per branch in the JSON (>500 changed lines).
echo -e "${BLUE}Branch Size Analysis:${NC}"
large_branch_found=false

while IFS=$'\t' read -r branch additions deletions; do
    [ -z "$branch" ] && continue
    total_lines=$((additions + deletions))
    large_branch_found=true
    echo -e "${YELLOW}⚠️  Large branch: $branch${NC}"
    echo "   +$additions/-$deletions lines ($total_lines total)"
    echo "→ Consider: stackit split (to break into smaller PRs)"
    echo
done < <(echo "$STACK_JSON" | jq -r '.branches[]? | select((.is_trunk | not) and ((.additions + .deletions) > 500)) | "\(.name)\t\(.additions)\t\(.deletions)"')

if [ "$large_branch_found" = false ]; then
    echo -e "${GREEN}✓ All branches are reasonably sized${NC}"
    echo
fi

# Run stackit doctor for additional diagnostics
echo -e "${BLUE}Running stackit doctor:${NC}"
if stackit doctor > /dev/null 2>&1; then
    echo -e "${GREEN}✓ No issues detected${NC}"
else
    echo -e "${YELLOW}⚠️  Stackit doctor found issues${NC}"
    echo "→ Run: stackit doctor (for details)"
    echo
fi

echo
echo "=== Analysis Complete ==="

# Suggest next action based on state
echo
echo -e "${BLUE}Suggested Next Steps:${NC}"

if [ "$branches_no_pr" -gt 0 ]; then
    echo "1. Submit branches: stackit submit --stack"
elif [ "$merged_or_closed" -gt 0 ]; then
    echo "1. Sync with trunk: stackit sync --restack"
elif ! git diff-index --quiet HEAD -- 2>/dev/null; then
    echo "1. Commit changes: git add -A (then) stackit modify"
else
    echo -e "${GREEN}✓ Stack is healthy! Ready for development.${NC}"
fi
