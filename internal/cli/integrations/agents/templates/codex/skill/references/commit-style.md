# Commit And PR Style

Read `README.md`, `CONTRIBUTING.md`, and recent `git log --oneline` output before choosing a message.

## Commit Messages

Default to Conventional Commits: `<type>: <description>`.

Common types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `chore`, `ci`.

```text
feat(auth): implement JWT authentication

Add login endpoint and token validation middleware.
```

If the project does not document a format, write a clear subject line, a blank line, then a short explanation of why the change exists.

For Stackit mutations, write generated messages to a file and pass `-F` so the
mutating shell command starts with `stackit`:

```bash
mkdir -p tmp
printf '%s\n' "feat: add x" > tmp/stackit-message.txt
stackit create -F tmp/stackit-message.txt --no-interactive
```

## PR Descriptions

Follow `.github/pull_request_template.md` when present. Otherwise:

```markdown
## Summary
- What changed

## Test Plan
- Command or manual verification performed
```

Avoid placeholder titles or bodies.

## Branch Names

Let Stackit auto-generate branch names from commit messages unless the user requested a branch name or config requires one.
