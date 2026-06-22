---
description: Use when the user wants stack or PR descriptions generated or refreshed from commit history. Trigger phrases include "describe the stack", "generate PR descriptions", and "update the stack description".
---

# Stack Describe

Generate or refresh stack and PR descriptions from the current branch history.

## Workflow

1. Inspect:

   ```bash
   stackit tree --no-interactive
   git log --oneline --decorate -20
   ```

2. Generate a title and description from the history:
   - **Title** — max 72 chars, imperative mood, summarizes the whole stack.
   - **Description** — markdown body with a summary paragraph, bullet points of
     key changes (grouped by branch/concern), and a concrete test plan.

3. Set the description with explicit flags (a bare non-interactive `describe`
   with no input now errors with "nothing to set" — it does not write anything):

   ```bash
   stackit describe -m "<title>" -d "<description>" --no-interactive
   ```

   `-d` requires `-m`, and the body supports multiline text. Alternatively pipe
   the whole thing in editor format (first line title, blank line, then body):

   ```bash
   printf '<title>\n\n<description>' | stackit describe -F - --no-interactive
   ```

4. Confirm what was set and report it:

   ```bash
   stackit describe --show --no-interactive
   ```

Descriptions should include a concrete summary and test plan. Do not use placeholders.
