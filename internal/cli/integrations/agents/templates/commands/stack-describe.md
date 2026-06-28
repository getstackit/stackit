---
description: Generate or update stack description from changes
model: sonnet
allowed-tools: Bash(stackit:*), Bash(git:*), Bash(gh:*), Read, Glob, Grep, Task
---

# Stack Describe

Generate a comprehensive description for the current stack based on all changes.

## Context
- Current branch: !`git branch --show-current`
- Stack info (branches, parents, commit messages, diff stats): !`stackit info --stack --json --no-interactive`
- Current description: !`stackit describe --show --no-interactive`

## Instructions

### Step 1: Build Context (cheapest sources first)

Build just enough understanding to describe the stack accurately. Work **down**
this ladder and **stop as soon as you can write an accurate description** — each
rung costs more context than the last, and most stacks never need past rung 1.

**Never run a full `git diff` of the stack into this context.** It burns enormous
context for little marginal signal over the cheaper sources below. If you truly
need to read code, do it via a sub-agent (rung 4) so the cost stays out of this
conversation.

1. **Commit messages + diff stats — already in context, free.** The injected JSON
   has `commit_messages` and `diff_stats` per branch. For most stacks — especially
   ones with clear conventional-commit subjects — this is enough. Write the
   description and skip the rest.

2. **PR descriptions — cheap, high-signal.** When commit subjects are thin but
   branches have a `pr_number`, fetch the existing PR bodies; a human or agent
   already summarized intent there. Synthesize from these rather than from code:

   ```bash
   gh pr view <pr_number> --json title,body -q '.title, .body'
   ```

3. **Changed file list — cheap, names only.** To see which subsystems a branch
   touches without reading contents (`diff_stats` already gives the counts):

   ```bash
   git diff --name-only <parent>..<branch>
   ```

4. **Sub-agent deep dive — for large/complex stacks only.** If the stack is large
   (many branches, or large `diff_stats`) and rungs 1–3 still leave intent
   unclear, **do not pull diffs into this context.** Dispatch sub-agents instead:
   each reads in its **own** context window and returns only a short summary, so
   this conversation stays lean. Spawn one `Explore` agent per branch (or per
   logical group), running independent agents in parallel:

   > Read `git diff <parent>..<branch>` and return 2–3 sentences: what changed and
   > why. Return only the summary, never the diff itself.

   Then synthesize the returned summaries into the description.

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

Note: The description argument supports multiline text. You can also pipe the
title and body in editor format (first line title, blank line, then body) via
`-F -`: `printf '<title>\n\n<description>' | stackit describe -F - --no-interactive`.
A bare non-interactive `describe` with no input errors ("nothing to set") rather
than silently doing nothing.

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
