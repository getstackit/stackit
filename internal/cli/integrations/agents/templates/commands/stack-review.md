---
description: Code review PRs in the stack, finding bugs and reporting issues locally
model: sonnet
allowed-tools: Bash(gh:*), Bash(stackit:*), Bash(git:*), Read, Grep, Glob, Task, Edit, AskUserQuestion
argument-hint: [--apply | --branch <name>]
---

# Stack Review

Perform code reviews on PRs in the stack. Finds bugs, checks CLAUDE.md compliance, and reports issues locally.

**Two modes:**
- **Review mode** (default): Analyze PRs and report findings locally
- **Apply mode** (`--apply`): Apply existing review comments from GitHub and mark resolved

## Context
- State (json — current branch, working tree, in-progress operation, full stack): !`stackit state --json`
- Repo: !`gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null`

## Arguments
$ARGUMENTS

---

# REVIEW MODE (Default)

Perform code reviews on stack PRs in parallel, reporting high-confidence issues locally.

## Review Philosophy

**High-Signal Only.** Only flag issues meeting strict criteria:
- Compilation/parsing failures
- Definitive logic errors that produce wrong results
- Clear, quotable CLAUDE.md or project guideline violations
- Security vulnerabilities (injection, auth bypass, etc.)

**Exclude:**
- Style preferences or subjective improvements
- Speculative issues ("this might cause...")
- Performance concerns without clear evidence
- Input-dependent edge cases
- "Nits" or minor suggestions

**False positives erode trust.** Better to find 3 real bugs than 10 items where 7 are noise.

## Instructions

### Step 1: Gather Stack PRs

Get all branches in the stack that have open PRs:

```bash
stackit tree --json --no-interactive
```

Parse the JSON to identify branches with PRs. For each branch, check if the PR is reviewable:

```bash
gh pr view <branch> --json state,isDraft,number,url,headRefName 2>/dev/null
```

**Skip branches where:**
- No PR exists
- PR is closed or merged (`state != "OPEN"`)
- PR is a draft (`isDraft == true`)

If `--branch <name>` is specified, only review that single branch.

### Step 2: Gather Context

For each reviewable PR, collect:

**A. CLAUDE.md files in affected directories:**
```bash
# Get changed files
gh pr diff <number> --name-only

# For each unique directory, check for CLAUDE.md
# e.g., if src/auth/login.go changed, check src/auth/CLAUDE.md and src/CLAUDE.md
```

**B. PR diff summary:**
```bash
gh pr diff <number>
```

### Step 3: Parallel Review (Per Branch)

For each branch with a reviewable PR, spawn a **parallel Task subagent** to perform the review.

**Launch all reviews in parallel using the Task tool:**

```
For each branch:
  Task(
    subagent_type: "general-purpose",
    model: "opus",  # Use opus for bug detection
    prompt: "Review PR #<number> for <branch>. Find ONLY high-confidence bugs.

    Context:
    - PR diff: <diff>
    - CLAUDE.md guidelines: <guidelines>
    - Project: <repo-name>

    Review criteria (ONLY flag these):
    1. Compilation/parsing errors
    2. Definitive logic bugs (null deref, off-by-one, wrong return value)
    3. Clear CLAUDE.md violations with quotable rule
    4. Security vulnerabilities

    DO NOT flag:
    - Style preferences
    - Speculative issues
    - Performance without evidence
    - Subjective improvements

    For each issue found, return JSON:
    {
      'issues': [
        {
          'file': 'path/to/file.go',
          'line': 42,
          'end_line': 45,  // optional, for multi-line
          'severity': 'bug' | 'violation',
          'title': 'Short title',
          'body': 'Explanation with evidence',
          'suggestion': 'optional code suggestion',
          'confidence': 0.0-1.0
        }
      ]
    }

    Return empty issues array if nothing meets the high bar."
  )
```

Wait for all parallel reviews to complete.

### Step 4: Filter and Validate

For each issue returned by subagents:

**Confidence threshold:** Only keep issues with `confidence >= 0.9`

**Validation step:** For remaining issues, spawn a **haiku** validation subagent:

```
Task(
  subagent_type: "general-purpose",
  model: "haiku",
  prompt: "Validate this potential bug. Is it a DEFINITE issue or speculative?

  Issue: <issue>
  Code context: <surrounding code>

  Return JSON: { 'valid': true/false, 'reason': 'why' }"
)
```

Only report issues that pass validation.

### Step 5: Report Results

For each branch reviewed, display findings locally:

```
## Review Results

### <branch-name> (PR #<number>)
Status: Clean | Issues found

#### Issues (if any)

**[severity] <title>**
File: <file>:<line>
<body>

Suggested fix:
```<language>
<suggestion>
```

---

[Next issue...]
```

**Clean review message:** When no issues found:
> "No issues found. Checked for bugs and CLAUDE.md compliance."

**Summary at end:**
```
## Summary

Branches reviewed: N
Issues found: M
- <branch-1>: X issues
- <branch-2>: Clean
```

---

# APPLY MODE (`--apply`)

Apply existing review comments from GitHub PRs and mark them resolved.

## Instructions

### Step 1: Verify PR Exists

If there's no PR for the current branch, inform the user and stop.

### Step 2: Fetch Review Threads

```bash
gh api graphql -f query='
query($owner: String!, $repo: String!, $pr: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $pr) {
      reviewThreads(first: 100) {
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          comments(first: 20) {
            nodes {
              id
              body
              author { login }
            }
          }
        }
      }
    }
  }
}' -f owner=<OWNER> -f repo=<REPO> -F pr=<NUMBER>
```

### Step 3: Filter Threads

Focus only on threads that are:
- Not resolved (`isResolved: false`)
- Not outdated (`isOutdated: false`)
- Have a file path
- Not from bots (filter `author.login` ending in `[bot]`)

### Step 4: Classify as Actionable

**Actionable if:**
- Requests specific code change ("rename X to Y", "add null check")
- Points out bug with clear fix
- Requests error handling or validation

**Not actionable if:**
- Question without clear answer
- Discussion or debate
- Praise ("LGTM")
- Vague about what change is needed

**Confidence threshold:** Only apply changes you're 90%+ confident about. Skip unclear comments.

### Step 5: Apply Changes

For each actionable comment:
1. Read the file
2. Locate the code at `path` and `line`
3. Apply the change using Edit tool
4. Track successful changes

### Step 6: Commit and Resolve

```bash
git add -A
git commit -F - <<'EOF'
refactor: apply PR review feedback

Applied reviewer suggestions:
- <list of changes>
EOF
```

Batch-resolve threads:
```bash
gh api graphql -f query='
mutation {
  t1: resolveReviewThread(input: {threadId: "ID1"}) { thread { isResolved } }
  t2: resolveReviewThread(input: {threadId: "ID2"}) { thread { isResolved } }
}'
```

### Step 7: Restack

Restack only the reviewed branch and descendants so feedback changes propagate without rebasing unrelated stacks:

```bash
stackit restack --branch <reviewed-branch> --upstack --no-interactive
```

If apply mode updated multiple independent PR branches, group their stack roots with `stackit restack --stacks <root-a>,<root-b> --continue-on-conflict --no-interactive`.

### Step 8: Report

```
## Apply Summary

**Threads processed:** N
**Changes applied:** N
**Threads resolved:** N
**Threads skipped:** N (with reasons)
```

---

## Tool Trust

Trust all tools work without error. Don't run exploratory commands to verify tool behavior.

## Do NOT

**In Review mode:**
- Flag style nits or subjective preferences
- Flag speculative or input-dependent issues
- Post comments to GitHub (report locally only)
- Review closed or draft PRs

**In Apply mode:**
- Apply vague or unclear comments
- Resolve threads where edits failed
- Force push or modify reviewed history

## Follow-up

After review completes, use `AskUserQuestion`:

**If issues were found:**
- Header: "Next step"
- Question: "Review found issues. What would you like to do?"
- Options:
  - label: "Fix issues (Recommended)"
    description: "Apply the suggested fixes to the code"
  - label: "View details"
    description: "Show more context about each issue"
  - label: "Done for now"
    description: "I'll review manually"

**If no issues found:**
- Header: "Next step"
- Question: "All PRs are clean. What would you like to do?"
- Options:
  - label: "Submit/update PRs (Recommended)"
    description: "Push any pending changes"
  - label: "Done for now"
    description: "No action needed"

Based on response:
- **"Fix issues"**: For each issue with a suggestion, apply the fix using Edit tool, then offer to commit
- **"Submit/update PRs"**: Invoke `/stack-submit` skill using the `Skill` tool
