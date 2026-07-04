---
description: Use when the user wants to amend the current stacked branch's commit. Trigger phrases include "amend this", "fix the current commit", "modify the branch", and "add this to the current commit". Runs `stackit modify`.
---

# Stack Modify

Amend the current stacked branch or add a follow-up commit to it.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

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

   If the commit's intent changed and the message should be regenerated, write
   the message to a file and pass it with `-F`:

   ```bash
   mkdir -p tmp
   printf '%s\n' "<message>" > tmp/stackit-message.txt
   stackit modify --no-interactive -F tmp/stackit-message.txt
   ```

   If the user asked for a *new* commit on this branch rather than an amend:

   ```bash
   stackit modify --no-interactive -c -F tmp/stackit-message.txt
   ```

   If sandbox metadata shows `.git` is read-only, run `stackit modify` with escalation on the first attempt.

4. Verify with the compact current-branch view:

   ```bash
   stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
   ```

`stackit modify` automatically restacks descendant branches — do not run a manual
`stackit restack` or `git rebase` afterward. Never use `git commit --amend`;
`stackit modify` handles stack metadata.
