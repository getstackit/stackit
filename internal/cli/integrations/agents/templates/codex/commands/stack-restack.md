---
description: Use when the stack needs to be rebased to restore proper ancestry. Trigger phrases include "restack", "rebase the stack", "my stack is out of sync", and "fix parent relationships". Runs `stackit restack`.
---

# Stack Restack

Rebase stack branches according to Stackit metadata.

## Workflow

1. Inspect:

   ```bash
   stackit state --json
   ```

2. Precondition: if the state JSON's `working_tree.clean` is false (uncommitted
   changes), stop and tell the user to commit (via `stackit create`/`stackit
   modify`) or stash before restacking — a dirty tree will fail the rebase
   mid-flight.

3. Choose the narrowest scope that covers what changed:
   - Current stack: `stackit restack --upstack --no-interactive`
   - A specific root: `stackit restack --branch <root> --upstack --no-interactive`
   - Several independent roots: `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`
   - Every independent stack (only if requested): `stackit restack --all-stacks --continue-on-conflict --no-interactive`

4. If conflicts occur: resolve the files, stage them, then run
   `stackit continue --no-interactive` to finish (or invoke `stack-resolve`). Do
   not use raw `git rebase`. If `--continue-on-conflict` reports skipped branches,
   no rebase is active for them — restack one with
   `stackit restack --branch <conflicted-branch> --upstack --no-interactive`, then
   resolve and `stackit continue`.

5. Verify with `stackit tree --no-interactive`. When done, report the result and
   suggest `stackit submit` to update PRs, then stop.
