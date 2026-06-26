# Shipping (Merge) System

## Quick Reference

**Commands:**
```bash
stackit merge              # Launch interactive merge wizard (requires TTY)
stackit merge status       # Show your mergeable work
stackit merge status --all # Show entire team's mergeable work
stackit merge next         # Merge bottom PR (fire-and-forget by default)
stackit merge drain        # Merge all PRs bottom-up, waiting for each
stackit merge ship         # Consolidate stack into single atomic PR (waits by default)
```

**Key files:**
- `internal/actions/merge/` - Core merge logic
- `internal/cli/stack/merge/` - CLI commands
- `internal/shippable/` - Shippability analysis and multi-stack consolidation
- `internal/cli/dashboard/shippable.go` - Ship dashboard TUI

**Key differentiators:**
- Octopus merge consolidation (atomic stack merging, not squashing)
- Multi-stack shipping (merge multiple independent stacks atomically)
- Fire-and-forget merging (enable automerge and return immediately)
- Worktree-based conflict detection (test merges without affecting main repo)
- Binary search for working subset (find largest mergeable set when CI fails)

---

## Overview

Shipping stacked changes is fundamentally different from shipping linear PRs. Stackit provides multiple merge strategies optimized for different scenarios, plus advanced multi-stack consolidation for high-velocity teams.

## GitHub Merge Methods

GitHub supports three PR merge methods through the merge button. Stackit must
support all three because repositories can choose any combination of them, and a
stack may be merged by a teammate or automation outside of Stackit.

| Method | What GitHub writes to the base branch | Original PR commits reachable from base? | Detection implications |
|--------|----------------------------------------|------------------------------------------|------------------------|
| Merge commit | A merge commit whose parents include the old base and the PR head | Yes | Ancestry checks work: the PR branch tip is reachable from base. |
| Squash merge | One new commit containing the PR's combined final diff | No | Ancestry fails. Multi-commit PRs may also fail per-commit patch matching because one combined commit replaced many commits. |
| Rebase merge | New commits replaying the PR commits onto the current base | No | Ancestry fails because SHAs are rewritten. Per-commit patch matching usually works, but conflict resolution or rewritten patches can change the result. |

For stacked changes, these differences affect sync, restack, cleanup, and stack
navigation:

- A branch can be merged even when its tip is not an ancestor of trunk.
- A merged parent must be skipped when restacking descendants, or the parent's
  already-landed commits can be replayed into children.
- A merged sibling must never be treated as a new base for an unrelated branch.
- PR state and landed-commit metadata are safety signals, not just display
  fields.

The configured merge method (`stackit.merge.method`, or `--method` on merge
commands) controls how Stackit asks GitHub to merge PRs. It does not limit what
sync/restack must understand: those commands must recognize all three methods
when reading repository history and PR state.

## Merge Status View

Use `stackit merge status` to see an overview of your mergeable work:

```
📦 Your Mergeable Work

Ready (2):
  ✅ add-oauth              3 branches  All approved, CI passing
  ✅ fix-login              1 branch    All approved, CI passing

Pending (1):
  ⏳ new-dashboard          3 branches  CI running

Blocked (1):
  ❌ perf-improvements      2 branches  CI failed
```

This helps you understand what's ready to ship before diving into the merge wizard.

**Filtering:**
- By default, shows only your stacks (matched by PR author)
- Use `--all` to see the entire team's work

## Merge Strategies

### Bottom-Up (Individual)

Merges PRs one at a time from the bottom of the stack upward.

```bash
stackit merge next
```

**How it works:**
1. Finds the bottom-most mergeable branch in the stack
2. Enables GitHub automerge (fire-and-forget)
3. Returns immediately - run again to merge next PR
4. After merge completes, remaining branches are automatically restacked

**Best for:**
- Stacks with fewer than 3 branches
- Incremental shipping as PRs get approved
- When you want granular merge commits

### Drain (Bottom-Up All)

Merges all PRs in the stack bottom-up, waiting for each to complete before proceeding to the next.

```bash
stackit merge drain
```

**How it works:**
1. Creates a plan of all unmerged PRs in the stack
2. For each PR (bottom to top):
   - Enables automerge on the bottom-most unmerged PR
   - Waits for the PR to merge
   - Syncs trunk and restacks remaining branches
3. Repeats until all PRs are merged

Equivalent to running `merge next --wait` in a loop.

**Best for:**
- Fully draining a stack in one command
- CI/CD pipelines that need sequential merging
- Stacks where each PR should land independently

### Ship (Consolidation)

Consolidates the entire stack into a single PR for atomic merging.

```bash
stackit merge ship
```

**How it works:**
1. Creates a "consolidation branch" from trunk
2. Performs an **octopus merge** - a single merge commit with multiple parents
3. Creates a PR for the consolidation branch
4. Waits for CI to pass and the PR to merge (default behavior)
5. Individual PRs are closed with references to the consolidation PR

**Best for:**
- Stacks with 3+ branches
- All-or-nothing deployment semantics
- Features that must ship together

#### Stack Description Prompt

When shipping a stack with **more than one PR** that has no stack description
set, `merge ship` prompts you to add a title before the consolidation PR is
created. The consolidation PR uses the stack title/description for its own
title and body, so setting one keeps the merged PR readable.

- **Interactive:** you are asked `Add a title for this stack now?`. Entering a
  title stores it via `stackit describe` semantics; declining or leaving it
  blank proceeds without a description.
- **Non-interactive or `--yes`:** a warning is printed
  (`Run 'stackit describe' to add one.`) and the ship continues unblocked, so
  CI flows are unaffected.

The prompt is best-effort — cancelling it never aborts the ship.

To skip the prompt entirely, either set a description ahead of time with
`stackit describe`, or automate it with a `pre-merge-ship` hook. Because the
hook runs before the ship checks for a description, a hook that sets one causes
the prompt to be skipped automatically. See
[Auto-generate a stack description before shipping](hooks.md#auto-generate-a-stack-description-before-shipping)
in the hooks guide — including an example that uses Claude Code's
`/stack-describe` command.

### Octopus Merge

The consolidation strategy uses Git's octopus merge feature. Unlike squashing, octopus merge:

- Preserves full history of all branches
- Creates a single merge commit with N parents (one per branch)
- Maintains authorship and commit metadata
- Shows complete branch structure in git log

```
Before:                    After consolidation:

main ─── a ─── b ─── c     main ─────────────────○ (octopus merge)
                                  ↖     ↑     ↗
                                   a    b    c
```

## Multi-Stack Consolidation

Stackit's most powerful shipping feature: merge multiple independent stacks atomically.

```bash
stackit merge ship --stacks stack1,stack2,stack3
```

**How it works:**
1. Identifies all independent stacks rooted at trunk
2. Creates a temporary worktree to test merge feasibility
3. Attempts global octopus merge of all selected stacks
4. If successful, creates single consolidation PR
5. If merge fails, identifies conflicting stacks
6. Runs local CI to validate combined code

### Conflict Handling

When multiple stacks can't be merged together:

1. **Per-stack fallback**: Attempts to merge stacks individually to identify which conflict
2. **Binary search**: Finds the largest subset of stacks that can merge successfully
3. **Partial consolidation**: Creates PR for working subset, reports excluded stacks

### Worktree-Based Testing

All multi-stack operations happen in temporary worktrees:
- Main repository state is never modified during testing
- Merge feasibility is validated before any GitHub operations
- Local CI can run on combined code before creating PRs

## Wait Behavior

Different commands have different default wait behavior:

| Command | Default | Behavior |
|---------|---------|----------|
| `merge next` | `--wait=false` | Fire-and-forget: enables automerge and returns immediately |
| `merge drain` | Always waits | Waits for each PR to merge before proceeding to next |
| `merge ship` | `--wait=true` | Waits for consolidation PR to merge, then cleans up |

### Fire-and-Forget (`merge next`)

By default, `merge next` returns immediately after enabling GitHub automerge:

```bash
stackit merge next  # Returns in <1 second
```

This enables a workflow where you run `merge next` repeatedly as PRs become ready, without waiting for CI.

Use `--wait` to block until the merge completes:

```bash
stackit merge next --wait
```

### Wait Mode (`merge ship`)

By default, `merge ship` waits for the consolidation PR to merge:

```bash
stackit merge ship  # Waits for CI + merge, then cleans up
```

Use `--wait=false` to return immediately after creating the PR:

```bash
stackit merge ship --wait=false  # Fire-and-forget
```

When waiting, the command polls GitHub until:
- CI checks pass
- PR is merged
- Timeout is reached (default: 10 minutes)

## Shippability Analysis

Stackit analyzes each stack's readiness to ship:

| Status | Meaning |
|--------|---------|
| `Shippable` | Ready to merge (approved, CI passing) |
| `Pending` | Waiting on CI or review |
| `Blocked` | CI failed or changes requested |
| `Incomplete` | Missing PRs or has drafts |

Access via the ship dashboard or programmatically through the shippable package.

## Ship Dashboard

Interactive TUI for managing multiple stacks:

```bash
stackit dashboard ship
```

Features:
- View all stacks and their shippability status
- Select multiple stacks for batch shipping
- Pre-merge combination analysis
- Visual navigation and selection

## PR Cleanup

After consolidation, Stackit automatically:
- Closes individual PRs
- Adds footer linking to consolidation PR
- Records who performed the consolidation
- Updates branch metadata

Footer example:
```markdown
---
This PR was shipped as part of [#123](link) by @username
```

## Configuration

| Key | Default | Description |
|-----|---------|-------------|
| `stackit.merge.method` | (prompted) | GitHub merge method: `squash`, `merge`, or `rebase` |
| `stackit.ci.command` | `""` | Local CI command to run before consolidation |
| `stackit.ci.timeout` | `600` | CI command timeout in seconds |

## Command Reference

### `stackit merge`

Launches an interactive merge wizard. Requires a TTY (use `merge next` or `merge ship` for non-interactive mode).

| Flag | Description |
|------|-------------|
| `--dry-run` | Show merge plan without executing |
| `--force` | Skip validation checks (draft PRs, failing CI) |
| `--wait` | Wait for merge to complete (default: fire-and-forget) |
| `--scope` | Pre-select scope to merge (skips scope prompt) |
| `--branch` | Pre-select target branch to merge from (skips branch prompt) |

**Mutual exclusions:** `--scope`/`--branch`

### `stackit merge status`

Show shippability status of your stacks.

| Flag | Description |
|------|-------------|
| `--all` | Show all team members' stacks (default: your stacks only) |

### `stackit merge next`

Merge the bottom-most ready PR in the stack.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show merge plan without executing |
| `--yes`, `-y` | Skip confirmation prompt |
| `--force` | Skip validation checks (draft PRs, failing CI) |
| `--wait` | Wait for merge to complete (default: false) |
| `--method` | Merge method: `squash`, `merge`, or `rebase` (uses config if not specified) |
| `--branch` | Target branch to merge from (default: current branch) |
| `--scope` | Merge the next PR within the specified scope |

### `stackit merge drain`

Merge all PRs in the stack bottom-up, waiting for each to complete.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show merge plan without executing |
| `--yes`, `-y` | Skip confirmation prompt |
| `--force` | Skip validation checks (draft PRs, failing CI) |
| `--method` | Merge method: `squash`, `merge`, or `rebase` (uses config if not specified) |
| `--branch` | Target branch to merge from (default: current branch) |
| `--scope` | Merge PRs within the specified scope |
| `--count` | Maximum number of PRs to drain (0 = all) |

### `stackit merge ship`

Consolidate stack(s) into a single PR.

| Flag | Description |
|------|-------------|
| `--dry-run` | Show merge plan without executing |
| `--yes`, `-y` | Skip confirmation prompt |
| `--force` | Skip validation checks (draft PRs, failing CI) |
| `--wait` | Wait for merge to complete (default: true) |
| `--method` | Merge method: `squash`, `merge`, or `rebase` (uses config if not specified) |
| `--scope` | Consolidate all branches within the specified scope |
| `--branch` | Target branch to merge from (default: current branch) |
| `--stacks` | Combine multiple stacks (comma-separated stack roots) |
| `--skip-local-ci` | Skip local CI validation for multi-stack merge |

**Mutual exclusions:** `--scope`/`--branch`, `--stacks`/`--scope`, `--stacks`/`--branch`, `--stacks`/`--force`

## Implementation

### Core Files

```
internal/actions/merge/
├── merge.go               # Main action entry point (Action function)
├── plan.go                # Merge planning, strategy types, plan building
├── consolidate.go         # Single-stack consolidation (octopus merge)
├── execute.go             # Merge execution orchestration
├── execute_steps.go       # Individual step execution (CI wait, PR base update, consolidation)
├── handler.go             # Event handler interfaces for progress reporting
├── helpers.go             # Merge method resolution, CI estimation, error classification
├── interactive.go         # Interactive prompt interfaces for TUI
├── wizard.go              # Interactive wizard logic
├── cleanup.go             # Post-merge PR cleanup
├── ci_waiter.go           # CI polling logic
├── pr_generator.go        # PR title/body generation for consolidation PRs
├── worktree.go            # Worktree-based merge operations
├── multistack.go          # Multi-stack consolidation orchestration
├── multistack_worktree.go # Worktree-based merge testing for multi-stack
├── multistack_ci.go       # Local CI for multi-stack validation
├── multistack_discover.go # Stack discovery for multi-stack shipping
├── multistack_pr.go       # PR creation for multi-stack consolidation
└── multistack_types.go    # Types for multi-stack operations
```

### Key Types

```go
// Merge strategies (internal/actions/merge/plan.go)
type Strategy string
const (
    StrategyBottomUp Strategy = "bottom-up"
    StrategyShip     Strategy = "ship"
)

// Merge plan - created before execution
type Plan struct {
    Strategy        Strategy
    CurrentBranch   string
    BranchesToMerge []BranchMergeInfo
    UpstackBranches []string
    Steps           []PlanStep
    Warnings        []string
    Infos           []string
}

// Shippability status (internal/shippable/types.go)
type Status string
const (
    StatusShippable  Status = "shippable"
    StatusPending    Status = "pending"
    StatusBlocked    Status = "blocked"
    StatusIncomplete Status = "incomplete"
)
```

### Flow: Bottom-Up Merge

```
merge next
  → CreateMergePlan(StrategyBottomUp)
  → Select bottom-most PR from plan
  → orchestrateMerge() (direct merge → automerge → poll fallback)
  → Return immediately (or wait if --wait)

[GitHub merges PR when CI passes]

merge next (again)
  → CreateMergePlan(StrategyBottomUp)
  → Find next unmerged PR
  → orchestrateMerge()
  → ...
```

### Flow: Consolidation

```
merge ship
  → CreateMergePlan(StrategyShip)
  → Action()
    → Execute()
      → ConsolidateMergeExecutor.Execute()
        → preValidateStack()
        → createMergeBranch() (octopus merge)
        → createConsolidationPR()
        → waitForConsolidationMerge() (if --wait)
        → lockAndNotifyIndividualPRs()
```

### Flow: Multi-Stack Consolidation

```
merge ship --stacks a,b,c
  → DiscoverStacks()
  → ExecuteMultiStack()
    → CreateWorktreeSession()
    → Test global merge feasibility
    → If fails: binary search for working subset
    → Run local CI (unless --skip-local-ci)
    → Create consolidation PR
    → lockAndNotifyMultiStackPRs()
```

## Why This Matters

Traditional stacked PR workflows suffer from:

1. **Merge cascades**: Merging bottom PR requires rebasing all children
2. **CI thrashing**: Each rebase triggers new CI runs
3. **Coordination overhead**: Multiple PRs must be merged in sequence

Stackit's consolidation approach:

1. **Atomic shipping**: All changes land in one merge commit
2. **Single CI run**: Consolidation branch runs CI once
3. **No rebasing**: Individual branches don't need updates
4. **Multi-stack batching**: Ship hours of work in one operation

## Best Practices

1. **Use consolidation for stacks of 3+**: Reduces merge overhead significantly
2. **Run local CI before consolidation**: Catches issues before creating PRs
3. **Use `merge next` for incremental shipping**: Fire-and-forget as PRs get approved
4. **Use `merge drain` for full stack teardown**: Merges everything in one command
5. **Leverage multi-stack consolidation**: Ship related work together
6. **Configure merge method once**: Set `stackit.merge.method` to avoid prompts

## Troubleshooting

### CI Timeout

If CI takes longer than the default timeout (10 minutes), the merge command will fail with a timeout error. Workarounds:
- Use `merge ship --wait=false` (fire-and-forget) and run `stackit sync --restack` after the PR merges
- Use `merge next` without `--wait` for fire-and-forget behavior

### Automerge Not Enabled

If your repository doesn't have GitHub automerge enabled, you'll see an error like "auto-merge is not enabled for this repository". Solutions:
- Enable automerge in your repository settings (Settings → General → Allow auto-merge)
- Use `--wait` flag which falls back to polling + direct merge when automerge isn't available

### Merge Conflicts During Consolidation

If the octopus merge fails due to conflicts between branches:
- Resolve conflicts in the individual branches first
- Push the fixed branches
- Re-run the merge command

### PR Already Merged Externally

If a PR is merged outside of stackit while a merge operation is in progress:
- The merge command will detect this and skip the already-merged PR
- Run `stackit sync --restack` to clean up and update your stack

### Fire-and-Forget Cleanup

After using `merge ship --wait=false`, clean up by running:
```bash
stackit sync --restack
```

This will pull merged changes, delete merged branches, and restack remaining branches.
