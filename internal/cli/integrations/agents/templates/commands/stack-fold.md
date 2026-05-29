---
description: Intelligently fold granular branches into their parents
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), Read, AskUserQuestion, Skill
---

# Stack Fold

Fold (squash) granular branches into their parent branches.

## Context
- Current branch: !`git branch --show-current`
- Stack state: !`stackit log --no-interactive`
- Stack info: !`stackit info --stack --json --no-interactive`

## Instructions

1. Analyze the stack info JSON to identify fold candidates:
   - Small branches (few commits, small diffs)
   - Minor fix messages ("fix typo", "address review", "tweak")
   - Must not be locked or frozen
   - Parent must not be locked or frozen
   - Must match parent's scope
2. Propose a fold plan to user with reasoning
3. Run `stackit fold --dry-run --no-interactive` to preview
4. Use `AskUserQuestion` to confirm fold plan:
   - Header: "Fold plan"
   - Question: "Ready to fold these branches into their parents?"
   - Options:
     - "Execute" - Proceed with folding
     - "Show details" - Show what will be squashed
     - "Cancel" - Abort fold
5. Before each fold, verify:
   - Branch has no children (fold leaf branches first)
   - Parent still not locked/frozen
6. Execute as two separate commands (don't chain with `&&` — keeps permission rules stable):
   ```bash
   stackit checkout <branch> --no-interactive
   stackit fold --no-interactive
   ```
7. `stackit fold` automatically restacks descendants. Only restack manually if it reports remaining work: `stackit restack --branch <parent-branch> --upstack --no-interactive`
8. Show final stack state

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Do NOT
- Fold into trunk unless user explicitly requests it
- Fold locked or frozen branches
- Fold across different scopes
- Fold branches that have children (fold children first)

## Follow-up

After successful fold, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Branches folded successfully. What would you like to do next?"
- Options:
  - label: "Restack branches (Recommended)"
    description: "Rebase affected descendants to ensure consistency after fold"
  - label: "Submit changes"
    description: "Push folded changes to update PRs"
  - label: "Done for now"
    description: "No follow-up action needed"

Based on response:
- **"Restack branches"**: Invoke `/stack-restack` skill using the `Skill` tool and pass the folded parent branch as the target
- **"Submit changes"**: Invoke `/stack-submit` skill using the `Skill` tool
- **"Done for now"**: End with summary of what was folded
