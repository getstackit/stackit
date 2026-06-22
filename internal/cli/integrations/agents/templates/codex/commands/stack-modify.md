---
description: Use when the user wants to amend the current stacked branch's commit. Trigger phrases include "amend this", "fix the current commit", "modify the branch", and "add this to the current commit". Runs `stackit modify`.
---

# Stack Modify

Amend the current stacked branch or add a follow-up commit to it.

## Workflow

1. Check for changes first:

   ```bash
   git status --short
   ```

   If there are no changes, stop.

2. Stage changes unless the user explicitly requested staged-only behavior:

   ```bash
   git add -A
   ```

3. Amend, keeping the existing message (the default — use this when you are only
   adding code to the current commit):

   ```bash
   stackit modify --no-interactive --no-edit
   ```

   If the commit's intent changed and the message should be regenerated, pipe it
   via stdin (keeps permission rules stable across messages):

   ```bash
   printf '%s\n' "<message>" | stackit modify --no-interactive -F -
   ```

   If the user asked for a *new* commit on this branch rather than an amend:

   ```bash
   printf '%s\n' "<message>" | stackit modify --no-interactive -c -F -
   ```

4. Verify (run `stackit tree` only after the mutation):

   ```bash
   stackit tree --no-interactive
   ```

`stackit modify` automatically restacks descendant branches — do not run a manual
`stackit restack` or `git rebase` afterward. Never use `git commit --amend`;
`stackit modify` handles stack metadata.
