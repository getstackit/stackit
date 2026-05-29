---
description: Use when there is an in-progress rebase or absorb conflict to resolve. Trigger phrases include "resolve conflicts", "continue the rebase", and "fix merge conflicts". Walks conflicted files and runs `stackit continue`.
---

# Stack Resolve

Resolve an in-progress Stackit conflict and continue the operation.

## Workflow

1. Inspect:

   ```bash
   stackit state --json
   ```

   `operation.conflicted_files` lists the unmerged paths; `operation.kind` and
   `operation.stackit_halted` tell you what's in progress.

2. Read each conflicted file and resolve markers.

3. Stage resolved files:

   ```bash
   git add <resolved-files>
   ```

4. Continue:

   ```bash
   stackit continue --no-interactive
   ```

5. If another conflict appears, repeat.

6. Verify with `stackit log --no-interactive` and the relevant check command.

Use `stackit abort --no-interactive` only if the user asks to abort.
