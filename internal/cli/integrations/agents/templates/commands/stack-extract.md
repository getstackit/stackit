---
description: Extract commits or files to an independent branch
model: haiku
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Glob, Grep
---

# Stack Extract

Extract commits or file changes from the current branch to a new independent branch.

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`
- Recent commits: !`git log --oneline -5`

## Instructions

### Use Case 1: Extract file changes to sibling branch (Recommended)

If the user wants to extract specific file(s) to an independent branch:

```bash
stackit split --by-file <file-path> --as-sibling --name "<branch-name>"
```

This creates a new branch on the same parent (usually main) with just those file changes.
The current branch is unchanged - files are NOT removed.

**Example:**
```bash
# Extract docs changes to a sibling branch
stackit split --by-file docs/README.md --as-sibling --name "docs-update"
```

### Use Case 2: Extract to parent branch (Default split behavior)

If the extracted files should logically come before the current changes (dependency extraction):

```bash
stackit split --by-file <file-path>
```

This creates the split branch as a NEW PARENT of the current branch.
The files ARE removed from the current branch.

### Use Case 3: Split commits into sibling branches

If you need to split a branch with multiple commits into sibling branches:

```bash
stackit split --by-commit --as-sibling
```

This interactively lets you select split points. All resulting branches will be
siblings on the same parent instead of a linear chain.

### Verification

After extraction, verify:
```bash
stackit log  # Both branches should be siblings on the same parent
<build-command>      # All checks should pass (check README.md for project's build command)
```

## Choosing Between Sibling and Parent

| Use --as-sibling when... | Use default (parent) when... |
|--------------------------|------------------------------|
| Changes don't belong in this stack | Changes are a dependency for this stack |
| You want an independent PR | The split should be merged first |
| Files should exist on both branches | Files should only be on the split branch |

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Do NOT
- Use raw git commands when stackit commands are available
- Forget to verify the stack structure after extraction
- Extract all files from a branch (at least one must remain)
