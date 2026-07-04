---
description: Use when the user wants to create a new Stackit stacked branch from current working tree changes. Trigger phrases include "stack these changes", "create a stacked change", "make a stackit branch", "commit this with stackit", and "turn this into a stacked PR". Stages changes and runs `stackit create`.
---

# Stack Create

Create one new stacked branch from the current working tree.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

Use a staged preflight. Do not eagerly gather every git view up front.

1. Start with one cheap check:

   ```bash
   git status --short --branch
   ```

2. If there are no staged or unstaged changes, tell the user and stop.

3. Only if you need to decide between `stack-create` and `stack-plan`, inspect changed paths with exactly one command:

   ```bash
   git diff --cached --name-only
   ```

   If nothing is staged yet and you still need the path list:

   ```bash
   git diff --name-only
   ```

   Route to `stack-plan` when the changed paths clearly represent multiple reviewable units.

4. Stage the intended changes:

   ```bash
   git add -A
   ```

5. If the user did not provide `-m`, generate a Conventional Commit message deterministically:

   - Read repo-local guidance first
   - Check recent `git log --oneline -5` for style
   - Choose the type from the dominant intent of the staged changes
   - Prefer a one-line subject under 72 characters
   - If the user gave a branch name but no message, still generate a non-empty message

6. If the user did not provide a branch name, preserve the repo's configured `branch.pattern` by omitting the branch-name argument. Only pass an explicit branch name when the user asked for one.

7. Write the message to a stable repo-local file, then create the branch:

   ```bash
   mkdir -p tmp
   printf '%s\n' "<commit message>" > tmp/stackit-message.txt
   stackit create -F tmp/stackit-message.txt --no-interactive
   ```

   With an explicit branch name:

   ```bash
   stackit create -F tmp/stackit-message.txt <branch-name> --no-interactive
   ```

   If config requires a scope, add `--scope <value>`.

8. If sandbox metadata shows `.git` is read-only, run the `stackit create` command with escalation on the first attempt. If creation still fails because Git cannot write under `.git/refs/heads` or create a `.lock` file, retry the exact same command with the required permission or escalation. Do not change the branch name just to work around that failure.

9. Verify after mutation, not before:

   ```bash
   stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
   ```

10. Report:

   - branch name
   - parent branch
   - commit subject
   - scope status: explicit, inherited, or none
   - worktree path if created
   - recommended next step

## Do Not

- Use `git commit` to create the branch.
- Use `git checkout -b`.
- Chain staging and creation in one shell command.
- Pipe messages into `stackit create`; use `tmp/stackit-message.txt` with `-F` so approval rules match `stackit create`.
- Run `git status`, multiple `git diff` variants, `git log`, and `stackit tree` all up front when one or two targeted checks would do.
- Introduce a fallback branch name after a permission failure. Preserve the pattern-driven command and retry it with approval instead.
