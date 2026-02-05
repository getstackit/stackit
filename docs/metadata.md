# Metadata Storage

This document describes the metadata system used by Stackit to track branch relationships, PR information, and stack-level data. All metadata is stored in Git refs, enabling atomic updates and sync across machines.

## Overview

Stackit uses **Git refs** as the storage mechanism for all metadata. Metadata is stored as JSON blobs, with refs pointing to blob SHAs. This approach provides:

- **Atomic updates**: Multi-ref changes via `git update-ref --stdin`
- **Version control**: Metadata history via reflog
- **Sync capability**: Push/fetch metadata alongside code
- **No external dependencies**: Everything lives in the Git repository

## Ref Namespaces

Stackit uses four ref namespaces:

| Namespace | Purpose | Synced to Remote |
|-----------|---------|------------------|
| `refs/stackit/metadata/{branch}` | Per-branch metadata (parent, PR info, scope, etc.) | Yes |
| `refs/stackit/local-metadata/{branch}` | Local-only branch state (frozen, cached IDs) | No |
| `refs/stackit/stacks/{stack-id}` | Stack-level metadata (title, description) | Yes |
| `refs/stackit/remote-stacks/{stack-id}` | Fetched remote stack metadata (read-only) | N/A (fetched) |

## Branch Metadata (`Meta`)

**Ref**: `refs/stackit/metadata/{branch-name}`

**Source**: `internal/git/metadata.go:41-61`

Branch metadata stores the relationship between branches and associated PR information.

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `parentBranchName` | `*string` | Name of the parent branch |
| `parentBranchRevision` | `*string` | Git SHA of parent at time of divergence (for detecting when restack is needed) |
| `prInfo` | `*PrInfoPersistence` | PR information (number, title, body, state, etc.) |
| `scope` | `*string` | Branch scope for inherited PR prefix (e.g., `"PROJ-123"`) |
| `lockReason` | `LockReason` | Why the branch is locked: `""`, `"user"`, or `"consolidating"` |
| `branchType` | `BranchType` | Type: `"user"`, `"utility"`, or `"worktree-anchor"` |
| `lastModifiedBy` | `*ModifiedBy` | Who last changed this metadata |
| `lastModifiedAt` | `*time.Time` | When metadata was last changed |
| `localOnlyHash` | `*string` | Hash of local-only state for change detection (never pushed) |
| `mergedDownstack` | `[]MergedParent` | Historical parents when reparented (max 5 entries) |
| `stackId` | `*string` | Links branch to stack ref |

### Example

```json
{
  "parentBranchName": "main",
  "parentBranchRevision": "abc123def456",
  "prInfo": {
    "number": 42,
    "base": "main",
    "url": "https://github.com/org/repo/pull/42",
    "title": "[PROJ-123] Add feature X",
    "body": "## Summary\n\nAdds feature X...",
    "state": "OPEN",
    "isDraft": false
  },
  "scope": "PROJ-123",
  "lockReason": "",
  "branchType": "user",
  "stackId": "1706789123456789-feature-x"
}
```

### Lifecycle

| Operation | Effect |
|-----------|--------|
| `stackit create` | Creates metadata with parent name/revision, inherits stack ID from parent |
| `stackit submit` | Updates `prInfo` with PR number, URL, title, body, state |
| `stackit restack` | Updates `parentBranchRevision` to current parent SHA |
| `stackit lock` | Sets `lockReason` to `"user"` |
| `stackit unlock` | Clears `lockReason` |
| `stackit scope set` | Sets `scope` field |
| Merged parent | Adds entry to `mergedDownstack`, updates `parentBranchName` |
| `stackit delete` | Deletes the metadata ref |

---

## Local Metadata (`LocalMeta`)

**Ref**: `refs/stackit/local-metadata/{branch-name}`

**Source**: `internal/git/metadata.go:82-87`

Local metadata is never pushed to remote. It stores per-machine state.

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `frozen` | `bool` | Branch is frozen locally (excluded from restack/submit) |
| `needsPRBodyUpdate` | `bool` | Flag indicating PR body needs update during sync |
| `navigationCommentId` | `*int64` | Cached GitHub comment ID for navigation commands |

### Example

```json
{
  "frozen": true,
  "needsPRBodyUpdate": false,
  "navigationCommentId": 12345678
}
```

### Lifecycle

| Operation | Effect |
|-----------|--------|
| `stackit freeze` | Sets `frozen` to `true` |
| `stackit unfreeze` | Sets `frozen` to `false` |
| `stackit sync` | May set `needsPRBodyUpdate` for deferred updates |
| Navigation comment posted | Caches `navigationCommentId` |
| `stackit delete` | Deletes the local metadata ref |

---

## Stack Metadata (`StackMeta`)

**Ref**: `refs/stackit/stacks/{stack-id}`

**Source**: `internal/git/stack_metadata.go:20-29`

Stack metadata stores information about an entire stack. It survives branch operations like merging the root branch, because it's stored separately from branch metadata.

### Stack ID Format

```
{timestamp-nanos}-{sanitized-root-branch}
```

**Example**: `1706789123456789-feature-x`

The sanitized branch name:
- Contains only alphanumeric characters and hyphens
- Maximum 50 characters
- No leading/trailing hyphens
- Falls back to `"stack"` if all characters are invalid

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | `string` | Stack ID (matches ref name) |
| `title` | `string` | Stack title (displayed in UI) |
| `description` | `string` | Stack description (longer narrative) |
| `createdAt` | `time.Time` | When the stack was created |
| `createdBy` | `string` | Who created the stack (git user name) |

### Example

```json
{
  "id": "1706789123456789-feature-x",
  "title": "Add user authentication",
  "description": "This stack implements OAuth2 authentication with Google and GitHub providers.\n\nPhase 1: Core auth flow\nPhase 2: Provider integrations\nPhase 3: Session management",
  "createdAt": "2024-02-01T10:30:00Z",
  "createdBy": "Jane Developer"
}
```

### Lifecycle

| Operation | Effect |
|-----------|--------|
| `stackit create` (first branch off trunk) | Generates new stack ID, creates stack ref |
| `stackit create` (off tracked branch) | Inherits parent's stack ID |
| `stackit describe` | Sets/updates `title` and `description` |
| Reparenting to different stack | Updates branch's `stackId` to match new parent |
| All branches deleted | Stack ref remains (for history) |

---

## Supporting Types

### LockReason

**Source**: `internal/git/metadata.go:13-23`

| Value | Description |
|-------|-------------|
| `""` (empty) | Not locked |
| `"user"` | Manually locked by user via `stackit lock` |
| `"consolidating"` | Locked during merge consolidation operation |

### BranchType

**Source**: `internal/git/metadata.go:30-38`

| Value | Description |
|-------|-------------|
| `"user"` | Normal stacked branch created by user |
| `"utility"` | Created by `stackit merge --consolidate` or internal operations |
| `"worktree-anchor"` | Anchor branch for worktree (has no commits of its own) |

### PrInfoPersistence

**Source**: `internal/git/metadata.go:96-107`

PR information persisted in branch metadata.

| Field | Type | Description |
|-------|------|-------------|
| `number` | `*int` | PR number |
| `base` | `*string` | Base branch name |
| `url` | `*string` | PR URL |
| `title` | `*string` | PR title |
| `body` | `*string` | PR description/body |
| `state` | `*string` | PR state: `"OPEN"`, `"MERGED"`, `"CLOSED"` |
| `isDraft` | `*bool` | Whether PR is a draft |
| `lockReason` | `*LockReason` | Lock status parsed from PR footer |
| `mergeBranch` | `*string` | Merge branch name for consolidated PRs |

### MergedParent

**Source**: `internal/git/metadata.go:76-80`

Records historical parent relationships when branches are reparented.

| Field | Type | Description |
|-------|------|-------------|
| `branchName` | `string` | Name of the former parent branch |
| `prNumber` | `*int` | PR number of the merged/closed parent |
| `prState` | `*string` | State when reparented: `"MERGED"`, `"CLOSED"` |

Limited to 5 entries (oldest entries dropped when limit exceeded).

### ModifiedBy

**Source**: `internal/git/metadata.go:89-94`

Tracks who last modified metadata.

| Field | Type | Description |
|-------|------|-------------|
| `gitName` | `string` | Git user name |
| `gitEmail` | `string` | Git user email |
| `githubUsername` | `*string` | GitHub username (if known) |

---

## Scope Inheritance

**Source**: `internal/engine/types.go:57-155`

Scopes are inherited from parent branches. When a branch doesn't have an explicit scope, Stackit walks up the parent chain until it finds one.

| Scope Value | Behavior |
|-------------|----------|
| `""` (empty) | No explicit scope, inherit from parent |
| `"none"` or `"clear"` | Breaks inheritance, children don't inherit |
| Any other string | Scope prefix (e.g., `"PROJ-123"`) |

Scopes are applied to PR titles: `[PROJ-123] Feature description`

---

## Transactions

**Source**: `internal/engine/transaction.go`

Metadata updates use transactions for atomicity. A `MetadataTx`:

1. Batches multiple ref updates
2. Uses compare-and-swap (CAS) validation to detect concurrent modifications
3. Commits all changes atomically via `git update-ref --stdin`
4. Updates in-memory cache on success

```go
tx := e.BeginTx("set parent: feature -> main")
tx.UpdateMeta(branchName, meta)
tx.UpdateLocalMeta(branchName, localMeta)
err := tx.Commit(ctx)  // All or nothing
```

Concurrent modification errors trigger automatic retry with exponential backoff.

---

## Viewing Metadata

To inspect raw metadata:

```bash
# List all metadata refs
git for-each-ref refs/stackit/

# Read branch metadata
git show refs/stackit/metadata/feature-branch

# Read stack metadata
git show refs/stackit/stacks/1706789123456789-feature-x

# Read local metadata
git show refs/stackit/local-metadata/feature-branch
```

---

## Storage Details

- **JSON format**: All metadata is stored as pretty-printed JSON
- **Blob storage**: JSON is stored in Git blobs (`git hash-object -w`)
- **Ref pointing**: Refs point to blob SHAs (not commits)
- **Atomic updates**: `git update-ref --stdin` for batch operations
- **Reflog**: Updates are recorded in reflog with descriptive messages
