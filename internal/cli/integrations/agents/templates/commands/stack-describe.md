---
description: Generate or update stack description from changes
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), Read, Glob, Grep
---

# Stack Describe

Generate a comprehensive description for the current stack based on all changes.

## Context
- Current branch: !`git branch --show-current`
- Stack state: !`stackit log --no-interactive`
- Stack info: !`stackit info --stack --json --no-interactive`
- Current description: !`stackit describe --show --no-interactive`

## Instructions

### Step 1: Analyze Stack Changes

For each branch in the stack (from the stack info JSON), gather:

1. **Commit messages** - Already available in `commit_messages` field
2. **Diff statistics** - Already available in `diff_stats` field
3. **File changes** - Prefer the loaded `diff_stats`; only run `git diff <parent>..<branch> --stat` if you need per-file detail it doesn't cover

If the stack has complex changes, optionally read key files to understand:
- What features are being added
- What bugs are being fixed
- What refactoring is happening

### Step 2: Generate Description

Based on the analysis, generate:

**Title** (max 72 chars):
- Summarize the overall purpose of the stack
- Use imperative mood (e.g., "Add user authentication feature")
- Be specific but concise

**Description** (multi-line):
- Provide a high-level overview of what the stack accomplishes
- List the key changes organized by branch or concern
- Mention any important implementation details
- Note dependencies or migration requirements if applicable

Format the description as markdown with:
- A brief summary paragraph
- Bullet points for key changes
- Optional sections for "Implementation Notes" or "Testing"

### Step 3: Set the Description

Run the describe command:

```bash
stackit describe -m "<title>" -d "<description>" --no-interactive
```

Note: The description argument supports multiline text.

### Step 4: Confirm Success

Show the user what was set:

```bash
stackit describe --show --no-interactive
```

## Example Output

For a stack with auth feature branches, the skill might generate:

**Title:** "Add OAuth2 authentication with GitHub provider"

**Description:**
```
Implements OAuth2 authentication allowing users to sign in with GitHub.

Key changes:
- **auth-foundation**: Core OAuth2 flow and token management
- **github-provider**: GitHub-specific OAuth configuration
- **user-session**: Session management and logout functionality

Implementation notes:
- Uses standard OAuth2 PKCE flow for security
- Tokens stored in HTTP-only cookies
- Session expires after 24 hours of inactivity
```

## Do NOT
- Generate placeholder content ("TODO", "TBD")
- Include sensitive information (API keys, secrets)
- Make up changes that aren't in the stack
- Overwrite existing description without analyzing current content first
