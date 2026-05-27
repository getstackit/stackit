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

## Security posture

The container is safe to run on a public hostname **only** behind:

1. **TLS termination.** The server speaks plain HTTP; the PaaS / reverse
   proxy in front (Railway, Fly, Cloudflare, Caddy, nginx) must terminate
   HTTPS. The `Strict-Transport-Security` header the server emits assumes
   this.
2. **An authentication gateway** until in-process auth lands. Use Tailscale,
   Cloudflare Access, an oauth2-proxy sidecar, or similar. Without one,
   any caller can read every repo's branches/diffs and trigger
   `POST /api/v1/repos/{id}/stacks/{branch}/submit`, which pushes branches
   and creates PRs using the container's GitHub credentials.

Built-in security controls (the server hardening pass that lands with
this doc revision):

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
- Branch names supplied via path/query are validated against the same
  rules `stackit` enforces locally before reaching git.

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
