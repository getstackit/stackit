# Stack Plan Recovery

`stack-plan` creates a backup branch before splitting changes. The backup is deleted only after all planned branches are created and verified.

## Partial Run State

A partial run may leave:

- A branch named `stack-plan-backup-<timestamp>` with all original changes.
- Some created stacked branches.
- The original branch with an empty or partial working tree.

## Start Over

Reset the original branch to the backup in one step (`-B` already moves the branch
to the given start point, so no separate `git reset` is needed):

```bash
git checkout -B <original-branch> stack-plan-backup-<timestamp>
```

If `stack-plan` created partial stacked branches, unwind them with `stackit undo`.
Each call reverts one prior Stackit snapshot — run it until the partial-run
branches are gone, checking after each:

```bash
stackit undo --no-interactive --yes
stackit log --no-interactive
```

## Continue From The Last Successful Branch

```bash
stackit log --no-interactive
git checkout stack-plan-backup-<timestamp>
git diff <last-created-branch>..stack-plan-backup-<timestamp> --stat
git checkout <last-created-branch>
git checkout stack-plan-backup-<timestamp> -- <files-for-next-branch>
printf '%s\n' "<commit message>" | stackit create -F - <next-branch> --no-interactive
```

## Clean Up

Only after confirming every intended change is in the stack:

```bash
git branch -D stack-plan-backup-<timestamp>
```
