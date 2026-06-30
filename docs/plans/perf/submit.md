# `submit` (and `ss`) — Performance Analysis

**Tier:** medium (frequent, but cost is dominated by remote API and `git push` latency, not local stackit work).

## Call graph

```
NewSubmitCmd → executeSubmit → submit.Action         internal/actions/submit/submit.go:77
  ├─ if untracked target: prompt + eng.TrackBranch
  ├─ getBranchesToSubmit                              graph walk over the stack (planning.go:165)
  ├─ eng.ReadBranchStatuses(branchObjs)               one batched parent rev-parse for all branches (submit.go)
  ├─ tree.NewStackTree + normalizeDisplayTreeParents  in-memory (builds its own parentMap, no eng.Graph)
  ├─ handler.OnEvent(StackDisplayEvent)               render
  │
  ├─ if --restack: actions.RestackBranches            ← per-spec worktree validation (see modify.md #1)
  ├─ (real submit) go eng.ReadBranchRemoteStatuses    one `ls-remote`, concurrent with validation (submit.go:195)
  ├─ ValidateBranchesToSubmit                         per-branch checks (submit_validation.go)
  ├─ prepareBranchesForSubmit                         batched PR-submission-status + revisions (planning.go:15)
  │
  ├─ getGitHubClient                                  lazy init (one-time)
  ├─ pushSubmittedBranches                            ← single batched `git push` of all branch refs (submit.go:275)
  ├─ for each branch:
  │     submitBranch                                  ← GraphQL PR create/update (push already done)
  │       sequential for creates (PR number ordering)
  │       parallel for updates (utils.Run)
  │
  ├─ if SubmitFooter: FetchPRContentForBranches (1 query) + utils.Run(UpdateBranchPRMetadataWithContent)  (submit.go:314)
  └─ pushMetadataRefs(branches)                       one `git push refs/stackit/metadata/...` (submit.go:330)
```

> Note: the branch push is already a single batched `git push` (`pushSubmittedBranches`,
> submit.go:448), and remote SHAs come from `eng.ReadBranchRemoteStatuses` (the old
> `PopulateRemoteShas` is gone). The footer pass already fetches all PR content in one
> GraphQL query before fanning out. The per-branch up-to-date checks now go through one
> batched `eng.ReadBranchStatuses` (both the display `fixedMap` and `validateBaseRevisions`),
> replacing the old N × uncached `git rev-parse`. Several originally-planned wins have
> landed; the remaining ones are below.

## Where time goes

For an **update-style submit** (PRs already exist, no restack needed):
1. **Branch `git push`** — now a single batched push of all branch refs (`pushSubmittedBranches`), not one push per branch. Still the dominant network cost.
2. **GitHub GraphQL "update PR" calls** — network-bound, parallelized via `utils.Run`.
3. **`SubmitFooter` updates** — a second parallel pass of N GraphQL updates after the create/update loop. Content is now pre-fetched in one query, but each branch's footer/title write is still its own round trip.
4. **`pushMetadataRefs`** — one combined push for `refs/stackit/metadata/*`. Separate round trip from the branch push.

For a **new-stack submit** (creates):
- Submission is **sequential** (`internal/actions/submit/submit.go:281–291`) to guarantee sequential PR numbers. By design, but pays N × create-latency serially.

For a **--restack submit**:
- `RestackBranches` pays the per-spec worktree validation tax (`modify.md` #1).

## Proposed wins (ranked)

### 1. Cache PR check/info reads across commands (shared with tree.md #4)

> **Status:** Not started. The per-branch PR-info read this win originally
> targeted is already batched: `ValidateBranchesToSubmit` syncs all PRs in one
> `github.SyncPrInfo` query (submit_validation.go:31), and planning resolves
> submission status via `BatchGetPRSubmissionStatusWithRemote`
> (engine/engine_pr.go:179). What remains is the cross-command cache.

There is no process-/disk-level TTL cache today. If `tree full` or a `state`/`tree` view ran
recently, a short TTL cache (~30s) on `BatchGetPRChecksStatus` and the PR-info sync would let
a following `submit` skip the redundant GraphQL round trip. This is the cross-cutting cache
shared with tree.md #4; submit is one consumer.

### 2. Pre-build worktrees for parallel update path *(low impact today, useful for `--restack` path; shared with restack.md #3)*

> **Status:** Not started.

When `--restack` is on, the inner `RestackBranches` builds worktrees per spec. If submit ran
restack frequently (or for `--all-stacks`), a reusable worktree pool would help. Same shape as
`restack.md` #3 — track the work there; submit benefits for free.

### 3. Combine the `SubmitFooter` update with the PR create/update call *(medium impact, low risk)*

> **Status:** Partially done. The footer pass already fetches every PR's current
> content in **one** GraphQL query up front (`actions.FetchPRContentForBranches`,
> submit.go:315) instead of a GET per branch. The remaining cost is the second
> write pass.

`internal/actions/submit/submit.go:314–327` still runs a separate pass of N GraphQL
*update* calls (`UpdateBranchPRMetadataWithContent`) solely to write the footer, after
`submitBranch` already issued a create/update for each branch. Fold the rendered footer into
the body/title sent by `createPullRequestQuiet`/`updatePullRequestQuiet` so the footer ships in
the same call. Saves the entire second-pass round trip when `--submit-footer` is on (the
default for many users).

### 4. Push metadata refs in the same `git push` as branch refs *(medium impact, medium risk)*

> **Status:** Not started.

`pushMetadataRefs` (called at submit.go:330, defined at submit.go:696) is a separate
`git push refs/stackit/metadata/*` after `pushSubmittedBranches` (submit.go:275) has already
pushed all branch refs. The pattern is "1 batched branch push + 1 metadata push". A single
`git push --atomic` listing both ref groups would save one round trip. Risk: atomic pushes
fail-as-a-group; today's behavior tolerates a metadata-only push failure ("Run 'st sync' and
try submitting again"). Keep the fallback.

### 5. Sequential create path could parallelize after the first PR *(small impact, medium risk)*

> **Status:** Not started.

Creates are sequential (`submit.go:281–291`) to get sequential PR numbers. After the first
branch's PR is created (number known), subsequent creates only need *its number* — not the
sequence. Could submit in batches: first one alone, then the rest in parallel referencing each
other by number. Tricky because stacked PR descriptions reference each other; needs careful
ordering.

## Validation

```
STACKIT_NO_LOGGING=1 hyperfine \
  'stackit submit --dry-run' \
  'stackit submit'
```

The delta is the push + GraphQL cost — almost entirely network. Instrument: the batched
`pushSubmittedBranches`, `pushMetadataRefs`, the per-branch `IsBranchUpToDate` loop (win #1),
and the `SubmitFooter` update phase (win #4). The `--dry-run` baseline isolates planning from
network.
