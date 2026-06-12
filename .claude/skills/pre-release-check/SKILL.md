---
name: pre-release-check
description: Decide whether it's safe to cut a new release. Auto-detects the last release tag, gathers everything merged since, runs a build/test health gate, and fans out parallel review agents to hunt for release blockers — regressions, half-finished work, risky edge cases, breaking changes, missing tests, and release-readiness gaps. Produces a GO / NO-GO verdict with categorized blockers. Keywords pre-release, release blockers, go/no-go, release readiness, ship check, regression review.
allowed-tools: Bash(bash:*), Bash(git:*), Bash(gh:*), Bash(mise:*), Read, Grep, Glob, Task
---

# /pre-release-check

Answer one question: **are there any blockers to cutting a new release?**

Auto-detect the last release, review everything that has landed since it for bugs / edge cases / risky changes, verify the build and tests are healthy, and return a clear **GO / NO-GO** verdict. This is a *gate*, not a cleanup pass — the goal is to catch things that should be fixed *before* a release, not to enumerate every possible nice-to-have.

This skill only **reads and reviews**. It never tags, pushes, or edits releases — handing off to `/create-release` is the natural next step once it returns GO.

## Usage

```
/pre-release-check [baseline] [--head=<ref>] [--no-tests]
```

- `baseline` (optional) — pin the comparison to an explicit ref (tag, branch, SHA). Default: the latest GitHub release tag (falls back to the latest git tag).
- `--head=<ref>` (optional) — the tip to review. Default: `HEAD`.
- `--no-tests` (optional) — skip the build/test health gate (faster, review-only). Default: run it.

Examples:

```
/pre-release-check                 # everything since the last release, with health gate
/pre-release-check v0.18.0         # review against an older baseline
/pre-release-check --no-tests      # skip mise run check, review only
```

## Scripts

Deterministic steps live in `.claude/skills/pre-release-check/scripts/`. Call them instead of inlining git/gh — they encode the right fallbacks and keep invocations identical across runs (small permission allowlist).

| Script | Purpose |
|--------|---------|
| `last-release.sh [ref]` | Resolve the baseline: the latest release tag, or echo back a verified explicit ref |
| `release-context.sh <baseline> [head]` | Print the situational-awareness bundle (diffstat, changed files, commit subjects, merged PRs) — summaries only, not the full diff |

Both use `set -euo pipefail` and fail loudly. Treat their exit codes as authoritative.

## Execution Steps

### Step 1: Resolve the baseline

```bash
BASELINE=$(bash .claude/skills/pre-release-check/scripts/last-release.sh "$ARG")
```

Pass the user's `baseline` argument if they gave one, otherwise call with no argument to auto-detect. Surface the resolved baseline to the user up front ("Reviewing everything since **v0.19.0**…") so the scope is explicit.

### Step 2: Start the health gate (background)

Unless `--no-tests` was passed, kick off the build + fast test suite **in the background** so it runs while the review agents work. Use the lightest command that still covers a release: `mise run check` (fmt + lint + fast tests, ~30s). For engine/integration-heavy releases, prefer `mise run test`.

Start it with `run_in_background: true` and remember the task — you'll collect its result in Step 5. A non-zero exit here is an automatic 🔴 **BLOCKER**: do not hand-wave a failing build or test as "probably flaky" without evidence.

> If the working tree is dirty or you're mid-rebase, note it — test results may not reflect what would actually ship.

### Step 3: Gather context

```bash
bash .claude/skills/pre-release-check/scripts/release-context.sh "$BASELINE" > /tmp/stackit-prerelease-context.md
```

Read the file. If it reports "No commits since baseline", stop — there's nothing to release. Otherwise use the diffstat, file list, and commit/PR log to understand the shape and size of the release.

The full diff across a release can be tens of thousands of lines — **do not** paste it into context. The review agents pull targeted diffs themselves from the range.

### Step 4: Fan out review agents (parallel)

Launch the following agents **in parallel, in a single message** using the Task tool — one Task call per dimension. Give each agent: the baseline, the head, the changed-file list, and the commit/PR log from Step 3. Each agent runs its own `git diff <baseline>..<head> -- <path>`, `git log`, and `Read`/`Grep` to investigate, then returns JSON.

Scale the roster to the release: for a tiny release (a handful of commits) you may collapse to the first three dimensions; for a large one (dozens of PRs), run all six and consider splitting a dimension across two agents by subsystem.

Shared instructions to embed in every agent prompt:

```
You are a release-gate reviewer for the stackit Go CLI (a tool for managing
stacked Git changes). You are reviewing everything that landed between
{BASELINE} and {HEAD} to decide if it is SAFE TO RELEASE.

Range:          {BASELINE}..{HEAD}
Changed files:  {CHANGED_FILES}
Commit log:     {COMMIT_LOG}

Investigate using git directly — pull diffs for the files relevant to your
dimension with `git diff {BASELINE}..{HEAD} -- <path>`, read surrounding code,
and grep for related call sites. Review the CHANGE, not pre-existing code,
unless a change exposes a latent issue.

A finding is a BLOCKER only if a reasonable maintainer would hold the release
for it: a regression, data-loss/corruption risk, a broken user-facing path, a
breaking change shipped by accident, or a build/test failure. Everything else
is a warning or non-blocking note. Be conservative — false blockers erode trust
in this gate. Prefer linking to evidence (file:line, the offending hunk) over
assertion.

Project-specific risk areas to weigh heavily:
- Stack metadata lives in `refs/stackit/metadata/` — changes to ref handling,
  metadata read/write, or on-disk formats can silently corrupt users' stacks.
- Destructive git operations (rebase, reset, branch delete, force-push) must
  preserve the safety invariants in `.claude/rules/safety-invariants.md`
  (no detached HEAD on failure, worktrees stay detached, GitHub writes only
  during sync).
- CLI flags/commands, config keys, and `internal/contracts/http` shapes are a
  public surface — renames/removals are breaking changes.

OUTPUT FORMAT (JSON):
{
  "agent": "<dimension>",
  "findings": [
    {
      "blocker": true,
      "severity": "HIGH|MEDIUM|LOW",
      "title": "Brief description",
      "file": "path/to/file.go",
      "line": 142,
      "evidence": "What you saw — the hunk, call site, or test that proves it",
      "why_it_blocks": "Concrete user/release impact if shipped as-is",
      "suggestion": "What to do before releasing",
      "effort": "LOW|MEDIUM|HIGH"
    }
  ]
}

Only report real issues found in the change. Empty findings is a valid,
expected answer — do not invent problems to look thorough.
```

Per-dimension focus (append to the shared prompt):

1. **Regressions & correctness** (`regressions`) — logic errors, nil/empty-slice derefs, dropped/ignored errors, incorrect error wrapping, broken conditionals, and races introduced by the changes. Did any change alter behavior of an existing command in a way that breaks a previously-working flow?

2. **Unfinished & in-flight work** (`unfinished`) — `TODO`/`FIXME`/`XXX`, `panic("not implemented")` or stubs on reachable paths, half-wired feature flags, dead or unreachable branches, commented-out code, and leftover debug logging / `fmt.Println` / verbose traces that shouldn't ship.

3. **Edge cases & data safety** (`data-safety`) — destructive or irreversible operations, metadata/ref read-modify-write correctness, on-disk/format compatibility for existing stacks, empty/zero/large inputs, and concurrent invocation. Treat anything touching `refs/stackit/metadata/` or worktrees as high-scrutiny.

4. **Breaking changes & compatibility** (`compat`) — removed/renamed CLI commands or flags, changed defaults, config-key changes, output-format changes that would break user scripts, and `internal/contracts/http` / OpenAPI / web-contract drift. Flag anything that breaks existing users without a migration path or a deliberate major-version note.

5. **Test & CI health** (`tests`) — new code on critical paths without tests, skipped/`t.Skip`'d or commented-out tests, tests weakened or deleted alongside the code they covered, and missing coverage for new error paths. Note (don't duplicate as a blocker) anything the Step 2 health gate will already catch.

6. **Release readiness** (`readiness`) — version references that need bumping, docs out of sync with new flags/commands/config (`README.md`, `docs/*`), changelog-worthy items, and any user-facing feature shipped without help text or examples.

### Step 5: Collect the health gate

Retrieve the background `mise run check` (or `mise run test`) result from Step 2. If it failed, capture the failing package/lint/test output verbatim — it becomes a top 🔴 BLOCKER. If it passed, note that the suite is green. If it's still running when reviews finish, wait for it; don't issue a verdict without it (unless `--no-tests`).

### Step 6: Synthesize the verdict

Merge findings across agents, de-duplicate (same file+line from two agents → one entry, keep the sharper write-up), and classify:

- 🔴 **BLOCKERS** — `blocker: true`, plus any health-gate failure. Must fix before releasing.
- 🟡 **WARNINGS** — real issues that should be fixed but don't strictly hold the release.
- 🟢 **NON-BLOCKING** — minor polish, can ship and follow up.

**Verdict:**
- **NO-GO** if there is ≥1 blocker.
- **GO (with warnings)** if there are no blockers but warnings exist.
- **GO** if clean.

Be decisive. The user wants a recommendation, not a survey.

### Step 7: Report

```
═══════════════════════════════════════════════════════════════════
 PRE-RELEASE CHECK — {BASELINE}..{HEAD}
 {N} commits · {M} files changed · health gate: {PASS|FAIL|SKIPPED}
═══════════════════════════════════════════════════════════════════

 VERDICT:  🔴 NO-GO   (or  🟢 GO   /  🟡 GO WITH WARNINGS)

 {One-sentence rationale.}

─ 🔴 BLOCKERS ({count}) ───────────────────────────────────────────
 1. {title}
    {file}:{line}  ·  effort: {effort}
    {why_it_blocks}
    → {suggestion}

─ 🟡 WARNINGS ({count}) ───────────────────────────────────────────
 • {title} — {file}:{line} ({dimension})

─ 🟢 NON-BLOCKING ({count}) ───────────────────────────────────────
 • {title} ({dimension})

─ Coverage ────────────────────────────────────────────────────────
 Reviewed {N} commits across {M} files via {agent_count} agents.
 Dimensions: regressions, unfinished, data-safety, compat, tests, readiness
 Health gate: {command} → {result}
═══════════════════════════════════════════════════════════════════
```

Close with the recommended next step:
- **NO-GO** → list the blockers to fix; offer to dig into any one.
- **GO** → "No blockers found. Run `/create-release` to cut the release." (Confirm the bump kind there, not here.)

## Permission Stability

Per `.claude/rules/stackit-workflow.md`:

- Run one command per Bash call — don't chain with `&&`.
- Write the context bundle to a file (`> /tmp/stackit-prerelease-context.md`), don't pipe huge output through inline commands repeatedly.
- Don't append `2>&1` — the Bash tool already captures stderr.

Calling the two scripts keeps the command shape `bash .claude/skills/pre-release-check/scripts/<name>.sh ...` constant across runs, so a single permission rule covers them.

## Common Pitfalls

| Mistake | Fix |
|---------|-----|
| Hardcoding the previous version | `last-release.sh` auto-detects it; only pass a baseline to override |
| Dumping the whole release diff into context | Use `release-context.sh` summaries; agents pull targeted diffs themselves |
| Running review agents sequentially | Launch all dimensions in one message (parallel Task calls) |
| Calling a flaky-looking test failure "not a blocker" | A red health gate is a 🔴 BLOCKER until proven otherwise with evidence |
| Inflating warnings into blockers | Blocker = a reasonable maintainer holds the release; be conservative |
| Issuing a verdict before tests finish | Wait for the Step 2 background gate (unless `--no-tests`) |
| Tagging or editing the release here | This skill is read-only; hand off to `/create-release` |

## Notes

- This is the read-only counterpart to `/create-release`. A clean GO is the precondition for running it.
- `/improve` reviews a single in-progress stack for general quality; `/pre-release-check` reviews the *cumulative* set of merged changes for *ship-readiness*. Different scope, different bar (blocker vs. suggestion).
- Pre-1.0, breaking changes can ride a minor bump — flag them so the human decides, don't auto-classify them as non-blocking.
