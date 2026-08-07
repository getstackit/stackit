# Hooks

Stackit can run user-defined shell commands at specific points in a command's
lifecycle. Hooks are configured per-repo in `.stackit.yaml` and require
explicit approval per user before they execute.

> The runner is **zero-cost when no hook is configured.** If `.stackit.yaml`
> has no `pre-modify` entry, `stackit modify` does no extra work — no I/O,
> no extra git call, no GitHub fetch. Hooks never *introduce* network calls;
> if a hook wants fresh GitHub data, the hook itself fetches it.

## Configuration

Hooks live under `hooks:` in `.stackit.yaml`. Each phase is a list of shell
command strings, executed via `sh -c`:

```yaml
hooks:
  post-worktree-create:
    - "mise trust"
    - "mise run shims"
  pre-modify:
    - "scripts/check-review.sh"
  post-submit:
    - "scripts/notify.sh"
```

### Phase names

The phase key is `<pre|post>-<command-path>`. The command path is the cobra
command path without the leading `stackit `, joined by hyphens.

| Command | Pre-phase | Post-phase |
|---|---|---|
| `stackit modify` | `pre-modify` | `post-modify` |
| `stackit submit` | `pre-submit` | `post-submit` |
| `stackit absorb` | `pre-absorb` | `post-absorb` |
| `stackit merge ship` | `pre-merge-ship` | `post-merge-ship` |
| `stackit worktree create` | — | `post-worktree-create` |

`post-worktree-create` runs only when a worktree actually exists — it also fires
for `stackit create -w`, `worktree attach`, and `worktree run`, but is skipped
entirely when worktree creation fails. This is deliberate: the hook is invoked
with the new worktree as its working directory, and an empty path resolves to
the process CWD, which on the failure path is the user's main checkout sitting
on trunk. A hook like `pnpm install` would run there instead.

Every command that goes through `common.Run` participates in the lifecycle —
including read-only commands like `log`, `status`, and `get`. When no hook is
configured for a given command + phase the runner short-circuits, so the
practical cost on uninstrumented commands is one slice-length check.

### Failure semantics

- **Pre-hook non-zero exit:** the command is aborted with the hook's stderr
  surfaced to the user. Subsequent hooks in the same phase do not run.
- **Post-hook non-zero exit:** the command's result is unaffected; the
  failure is surfaced as a warning on stdout.

### Hook timeout

Each hook is killed after 60 seconds (`hooks.DefaultTimeout`). This is not
currently configurable per-phase.

## Approval system

Hooks defined in `.stackit.yaml` (shared, committed) do not run until the
local user approves them. Approvals are stored per-user in git config and
never leave the machine.

### Flow

1. Repo defines hook in `.stackit.yaml`.
2. User runs a command that would fire the hook.
3. Stackit prompts: `This repo wants to run "<cmd>" at <phase>. Allow?`.
4. On approval, the command is added to
   `stackit.hooks.approved.<phase>` in local git config.
5. Subsequent runs of that exact command skip the prompt.

### Managing approvals

```bash
# List approvals for a phase
git config --get-all stackit.hooks.approved.pre-modify

# Pre-approve a hook command without prompting
git config --add stackit.hooks.approved.pre-modify "scripts/check-review.sh"

# Remove a single approval
git config --unset stackit.hooks.approved.pre-modify "scripts/check-review.sh"

# Clear all approvals for a phase
git config --unset-all stackit.hooks.approved.pre-modify
```

### Legacy key

Approvals from versions before per-phase keys are stored under the single key
`stackit.hooks.approvedPostWorktreeCreate`. New code reads both, dedupes,
and writes only to the per-phase key. No automatic migration is performed.

## Skipping hooks: `--no-verify`

The persistent flag `--no-verify` disables both git hooks and stackit hooks
for the invocation:

```bash
stackit modify --no-verify    # skips pre-modify and post-modify
```

## Hook payload

When a hook fires, stackit sets a small env-var payload on the spawned
process. All values come from data already in memory — no fresh GitHub
fetch is performed.

| Variable | Value |
|---|---|
| `STACKIT_HOOK_PHASE` | Full phase name, e.g. `pre-modify` |
| `STACKIT_COMMAND` | Leaf cobra command name, e.g. `create` |
| `STACKIT_COMMAND_PATH` | Full command path, kebab-cased, e.g. `worktree-create` |
| `STACKIT_BRANCH` | Current branch name (if available) |
| `STACKIT_PARENT` | Parent branch name (if available) |
| `STACKIT_PR_NUMBER` | PR number from local metadata (if submitted) |
| `STACKIT_PR_STATE` | `OPEN`, `MERGED`, or `CLOSED` (from local metadata) |
| `STACKIT_PR_DRAFT` | `true` or `false` (from local metadata) |

`STACKIT_PR_*` fields reflect the last `sync` snapshot — they can be stale.
Hooks that need fresh GitHub state should fetch it themselves (see recipe
below).

## Recipes

### Block `stackit modify` once review has started

A hook that fetches the live PR review decision and refuses to amend if
anyone has reviewed or been formally requested for changes:

```sh
#!/bin/sh
# scripts/check-review.sh
# Configure in .stackit.yaml:
#   hooks:
#     pre-modify:
#       - "scripts/check-review.sh"
set -e

if [ -z "$STACKIT_PR_NUMBER" ]; then
  exit 0  # No PR yet — nothing to protect.
fi

decision=$(gh pr view "$STACKIT_PR_NUMBER" \
  --json reviewDecision -q .reviewDecision 2>/dev/null || true)
case "$decision" in
  APPROVED|CHANGES_REQUESTED)
    echo "blocked: PR #$STACKIT_PR_NUMBER review in progress (decision=$decision)" >&2
    echo "use 'stackit modify --no-verify' if you really want to rewrite history." >&2
    exit 1
    ;;
esac
```

### Lint before submitting

```yaml
hooks:
  pre-submit:
    - "mise run lint"
```

### Auto-generate a stack description before shipping

When `stackit merge ship` runs on a stack with more than one PR that has no
description, it prompts you to add a title (see
[Stack Description Prompt](shipping.md#stack-description-prompt)). A
`pre-merge-ship` hook can fill that description in automatically before the
consolidation PR is created. Because the hook runs *before* the ship checks for
a description, setting one in the hook means the interactive prompt is skipped.

**Simple: derive a title from the existing PRs**

```yaml
hooks:
  pre-merge-ship:
    - "scripts/auto-describe.sh"
```

```sh
#!/bin/sh
# scripts/auto-describe.sh
# Generates a stack description using the GitHub CLI, then applies it with
# 'stackit describe'.
set -e

if [ -z "$STACKIT_BRANCH" ]; then
  exit 0
fi

# Example: generate a title from the PR titles in the stack
title=$(gh pr list --head "$STACKIT_BRANCH" --json title --jq '.[0].title' 2>/dev/null || true)
if [ -n "$title" ]; then
  stackit describe -m "$title"
fi
```

**Smarter: let Claude Code describe the stack**

Stackit ships a `/stack-describe` command for Claude Code (installed via
`stackit agent install`) that analyzes every branch's commits and diffs,
generates a title and a structured description, and applies it with
`stackit describe`. A `pre-merge-ship` hook can run it headlessly so the
consolidation PR gets a high-quality, AI-written summary with no manual step.

```yaml
hooks:
  pre-merge-ship:
    - "scripts/claude-describe.sh"
```

```sh
#!/bin/sh
# scripts/claude-describe.sh
# Asks Claude Code to generate and apply a stack description via the
# /stack-describe command. Requires the `claude` CLI to be installed and
# authenticated, and the stackit agent commands installed
# (`stackit agent install`).
set -e

# Only describe stacks that don't already have one (avoids spending tokens on
# stacks you've already described by hand). `describe --show` prints a "no
# description set" line when the stack is undescribed.
if ! stackit describe --show --no-interactive 2>/dev/null | grep -qi "no description set"; then
  exit 0
fi

# Skip gracefully if the CLI isn't available (e.g. in CI).
command -v claude >/dev/null 2>&1 || exit 0

# Run the slash command non-interactively. The hook is an automated context, so
# pre-approve the tools /stack-describe needs instead of prompting.
claude -p "/stack-describe" \
  --allowedTools "Bash(stackit:*)" "Bash(git:*)" "Read" "Glob" "Grep"
```

> Tip: the hook runs from the repo root with `STACKIT_BRANCH` and the other
> `STACKIT_*` variables set, so the same script also works as a `pre-submit`
> hook if you prefer to describe stacks when first opening PRs.

### Send a notification after a successful merge

```yaml
hooks:
  post-submit:
    - "scripts/notify-slack.sh"
```

`scripts/notify-slack.sh` reads `$STACKIT_BRANCH` and `$STACKIT_PR_NUMBER`
from the environment.

## Implementation

| Concern | Location |
|---|---|
| Shell executor (`sh -c` + timeout) | `internal/hooks/runner.go` |
| Approval gate + per-hook orchestration | `internal/actions/hooks/resolve.go` |
| Cobra lifecycle middleware | `internal/cli/common/hookmiddleware.go` |
| YAML schema (`hooks.For(phase)`) | `internal/config/project_config.go` |
| Per-phase approval keys | `internal/config/config_git.go` |
