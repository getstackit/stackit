# Stackit Workflows Reference

These notes support the narrow Codex skills. Prefer the narrow skill body when it contains more specific instructions.

## Create A Stacked Branch

```bash
git add -A
mkdir -p tmp
printf '%s\n' "feat: add auth middleware" > tmp/stackit-message.txt
stackit create -F tmp/stackit-message.txt --no-interactive
stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
```

With an explicit branch name:

```bash
stackit create -F tmp/stackit-message.txt my-branch --no-interactive
```

If branch config requires a scope, retry with `--scope <value>`.

## Add Work To An Existing Branch

- Follow-up commit: `git add -A` then `git commit -m "<message>"`
- Amend latest commit: `stackit modify --no-interactive`
- Distribute fixes through the stack: `stackit absorb --force --no-interactive`

Use another stacked branch when the work is a separate reviewable unit.

## Submit PRs

```bash
stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
stackit submit --no-interactive
stackit submit --stack --no-interactive
stackit submit --draft --no-interactive
```

## Sync And Restack

```bash
stackit sync --no-interactive
stackit restack --branch <root> --upstack --no-interactive
```

Use `stackit restack --all-stacks --continue-on-conflict --no-interactive` only when the user asked for all stacks.

## Recovery

| Symptom | Action |
|---|---|
| Rebase conflicts | Resolve files, then `stackit continue --no-interactive` |
| Absorb conflict | `stackit absorb --show-conflict --no-interactive` |
| Abort in-progress operation | `stackit abort --no-interactive` |
| Undo last Stackit command | `stackit undo --no-interactive --yes` |
| Partial `stack-plan` backup exists | See [stack-plan-recovery.md](stack-plan-recovery.md) |
