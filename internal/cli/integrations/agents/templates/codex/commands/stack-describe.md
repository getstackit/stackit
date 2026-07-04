---
description: Use when the user wants stack or PR descriptions generated or refreshed from commit history. Trigger phrases include "describe the stack", "generate PR descriptions", and "update the stack description".
---

# Stack Describe

Generate or refresh stack and PR descriptions from the current branch history.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Build context cheapest-first. Work down this ladder and **stop as soon as you
   can write an accurate description** — most stacks never need past the first rung.
   **Never run a full `git diff` of the stack;** it burns enormous context for
   little signal over the cheaper sources below.

   1. **Commit subjects — cheapest.** Usually all you need:

      ```bash
      stackit state --json | jq -r '.stack.branches[] | select(.is_trunk|not) | [.name,(.parent // ""),(.pr.title // "")] | @tsv'
      git log --oneline --decorate -20
      ```

   2. **PR descriptions — cheap, high-signal.** When subjects are thin and a
      branch has a PR, read the existing PR body (a human/agent already
      summarized intent) and synthesize from it rather than from code:

      ```bash
      gh pr view <pr_number> --json title,body -q '.title, .body'
      ```

   3. **Changed file names — cheap, names only.** To see which subsystems a
      branch touches without reading contents:

      ```bash
      git diff --name-only <parent>..<branch>
      ```

   If after all of this the intent is still unclear on a large stack and your
   harness supports delegating to a sub-agent, have the sub-agent read the diff
   and return a 2–3 sentence summary so the diff stays out of your main context.
   Otherwise describe from the signals above — do not read the full diff yourself.

2. Generate a title and description from the history:
   - **Title** — max 72 chars, imperative mood, summarizes the whole stack.
   - **Description** — markdown body with a summary paragraph, bullet points of
     key changes (grouped by branch/concern), and a concrete test plan.

3. Set the description from a file in editor format (first line title, blank
   line, then body). A bare non-interactive `describe` with no input now errors
   with "nothing to set" — it does not write anything:

   ```bash
   mkdir -p tmp
   printf '%s\n\n%s\n' "<title>" "<description>" > tmp/stackit-description.txt
   stackit describe -F tmp/stackit-description.txt --no-interactive
   ```

   If sandbox metadata shows `.git` is read-only, run `stackit describe` with escalation on the first attempt.

4. Confirm what was set and report it:

   ```bash
   stackit describe --show --no-interactive
   ```

Descriptions should include a concrete summary and test plan. Do not use placeholders.
Report only the title and a short summary of the body that was set.
