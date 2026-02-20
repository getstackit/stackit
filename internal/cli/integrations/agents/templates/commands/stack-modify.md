---
description: Modify the current branch by amending or creating a new commit
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), AskUserQuestion, Skill
argument-hint: [-m "message"] [-a] [-c] [--no-edit]
---

# Stack Modify

## Context
- Current branch: !`git branch --show-current`
- Unstaged changes: !`git diff --stat | head -20`
- Staged changes: !`git diff --cached --stat | head -20`
- Recent commits on branch: !`git log --oneline -5`
- Stack state: !`stackit tree --no-interactive`

## Arguments
$ARGUMENTS

## Task

Modify the current branch by amending its commit or creating a new commit. Automatically restacks descendants after modification.

**Two modes:**
- **Amend (default):** Amends the current branch's latest commit
- **New commit (`-c`):** Creates a new commit on the branch

### Steps

1. **Check for changes and stage:**
   - If there are no staged and no unstaged changes, inform user and stop.
   - Otherwise run `git add -A` once to stage everything (this is the same whether
     or not the user passed `-a` — don't double-stage). Skip only if the user
     explicitly asked to amend with the currently-staged set only.

2. **Determine the message:**
   - If `--no-edit` or `-n` provided, keep the existing commit message (amend mode only).
   - If `-m "message"` provided, use that message.
   - If neither provided, generate a message describing the change from the diff
     (always — both for amend and for a new commit with `-c`).

3. **Run the command** (pipe generated messages via stdin so permission rules stay
   stable across messages):
   ```bash
   # Amend (default), keeping the existing message
   stackit modify --no-edit

   # Amend with a regenerated message
   printf '%s\n' "<message>" | stackit modify -F -

   # New commit on this branch
   printf '%s\n' "<message>" | stackit modify -c -F -
   ```
   Changes are already staged from Step 1, so no `-a` is needed.

4. **Handle results:**
   - On success, report what was modified and that descendants were restacked
   - On failure, report the error clearly

**Never use:** `git commit --amend` — always use `stackit modify` so descendants are restacked.

## Prompt Note

After a successful modify, record what was done by running:
```bash
command stackit notes add \
  --prompt "<the user's original instruction that led to these changes>" \
  --summary "<brief summary of what was actually done>" \
  --model "<your model name, e.g. claude-sonnet-4>"
```
This captures LLM context on the commit for audit and memory purposes. Keep the prompt and summary concise (1-2 sentences each).

## Follow-up

After successful modification, use `AskUserQuestion`:
- Header: "Next step"
- Question: "Branch modified. What would you like to do next?"
- Options:
  - "Submit to update PRs (Recommended)" → Invoke `/stack-submit` using Skill tool
  - "Continue working" → Tell user to make more changes
  - "Done for now" → End with summary
