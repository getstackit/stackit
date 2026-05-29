---
name: commit-message
description: Generate a commit message from staged changes
model: haiku
---

# Commit Message Generator

This template documents how to construct a prompt for the haiku subagent when generating commit messages.

## Usage

Use the Task tool with `model: haiku` and `subagent_type: general-purpose`. Construct the prompt by gathering the context below and inserting it into the prompt structure.

## Required Context

Gather this information before spawning the subagent:

```bash
# Diff statistics
git diff --cached --stat

# Diff content (truncate if very large)
git diff --cached | head -200

# Recent commits for style reference
git log --oneline -5

# Project conventions (optional)
head -100 CONTRIBUTING.md 2>/dev/null || head -50 README.md 2>/dev/null || echo ""
```

**Edge case - empty diff:** If `git diff --cached --stat` returns nothing, there are no staged changes. Do not spawn the subagent; instead inform the user to stage changes first with `git add`.

## Prompt Structure

Construct the subagent prompt like this:

```
Generate a commit message for these staged changes.

## Staged Changes

### Diff Statistics
<insert git diff --cached --stat output>

### Diff Content
```diff
<insert git diff --cached output>
```

## Recent Commits (for style reference)
<insert git log --oneline -5 output>

## Project Conventions
<insert CONTRIBUTING.md or README.md content, or "None documented">

## Instructions

Generate a commit message for the staged changes above.

**Determine the format from project documentation:**
1. Check CONTRIBUTING guidelines for commit message requirements
2. Check README for any commit conventions mentioned
3. Look at recent commits for the established style
4. If no conventions are specified, default to Conventional Commits:
   `<type>: <description>` where type is one of
   feat, fix, docs, style, refactor, perf, test, chore, ci
   (e.g., `feat: add user authentication`)

**General guidelines:**
- First line under 72 characters
- Use imperative mood ("Add" not "Added")
- Focus on WHAT and WHY, not HOW
- Match the style and format of recent commits

## Response Format

Respond with EXACTLY this format:

MESSAGE:
<first line>

<optional body - only if change is complex>
END_MESSAGE
```

## Parsing the Response

Extract the commit message between `MESSAGE:` and `END_MESSAGE` markers. The message may be a single line or include a body separated by a blank line. Trim leading/trailing whitespace from the extracted content.

## Applying the Message

Apply the generated message with stackit — never `git commit` for a new stacked
branch. Pipe it via `-F -` so the literal message stays off the command line
(keeps permission rules stable):

```bash
# New stacked branch (changes already staged)
printf '%s\n' "<message>" | stackit create -F - --no-interactive

# Amend the current branch's commit with a new message
printf '%s\n' "<message>" | stackit modify -F - --no-interactive
```
