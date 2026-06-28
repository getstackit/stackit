# Web App Architecture

> **Experimental:** The web dashboard is under active development and may change significantly.

The stackit web app is a dashboard for visualizing stacked branches. It displays branch stacks in a swimlane layout organized by owner, with real-time updates via server-sent events.

## Tech Stack

- **Next.js 16** (React 19) with static export
- **TypeScript** (strict mode)
- **Tailwind CSS 4** with OKLch color space
- **shadcn/ui** (New York style, Lucide icons)
- **Motion** (Framer Motion replacement) for animations
- **Vitest** + Testing Library for tests
- **pnpm** for package management

## Architecture

The web app is built as a **static export** (no server-side rendering). The built output (`apps/web/out/`) is copied into `apps/server/static/` and embedded in the Go binary via `//go:embed static`. The API server serves these static files alongside the `/api/` endpoints, creating a single self-contained binary.

```
Browser → Go API server → /api/* (JSON endpoints)
                        → /*    (embedded static files)
```

## Directory Structure

```
apps/web/
├── src/
│   ├── app/                    Pages and layouts
│   │   ├── layout.tsx          Root layout (providers, fonts, metadata)
│   │   ├── [[...slug]]/page.tsx Optional catch-all → renders AppShell (SPA)
│   │   └── globals.css         Global styles, CSS variables, animations
│   ├── components/
│   │   ├── app-shell.tsx       Path → picker vs per-repo view dispatcher
│   │   ├── providers/
│   │   │   ├── repo-provider.tsx   Main data context (repo, stacks, events)
│   │   │   └── theme-provider.tsx  Light/dark/system theme context
│   │   ├── ui/                 Reusable UI primitives (shadcn)
│   │   ├── status/             Status badge components
│   │   ├── stack-tree/         SVG tree visualization
│   │   ├── branch-detail/      Branch info panel components
│   │   ├── stack-column.tsx    Vertical stack of branch cards
│   │   ├── stack-list.tsx      Stack list container
│   │   ├── owner-swimlane.tsx  Horizontal owner grouping
│   │   ├── swimlane-label.tsx  Owner header with avatar
│   │   ├── recently-merged.tsx Trunk commit history
│   │   └── event-feed.tsx      Activity feed
│   ├── hooks/
│   │   ├── use-confetti.ts     Confetti animation on PR merge
│   │   └── use-previous.ts     Track previous state value
│   ├── lib/
│   │   ├── api.ts              API client, fetch functions, type definitions
│   │   ├── repo-route.ts       Parse/build GitHub-style /{owner}/{repo}/... URLs
│   │   ├── use-sse.ts          SSE hook for real-time updates
│   │   ├── diff-views.ts       View snapshot diffing for event detection
│   │   ├── utils.ts            cn() helper for class merging
│   │   └── time.ts             Time formatting utilities
│   └── test/
│       └── setup.ts            Vitest setup (jest-dom)
├── next.config.ts              Static export configuration
├── vitest.config.ts            Test configuration (jsdom)
├── components.json             shadcn/ui configuration
├── tsconfig.json               TypeScript config (@/ path alias)
└── package.json                Dependencies and scripts
```

## Component Hierarchy

```
layout.tsx
└── ThemeProvider → RepoProvider → TooltipProvider
    └── [[...slug]]/page.tsx → AppShell
        ├── Header (repo info, refresh, theme toggle)
        ├── LeftPanel (scrollable)
        │   ├── OwnerSwimlane ("You")
        │   │   └── StackColumn (per stack)
        │   │       ├── BranchCard (stacked with overlap)
        │   │       └── StackStatusFooter
        │   ├── OwnerSwimlane (teammates)
        │   │   └── ...
        │   ├── TrunkLine (divider)
        │   └── RecentlyMerged (trunk commit history)
        └── RightPanel (400px fixed)
            ├── BranchDetail / StackDetailPanel
            └── EventFeed
```

## Data Flow

1. **RepoProvider** calls `fetchView(repoRef)` on mount → GET `/api/v1/repos/{owner}/{repo}/view`
2. Response contains: repo metadata, all stack details, recently merged commits
3. **SSE hook** (`useSSE`) connects to `/api/v1/repos/{owner}/{repo}/events` for real-time updates
4. On SSE event (`stacks_updated`, `branch_changed`, `refresh`, `branch_switched`), RepoProvider refetches
5. **View diffing** (`diff-views.ts`) compares old and new snapshots to generate `FeedEvent` objects
6. Events are displayed in the **EventFeed** component (capped at 100 events)

## API Integration

### Client

All fetch functions live in `src/lib/api.ts`. Per-repo endpoints are keyed by
GitHub coordinates: the functions take a `RepoRef` (`{ owner, repo }`) and build
`/api/v1/repos/{owner}/{repo}/...` URLs via the `repoPath` helper. `RepoProvider`
holds a memoized `repoRef` and exposes it through `useRepo()`.

| Function | Method | Endpoint | Purpose |
|----------|--------|----------|---------|
| `fetchRepos()` | GET | `/api/v1/repos` | Repos the caller may see (picker) |
| `onboardRepo(owner, name)` | POST | `/api/v1/repos` | Clone & serve a GitHub repo |
| `fetchView(ref)` | GET | `/api/v1/repos/{owner}/{repo}/view` | Combined dashboard payload |
| `fetchRepo(ref)` | GET | `/api/v1/repos/{owner}/{repo}/repo` | Repository metadata |
| `fetchStacks(ref)` | GET | `/api/v1/repos/{owner}/{repo}/stacks` | All stack summaries |
| `fetchStack(ref, root)` | GET | `/api/v1/repos/{owner}/{repo}/stacks/{root}` | Single stack detail |
| `fetchBranch(ref, name)` | GET | `/api/v1/repos/{owner}/{repo}/branches/{name}` | Single branch detail |
| `fetchBranchDiff(ref, name)` | GET | `/api/v1/repos/{owner}/{repo}/branch-diff/{name}` | Raw branch patch |
| `submitStack(ref, root)` | POST | `/api/v1/repos/{owner}/{repo}/stacks/{root}/submit` | Trigger stack submission |

The repo picker (`src/components/repo-picker/`) renders an `AddRepository`
form that calls `onboardRepo`; on success it navigates to the new repo's
`/{owner}/{repo}` path. The form self-hides on a read-only server
(`useConfig().readOnly`) since writes are refused there. See
[Repository onboarding](./deploy.md#repository-onboarding).

In single-repo mode (`useConfig().singleRepo`, set by a server with no
`STACKIT_DATABASE_URL`), the picker is skipped entirely: `RepoPicker`
`router.replace`s straight to the sole repo once it loads, so Back doesn't
bounce through it. It falls back to the normal render if that repo has no
GitHub coordinates (no remote) and so can't be addressed in the path UI.

The API base URL is configured via `NEXT_PUBLIC_API_URL`. Default is empty
(same-origin) so the embedded production build, served by the Go binary,
hits whatever host the page came from. Set it explicitly when running
`next dev` on `:3000` against a Go server on a different port — e.g.
`NEXT_PUBLIC_API_URL=http://localhost:8080` in `apps/web/.env.local`.

### Type Contracts

API response types in the frontend (`src/lib/api.ts`) mirror Go structs in `internal/contracts/http/responses.go`. When modifying API shapes, update both.

### SSE Events

The SSE hook in `src/lib/use-sse.ts` takes a `RepoRef` and subscribes to
`/api/v1/repos/{owner}/{repo}/events`:

| Event Type | Trigger |
|------------|---------|
| `stacks_updated` | Stack structure changed |
| `branch_changed` | Branch metadata updated |
| `refresh` | General refresh signal |
| `branch_switched` | User changed branches in CLI |

## Styling

### Tailwind CSS 4

Styles use Tailwind CSS 4 with the new `@import` syntax. All custom CSS variables are in `src/app/globals.css`.

### Color System

Colors use the **OKLch** color space via CSS custom properties:

```css
--background: oklch(1 0 0);           /* Light mode */
--background: oklch(0.145 0.015 286); /* Dark mode */
```

Status colors: green (shippable), amber (pending), red (blocked), gray (incomplete).

### Theme

Light/dark/system themes managed by `ThemeProvider`. Toggle via `ThemeToggle` component. Theme preference persisted to `localStorage`.

### Animations

Defined in `globals.css`: `shimmer`, `pulse-dot`, `checkmark-draw`, `shake`, `edge-flow`, `gradient-shift`, `mesh-float`, `fade-in-up`. All animations respect `prefers-reduced-motion`.

### shadcn/ui

Components generated with `shadcn` CLI using New York style. Config in `components.json`. Use the `cn()` helper from `src/lib/utils.ts` for conditional class merging.

## Development

### Running Locally

```bash
# Both server + web (recommended) — local single-repo mode: serves the
# current git repo and the UI opens it directly (no picker).
mise run dev

# Both server + web in DB-backed multi-tenant mode (the hosted model;
# shows the repo picker). Run `mise run db:up` first.
mise run dev:hosted

# Web only (needs API running separately)
mise run web:dev

# Server only
go run ./apps/server --port 8080
```

`dev` and `dev:hosted` share `Procfile.dev`; they differ only in whether
`STACKIT_DATABASE_URL` is set (the `dev` task clears it). When the server
reports `singleRepo` via `/api/v1/config`, the web client skips the picker
and navigates straight to the sole repo.

The web dev server runs at `http://localhost:3000` and proxies API requests to `http://localhost:8080`.

### Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `NEXT_PUBLIC_API_URL` | `http://localhost:8080` | API server URL |

### Build & Deploy

```bash
# Build and run the embedded server locally
mise run server:run:embedded

# Or build the embedded binary without running it
mise run server:build:embedded

# Optional overrides
STACKIT_SERVER_PORT=9090 mise run server:run:embedded
STACKIT_SERVER_CWD=/path/to/repo mise run server:run:embedded
```

### Testing

```bash
# Run all web tests
mise run web:test

# Run with watch mode
cd apps/web && pnpm test:watch
```

Tests use **Vitest** with **jsdom** environment and **Testing Library**. Test files are co-located in `__tests__/` directories alongside their source:

```
src/lib/__tests__/api.test.ts
src/lib/__tests__/utils.test.ts
src/hooks/__tests__/use-previous.test.ts
src/components/stack-tree/__tests__/tree-layout.test.ts
src/components/status/__tests__/status-badge.test.tsx
```

## Common Tasks

### Add a New Component

1. For shadcn primitives: `cd apps/web && pnpm dlx shadcn@latest add <component>`
2. For project components: create in `src/components/`, co-locate tests in `__tests__/`
3. Import with `@/components/...` path alias

### Add an API Endpoint Consumer

1. Add the fetch function to `src/lib/api.ts`
2. Add TypeScript types matching the Go contract in `internal/contracts/http/responses.go`
3. Use the function from a component or hook

### Modify Styling

1. For theme colors: edit CSS variables in `src/app/globals.css`
2. For component styles: use Tailwind utility classes
3. For new animations: add `@keyframes` in `globals.css`, use via Tailwind `animate-*` class

### Routing (single SPA shell)

The app is a single client-rendered shell, not per-route pages. `output: "export"`
emits one `index.html` from an optional catch-all route, `src/app/[[...slug]]/page.tsx`
(a server component that exports `generateStaticParams` returning `[{ slug: [] }]`
and renders `<AppShell>`). `AppShell` (`src/components/app-shell.tsx`) reads
`usePathname()` and dispatches: an empty path shows the repo picker; `/{owner}/{repo}`
mounts `RepoProvider` + `RepoView`. Deep links work on hard refresh because the Go
server serves the same `index.html` for extension-less paths (SPA fallback in
`internal/api/static.go`).

To add a genuinely separate page you would add another route folder, but prefer
extending the path scheme parsed in `src/lib/repo-route.ts`.

### Branch Selection & URL State

URLs mirror GitHub and are the source of selection state:

| Path | View |
|------|------|
| `/{owner}/{repo}` | Repo home, no selection |
| `/{owner}/{repo}/tree/{branch}` | A branch (branch may contain slashes) |
| `/{owner}/{repo}/stack/{rootBranch}` | A whole stack |
| `/{owner}/{repo}/pull/{number}` | A PR, resolved client-side to its branch |

`src/lib/repo-route.ts` is the single place that parses and builds these paths
(`parseRepoPath` / `buildRepoPath`), encoding branch/stack names per-segment so
slashes stay as separators. `useUrlSelection` (`src/hooks/use-url-selection.ts`)
reads the initial selection from the path and updates it with
`history.replaceState` (no history entry per in-repo selection, matching the
prior behavior). A `pull` selection has no branch until the view loads; the hook
resolves it to the branch carrying that PR number.
