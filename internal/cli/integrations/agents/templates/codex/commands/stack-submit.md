---
description: Use when the user wants to open or refresh PRs for the stack. Trigger phrases include "submit the stack", "open PRs", "push and create PRs", and "send for review". Runs `stackit submit`.
---

# Stack Submit

Submit branches as PRs or update existing PRs. This touches remotes, so confirm before running unless the user explicitly asked to submit.

## Workflow

1. Inspect:

   ```bash
   git status --short
   stackit log --no-interactive
   stackit info --json --no-interactive
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
