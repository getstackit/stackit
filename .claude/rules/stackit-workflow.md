# Stackit Workflow Rules

This project uses stackit for stacked changes. **NEVER use raw git commands for branch/commit operations.**

## Forbidden → Required

| Never Use | Use Instead |
|-----------|-------------|
| `git commit -m "..."` | `stackit create -m "..."` |
| `git checkout -b` | `stackit create -m "..."` |
| `gh pr create` | `stackit submit` |
| `git rebase` | `stackit restack --upstack` (or `--all-stacks`) |

**Exception:** `git commit` is allowed when adding commits to an existing stacked branch.

## Required Workflow

```bash
# 1. Make changes
# 2. Stage changes FIRST (required!)
git add -A
# 3. Create stacked branch with commit
stackit create -m "feat: description"
# 4. Submit when ready
stackit submit
```

**Critical:** `stackit create` requires staged changes. Without staged changes, it creates an empty branch.

## Skills (Preferred)

Use skills instead of manual commands:

| Skill | Purpose |
|-------|---------|
| `/stack-create` | Create stacked branch (handles workflow correctly) |
| `/stack-submit` | Submit PRs for the stack |
| `/stack-status` | Check stack health |
| `/stack-fix` | Diagnose and fix issues |
| `/stack-sync` | Sync with trunk, cleanup merged branches |
| `/stack-restack` | Rebase branches (scoped, multi-stack, or parallel) |
| `/stack-tidy` | Clean up fixup/WIP commits across the stack |

Run `/stackit` for the full guide.

## Keeping Permission Rules Stable

Claude's permission system matches Bash commands by prefix. Every literal piece of a command becomes part of the match string, so commands with a consistent shape across runs let a small number of rules cover everything. Inconsistent shapes blow up `.claude/settings.local.json` (one entry per literal commit message, redirection variant, etc.) and cause redundant approval prompts.

**Drop the per-command `--no-interactive` flag where you can.** Set `STACKIT_NO_INTERACTIVE=1` in the environment once (stackit also auto-disables interactive features when there's no TTY); commands then keep a shorter, more stable shape — `stackit create -F -` instead of `stackit create -F - --no-interactive` on every call — so fewer permission rules cover them. An explicit `--interactive` still overrides the env var when you need it.

**Run one command per Bash call.** Don't chain with `&&`. Each command should match its own rule (`Bash(git add:*)`, `Bash(stackit:*)`) rather than requiring a permission entry for the full compound string — which is what generates entries like `Bash(git add -A && stackit create -m "feat: very specific message" 2>&1)`.

```bash
# Avoid — compound command needs its own rule
git add -A && stackit create -m "feat: foo"

# Prefer — separate Bash calls, each matches its own rule
git add -A
stackit create -m "feat: foo"
```

**Don't append `2>&1`.** Claude's Bash tool already captures stderr. Appending the redirection extends the literal command string, breaks prefix matching, and creates one-off approval entries.

**Prefer file paths or stdin for variable content.** When a command embeds long or unique text (commit messages, PR descriptions), reading from a file or stdin keeps the command line stable across runs so the same permission rule covers every message.

```bash
# Avoid — every new message text widens the permission surface
stackit create -m "feat: very specific message"

# Prefer — pipe via stdin; command shape stays constant across messages
echo "feat: anything" | stackit create -F -

# Or use a file path (per-invocation `mktemp` avoids collisions between
# concurrent agents; a fixed path like /tmp/.stackit-msg is unsafe to share)
msg=$(mktemp -t stackit-msg) && printf "feat: anything" > "$msg" && stackit create --message-file "$msg"
```

`create`, `modify`, `squash`, and `split --by-file` all accept `--message-file` with the `-F` shorthand (on `split`, `-F` reads the message; pass `--by-file-interactive` by long name). The flag errors loudly on empty input, missing files, or being passed alongside `-m`, so silent fallthrough to other input paths can't mask a typo.

## Common Pitfalls

| Mistake | Fix |
|---------|-----|
| Forgetting to stage before `create` | Always `git add -A` before `stackit create` |
| Empty branch created | You forgot to stage; delete branch and retry with staged changes |
| Using `git commit` for new branch | Use `stackit create` - it creates branch + commit together |
| Using `git checkout -b` | Use `stackit create` - branch name auto-generated from message |
| Manual rebase broke stack | Use `stackit restack --upstack` to safely rebase children (or `--all-stacks` for all) |
| Using `gh pr create` | Use `stackit submit` - it handles stacked PR dependencies |
| Amending wrong commit | Use `stackit absorb` to auto-route changes to correct commits |
| Stack out of sync after merge | Run `stackit sync` to cleanup merged branches and update trunk |
| Permission prompts every commit | Don't chain with `&&`; run `git add` and `stackit create` as separate commands |
| `settings.local.json` ballooning with literal commit messages | Same root cause — split chained commands so `Bash(stackit:*)` covers the call |
