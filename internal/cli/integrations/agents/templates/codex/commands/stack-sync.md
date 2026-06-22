---
description: Use when the user wants to sync the stack with trunk and clean up merged branches. Trigger phrases include "sync with main", "pull trunk", "clean up merged branches", and "update from origin". Runs `stackit sync`.
---

# Stack Sync

Sync with trunk and clean up branches that have landed.

## Workflow

1. Inspect:

   ```bash
   stackit state --json
   ```

2. If there are uncommitted changes, stop and ask the user to commit (via
   `stackit create`/`stackit modify`) or abort. Do not stash.

3. Preview what sync would do:

   ```bash
   stackit sync --dry-run --json --restack --no-interactive
   ```

   Parse `would_clean` (branches to delete), `would_restack`, and
   `would_restack_stacks` (independent stack roots) from the JSON.

4. If **all** branches would be deleted, stop and confirm with the user first.

5. Run cleanup without restacking, so restack can use a refreshed scope:

   ```bash
   stackit sync --no-restack --no-interactive
   ```

6. Recompute the restack scope (cleanup/reparenting may have changed roots):

   ```bash
   stackit sync --dry-run --json --restack --no-interactive
   ```

   Then restack using the refreshed `would_restack_stacks`:
   - One root → `stackit restack --branch <root> --upstack --no-interactive`
   - Several roots → `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`
   - Roots unavailable / all stacks affected → `stackit restack --all-stacks --continue-on-conflict --no-interactive`

7. Verify:

   ```bash
   stackit tree --no-interactive
   ```
