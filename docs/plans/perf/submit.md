# `submit` (and `ss`) — Performance Analysis

**Tier:** medium (frequent, but cost is dominated by remote API and `git push` latency, not local stackit work).

## Call graph

```
NewSubmitCmd → executeSubmit → submit.Action         internal/actions/submit/submit.go:110
  ├─ if untracked target: prompt + eng.TrackBranch
  ├─ getBranchesToSubmit                              graph walk over the stack
  ├─ pr.PopulateRemoteShas                            one `for-each-ref refs/remotes/...`
  ├─ for each branch: branch.IsBranchUpToDate         N × parent revision lookup/status check
  ├─ tree.NewStackTree + normalizeDisplayTreeParents  in-memory
  ├─ handler.OnEvent(StackDisplayEvent)               render
  │
  ├─ if --restack: actions.RestackBranches            ← per-spec worktree validation (see modify.md #1)
  ├─ ValidateBranchesToSubmit                         per-branch checks
  ├─ prepareBranchesForSubmit                         per-branch metadata + PR planning
  │     ↳ may call GitHub for existing PR info
  │
  ├─ getGitHubClient                                  lazy init (one-time)
  ├─ for each branch:
  │     submitBranch                                  ← `git push` + GraphQL PR create/update
  │       sequential for creates (PR number ordering)
  │       parallel for updates (utils.Run)
  │
  ├─ if SubmitFooter: utils.Run(branches, UpdateBranchPRMetadata)   ← N × GraphQL update
  └─ pushMetadataRefs(branches)                       one `git push refs/stackit/metadata/...`
```

## Where time goes

For an **update-style submit** (PRs already exist, no restack needed):
1. **`git push` per branch** — each is a network round trip. Parallelized. Dominates everything else; typical 200–500ms per push, bounded by `MaxConcurrency` and remote rate limits.
2. **GitHub GraphQL "update PR" calls** — also network-bound, also parallelized.
3. **`SubmitFooter` updates** — another N GraphQL round-trips after the pushes. Already parallel. Often the slowest single phase because it sets per-branch PR body footers that include the stack listing.
4. **`pushMetadataRefs`** — one combined push for `refs/stackit/metadata/*`. Single round trip.
5. **`PopulateRemoteShas`** — one shell git command.

For a **new-stack submit** (creates):
- Submission is **sequential** (`internal/actions/submit/submit.go:296`) to guarantee sequential PR numbers. This is by design but pays N × push-latency serially.

For a **--restack submit**:
- `RestackBranches` pays the per-spec worktree validation tax (`modify.md` #1).

Local (non-network) overhead is small compared to remote calls. The dominant local cost is bootstrap plus per-branch up-to-date checks while walking the stack.

## Proposed wins (ranked)

### 1. Batch per-branch `IsBranchUpToDate` checks *(shared with cross-cutting.md #2)*

`internal/actions/submit/submit.go` calls `branch.IsBranchUpToDate()` for every branch in the stack. Route this through `ReadBranchStatuses` or an equivalent batched parent-revision lookup so the submit planning path does one grouped read instead of N individual checks.

### 2. Cache PR check status reads (shared with tree.md #4)

`prepareBranchesForSubmit` reads existing PR info per branch. If `tree full` ran recently or if the user invoked `submit` after a `state`/`tree` view, a process-level TTL cache (~30s) on `BatchGetPRChecksStatus` and per-PR info reads would skip redundant GraphQL calls.

### 3. Pre-build worktrees for parallel update path *(low impact today, useful for `--restack` path)*

When `--restack` is on, the inner `RestackBranches` builds worktrees per spec. If submit ran restack frequently (or for `--all-stacks`), a reusable worktree pool would help. Same shape as `restack.md` #4.

### 4. Combine the `SubmitFooter` update with the PR create/update call *(medium impact, low risk)*

`internal/actions/submit/submit.go:325–337` runs a second pass of N GraphQL calls solely to write the footer. Each existing `submitBranch` call already issues a GraphQL update — extend it to include the rendered footer in the same call. Saves the entire second-pass round trip when `--submit-footer` is on (which is the default for many users).

### 5. Push metadata refs in the same `git push` as branch refs *(medium impact, medium risk)*

`pushMetadataRefs` is a separate `git push refs/stackit/metadata/*` at the end. For a stack of N branches the push pattern is "N branch pushes + 1 metadata push". A single `git push --atomic` listing both groups would save one round trip. Risk: atomic pushes fail-as-a-group; today's behavior tolerates a metadata-only push failure ("Run 'st sync' and try submitting again"). Keep the fallback.

### 6. Sequential create path could parallelize after the first PR *(small impact, medium risk)*

The reason creates are sequential is to get sequential PR numbers. After the first branch's PR is created (and its number known), subsequent creates only need *its number* — not the sequence. Could submit branches in batches: first one alone, then the rest in parallel referencing each other by number. Tricky because stacked PR descriptions reference each other; needs careful ordering.

### 7. `getBranchesToSubmit` could skip the redundant `Graph` walk *(trivial)*

If the action wants the stack containing the current branch, `eng.Graph` is built lazily; submit already builds one for `tree.NewStackTree`. Reuse the same graph rather than rebuilding inside `getBranchesToSubmit`.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit submit --dry-run' \
  'stackit submit'
```

The delta is the push + GraphQL cost — almost entirely network. Instrument: each `submitBranch`, `pushMetadataRefs`, the per-branch `IsBranchUpToDate` loop, and the `SubmitFooter` update phase. The `--dry-run` baseline isolates planning from network.
