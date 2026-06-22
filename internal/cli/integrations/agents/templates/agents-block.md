<!-- stackit:start -->
## Git Workflow: Stacked PRs

This project uses [stackit](https://github.com/getstackit/stackit) for stacked changes.
AI agents should proactively work in stacks.

### Why Stack?
Small PRs get reviewed faster. Break features into focused, reviewable units.

### When to Stack
Stack when your change has 2+ logical phases, exceeds ~400 lines,
or would benefit from early review of foundational work.

### Workflow
```bash
git add -A                              # Stage first
echo "feat: ..." | stackit create -F -  # Create stacked branch (message via stdin)
# ... continue working ...
stackit submit                          # Submit all PRs
```

### Key Commands
| Command | Purpose |
|---------|---------|
| `echo "feat: msg" \| stackit create -F -` | Create stacked branch |
| `stackit submit` | Push & create/update PRs |
| `stackit restack --upstack` | Rebase children after editing a branch (never manual `git rebase`) |
| `stackit sync` | Pull trunk, cleanup merged |
| `stackit tree` | Visualize branch tree |

Run `/stackit` for the full skill, or `/stack-status` to check current state.
<!-- stackit:end -->
