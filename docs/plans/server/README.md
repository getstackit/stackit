# Hosted multi-tenant stackit-server

Plan for turning the single-repo dev server into a hosted multi-user service
deployable to Railway as a public container image
(`ghcr.io/getstackit/stackit-server`).

## Context

Stackit-server today is a local dev tool: one binary, bound to one repo via
`-cwd`, designed to run on the same machine as the user's working tree. The
goal is to deploy a hosted instance where:

- Multiple users sign in with GitHub OAuth
- Each user adds their own repositories by clone URL
- The server clones repos into a persistent volume and serves a stack UI for
  each one
- The whole thing ships as a public container

This is a substantial change because the current server is single-tenant and
single-repo at every layer: bootstrap, engine, all 6 handlers, the ref watcher,
the API routes, the web client, and the web UI. Containerization is the *last*
step — most of the work is making the server multi-repo and multi-user first.

The plan is delivered as 6 phases, each shippable as its own stack of PRs so
review stays focused. If we want to dogfood Railway sooner, Phase 1 + Phase 6
alone is enough for a private single-user multi-repo deployment.

## Phased delivery

### Phase 1 — Multi-repo registry (no auth, no cloning)

Goal: server holds N engines keyed by `repoID`, with all API routes scoped by
repo. Repos are listed via a config file at startup (no runtime add yet).

**New package: `internal/api/registry`**

- `Registry` struct: `map[string]*RepoEntry` + `sync.RWMutex`
- `RepoEntry` holds `Engine`, `RefWatcher`, `RepoRoot`, `Remote`, `DisplayName`
- `Get(repoID) (*RepoEntry, bool)`, `List() []*RepoEntry`, `Add`/`Remove` (used
  by later phases)
- `Close()` stops every `RefWatcher` and closes broadcasters

**Refactor handlers (`internal/api/handlers/*.go`)**

Each of the 6 handlers (`ViewHandler`, `RepoHandler`, `StacksHandler`,
`BranchesHandler`, `BranchDiffHandler`, `EventsHandler`) currently embeds an
`engine.Engine` field. Change them to embed the `*Registry` and resolve the
engine from the request:

```go
entry, ok := h.registry.Get(repoIDFromRequest(r))
if !ok { http.NotFound(w, r); return }
// use entry.Engine, entry.Broadcaster, ...
```

**Route shape**

Move every route under a repo prefix:

```
GET  /api/v1/repos                          # list repos (Phase 1: from config)
GET  /api/v1/repos/{repoID}/view
GET  /api/v1/repos/{repoID}/repo
GET  /api/v1/repos/{repoID}/stacks
GET  /api/v1/repos/{repoID}/branches
GET  /api/v1/repos/{repoID}/branch-diff
GET  /api/v1/repos/{repoID}/events          # SSE — per-repo stream
```

Drop the legacy `/api` prefix during this phase (it served single-repo only).

**Per-repo broadcaster + watcher**

Currently `Server.broadcaster` is a single `EventBroadcaster` and
`Server.refWatcher` watches one `RepoRoot`. Move both onto `RepoEntry` so
events stay scoped: the watcher attached to repo A only rebuilds engine A and
publishes to A's broadcaster.

**Server bootstrap (`apps/server/main.go`)**

Replace the `-cwd` flag with `-repos-config <path>` pointing at a small TOML
file:

```toml
[[repos]]
id = "stackit"
path = "/repos/stackit"
remote = "origin"

[[repos]]
id = "myproj"
path = "/repos/myproj"
```

At startup, iterate the config, call `app.GetContext` with each path's `Cwd`,
and `registry.Add(repoEntry)`. Keep `-cwd` as a deprecated shortcut that
synthesises a one-entry config so the existing dev workflow keeps working.

**Web app (`apps/web/`)**

- New page `src/app/page.tsx` becomes a repo picker; existing UI moves under
  `src/app/[repoId]/` so URLs carry the repo
- `RepoProvider` (`src/components/providers/repo-provider.tsx`) takes
  `repoId` from the route and passes it to every fetch
- Update `src/lib/api.ts`: every function (`fetchView`, `fetchBranches`, ...)
  takes `repoId` and builds `/api/v1/repos/{repoId}/...`
- Update `useSSE` hook (`src/lib/use-sse.ts`) to subscribe to the per-repo
  events endpoint

**Critical files**

- `apps/server/main.go`
- `internal/api/server.go`
- `internal/api/handlers/{view,repo,stacks,branches,branch_diff,events}.go`
- `internal/api/watcher/watcher.go` (no logic change; instantiated per-repo)
- `internal/api/middleware.go` (path-prefix matcher handles the new shape)
- `internal/contracts/http/responses.go` (add `RepoID` to relevant responses)
- `apps/web/src/lib/api.ts`, `src/components/providers/repo-provider.tsx`,
  `src/app/**`

### Phase 2 — Persistent registry (SQLite on a volume)

Goal: replace the startup config file with a database so repos can be added
and removed at runtime, and so Phase 3 has somewhere to put users.

**New package: `internal/store`**

- Driver: `modernc.org/sqlite` (pure-Go; keeps `CGO_ENABLED=0` so multi-arch
  builds stay trivial — important for goreleaser parity with the rest of the
  binaries)
- Single file at `$STACKIT_DATA_DIR/stackit.db` (default `/data` in container)
- Migrations run at boot via a tiny in-process migrator (no external tool)
- Schema (Phase 2 subset):

```sql
CREATE TABLE repos (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    path         TEXT NOT NULL,
    clone_url    TEXT,
    remote       TEXT NOT NULL DEFAULT 'origin',
    created_at   INTEGER NOT NULL
);
```

**Server bootstrap**

On startup, query `repos`, build a `RepoEntry` for each row, populate the
registry. `-repos-config` becomes a one-time bootstrap import
(`stackit-server import-repos <file>`), so existing single-repo dev still
works without a DB.

**New write endpoints**

```
POST   /api/v1/repos                # body: {displayName, path}
DELETE /api/v1/repos/{repoID}
```

No clone logic yet — `path` must point at an existing directory inside the
container. This keeps Phase 2 small; Phase 4 adds clone-from-URL.

### Phase 3 — GitHub OAuth + user model

Goal: every request is associated with a logged-in user; repos are scoped to
their owner.

**New package: `internal/auth`**

- OAuth client config from env: `STACKIT_GH_CLIENT_ID`,
  `STACKIT_GH_CLIENT_SECRET`, `STACKIT_OAUTH_REDIRECT_URL`
  (defaults to `<host>/api/v1/auth/github/callback`)
- Cookie-signed sessions using `gorilla/securecookie` or `crypto/hmac`
  directly; cookie name `stackit_session`, `HttpOnly`, `Secure`,
  `SameSite=Lax`
- Session secret from `STACKIT_SESSION_SECRET` (32+ bytes, required)
- Token at rest: encrypt `github_access_token` with a key from
  `STACKIT_TOKEN_ENC_KEY` (AES-GCM)

**Schema additions**

```sql
CREATE TABLE users (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    github_id                   INTEGER NOT NULL UNIQUE,
    github_login                TEXT NOT NULL,
    github_access_token_enc     BLOB NOT NULL,
    created_at                  INTEGER NOT NULL
);

ALTER TABLE repos ADD COLUMN owner_user_id INTEGER REFERENCES users(id);
CREATE INDEX repos_owner_idx ON repos(owner_user_id);
```

**New routes**

```
GET  /api/v1/auth/github/login      # 302 to GitHub authorize URL
GET  /api/v1/auth/github/callback   # exchanges code, sets session cookie
POST /api/v1/auth/logout            # clears cookie
GET  /api/v1/me                     # returns logged-in user, used by web
```

**Auth middleware**

Wrap everything under `/api/v1/repos` and `/api/v1/me` with middleware that:

- Parses `stackit_session` cookie → resolves `User`
- Injects `User` into `context.Context`
- Returns 401 if missing/invalid

The registry's `Get(repoID)` becomes `GetForUser(userID, repoID)` so users
can't access someone else's repo by guessing IDs.

**Per-user GitHub client**

The shared `runtimeCtx.GitHub()` goes away. Each `RepoEntry` instead holds a
factory that builds a GitHub client using the owner's encrypted token. The
single-instance GitHub client today is bound to whatever token the server
process found; that doesn't work in multi-tenant.

### Phase 4 — Clone on add

Goal: users add a repo by pasting a clone URL; the server clones it into the
volume using their GitHub token.

**`POST /api/v1/repos`** body becomes:

```json
{ "cloneUrl": "https://github.com/owner/name.git", "displayName": "name" }
```

Server steps:

1. Generate `repoID` = ULID
2. `mkdir -p $STACKIT_DATA_DIR/repos/{userID}/{repoID}`
3. Shell out to `git clone --filter=blob:none <authenticatedUrl> <path>`
   using the user's token in the URL, then immediately scrub credentials from
   `.git/config` to avoid leaving the token on disk
4. Insert row in `repos` with `owner_user_id`, `clone_url`, `path`
5. Construct `RepoEntry` and call `registry.Add`

**`DELETE /api/v1/repos/{repoID}`** removes the registry entry, stops the
watcher, deletes the DB row, and `os.RemoveAll`s the working tree.

**`POST /api/v1/repos/{repoID}/fetch`** runs `git fetch origin` for the
working tree (Phase 4 nicety so users can pull remote updates without a
restart).

The project already shells out to `git` everywhere (per `internal/git`), so
clone/fetch slot into the same `git.Runner` rather than pulling in `go-git`.

### Phase 5 — Web app multi-repo UX

Goal: actual UX for sign-in, repo list, add/remove.

- New `/login` page with a single "Sign in with GitHub" button → hits
  `/api/v1/auth/github/login`
- `RepoProvider` becomes a two-level provider: outer fetches `/api/v1/me`,
  inner fetches the user's repo list and the selected repo's state
- New `/repos` page: list with delete buttons + an "Add repo" form (paste
  clone URL, free-text display name, submit → POST `/api/v1/repos`)
- Header gets a repo switcher dropdown (reuses existing shadcn `Select`)
- Empty state on `/`: if user has no repos, link to `/repos`

### Phase 6 — Container build + publish

Goal: every release tag and every push to `main` produces a multi-arch image
at `ghcr.io/getstackit/stackit-server`.

**New file: `Dockerfile.server`**

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache git ca-certificates tzdata
COPY stackit-server /usr/local/bin/stackit-server
EXPOSE 8080
ENV STACKIT_DATA_DIR=/data
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/stackit-server"]
```

Alpine is chosen (not distroless-static) because the server shells out to
`git` — distroless-static has no git binary. Image size with git is ~30MB.

**`.goreleaser.yml` additions**

```yaml
dockers:
  - id: server-amd64
    image_templates:
      - 'ghcr.io/getstackit/stackit-server:{{ .Version }}-amd64'
    ids: [server]
    dockerfile: Dockerfile.server
    use: buildx
    goos: linux
    goarch: amd64
    build_flag_templates:
      - "--platform=linux/amd64"
      - "--label=org.opencontainers.image.source=https://github.com/getstackit/stackit"
      - "--label=org.opencontainers.image.version={{.Version}}"
      - "--label=org.opencontainers.image.revision={{.Commit}}"
  - id: server-arm64
    image_templates:
      - 'ghcr.io/getstackit/stackit-server:{{ .Version }}-arm64'
    ids: [server]
    dockerfile: Dockerfile.server
    use: buildx
    goos: linux
    goarch: arm64
    build_flag_templates:
      - "--platform=linux/arm64"
      # ...same labels

docker_manifests:
  - name_template: 'ghcr.io/getstackit/stackit-server:{{ .Version }}'
    image_templates:
      - 'ghcr.io/getstackit/stackit-server:{{ .Version }}-amd64'
      - 'ghcr.io/getstackit/stackit-server:{{ .Version }}-arm64'
  - name_template: 'ghcr.io/getstackit/stackit-server:latest'
    image_templates:
      - 'ghcr.io/getstackit/stackit-server:{{ .Version }}-amd64'
      - 'ghcr.io/getstackit/stackit-server:{{ .Version }}-arm64'
```

**`.github/workflows/release.yml` additions**

Inside the `goreleaser` job, before the goreleaser step:

```yaml
- name: Set up QEMU
  uses: docker/setup-qemu-action@v3
- name: Set up Docker Buildx
  uses: docker/setup-buildx-action@v3
- name: Log in to GHCR
  uses: docker/login-action@v3
  with:
    registry: ghcr.io
    username: ${{ github.actor }}
    password: ${{ secrets.GITHUB_TOKEN }}
```

`packages: write` is already on the workflow, so no permissions change is
needed.

**New workflow: `.github/workflows/docker-main.yml`**

Triggers on every push to `main`. Runs only the goreleaser docker step
(`goreleaser release --snapshot --skip=archive,brew,checksum`) and publishes
`ghcr.io/getstackit/stackit-server:main` and `:<short-sha>`. Lets Railway
auto-deploy from `main` without cutting a release.

**Railway deployment notes**

- Service: deploy from `ghcr.io/getstackit/stackit-server:latest`
- Volume: mount at `/data`
- Env vars: `STACKIT_GH_CLIENT_ID`, `STACKIT_GH_CLIENT_SECRET`,
  `STACKIT_OAUTH_REDIRECT_URL`, `STACKIT_SESSION_SECRET`,
  `STACKIT_TOKEN_ENC_KEY`, `PORT` (Railway sets this — server already reads
  `-port`, add fallback to `$PORT`)
- Add a `docs/deploy.md` capturing the above so users can self-host

## Reused utilities and patterns

- `app.GetContext` → still the right entry for constructing an `Engine` for a
  given path; just called N times instead of once (Phase 1)
- `git.Runner` (`internal/git`) already shells out for every git op — reuse
  for clone/fetch in Phase 4
- `watcher.RefWatcher` is a self-contained per-path watcher — just
  instantiated per `RepoEntry`
- `handlers.EventBroadcaster` is a self-contained pub/sub — one per
  `RepoEntry`
- shadcn `Select` and existing `RepoProvider` pattern in `apps/web` cover the
  repo switcher UI

## Verification

### Phase 1 (multi-repo bootstrap)

```bash
mise run build
mkdir -p /tmp/r1 /tmp/r2
(cd /tmp/r1 && git init -b main && touch x && git add x && git commit -m init)
(cd /tmp/r2 && git init -b main && touch y && git add y && git commit -m init)
cat > /tmp/repos.toml <<EOF
[[repos]]
id="r1"
path="/tmp/r1"
[[repos]]
id="r2"
path="/tmp/r2"
EOF
./bin/stackit-server -repos-config /tmp/repos.toml &

curl -s localhost:8080/api/v1/repos | jq          # lists r1 + r2
curl -s localhost:8080/api/v1/repos/r1/view | jq  # view scoped to r1
curl -s localhost:8080/api/v1/repos/r2/view | jq  # view scoped to r2
curl -s localhost:8080/api/v1/repos/nope/view     # 404
```

Web: open `http://localhost:8080`, confirm picker shows both repos, switching
updates the URL and the data.

### Phase 2 (persistent registry)

`POST` a new repo, restart the server, confirm it persists.

### Phase 3 (auth)

- `curl localhost:8080/api/v1/repos` without cookie → 401
- Walk through OAuth in a browser, confirm cookie is set, hit `/me`
- User A cannot `GET /api/v1/repos/{B's repoID}/view` (returns 404 to avoid
  leaking existence)

### Phase 4 (clone)

- `POST /api/v1/repos` with a public repo URL, confirm clone lands under
  `$STACKIT_DATA_DIR/repos/<userID>/<repoID>` and stack data loads
- `DELETE` the repo, confirm directory + DB row both gone

### Phase 5 (web UX)

- Manual: sign in, add a repo via the form, see it in the switcher, delete it
- `mise run web:test` for component tests; `mise run check:web` before merging

### Phase 6 (container)

```bash
# Local snapshot build
goreleaser release --snapshot --clean --skip=publish

# Run the locally-built image
docker run --rm -p 8080:8080 \
  -v $(pwd)/data:/data \
  -e STACKIT_GH_CLIENT_ID=... \
  -e STACKIT_GH_CLIENT_SECRET=... \
  -e STACKIT_SESSION_SECRET=$(openssl rand -hex 32) \
  -e STACKIT_TOKEN_ENC_KEY=$(openssl rand -hex 32) \
  ghcr.io/getstackit/stackit-server:0.0.0-next-amd64

# Multi-arch check after a real release
docker buildx imagetools inspect ghcr.io/getstackit/stackit-server:latest
# Should show both linux/amd64 and linux/arm64 manifests
```

End-to-end on Railway: deploy the published image, set env vars, mount a
volume at `/data`, walk through sign-in → add repo → view stack to confirm
the container works in a real environment.

## Out of scope

- Migrating CLI users to use the hosted server (CLI keeps using local engine)
- Webhook-driven refresh (currently the watcher only sees filesystem changes
  inside the container; GitHub webhooks would be a follow-up phase)
- Background fetches on a schedule
- The existing single-tenant `-cwd` mode is preserved as a dev shortcut in
  Phase 1, then removed once Phase 4 lands (configurable repos via API
  obviates it)
