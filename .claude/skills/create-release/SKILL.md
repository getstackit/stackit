---
name: create-release
description: Draft and publish a new GitHub release for stackit. Compares main against the latest release tag, generates release notes in the established format (Highlights, Features, Fixes, Performance, Refactors, Docs, Chore), pushes the tag (triggers goreleaser to build assets), then overwrites the release body with the curated notes. Keywords release, tag, changelog, ship.
allowed-tools: Bash(bash:*), Bash(git:*), Bash(gh:*), Bash(mktemp:*), Bash(cat:*), Read, Write, AskUserQuestion
---

# /create-release

Cut a new GitHub release for stackit by comparing `main` against the latest release tag and drafting curated notes.

## Mental Model

The repo is wired so that **pushing a `vX.Y.Z` tag triggers `.github/workflows/release.yml`**, which runs goreleaser to build cross-platform binaries, publish the Homebrew tap, and create the GitHub release with an auto-generated body. Our job in this skill is to:

1. Determine the next version.
2. Draft a curated release body in the project's established format.
3. Tag and push so goreleaser can do its thing.
4. Once goreleaser has created the release, overwrite the body with the curated notes.

Do **not** create the GitHub release yourself with `gh release create` — goreleaser owns release creation. We only *edit* the body after the fact.

## Scripts

The deterministic steps live in `.claude/skills/create-release/scripts/`. Use them instead of inlining commands — they encode the right error checks and keep invocations identical across runs (which keeps the permission allowlist small).

| Script | Purpose |
|--------|---------|
| `preflight.sh` | Verify on main, clean tree, in sync with origin |
| `last-tag.sh` | Print the latest release tag (or `git describe` fallback) |
| `gather-changelog.sh <tag>` | Build stackit from source and print the stack-aware changelog since `<tag>` as JSON (`{commits: [...]}`) — collapses consolidated stacks into one entry listing their constituent PRs |
| `gather-commits.sh <tag>` | Direct-push backstop: list commits since `<tag>` to spot pushes that bypassed PR review |
| `bump-version.sh <current> <patch\|minor\|major>` | Compute next semver tag |
| `tag-and-push.sh <tag>` | Create annotated tag + push to origin |
| `wait-for-release.sh <tag> [timeout]` | Poll until goreleaser materializes the release (default 1200s; goreleaser typically takes 9-11 minutes) |
| `apply-notes.sh <tag> <file>` | Overwrite release body with notes file, print URL |
| `append-github-footer.sh <new> <prev> <file>` | Append GitHub's auto-generated "What's Changed" footer |

All scripts use `set -euo pipefail` and fail loudly with diagnostic messages. Treat their exit codes as authoritative.

## Execution Steps

### Step 1: Preflight

```bash
bash .claude/skills/create-release/scripts/preflight.sh
```

If it fails, stop and surface the message. Do not attempt to auto-fix (checkout, stash, pull). The user owns those decisions.

### Step 2: Last Tag

```bash
LAST_TAG=$(bash .claude/skills/create-release/scripts/last-tag.sh)
```

### Step 3: Gather Changes

```bash
bash .claude/skills/create-release/scripts/gather-changelog.sh "$LAST_TAG" > /tmp/stackit-release-changelog.json
bash .claude/skills/create-release/scripts/gather-commits.sh "$LAST_TAG"
```

`gather-changelog.sh` builds stackit from the current source (≈30s) and runs
`stackit log --json`, so it works even when the installed stackit predates the
new `log` command.

Read the JSON. If `.commits` is empty, stop — nothing to release. Use
`gather-commits.sh` only as a sanity check: if it shows non-merge commits with no
corresponding PR (and not part of a stack), flag those as possible direct pushes.

### Step 4: Derive the PR List and Categorize (Model Judgment)

Each entry in `.commits` is either a `regular` commit or a `stack-merge` (a
consolidated stack). Flatten them into a deduplicated list of merged PRs:

- **`kind: "stack-merge"`** — emit each PR in `stackPRs`, titled from
  `stackPRTitles` (keyed by PR number). These are the real shipped PRs. Do **not**
  emit the entry's own `prNumber` — that's the "Merging N PRs" consolidation
  rollup, not a changelog item. Note `stackScope`: PRs sharing a scope shipped
  together as one stack.
- **`kind: "regular"` with `prNumber` set** — emit `(prNumber, message)`.
- **`kind: "regular"` with no `prNumber`** — skip. These are raw squashed commits
  already represented by a stack-merge's constituents (or a direct push, which
  `gather-commits.sh` surfaces separately).
- **Dedup by PR number** — the same PR can appear via both a merge commit and a
  consolidation commit.

Flattening stack-merges into `stackPRs` handles rollup-dropping automatically: the
"Merging N PRs" PR is the stack-merge's own `prNumber`, which we never emit.

Titles follow Conventional Commits. Bucket each PR by its title prefix:

| Bucket | Conventional types |
|--------|--------------------|
| Features | `feat` |
| Fixes | `fix` |
| Performance | `perf` |
| Refactors | `refactor` |
| Docs | `docs` |
| Chore | `chore`, `ci`, `test`, `style` |

Write each PR as a past-tense one-liner ending with `(#NNN).` — matching v0.17.11 /
v0.17.13 style. Combine related PRs onto one line when natural (e.g., dependency
bumps → `(#NNN, #MMM)`). When several PRs share a `stackScope`, prefer grouping
them adjacently so the stack reads as one feature.

### Step 5: Pick Highlights (Model Judgment)

Pick **2–5 Highlights**. A highlight is something a user will notice or care about — a new flag, a workflow improvement, a major refactor with visible impact. Skip cosmetic refactors and dependency bumps. Each highlight gets a 1–3 sentence bullet explaining *what* changed and *why it matters to the user*.

### Step 6: Confirm the Next Version

Propose a default based on the categorized PRs:

- **Patch** — only fixes, perf, refactor, docs, chore.
- **Minor** — any user-visible new features, new flags, new commands.
- **Major** — explicit breaking changes (rare pre-1.0).

**Always ask the user via `AskUserQuestion` before computing the tag**, even when the bump looks obvious. Tagging is durable and triggers a release pipeline; an unconfirmed bump is not recoverable without deleting a published tag.

Once the user picks a kind:

```bash
NEXT_TAG=$(bash .claude/skills/create-release/scripts/bump-version.sh "$LAST_TAG" "$BUMP_KIND")
```

### Step 7: Assemble Notes

```bash
NOTES_FILE=$(mktemp -t stackit-release-notes)
```

Write the notes via the `Write` tool to `$NOTES_FILE`. Structure:

```markdown
<Optional 1–2 sentence opener describing the theme of the release. Omit if
the release has no unifying theme.>

### Highlights

- <highlight 1>
- <highlight 2>

### Features

- <feature> (#NNN).

### Fixes

- <fix> (#NNN).

### Performance

- <perf change> (#NNN).

### Refactors

- <refactor> (#NNN).

### Docs

- <docs change> (#NNN).

### Chore

- <chore> (#NNN).
```

Omit empty sections entirely.

Optionally append GitHub's auto-generated footer (the "What's Changed" PR list + "Full Changelog" diff link) so the body matches the historical shape:

```bash
bash .claude/skills/create-release/scripts/append-github-footer.sh "$NEXT_TAG" "$LAST_TAG" "$NOTES_FILE"
```

Default: include the footer. Skip only if the user explicitly asks for a minimal body.

### Step 8: Final Confirmation Before Tagging

Read the notes file back and show the user both `$NEXT_TAG` and the full notes. Use `AskUserQuestion`:

- Approve and tag — proceeds to Step 9.
- Edit notes — rewrite the file with the user's corrections, then re-confirm.
- Change version — loop back to Step 6.
- Cancel — stop without tagging.

Do **not** push the tag without explicit approval at this step.

### Step 9: Tag, Wait, Apply Notes

```bash
bash .claude/skills/create-release/scripts/tag-and-push.sh "$NEXT_TAG"
bash .claude/skills/create-release/scripts/wait-for-release.sh "$NEXT_TAG"
bash .claude/skills/create-release/scripts/apply-notes.sh "$NEXT_TAG" "$NOTES_FILE"
```

`apply-notes.sh` prints the release URL on success.

### Step 10: Report

Print to the user:

- The new tag and release URL.
- Reminder that the Homebrew tap will update once goreleaser finishes.
- If `gh run list --workflow=release.yml --limit 1` shows the run failed, surface that — the tag exists but assets and Homebrew may be missing.

## Permission Stability

Per `.claude/rules/stackit-workflow.md`:

- Run one command per Bash call — don't chain with `&&`.
- Pass release notes via a file path, never inline with `-m`/`--notes`.
- Don't append `2>&1`.

Calling the scripts (rather than inlining `gh`/`git` commands) keeps the command shape `bash .claude/skills/create-release/scripts/<name>.sh ...` across every invocation, so a single permission rule covers all of them.

## Common Pitfalls

| Mistake | Fix |
|---------|-----|
| Tagging from a non-main branch | `preflight.sh` aborts |
| Tagging with a dirty tree | `preflight.sh` aborts |
| Using `gh release create` directly | Goreleaser owns release creation; we only `edit` after |
| Forgetting to push the tag | `tag-and-push.sh` does both atomically |
| Overwriting goreleaser's release body before it's created | `wait-for-release.sh` polls until ready |
| Patch bump when adding a user-visible feature | Re-check Step 6 — feat-level changes warrant a minor bump |
| Emitting a stack-merge's own `prNumber` (the "Merging N PRs" rollup) | Emit its `stackPRs` children instead; never the consolidation number |
| Skipping version confirmation | Step 6 requires `AskUserQuestion` every time |

## Notes

- Pre-1.0: minor bumps can carry breaking changes. Confirm with the user before assuming major.
- The release workflow is `.github/workflows/release.yml`. If it fails (lint, build, or goreleaser step), the tag still exists — the user must either fix forward (next patch) or delete the tag and re-push.
- `gh release edit` overwrites the body, not the title or assets. Goreleaser owns those.
- `gather-changelog.sh` runs `stackit log --json`, whose shape (`commits[]` with `sha`, `message`, `kind`, `prNumber`, `stackSize`, `stackPRs`, `stackPRTitles`, `stackScope`) is a stability contract this skill depends on. Treat field changes in that command as breaking for the release flow.
