---
description: Use when the user wants to open or refresh PRs for the stack. Trigger phrases include "submit the stack", "open PRs", "push and create PRs", and "send for review". Runs `stackit submit`.
---

# Stack Submit

Submit branches as PRs or update existing PRs. This touches remotes, so confirm before running unless the user explicitly asked to submit.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Inspect:

   ```bash
   stackit state --json | jq '{current_branch,trunk,working_tree,operation}'
   ```

2. If there are uncommitted changes, warn and stop unless the user asked to submit anyway.

3. Check for a PR template, and if one exists, read it and follow its structure
   when generating PR bodies (otherwise skip straight to submit):

   ```bash
   git ls-files .github/pull_request_template.md CONTRIBUTING.md
   ```

4. Submit current branch plus ancestors by default:

   ```bash
   stackit submit --no-interactive
   ```

   After the user has approved this remote-affecting action, run `stackit submit` with escalation on the first attempt when network or `.git` access is sandboxed.

   Entire stack:

   ```bash
   stackit submit --stack --no-interactive
   ```

   Drafts:

   ```bash
   stackit submit --draft --no-interactive
   ```

5. Report created or updated PR URLs from the command output.

Do not use `gh pr create`; Stackit owns PR parentage and metadata. Do not submit
PRs with placeholder content (TODO/TBD, empty sections) — generate a real summary
and test plan, or leave the field for stackit to auto-generate.
