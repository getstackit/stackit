---
description: Use when fixes in the working tree should be routed back to the correct commits across the stack. Trigger phrases include "absorb these fixes", "distribute fixes", "amend across the stack", and "route this fix to the right commit". Runs `stackit absorb`.
---

# Stack Absorb

Absorb staged fixes into the commits that last touched the changed lines.

## Workflow

1. Inspect:

   ```bash
   stackit state --json
   ```

2. Stage intended fixes:

   ```bash
   git add -A
   ```

3. Run absorb with machine-readable output:

   ```bash
   stackit absorb --json --force --no-interactive
   ```

4. Parse absorbed branches, unabsorbable hunks, and new files from the output.

5. Determine a verification command from project docs or common files. Prefer the lightest command that covers the changed packages.

6. Verify the stack (structured output, stop at the first failing depth):

   ```bash
   stackit foreach --stack --json --find-first-failure --no-interactive "<check-command>"
   ```

   Parse `results[]` for the first entry with a non-zero exit code — that is the
   earliest failing branch.

7. If a branch fails, fix the earliest failing branch from the absorb output
   sources: unabsorbable hunks, new files, or code absorbed too far upstack. Commit
   the fix at that source branch (`stackit modify --no-interactive`), then restack
   that branch and its descendants:

   ```bash
   stackit restack --branch <failing-branch> --upstack --no-interactive
   ```

8. Finish with:

   ```bash
   stackit log --no-interactive
   ```

Do not continue past repeated verification failures; report the failing branch and recovery options.
