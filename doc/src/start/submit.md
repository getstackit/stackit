---
icon: material/upload
title: Submit PRs for Your Stack
description: Push your entire stack to GitHub and create PRs with one command. Learn about PR descriptions, updating PRs, and merging strategies.
---

# Submit your PRs

Once you have a stack of branches, it's time to create Pull Requests on GitHub.

## Submit the entire stack

```bash
stackit submit
```

This command:

1. Pushes all branches to GitHub
2. Creates PRs for each branch
3. Sets the correct base branch for each PR (child branches point to their parent)
4. Generates PR descriptions with stack context

## Submit options

### Submit from a specific branch

```bash
stackit submit --branch feature
```

This selects that branch and its ancestors. Add `--stack` to include its descendants.

### Submit as draft PRs

```bash
stackit submit --draft
```

### Submit the current stack

```bash
stackit submit --stack
```

Or use the shorthand alias:

```bash
stackit ss  # Equivalent to: stackit submit --stack
```

## Submission ordering

When submitting PRs, stackit optimizes for both speed and PR number ordering:

- **All new PRs**: When all branches in the submission need new PRs created, stackit submits them sequentially from bottom to top. This preserves PR number ordering so that PR #1 is the base of the stack.
- **Updating existing PRs**: When updating PRs that already exist, stackit submits in parallel for faster completion.
- **Mixed submissions**: If some PRs are new and some exist, the new ones are created sequentially while existing ones update in parallel.

The submit TUI shows progress for each branch as they're processed, indicating whether each PR is being created or updated.

## PR descriptions

Stackit generates PR descriptions that include:

- Your commit message
- Position in the stack
- Links to parent and child PRs
- Visual representation of the stack structure

You can customize the footer behavior with:

```bash
stackit config set submit.footer true
```

## Updating PRs

After making changes to your stack:

1. Make your changes and commit:
   ```bash
   stackit modify  # Amend the current commit
   ```

   `modify` automatically restacks descendants; follow any reported recovery
   steps if it stops on a conflict.

2. Update the PRs:
   ```bash
   stackit submit
   ```

Stackit will update existing PRs instead of creating duplicates.

### Regenerate titles and descriptions

After changing commit messages or regrouping branches, replace existing PR text
with descriptions generated from the current commits:

```bash
stackit submit --regenerate --no-edit
```

This overwrites titles and descriptions for the selected PRs even when their
branches have no new commits to push. One commit supplies its subject and body;
multiple commits use the oldest subject and a chronological list of subjects.
An empty generated description clears the old editable text. Stackit maintains
its generated stack sections through the normal submission flow.

Preview replacement text without saving it or updating PRs:

```bash
stackit submit --regenerate --dry-run --no-edit
```

Add `--json` for structured previews, or use `--regenerate --edit` to edit the
regenerated text before submitting. `--no-edit` suppresses prompts; it does not
cancel regeneration. Without `--regenerate`, existing text is preserved.
`--always` forces submission but does not regenerate text, and `--force` controls
Git push protection independently.

## Merge your stack

Once your PRs are approved, merge the entire stack:

### Interactive merge wizard

```bash
stackit merge
```

Launches an interactive wizard to guide you through merging options.

### Merge bottom PR, then restack

```bash
stackit merge next
```

Merges the bottom-most unmerged PR using GitHub automerge, then restacks remaining branches.

### Consolidate and merge

```bash
stackit merge ship
```

Consolidates all branches into a single PR for atomic merging.

## Next steps

- [Workflows →](../workflows/index.md)
- [Understand core concepts →](../guide/concepts.md)
- [Explore the CLI reference →](../cli/reference.md)
