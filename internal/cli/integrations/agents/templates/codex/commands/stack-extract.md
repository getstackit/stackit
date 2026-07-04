---
description: Use when files or commits should be extracted to an independent branch on the same parent, or moved to a new parent or child branch. Trigger phrases include "extract this", "pull these files out", "move to its own branch off main", and "split into a sibling branch". Uses `stackit split`.
---

# Stack Extract

Move files or commits off the current branch onto a new sibling, parent, or child branch using `stackit split`.

## Workflow

When a `jq` snippet is shown, use it only if `jq` is available. If not, run `stackit state --no-interactive` and summarize only relevant lines; use raw `stackit state --json` only as a last resort and do not paste the full JSON.

1. Inspect:

   ```bash
   stackit state --json | jq '.current_branch as $c | {current_branch:$c, branch:(.stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen})}'
   git log --oneline -5
   ```

2. Confirm with the user what should be extracted (file list or commit set) and where it should go: a sibling off the same parent, a new parent, or a new child.

3. Write the message once, then choose the split form. Use `-F` so the mutating
   command starts with `stackit split` instead of a pipe:

   ```bash
   mkdir -p tmp
   printf '%s\n' "<message>" > tmp/stackit-message.txt
   ```

   | Goal | Command |
   |---|---|
   | Extract files to a sibling branch (current branch keeps the files) | `stackit split --by-file <files> --as-sibling --name "<branch>" -F tmp/stackit-message.txt --no-interactive` |
   | Extract files to a new parent (current branch loses the files) | `stackit split --by-file <files> --name "<branch>" -F tmp/stackit-message.txt --no-interactive` |
   | Extract files to a new child branch | `stackit split --by-file <files> --above --name "<branch>" -F tmp/stackit-message.txt --no-interactive` |
   | Split commit history into siblings | `stackit split --by-commit --as-sibling` — **interactive only**: `--by-commit` launches a selection wizard that needs a TTY, so it is unsuitable for autonomous runs. Use `--by-file`/`--patch` instead, or ask the user to run it. |
   | Extract specific hunks via patch | `stackit split --patch <patch-file> --above --name "<branch>" -F tmp/stackit-message.txt --no-interactive` |

   If sandbox metadata shows `.git` is read-only, run `stackit split` with escalation on the first attempt.

4. Verify:

   ```bash
   stackit state --json | jq '.current_branch as $c | .stack.branches[] | select(.name == $c) | {name,parent,children,needs_restack,is_locked,is_frozen,pr}'
   git status --short
   ```

5. Run the lightest relevant validation command. Use full `stackit tree --no-interactive` only if the compact view leaves ancestry ambiguous.

## Choosing Direction

| Use `--as-sibling` when... | Use the default (parent) when... |
|---|---|
| The extracted work belongs in its own PR off trunk. | The extracted work is a dependency the rest of the branch builds on. |
| The current branch should keep the files. | The files should leave the current branch. |

## Do Not

- Use raw `git cherry-pick` or `git checkout -b` when `stackit split` covers the case.
- Extract every file from a branch — at least one file must remain.
- Skip verification after extraction.
