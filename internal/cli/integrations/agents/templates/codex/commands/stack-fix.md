---
description: Use when the stack appears broken and the user wants automated diagnosis and repair. Trigger phrases include "fix the stack", "my stack is broken", and "diagnose stack issues". Runs Stackit diagnostic and recovery commands.
---

# Stack Fix

Diagnose stack problems and apply the smallest safe repair.

## Workflow

1. Inspect:

   ```bash
   stackit state --json
   stackit doctor --no-interactive
   ```

2. If an operation is in progress, inspect conflicts and route to `stack-resolve`.

3. If branches need ancestry repair, restack with the narrowest scope that covers
   the breakage. Anchor with `--branch <root>` when you know which stack is broken;
   otherwise cover multiple independent stacks:

   ```bash
   stackit restack --branch <root> --upstack --no-interactive          # known stack
   stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive  # several
   stackit restack --all-stacks --continue-on-conflict --no-interactive               # all stacks
   ```

4. If the last Stackit operation clearly caused the problem and rollback is safest, ask before:

   ```bash
   stackit undo --no-interactive --yes
   ```

5. Verify with `stackit tree --no-interactive` and the lightest relevant build/test command.

Do not delete branches or undo work without explicit user approval.
