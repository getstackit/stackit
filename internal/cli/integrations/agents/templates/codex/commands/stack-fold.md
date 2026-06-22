---
description: Use when granular branches should be folded into their parent. Trigger phrases include "fold this branch", "squash into parent", and "merge this branch into the previous one". Runs `stackit fold`.
---

# Stack Fold

Fold a branch into its parent when it is too small to review separately.

## Workflow

1. Inspect:

   ```bash
   stackit state --json
   ```

2. Check preconditions before folding (folding rewrites stack structure):
   - Fold **leaf branches first** — a branch with children cannot be folded until
     its children are folded or moved.
   - Skip **locked or frozen** branches, and skip if the **parent** is locked/frozen.
   - Do **not** fold across different scopes.
   - Do **not** fold into trunk unless the user explicitly asks (requires `--allow-trunk`).

3. Confirm the target branch and parent with the user.

4. Preview the fold before applying it:

   ```bash
   stackit fold --dry-run --no-interactive
   ```

5. Run the fold:

   ```bash
   stackit fold --no-interactive
   ```

6. `stackit fold` automatically restacks descendants — only restack manually if it
   reports remaining work:

   ```bash
   stackit restack --branch <parent-branch> --upstack --no-interactive
   ```

7. Verify with `stackit tree --no-interactive`.
