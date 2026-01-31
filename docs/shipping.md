# Shipping (Merge) System

## Quick Reference

**Commands:**
```bash
stackit merge              # Show your mergeable work, then wizard
stackit merge --all        # Show entire team's mergeable work
stackit merge next         # Merge bottom PR, enable automerge, return immediately
stackit merge squash       # Consolidate stack into single atomic PR
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

Shipping stacked changes is fundamentally different from shipping linear PRs. Stackit provides two merge strategies optimized for different scenarios, plus advanced multi-stack consolidation for high-velocity teams.

## Merge Status View

When you run `stackit merge`, you first see a status overview of your mergeable work:

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

### Squash (Consolidation)

Consolidates the entire stack into a single PR for atomic merging.

```bash
stackit merge squash
```

**How it works:**
1. Creates a "consolidation branch" from trunk
2. Performs an **octopus merge** - a single merge commit with multiple parents
3. Creates a PR for the consolidation branch
4. When merged, all changes land atomically
5. Individual PRs are closed with references to the consolidation PR

**Best for:**
- Stacks with 3+ branches
- All-or-nothing deployment semantics
- Features that must ship together

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
stackit merge squash --stacks stack1,stack2,stack3
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

## Fire-and-Forget Merging

By default, merge commands return immediately after enabling GitHub automerge:

```bash
stackit merge next  # Returns in <1 second
```

This enables a workflow where you run `merge next` repeatedly as PRs become ready, without waiting for CI.

### Blocking Mode

For automation or when you need to wait:

```bash
stackit merge next --wait
```

This polls GitHub until:
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
stackit dashboard ship  # or: stackit ship
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

Shows your mergeable work status, then launches an interactive wizard.

| Flag | Description |
|------|-------------|
| `--all` | Show all team members' stacks (default: your stacks only) |
| `--dry-run` | Show merge plan without executing |
| `--force` | Skip validation checks |
| `--wait` | Wait for merge to complete |

### `stackit merge next`

Merge the bottom-most ready PR in the stack.

| Flag | Description |
|------|-------------|
| `--wait` | Block until merge completes |
| `--timeout` | Timeout for --wait (default: 10m) |

### `stackit merge squash`

Consolidate stack(s) into a single PR.

| Flag | Description |
|------|-------------|
| `--scope <name>` | Merge all branches in a scope |
| `--stacks <roots>` | Combine multiple stacks (comma-separated root branches) |
| `--skip-local-ci` | Skip local CI validation |
| `--wait` | Block until consolidation PR is merged |

## Implementation

### Core Files

```
internal/actions/merge/
├── merge.go              # Main action entry point
├── consolidate.go        # Single-stack consolidation (octopus merge)
├── multistack.go         # Multi-stack consolidation
├── multistack_worktree.go # Worktree-based merge testing
├── plan.go               # Merge planning engine
├── interactive.go        # Handler interfaces for TUI
├── wizard.go             # Interactive wizard logic
├── cleanup.go            # Post-merge PR cleanup
├── ci_waiter.go          # CI polling logic
└── execute.go            # Merge execution
```

### Key Types

```go
// Merge strategies
type Strategy int
const (
    StrategyBottomUp Strategy = iota
    StrategySquash
)

// Merge plan - created before execution
type Plan struct {
    Branches     []BranchPlan
    Strategy     Strategy
    Warnings     []string
    Errors       []string
}

// Shippability status
type Status int
const (
    StatusShippable Status = iota
    StatusPending
    StatusBlocked
    StatusIncomplete
)
```

### Flow: Bottom-Up Merge

```
merge next
  → FindBottomMostMergeable()
  → ValidatePRReady()
  → EnableAutoMerge() (GraphQL API)
  → Return immediately

[GitHub merges PR when CI passes]

merge next (again)
  → DetectMergedBranches()
  → RestackRemaining()
  → FindNextMergeable()
  → ...
```

### Flow: Consolidation

```
merge squash
  → PlanConsolidation()
  → CreateConsolidationBranch()
  → OctopusMerge(all branches)
  → CreatePR(consolidation branch)
  → [Optional: --wait for merge]
  → CleanupIndividualPRs()
```

### Flow: Multi-Stack Consolidation

```
merge squash --stacks a,b,c
  → CreateWorktreeSession()
  → TestGlobalMerge(all stacks)
  → If fails: BinarySearchWorkingSubset()
  → RunLocalCI()
  → CreateConsolidationPR()
  → CleanupAllIndividualPRs()
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
3. **Use fire-and-forget for incremental shipping**: Don't wait for each merge
4. **Leverage multi-stack consolidation**: Ship related work together
5. **Configure merge method once**: Set `stackit.merge.method` to avoid prompts
