---
description: Resolve rebase conflicts with AI assistance
model: claude-sonnet-4-20250514
allowed-tools: Read, Edit, AskUserQuestion, Bash(git show *), Bash(git diff *), Bash(git add *), Bash(git rebase --continue), Bash(grep *), Bash(command stackit *), Bash(make *), Bash(npm *), Bash(yarn *), Bash(pnpm *), Bash(go *), Bash(cargo *), Bash(mise run *)
---

# Conflict Resolution

Resolve rebase conflicts during stackit operations with AI assistance.

## Context
- Current operation: !`cat .git/rebase-merge/message 2>/dev/null || echo "No rebase in progress"`
- Conflicted files: !`git diff --name-only --diff-filter=U 2>/dev/null || echo "No conflicts detected"`
- Current branch: !`git branch --show-current 2>/dev/null || cat .git/rebase-merge/head-name 2>/dev/null | sed 's|refs/heads/||'`
- Project type: !`if [ -f "mise.toml" ]; then echo "mise"; elif [ -f "Makefile" ]; then echo "make"; elif [ -f "package.json" ]; then echo "npm/yarn"; elif [ -f "Cargo.toml" ]; then echo "cargo"; elif [ -f "go.mod" ]; then echo "go"; else echo "unknown"; fi`

## Instructions

### Phase 1: Assess the Situation

1. **Check if there's an active rebase:**
   - If no rebase in progress and no conflicts, inform the user and exit
   - If rebase in progress, proceed with conflict resolution

2. **List conflicted files:**
   ```bash
   git diff --name-only --diff-filter=U
   ```

3. **For each conflicted file, gather context:**
   ```bash
   # See the incoming changes (what's being rebased)
   git show REBASE_HEAD:<file>

   # See the current state (target branch)
   git show HEAD:<file>
   ```

   Then use the **Read tool** to see the current file contents with conflict markers.

### Phase 2: Analyze and Explain Conflicts

For each conflicted file:

1. **Read the file** to see the conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`)

2. **Explain the conflict in plain language:**
   - What the **parent branch** (HEAD) changed
   - What **your branch** (REBASE_HEAD) changed
   - Why they conflict (same lines modified, structural changes, etc.)

3. **State your confidence level:**
   - **High**: Both changes are additive or clearly separable
   - **Medium**: Changes overlap but intent is clear
   - **Low**: Significant restructuring or unclear intent

### Phase 3: Propose Resolution

For each conflict:

1. **Propose a merged resolution** that preserves both intents

2. **Show the proposed code** with context

3. **Ask for approval using AskUserQuestion:**
   - Header: "Conflict"
   - Question: "Apply this resolution to <filename>?"
   - Options:
     - "Apply" - Use the proposed resolution
     - "Edit" - Let me modify the proposal
     - "Skip" - I'll handle this file manually

### Phase 4: Apply Resolutions

After user approves or edits:

1. **Apply the resolution:**
   ```bash
   # Use the Edit tool to replace the conflicted content
   ```

2. **Stage the resolved file:**
   ```bash
   git add <file>
   ```

3. **Verify no conflict markers remain:**
   ```bash
   grep -En "^<<<<<<<|^=======|^>>>>>>>" <file> || echo "No conflict markers"
   ```

### Phase 5: Validate and Continue

After all conflicts are resolved:

1. **Run the project's check command** based on project type detected in Context:

   | Project Type | Check Command |
   |--------------|---------------|
   | mise | `mise run check` or `mise run test` |
   | make | `make test` or `make check` |
   | npm/yarn | `npm test` or `yarn test` |
   | cargo | `cargo test` |
   | go | `go test ./...` |
   | unknown | Ask the user what command to run |

   If the first command fails to find a task/target, try alternatives or ask the user.

2. **If validation passes:**
   ```bash
   git rebase --continue
   ```

3. **If validation fails:**
   - Explain what broke
   - Offer to help fix the issue
   - Do NOT continue the rebase until tests pass

### Phase 6: Post-Resolution

After rebase continues successfully:

1. **Check if there are more conflicts** (rebase may stop again)

2. **If clean:**
   ```bash
   command stackit log --no-interactive
   ```
   Show the updated stack state.

3. **If the rebase is fully complete:**
   - Inform the user
   - Suggest `command stackit submit` if PRs need updating

## Communication Style

- **Explain WHY** the conflict happened, not just WHAT
- **Show both versions conceptually** - don't dump raw diffs
- **State confidence level** in suggested resolutions
- **Be conservative** - when in doubt, ask the user
- **Preserve both intents** - don't silently drop changes

## Example Output

```
Conflict in internal/auth/validator.go

What happened:
- Parent branch (feature-rate-limit) added rate limiting to validateToken() at lines 45-52
- Your branch (feature-auth-refactor) refactored validateToken() into smaller functions

Why they conflict:
Both branches modified the same function. The rate limiting code references
the old function structure that your refactor changed.

Suggested resolution:
Apply the rate limiting logic to your new validateRequest() function:

[code block showing merged result]

Confidence: High (both changes are additive)
```

## Do NOT

- Continue rebase without user approval of resolutions
- Apply resolutions that fail validation
- Leave conflict markers in files
- Make assumptions about which version to keep without explaining
- Run destructive git operations (reset, force push, etc.)
