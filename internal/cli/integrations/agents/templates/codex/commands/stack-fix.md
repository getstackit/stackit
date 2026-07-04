---
description: Use when the stack appears broken and the user wants automated diagnosis and repair. Trigger phrases include "fix the stack", "my stack is broken", and "diagnose stack issues". Runs Stackit diagnostic and recovery commands.
---

# Stack Fix

Diagnose stack problems and apply the smallest safe repair.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Inspect:

   ```bash
   stackit state --json | jq '{current_branch,trunk,working_tree,operation,branches:[.stack.branches[] | {name,parent,is_current,is_trunk,needs_restack,is_locked,is_frozen,children:(.children // [])}]}'
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

   If sandbox metadata shows `.git` is read-only, run mutating Stackit repair commands with escalation on the first attempt.

4. If the last Stackit operation clearly caused the problem and rollback is safest, ask before:

   ```bash
   stackit undo --no-interactive --yes
   ```

5. Verify with the compact stack-wide health view and the lightest relevant build/test command.

Do not delete branches or undo work without explicit user approval.
