---
description: Use when the user wants to run a build or test command across every branch in the stack. Trigger phrases include "verify each branch", "check every branch builds", and "run tests on the whole stack".
---

# Stack Verify

Run verification across stack branches.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Inspect:

   ```bash
   stackit state --json | jq '{current_branch,trunk,working_tree,operation}'
   ```

2. Determine the lightest check command from user input, project docs, or common files. Prefer targeted commands over full-suite checks.

3. Run across the current stack with structured output, stopping at the first
   failing depth:

   ```bash
   stackit foreach --stack --json --find-first-failure --no-interactive "<check-command>"
   ```

   For current branch and descendants only, swap `--stack` for `--upstack`.

   If sandbox metadata shows `.git` is read-only, run `stackit foreach` with escalation on the first attempt because it may switch branches.

4. Parse `results[]` for the first entry with a non-zero `exit_code`. Report the
   failing branch, repro command, and actionable failure block; do not paste
   passing branch output. If all pass, report that clearly.
