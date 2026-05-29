---
description: Use when files or commits should be extracted to an independent branch on the same parent, or moved to a new parent or child branch. Trigger phrases include "extract this", "pull these files out", "move to its own branch off main", and "split into a sibling branch". Uses `stackit split`.
---

# Stack Extract

Move files or commits off the current branch onto a new sibling, parent, or child branch using `stackit split`.

## Workflow

1. Inspect:

   ```bash
   git status --short
   stackit log --no-interactive
   git log --oneline -5
   ```

2. Confirm with the user what should be extracted (file list or commit set) and where it should go: a sibling off the same parent, a new parent, or a new child.

3. Choose the split form. Pipe the message via `-F -` (the `--message-file`
   shorthand, consistent with create/modify/squash):

   | Goal | Command |
   |---|---|
   | Extract files to a sibling branch (current branch keeps the files) | `printf '<message>' \| stackit split --by-file <files> --as-sibling --name "<branch>" -F - --no-interactive` |
   | Extract files to a new parent (current branch loses the files) | `printf '<message>' \| stackit split --by-file <files> --name "<branch>" -F - --no-interactive` |
   | Extract files to a new child branch | `printf '<message>' \| stackit split --by-file <files> --above --name "<branch>" -F - --no-interactive` |
   | Split commit history into siblings | `stackit split --by-commit --as-sibling` — **interactive only**: `--by-commit` launches a selection wizard that needs a TTY, so it is unsuitable for autonomous runs. Use `--by-file`/`--patch` instead, or ask the user to run it. |
   | Extract specific hunks via patch | `printf '<message>' \| stackit split --patch <patch-file> --above --name "<branch>" -F - --no-interactive` |

4. Verify:

   ```bash
   stackit log --no-interactive
   git status --short
   ```

5. Run the lightest relevant validation command. Prefer `mise run check` when available.

## Choosing Direction

| Use `--as-sibling` when... | Use the default (parent) when... |
|---|---|
| The extracted work belongs in its own PR off trunk. | The extracted work is a dependency the rest of the branch builds on. |
| The current branch should keep the files. | The files should leave the current branch. |

## Do Not

- Use raw `git cherry-pick` or `git checkout -b` when `stackit split` covers the case.
- Extract every file from a branch — at least one file must remain.
- Skip verification after extraction.
