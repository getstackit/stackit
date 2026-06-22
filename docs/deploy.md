# Deploying stackit-server

This guide covers running the hosted multi-repo `stackit-server` container.
At the time of writing the container ships **Phase 1 + Phase 6** from
[`docs/plans/server/README.md`](plans/server/README.md): multiple repos,
served via a config file, with no authentication yet. OAuth, clone-from-URL,
and per-user repo scoping arrive in later phases.

Treat the container as a single-tenant deployment behind your own
authentication (a tunnel like Tailscale, an oauth2-proxy, etc.) until the
auth phases land.

## Image

Published to [GitHub Container Registry](https://github.com/getstackit/stackit/pkgs/container/stackit-server)
as `ghcr.io/getstackit/stackit-server`.

| Tag | Source |
|-----|--------|
| `vX.Y.Z` | Cut from a release tag |
| `latest` | Most recent release |
| `main` | Latest push to the `main` branch |
| `<short-sha>` | Specific commit on `main` |

All tags are multi-arch (`linux/amd64`, `linux/arm64`).

## Configuration

The container reads its repo list from a JSON file. Mount a volume at `/data`
(or anywhere) and point `-repos-config` at a file inside it.

```json
{
  "repos": [
    { "id": "stackit",  "displayName": "Stackit",  "path": "/data/repos/stackit" },
    { "id": "myapp",    "displayName": "My App",    "path": "/data/repos/myapp" }
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | URL-safe identifier (`[a-zA-Z0-9_-]+`) used in `/api/v1/repos/{id}/...` routes |
| `path` | yes | Absolute path inside the container to a git working tree |
| `displayName` | no | Human label shown in the web UI; defaults to `id` |
| `remote` | no | Git remote name; defaults to `origin` |

You are responsible for cloning the repos into the mounted volume and
running `stackit init` inside each one before starting the container.
(Clone-from-URL + auto-init are Phase 4.)

### Environment

| Var | Purpose |
|-----|---------|
| `PORT` | Listen port. Honored when `-port` isn't passed explicitly — needed for Railway, Fly, Heroku. Defaults to `8080`. Setting this also implicitly switches the server into "public mode" (binds `0.0.0.0`, requires auth env). |
| `STACKIT_PUBLIC` | Explicit version of the same signal. Set when you mean to expose the server publicly without `$PORT` (e.g. behind a tunnel). |
| `STACKIT_READ_ONLY` | Set to `1`/`true` to serve in read-only mode: the submit endpoint is disabled and reads are served anonymously, so a configured repo can be exposed to the public without write access. See [Read-only public mode](#read-only-public-mode). Equivalent to `-read-only`. |
| `STACKIT_BASE_URL` | The canonical https:// URL the server is reachable at. Required when auth is enabled (used to build the OAuth callback URL). |
| `STACKIT_GITHUB_CLIENT_ID` | GitHub OAuth App client ID. |
| `STACKIT_GITHUB_CLIENT_SECRET` | GitHub OAuth App client secret. |
| `STACKIT_SESSION_KEY` | Base64-encoded 32-byte key for AES-GCM-encrypting access tokens at rest. Generate with `openssl rand -base64 32`. |
| `STACKIT_ALLOWED_GH_USERS` | Comma-separated GitHub logins allowed to sign in. |
| `STACKIT_ALLOWED_GH_ORG` | GitHub org slug; members may sign in. At least one of `_USERS` or `_ORG` is required. |

### Flags

The most useful flags:

| Flag | Default | Purpose |
|------|---------|---------|
| `-repos-config` | _(empty)_ | Path to the JSON repos file. Required for multi-repo mode. |
| `-port` | `8080` | Listen port; overrides `$PORT`. |
| `-bind` | `127.0.0.1` (or `0.0.0.0` if `$PORT`/`$STACKIT_PUBLIC` are set) | Interface to bind on. Pass `-bind 0.0.0.0` explicitly to expose the server on a host where the heuristics don't fire. |
| `-cors` | `http://localhost:3000,http://localhost:5173` | Comma-separated allowed CORS origins. Loopback origins are **not** allowed implicitly — list each origin you want to accept. |
| `-auth-disabled` | `false` | Skip the GitHub OAuth gate. **Refused** when `$PORT` or `$STACKIT_PUBLIC` is set. Use only for local dev or when fronted by platform auth (Tailscale, Cloudflare Access). |
| `-read-only` | `false` | Serve in read-only mode: disable the submit endpoint and serve reads anonymously. Safe to expose publicly. See [Read-only public mode](#read-only-public-mode). Also settable via `STACKIT_READ_ONLY`. |

Run `stackit-server -h` inside the container for the full list.

### GitHub OAuth setup

1. Create a GitHub OAuth App at [github.com/settings/developers](https://github.com/settings/developers).
   - Homepage URL: your `STACKIT_BASE_URL`.
   - Authorization callback URL: `${STACKIT_BASE_URL}/auth/callback`.
2. Copy the client ID + secret into env.
3. Generate a session key:
   ```bash
   openssl rand -base64 32
   ```
4. Set the allowlist (at least one of):
   ```
   STACKIT_ALLOWED_GH_USERS=jonnii,teammate
   STACKIT_ALLOWED_GH_ORG=getstackit
   ```
5. Boot the server. The startup log prints `auth: GitHub OAuth gate enabled` when configured correctly.

## Read-only public mode

Read-only mode turns the configured repos into a publicly viewable
dashboard with no write access. Enable it with `-read-only` (or
`STACKIT_READ_ONLY=1`).

In this mode:

- **The submit endpoint is removed.** `POST .../submit` — the server's only
  mutating route — is replaced with a handler that refuses with `405`. No
  code path can push branches or touch GitHub, regardless of who calls it or
  whether auth is configured. This is the key difference from
  `-auth-disabled`, which opens reads to everyone *and* leaves the write
  endpoint reachable.
- **Reads are anonymous.** The session gate is skipped, so anyone can load
  the stacks, branches, commits, diffs, and PR/CI status. The web app
  detects this via `/api/v1/config` and hides the submit button and the
  login prompt, showing a read-only banner instead.
- **The operator identity is withheld.** `currentUser` is omitted and the
  per-request GitHub "who am I" lookup is skipped, so anonymous visitors
  can't learn who runs the server and can't spend the operator's GitHub
  rate limit.

### What is exposed

Everything needed to render the dashboard: branch structure, commit
SHAs/messages/authors (git author *name* only, no email), diff patches, and
PR/CI status. For a public repo this is the same information already visible
on GitHub. **Do not point read-only mode at a private repo you don't intend
to make public** — the diff and branch data will be served to anyone who can
reach the port.

Not exposed: filesystem paths, GitHub tokens, session data, local config
values, or author emails.

### Still required for a public deployment

- **TLS termination** in front (the server speaks plain HTTP).
- The abuse bounds below stay on in read-only mode — keep them.

### Abuse bounds (on by default)

A public endpoint needs limits even when it only serves reads:

- **Per-IP rate limiting** on the API (token bucket; `429` with
  `Retry-After` when exceeded). Honors `X-Forwarded-For` from a trusted
  proxy.
- **Concurrent SSE caps** (global + per-IP) on `/events`, with a bounded
  connection lifetime and a keepalive so dead clients are reclaimed.
- **Branch-diff throttle** bounding concurrent `git` subprocesses; excess
  requests queue rather than fan out.

The defaults are generous enough for normal use. A deployment fronted by a
CDN/proxy should ensure `X-Forwarded-For` is set so the per-IP limits key on
the real client rather than collapsing every visitor onto the proxy IP.

## Security posture

The container is safe to run on a public hostname **only** behind:

1. **TLS termination.** The server speaks plain HTTP; the PaaS / reverse
   proxy in front (Railway, Fly, Cloudflare, Caddy, nginx) must terminate
   HTTPS. The `Strict-Transport-Security` header the server emits assumes
   this.
2. **An access control** for write-capable deployments. Either the built-in
   GitHub OAuth gate (`STACKIT_GITHUB_*` + an allowlist) or an external
   gateway (Tailscale, Cloudflare Access, an oauth2-proxy sidecar). Without
   one, any caller can trigger
   `POST /api/v1/repos/{id}/stacks/{branch}/submit`, which pushes branches
   and creates PRs using the container's GitHub credentials. To expose a
   repo publicly *without* this risk, run in
   [read-only mode](#read-only-public-mode), which removes the write
   endpoint entirely.

Built-in security controls (the server hardening pass that lands with
this doc revision and follow-on PRs):

- Process runs as the unprivileged `stackit` user (uid 10001) inside the
  container — neither user nor group is `root`.
- Default bind is `127.0.0.1`; the `$PORT`/`$STACKIT_PUBLIC` heuristic
  flips to `0.0.0.0` so PaaS routers can reach the port.
- Request body capped at 1 MiB; `MaxHeaderBytes` at 1 MiB; `WriteTimeout`
  30 s, `IdleTimeout` 120 s, `ReadHeaderTimeout` 10 s.
- Panic recovery middleware: a panicking handler returns 500 but cannot
  kill the process.
- Per-request `X-Request-ID` (16 random bytes) echoed back and logged.
- Security headers on every response: `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `HSTS` with a
  two-year max-age, and a CSP locked to `'self'` for scripts/connects.
- CORS allowlist is exact-match; no implicit loopback bypass.
- Per-IP request rate limiting (token bucket), concurrent-SSE caps
  (global + per-IP) with bounded lifetime, and a branch-diff concurrency
  throttle — abuse bounds for a public deployment, on by default.
- Read-only mode (`-read-only`) removes the write endpoint and serves reads
  anonymously while withholding the operator identity — see
  [Read-only public mode](#read-only-public-mode).
- Branch names supplied via path/query are validated against the same
  rules `stackit` enforces locally before reaching git.
- GitHub OAuth gate via `STACKIT_GITHUB_*` + an allowlist. Every
  `/api/*` route requires a valid session; the user's access token is
  stored AES-GCM-encrypted at rest in the session store.
- CSRF gate on every non-safe HTTP method: the server requires a
  `X-Stackit-CSRF: 1` request header on POST/PUT/PATCH/DELETE. The
  web app sends it automatically; scripts and direct API callers must
  include it.

### Audit logging

The server emits a single-line `audit action=...` log entry for every
identity-changing or mutating action. The fields are key-quoted so they
can be ingested by any structured log pipeline.

| Action | Emitted by | Fields |
|--------|------------|--------|
| `login` | `GET /auth/callback` after a successful exchange | `actor`, `user_id`, `target` (post-login URL), `request_id` |
| `denied` | `GET /auth/callback` for non-allowlisted users | `actor`, `request_id` |
| `logout` | `POST /auth/logout` | `actor`, `request_id` |
| `submit` | `POST /api/v1/repos/{id}/stacks/{branch}/submit` | `actor`, `repo`, `branch`, `request_id` |

Every request also carries a `X-Request-ID` response header with the
same value used as `request_id` in the audit lines. Pass it through
your reverse proxy (Caddy, Cloudflare) for end-to-end correlation.

### Log retention

The container writes everything to stdout/stderr; retention is the
platform's responsibility. A useful baseline:

- **Railway / Fly / Heroku** — keep the platform default (7–14 days),
  ship to an external sink (BetterStack, Datadog, S3) for longer.
- **Self-hosted Docker** — pipe `docker logs` to journald or an
  equivalent rotating sink; aim for 30+ days on the audit lines.

The `audit action=` prefix is stable; alert on `audit action=denied`
and on bursts of `audit action=submit` from a single `actor`.

### Calling the API from scripts

```bash
# Establish a session via your browser first; copy the stackit_session
# cookie into the call, then add the CSRF header for mutating requests.
curl https://stackit.example.com/api/v1/repos \
  -H "Cookie: stackit_session=$STACKIT_SESSION"

curl -X POST https://stackit.example.com/api/v1/repos/default/stacks/main/submit \
  -H "Cookie: stackit_session=$STACKIT_SESSION" \
  -H "X-Stackit-CSRF: 1"
```

## Local smoke test

```bash
# Clone something into ./data/repos/ first
mkdir -p data/repos
git clone https://github.com/getstackit/stackit data/repos/stackit
(cd data/repos/stackit && stackit init)
cat > data/repos.json <<'EOF'
{ "repos": [ { "id": "stackit", "path": "/data/repos/stackit" } ] }
EOF

docker run --rm -p 8080:8080 \
  -v "$(pwd)/data:/data" \
  ghcr.io/getstackit/stackit-server:latest \
  -repos-config /data/repos.json

# In another terminal:
curl -s localhost:8080/api/v1/repos | jq
open http://localhost:8080
```

## Railway

1. **Service** — deploy from image `ghcr.io/getstackit/stackit-server:main`
   (or pin to `:latest` / `:vX.Y.Z`).
2. **Volume** — mount at `/data`. Use at least a few GB; clones land here.
3. **Variables** — Railway injects `PORT` automatically; no other vars are
   required for the Phase 1 container.
4. **Start command** — leave blank to use the image's `ENTRYPOINT`, then set
   the **Command** (Railway's arg override) to:
   ```
   -repos-config /data/repos.json
   ```
5. **Seed the volume** — clone repos into `/data/repos/<id>/`, run
   `stackit init` in each, and create `/data/repos.json`. Do this once via
   a one-shot Railway shell session, then redeploy. (A `repo init` API
   endpoint replaces this manual step in Phase 4.)

## Pushing a snapshot without a release

For ad-hoc dogfooding (e.g. validating a feature branch on Railway before
merging) you can push a multi-arch image straight from your laptop without
cutting a release tag:

```bash
gh auth refresh -s write:packages,read:packages    # one-time
mise run server:publish:dev                        # pushes :dev (+arch tags)
TAG=preview mise run server:publish:dev            # pushes :preview
```

The script wraps `goreleaser release --snapshot`, then re-tags and pushes
the per-arch images and a manifest under `$TAG` (default `dev`). Skip the
~90 s rebuild with `SKIP_BUILD=1` when you only want to re-push.

## Updating

- Pin to `:vX.Y.Z` for reproducible deploys; bump tag and redeploy.
- Use `:main` for continuous deploys off the trunk — Railway can be
  configured to redeploy whenever the GHCR tag is updated.
- Use `:<short-sha>` to roll back to a known-good commit.

## What's not in this container yet

These all land in later phases:

- **GitHub OAuth** (Phase 3) — every request currently hits the server with
  no identity, and there is no per-user repo scoping.
- **Clone-from-URL** (Phase 4) — repos must be pre-cloned into the mounted
  volume.
- **Persistent registry** (Phase 2) — runtime `POST /api/v1/repos` is not
  available; edits to `repos.json` require a restart.

See [`docs/plans/server/README.md`](plans/server/README.md) for the full
phase plan.
