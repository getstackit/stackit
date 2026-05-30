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

# Fetch one authoritative snapshot as JSON: the working tree, any in-progress
# operation, and the full stack (branch relationships, sizes, PR/CI state). This
# replaces scraping rendered output and separate git probes for dirty/rebase state.
STATE_JSON=$(stackit state --json 2>/dev/null || echo '{}')
github_available=$(echo "$STATE_JSON" | jq -r '.stack.github_available // false')
# Use `== false` rather than `// default`: jq's `//` treats a boolean false as
# empty, so `.working_tree.clean // true` would wrongly yield true on a dirty tree.
working_tree_dirty=$(echo "$STATE_JSON" | jq -r '.working_tree.clean == false')

echo -e "${BLUE}Health Checks:${NC}"

# Uncommitted changes (from the snapshot's working_tree)
if [ "$working_tree_dirty" = "true" ]; then
    echo -e "${YELLOW}⚠️  Uncommitted changes detected${NC}"
    echo "→ Run: git status"
    echo "→ Consider: git add -A (then) stackit modify"
    echo
fi

# Check for branches without PRs (non-trunk branches with no "pr" field).
# Only meaningful when GitHub data is available.
branches_no_pr=0
if [ "$github_available" = "true" ]; then
    branches_no_pr=$(echo "$STATE_JSON" | jq '[.stack.branches[]? | select((.is_trunk | not) and (has("pr") | not))] | length')
    if [ "$branches_no_pr" -gt 0 ]; then
        echo -e "${YELLOW}ℹ️  $branches_no_pr branch(es) without PRs${NC}"
        echo "→ Run: stackit submit --stack"
        echo
    fi
fi

# Check if sync needed (any PR merged or closed)
merged_or_closed=$(echo "$STATE_JSON" | jq '[.stack.branches[]? | select(.pr.state == "MERGED" or .pr.state == "CLOSED")] | length')
if [ "$merged_or_closed" -gt 0 ]; then
    echo -e "${YELLOW}ℹ️  $merged_or_closed PR(s) have been merged/closed${NC}"
    echo "→ Run: stackit sync --restack"
    echo
fi

# Surface failing CI when GitHub data is available
failing_ci=$(echo "$STATE_JSON" | jq -r '[.stack.branches[]? | select(.pr.ci_status == "failing") | .name] | join(", ")')
if [ -n "$failing_ci" ]; then
    echo -e "${RED}⚠️  Failing CI on: $failing_ci${NC}"
    echo
fi

# In-progress operation (from the snapshot's operation field)
op_kind=$(echo "$STATE_JSON" | jq -r '.operation.kind // "none"')
if [ "$op_kind" != "none" ]; then
    n_conflicts=$(echo "$STATE_JSON" | jq -r '(.operation.conflicted_files // []) | length')
    echo -e "${RED}⚠️  $op_kind in progress ($n_conflicts conflicted file(s))${NC}"
    echo "→ Resolve conflicts and run: stackit continue"
    echo "→ Or abort with: stackit abort"
    echo
fi

# Current branch and trunk (from the snapshot)
current_branch=$(echo "$STATE_JSON" | jq -r '.current_branch // ""')
trunk_branch=$(echo "$STATE_JSON" | jq -r '.trunk // "main"')

# Check if on trunk
if [ -n "$current_branch" ] && [ "$current_branch" = "$trunk_branch" ]; then
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
done < <(echo "$STATE_JSON" | jq -r '.stack.branches[]? | select((.is_trunk | not) and ((.additions + .deletions) > 500)) | "\(.name)\t\(.additions)\t\(.deletions)"')

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
elif [ "$working_tree_dirty" = "true" ]; then
    echo "1. Commit changes: git add -A (then) stackit modify"
else
    echo -e "${GREEN}✓ Stack is healthy! Ready for development.${NC}"
fi
