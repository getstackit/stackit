---
description: Use when the user wants to inspect stack health and state. Trigger phrases include "show the stack", "what's in my stack", "stack status", and "stack health". Reads `stackit log` and reports.
---

# Stack Status

Report stack state without mutating anything.

## Workflow

Run:

```bash
git status --short
stackit log --json --no-interactive
```

Summarize:

- Current branch and parent.
- Children.
- PR status when present.
- Branches needing restack.
- Failing CI or ready-to-merge signals when present.

When you detect an issue, recommend the matching command (don't invent one):

| Detected | Recommend |
|---|---|
| Branches need restack | `stackit restack --branch <root> --upstack --no-interactive` (or `--stacks a,b` for several) |
| Branch has no PR | `stackit submit --no-interactive` |
| PR approved / ready to merge | `stackit merge --yes --no-interactive` (merge the next ready PR) or `stackit merge ship --yes --no-interactive` (consolidate the whole stack) |
| Trunk behind remote | `stackit sync --no-interactive` |

Use plain `stackit log --no-interactive` when the user wants the visual stack.
