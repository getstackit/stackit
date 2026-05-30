---
description: Use when the user wants to review PRs in the stack and report findings locally. Trigger phrases include "review the stack", "check PRs for issues", and "code review my stack".
---

# Stack Review

Review stack PRs for high-confidence issues and report findings locally.

## Workflow

1. Gather stack PRs:

   ```bash
   stackit state --json
   ```

2. For each open non-draft PR selected for review:

   ```bash
   gh pr view <branch> --json state,isDraft,number,url,headRefName
   gh pr diff <number>
   ```

3. Review only for high-confidence problems:

   - Compilation or parsing failures.
   - Definitive logic bugs.
   - Clear project-instruction violations.
   - Security vulnerabilities.

4. Do not report style preferences, speculative risks, or nits.

5. Return findings first, ordered by severity, with file and line references.

If no issues meet the bar, say so and mention residual test gaps.

## Applying feedback

This skill reports findings; it does not push fixes by default. If the user asks
you to apply changes, use stackit — never `gh pr` edits or a manual `git rebase`:

```bash
stackit checkout <reviewed-branch> --no-interactive
# edit files...
git add -A
stackit modify --no-interactive          # amends the branch; auto-restacks descendants
stackit submit --no-interactive           # update the PRs
```

For a fix that belongs on a different branch in the stack, use `stackit absorb`
instead of amending the wrong commit.
