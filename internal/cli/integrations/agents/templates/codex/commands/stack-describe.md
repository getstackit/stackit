---
description: Use when the user wants stack or PR descriptions generated or refreshed from commit history. Trigger phrases include "describe the stack", "generate PR descriptions", and "update the stack description".
---

# Stack Describe

Generate or refresh stack and PR descriptions from the current branch history.

## Workflow

1. Inspect:

   ```bash
   stackit log --no-interactive
   git log --oneline --decorate -20
   ```

2. Generate a title and description from the history:
   - **Title** — max 72 chars, imperative mood, summarizes the whole stack.
   - **Description** — markdown body with a summary paragraph, bullet points of
     key changes (grouped by branch/concern), and a concrete test plan.

3. Set the description (this is what actually writes it — the bare command only
   *displays* in non-interactive mode and changes nothing):

   ```bash
   stackit describe -m "<title>" -d "<description>" --no-interactive
   ```

   `-d` requires `-m`, and the body supports multiline text.

4. Confirm what was set and report it:

   ```bash
   stackit describe --show --no-interactive
   ```

Descriptions should include a concrete summary and test plan. Do not use placeholders.
