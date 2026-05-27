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
| `PORT` | Listen port. Honored when `-port` isn't passed explicitly — needed for Railway, Fly, Heroku. Defaults to `8080`. |

### Flags

The most useful flags:

| Flag | Default | Purpose |
|------|---------|---------|
| `-repos-config` | _(empty)_ | Path to the JSON repos file. Required for multi-repo mode. |
| `-port` | `8080` | Listen port; overrides `$PORT`. |
| `-cors` | `http://localhost:3000,http://localhost:5173` | Comma-separated allowed CORS origins. |

Run `stackit-server -h` inside the container for the full list.

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
