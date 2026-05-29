---
description: Use when the user wants to run a build or test command across every branch in the stack. Trigger phrases include "verify each branch", "check every branch builds", and "run tests on the whole stack".
---

# Stack Verify

Run verification across stack branches.

## Workflow

1. Inspect:

   ```bash
   stackit log --no-interactive
   git status --short
   ```

2. Determine check command from user input, project docs, or common files. Prefer `mise run check` when available.

3. Run across the current stack with structured output, stopping at the first
   failing depth:

   ```bash
   stackit foreach --stack --json --find-first-failure --no-interactive "<check-command>"
   ```

   For current branch and descendants only, swap `--stack` for `--upstack`.

4. Parse `results[]` for the first entry with a non-zero `exit_code` — report that
   failing branch and its output. If all pass, report that clearly.
