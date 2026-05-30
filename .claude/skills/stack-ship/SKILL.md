---
name: stack-ship
description: Evaluate shippable stacks, review their changes, check for merge conflicts, and produce a smart ship plan. Uses parallel multi-agent review and conflict analysis to determine what to ship, in what groupings, and what to hold. Keywords ship, merge, plan, stacks, PRs, conflicts.
allowed-tools: Bash(stackit:*), Bash(git:*), Bash(gh:*), Read, Workflow, AskUserQuestion
---

# /stack-ship

Smart ship planning: discover shippable stacks → parallel code review → conflict detection → prioritized merge plan → optional execution.

## Mental Model

Merging many PRs blindly causes conflict pain and wastes CI minutes. This skill works smarter:

1. **Discover** — `stackit merge status --json --all` surfaces every stack with CI/approval state
2. **Review** — Parallel agents review each shippable stack's diff for correctness, risks, and red flags
3. **Conflict check** — `stackit merge ship --dry-run --stacks ...` tests whether stacks can merge together atomically
4. **Plan** — Synthesize into: _ship these together_, _hold these_ (minor concerns), _fix these_ (blocking issues)
5. **Execute** — Run the merge command after user approval

## Execution

### Step 1: Launch the Ship Workflow

Use the **Workflow tool** with the script below. Pass no args.

```javascript
export const meta = {
  name: 'stack-ship',
  description: 'Discover, review, and plan shipping for stacks',
  phases: [
    { title: 'Discover', detail: 'Get shippability status of all stacks' },
    { title: 'Review', detail: 'Review each shippable stack in parallel' },
    { title: 'Conflict Check', detail: 'Test merge compatibility between stacks' },
    { title: 'Plan', detail: 'Synthesize findings into a ship plan' },
  ],
}

const STACK_SCHEMA = {
  type: 'object',
  properties: {
    stacks: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          root_branch: { type: 'string' },
          all_branches: { type: 'array', items: { type: 'string' } },
          pr_count: { type: 'number' },
          scope: { type: 'string' },
          status: { type: 'string' },
          author: { type: 'string' },
          pr_title: { type: 'string' },
          approval_ok: { type: 'boolean' },
          github_ci_ok: { type: 'boolean' },
          blocking_prs: { type: 'array', items: { type: 'object' } },
        },
        required: ['root_branch', 'all_branches', 'status'],
      },
    },
    shippable_count: { type: 'number' },
    pending_count: { type: 'number' },
    blocked_count: { type: 'number' },
    incomplete_count: { type: 'number' },
  },
  required: ['stacks', 'shippable_count', 'pending_count', 'blocked_count', 'incomplete_count'],
}

const REVIEW_SCHEMA = {
  type: 'object',
  properties: {
    root_branch: { type: 'string' },
    verdict: { type: 'string', enum: ['ship', 'hold', 'fix'] },
    confidence: { type: 'string', enum: ['high', 'medium', 'low'] },
    summary: { type: 'string' },
    risks: { type: 'array', items: { type: 'string' } },
    blocking_issues: { type: 'array', items: { type: 'string' } },
  },
  required: ['root_branch', 'verdict', 'confidence', 'summary', 'risks', 'blocking_issues'],
}

const CONFLICT_SCHEMA = {
  type: 'object',
  properties: {
    all_compatible: { type: 'boolean' },
    working_stacks: { type: 'array', items: { type: 'string' } },
    conflicting_stacks: { type: 'array', items: { type: 'string' } },
    notes: { type: 'string' },
  },
  required: ['all_compatible', 'working_stacks', 'conflicting_stacks'],
}

const PLAN_SCHEMA = {
  type: 'object',
  properties: {
    ship_groups: {
      type: 'array',
      description: 'Ordered groups of stacks to ship. Each group can be merged atomically.',
      items: {
        type: 'object',
        properties: {
          stacks: { type: 'array', items: { type: 'string' }, description: 'Root branches in this group' },
          strategy: { type: 'string', enum: ['ship', 'drain', 'next'], description: 'ship=atomic consolidation (3+ branches), drain=sequential bottom-up, next=fire-and-forget lowest' },
          command: { type: 'string', description: 'Exact stackit command to run' },
          reasoning: { type: 'string' },
        },
        required: ['stacks', 'strategy', 'command', 'reasoning'],
      },
    },
    hold: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          root_branch: { type: 'string' },
          reason: { type: 'string' },
        },
        required: ['root_branch', 'reason'],
      },
    },
    fix: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          root_branch: { type: 'string' },
          issues: { type: 'array', items: { type: 'string' } },
        },
        required: ['root_branch', 'issues'],
      },
    },
    summary: { type: 'string' },
  },
  required: ['ship_groups', 'hold', 'fix', 'summary'],
}

// Phase 1: Discover
phase('Discover')
const status = await agent(
  `Run: stackit merge status --json --all

Return the JSON output as a structured object. If the command fails or produces no output, return {"stacks":[],"shippable_count":0,"pending_count":0,"blocked_count":0,"incomplete_count":0}.`,
  { label: 'discover:status', schema: STACK_SCHEMA }
)

const shippable = status.stacks.filter(s => s.status === 'shippable')
const nonShippable = status.stacks.filter(s => s.status !== 'shippable')

log(`${status.stacks.length} stacks total — ${status.shippable_count} shippable, ${status.pending_count} pending, ${status.blocked_count} blocked, ${status.incomplete_count} incomplete`)

if (shippable.length === 0) {
  return {
    ship_groups: [],
    hold: status.stacks.filter(s => s.status === 'pending').map(s => ({ root_branch: s.root_branch, reason: `Waiting: ${(s.blocking_prs || []).map(b => b.reason).join(', ') || 'CI or review pending'}` })),
    fix: status.stacks.filter(s => s.status === 'blocked').map(s => ({ root_branch: s.root_branch, issues: (s.blocking_prs || []).map(b => b.reason) })),
    summary: `No stacks are ready to ship. ${status.pending_count} pending CI/review, ${status.blocked_count} blocked.`,
    status,
  }
}

// Phase 2: Review each shippable stack in parallel
phase('Review')
const reviews = (await parallel(shippable.map(stack => () => {
  const tip = stack.all_branches[stack.all_branches.length - 1]
  const branchChain = stack.all_branches.join(' → ')
  return agent(
    `Review this stack for ship-readiness.

Stack: ${stack.root_branch}
Title: ${stack.pr_title || stack.root_branch}
Branches: ${branchChain}
PRs: ${stack.pr_count}
CI passing: ${stack.github_ci_ok}
Approved: ${stack.approval_ok}

To inspect the changes:
  git log main..${tip} --oneline
  git diff main...${tip} --stat
  git diff main...${tip}

For PR details (if PRs exist):
  gh pr list --head ${stack.root_branch} --json number,title,body,additions,deletions,reviewDecision,state

Look for:
1. Correctness bugs (nil dereferences, missing error handling, off-by-one)
2. Missing tests for changed behavior
3. Security issues (command injection, path traversal, hardcoded secrets)
4. Breaking changes to public interfaces
5. Incomplete implementation (TODOs without tracking, panics, unhandled cases)
6. Race conditions or resource leaks

Return verdict:
- "ship": No blocking concerns
- "hold": Minor issues worth noting but not blocking (will be flagged in plan)
- "fix": Has bugs or security issues that should be addressed before merging`,
    { label: `review:${stack.root_branch}`, schema: REVIEW_SCHEMA }
  )
}))).filter(Boolean)

// Phase 3: Conflict check
phase('Conflict Check')
let conflictResult
if (shippable.length > 1) {
  const roots = shippable.map(s => s.root_branch).join(',')
  conflictResult = await agent(
    `Test whether these stacks can merge together without conflicts.

Run: stackit merge ship --dry-run --stacks ${roots}

Parse the output. Determine:
1. Can all stacks be merged atomically?
2. Which stacks can be included (working set)?
3. Which stacks conflict (would be excluded)?

If the command errors or no stacks can be combined, use working_stacks=[] and conflicting_stacks=[${shippable.map(s => `"${s.root_branch}"`).join(',')}].`,
    { label: 'conflict:check', schema: CONFLICT_SCHEMA }
  )
} else {
  conflictResult = {
    all_compatible: true,
    working_stacks: shippable.map(s => s.root_branch),
    conflicting_stacks: [],
    notes: 'Single stack, no conflict check needed',
  }
}

// Phase 4: Synthesize ship plan
phase('Plan')
const reviewSummary = reviews.map(r =>
  `${r.root_branch}: verdict=${r.verdict} (${r.confidence} confidence) — ${r.summary}` +
  (r.risks.length ? `\n  Risks: ${r.risks.join('; ')}` : '') +
  (r.blocking_issues.length ? `\n  Blocking: ${r.blocking_issues.join('; ')}` : '')
).join('\n')

const plan = await agent(
  `Create a smart ship plan.

SHIPPABLE STACKS:
${shippable.map(s => `- ${s.root_branch}: ${s.pr_count} PR(s), "${s.pr_title || s.root_branch}", CI=${s.github_ci_ok}, approved=${s.approval_ok}`).join('\n')}

CODE REVIEW RESULTS:
${reviewSummary}

CONFLICT ANALYSIS:
- All compatible: ${conflictResult.all_compatible}
- Working stacks (can merge together): ${conflictResult.working_stacks.join(', ') || 'none'}
- Conflicting stacks: ${conflictResult.conflicting_stacks.join(', ') || 'none'}

NON-SHIPPABLE STACKS (for context):
${nonShippable.map(s => `- ${s.root_branch}: ${s.status}`).join('\n') || 'none'}

Rules for the plan:
- Group compatible working stacks to ship atomically (prefer fewer merge operations)
- Use strategy="ship" (atomic consolidation) when a group has 3+ total branches across stacks
- Use strategy="drain" for smaller groups (1-2 branches per stack, sequential bottom-up)
- Conflicting stacks that can't merge together go in separate groups
- Move "hold" review stacks to the hold list with the specific concern
- Move "fix" review stacks to the fix list with the specific issues
- For each ship_group, generate the exact stackit command to run

Example commands:
  stackit merge ship --stacks root1,root2          # atomic multi-stack
  stackit merge ship --branch root1                # single stack ship
  stackit merge drain --branch root1               # sequential bottom-up`,
  { label: 'plan:synthesize', schema: PLAN_SCHEMA }
)

return { ...plan, status, reviews }
```

### Step 2: Present the Plan

After the workflow completes, format and show the plan to the user:

```
## Ship Plan

### Ship Now

**Group 1** (strategy: ship)
  Stacks: root-branch-a, root-branch-b
  Command: `stackit merge ship --stacks root-branch-a,root-branch-b`
  Reasoning: ...

### Hold
  - root-branch-c: minor concern about ...

### Fix
  - root-branch-d: missing error handling in X, potential nil dereference at Y

### Not Ready
  - root-branch-e: pending (CI running)
  - root-branch-f: blocked (changes requested)
```

### Step 3: Confirm and Execute

Use `AskUserQuestion` to ask the user:
- **Execute the plan** — Run the commands in order
- **Review the plan** — Show full review findings before deciding
- **Modify** — Let user adjust which stacks to include
- **Cancel** — Do nothing

If the user approves, run each group's command sequentially. Wait for each to complete before starting the next group (the merge commands handle their own waiting).

## Strategy Guide

| Stack size | Recommended strategy | Why |
|------------|---------------------|-----|
| 1-2 branches | `drain` | Simpler, sequential, each lands with its own CI |
| 3+ branches | `ship` | Atomic consolidation reduces CI overhead |
| Multiple stacks | `ship --stacks X,Y` | Single CI run for the combined set |

## Common Scenarios

**Many independent PRs ready:** Group all compatible stacks into one `merge ship --stacks` call. Fastest path.

**Mixed readiness:** Ship the ready stacks, leave pending/blocked for next run. Don't wait for stragglers.

**Conflicts detected:** Ship the working subset now, fix the conflicting stack separately (usually means branches need rebasing on each other).

**Review flagged issues:** Communicate the concern clearly. Let the author decide whether to hold or proceed.

## Permission Stability

Per `.claude/rules/stackit-workflow.md`:
- Run one command per Bash call, no `&&` chaining
- Don't append `2>&1`
- Use `STACKIT_NO_INTERACTIVE=1` or `--yes` for non-interactive execution

When executing the plan, pass `--yes` to skip confirmation prompts (the skill has already confirmed with the user).
