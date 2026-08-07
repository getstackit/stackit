# Common Change Recipes

Step-by-step file lists for cross-cutting changes that touch multiple layers.

## Add a New GitHub Client Method

Adding a method to the GitHub client interface requires updating 5 files:

| # | File | What to do |
|---|------|------------|
| 1 | `internal/github/client.go` | Add method to `Client` interface |
| 2 | `internal/github/client_real.go` | Implement on `StackitGitHubClient` |
| 3 | `testhelpers/github_mock_client.go` | Implement on `MockGitHubClient` (synthetic data) |
| 4 | `internal/demo/demo_github_client.go` | Implement on `GitHubClient` (fake data + `simulateDelay`) |
| 5 | `internal/app/context_test.go` | Add stub to `fakeGitHubClient` |

The client is bound to one repository at construction — methods must NOT take
`owner, repo` parameters; implementations use the stored `c.owner, c.repo`.

For GraphQL batch methods, follow the pattern in `status.go`:
- Build query with aliases via `strings.Builder`
- Execute via `executeGraphQLQuery()` (defined in `pr_operations.go`)
- Parse JSON response into typed results

## Add a New API Response Field (Backend to Frontend)

When adding a field that flows from Go through the API to the web app:

| # | File | What to do |
|---|------|------------|
| 1 | `internal/contracts/http/responses.go` | Add field to response struct |
| 2 | `internal/contracts/http/mappers.go` | Populate field in mapper function |
| 3 | `internal/api/handlers/view_assembler.go` | Fetch/compute data and pass to mapper |
| 4 | `internal/contracts/http/mappers_test.go` | Update existing test calls, add new test cases |
| 5 | `api/openapi/stackit.yaml` | Add field to OpenAPI schema |
| 6 | `apps/web/src/lib/api.ts` | Add field to TypeScript interface |
| 7 | `apps/web/src/components/...` | Use field in component |

When changing a mapper function signature, grep for all callers — typically `view_assembler.go` and `mappers_test.go`.

## Add a New Reusable Operation

When adding or refactoring a business operation that should be reusable across entry points:

| # | File/Area | What to do |
|---|-----------|------------|
| 1 | `internal/actions/<name>/` | Add request/result structs, dependency interfaces, and orchestration logic |
| 2 | `internal/cli/...` and/or `internal/api/...` | Add adapter code that parses input and renders output |
| 3 | `internal/app` / `internal/config` | Resolve config and construct concrete dependencies before calling the operation |
| 4 | Tests in the relevant packages | Use fake collaborators for action tests; keep adapter tests focused on mapping/rendering |

Use these rules:

- Do not pass `*app.Context` into new reusable orchestration code.
- Do not load config inside the operation; pass resolved values in the request.
- Do not construct GitHub clients inside the operation; inject ports/interfaces.
- Do not render terminal output inside the operation; emit structured results or events.
- **If the operation changes branch content, guard it with the worktree
  ownership check before mutating** — see below.

### Guard branch-content mutations by owning worktree

Any operation that rewrites a branch's commits or moves its ref must refuse to
run outside the worktree that owns the stack. Skipping the guard reintroduces
cross-worktree corruption: the mutation lands under a ref whose worktree still
holds the old content, and the next `modify -a` there commits the divergence.

```go
if err := actions.EnsureCanModifyNamesHere(ctx, branchName); err != nil {
    return err
}
// or, when you already hold engine.Branch values:
if err := actions.EnsureCanModifyHere(ctx, branches...); err != nil {
    return err
}
```

Call it **before** taking a snapshot or mutating anything, so a refusal is a
no-op. `create` is the example to copy (`internal/actions/create/create.go`):
it guards the current branch and, separately, the `--onto` target.

Whole-repository reconcilers — `sync` and `restack` — are the deliberate
exception. They run from any worktree and never check out a foreign branch;
they inspect each physical checkout and hold back what they cannot safely
reset instead. Do not add the guard to them.

**Source**: `internal/actions/worktree_ownership.go`. The invariant is stated in
`.claude/rules/worktree-safety.md`.

## Add a New CLI Command

| # | File | What to do |
|---|------|------------|
| 1 | `internal/actions/<name>/` | Implement or extend the reusable operation |
| 2 | `internal/cli/stack/<name>.go` | Cobra command definition (`Long` should include examples) |
| 3 | `internal/cli/stack/<name>_handlers.go` or related adapter files | Add prompt/progress/output handling if needed |
| 4 | `internal/cli/root.go` | Register the command on `rootCmd` via `AddCommand` |
| 5 | Tests in respective packages | Use action tests for orchestration and CLI tests for flags/output |

CLI commands should:

- resolve config before invoking the operation
- construct any concrete clients they need via bootstrap/runtime wiring
- convert command flags into request structs
- render results returned by the operation

Follow patterns in existing commands, but prefer the architecture in `docs/architecture.md` over older `internal/actions/*` examples when the two differ.

## Frontend Testing Notes

- Component tests use **vitest + @testing-library/react**
- Tests exist for pure/self-contained components (e.g., `status-badge.test.tsx`)
- Components requiring `RepoProvider` (e.g., `RecentlyMerged`) don't have test wrappers yet — test pure logic helpers instead
- Run `mise run web:test` for web tests, `mise run check:web` for full web validation (tests + typecheck + build)
