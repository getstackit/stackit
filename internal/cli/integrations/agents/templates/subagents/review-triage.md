---
name: review-triage
description: Classify PR review comments as actionable or not
model: haiku
---

# Review Comment Classifier

This template documents how to construct a prompt for the haiku subagent when triaging PR review comments.

## Usage

Use the Task tool with `model: haiku` and `subagent_type: general-purpose`.

**When to use:** This is most efficient when you have 5 or more review threads to classify. For fewer threads, it may be faster to classify them directly in the main agent. The threshold balances subagent overhead against parallel classification benefits.

## Required Context

Gather the review threads from the GitHub GraphQL API response. Format each thread as:

```
Thread ID: <id>
File: <path>
Line: <line> (or "file-level" if null)
Comments:
- @<author>: <body>
- @<author>: <reply body>
```

## Prompt Structure

Construct the subagent prompt like this:

```
Classify these PR review comments to determine which require code changes.

## Review Threads

<insert formatted threads here>

## Instructions

For each thread, analyze the comments and classify whether it requires a code change.

**ACTIONABLE if:**
- Requests a specific code change ("rename X to Y", "add null check", "remove this")
- Points out a bug with clear fix ("this will NPE", "missing return statement")
- Requests error handling, validation, or safety improvements
- Asks for documentation/comments on specific code
- The reviewer's intent is clear and unambiguous

**NOT ACTIONABLE if:**
- Question without answer ("why is this here?", "what does this do?")
- Discussion or debate without clear resolution
- Praise or approval ("LGTM", "nice!", "looks good")
- Vague or ambiguous ("this could be better", "consider improving")
- Already addressed (code has changed since comment)
- Requires clarification from author
- File-level comment without specific change request

## Response Format

Respond with EXACTLY this JSON format:

```json
{
  "threads": [
    {
      "id": "<thread_id>",
      "actionable": true,
      "reason": "Requests specific rename of variable",
      "suggested_change": "Rename 'foo' to 'fooBar'"
    },
    {
      "id": "<thread_id>",
      "actionable": false,
      "reason": "Question about design choice, needs discussion"
    }
  ]
}
```

Be conservative - when in doubt, mark as NOT actionable. It's better to skip an ambiguous comment than to make an incorrect change.
```

## Parsing the Response

Parse the JSON from the response. Each thread object contains:
- `id`: The thread ID to use when resolving
- `actionable`: Boolean indicating if code changes are needed
- `reason`: Brief explanation of the classification
- `suggested_change`: (Only for actionable threads) Description of what change to make

Use the `suggested_change` field as guidance when applying edits, but always verify against the actual code.

## After Classification

To get the actionable fixes back onto the PR, use stackit — not `gh` or a manual
push. On the branch under review:

```bash
stackit checkout <branch> --no-interactive
# apply the edits...
git add -A
stackit modify --no-interactive          # amends the commit; auto-restacks descendants
stackit submit --no-interactive           # update the PR(s)
```

If a fix belongs on a different branch in the stack, route it with `stackit absorb`
rather than amending the wrong commit. Resolve the GitHub review threads after the
update lands.

**Error handling - malformed response:** If the subagent returns invalid JSON or an unexpected format:
1. Log the raw response for debugging
2. Fall back to classifying threads manually in the main agent
3. Do not retry the subagent with the same input - it's likely a prompt issue
4. Consider the threads as "not classified" and evaluate them individually
