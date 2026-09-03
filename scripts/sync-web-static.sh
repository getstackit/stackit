#!/usr/bin/env sh

set -eu

cd "$(dirname "$0")/.."

mkdir -p apps/server/static
find apps/server/static -mindepth 1 -maxdepth 1 ! -name .gitignore -exec rm -rf {} +

# The web build output is gitignored, so a fresh clone has none. Failing here
# would break `mise run build` — which builds the CLI first — leaving a usable
# bin/stackit behind a non-zero exit. Emit a placeholder instead so the server
# still compiles (//go:embed needs the directory populated) and says plainly
# why the UI is missing.
if [ ! -f apps/web/out/index.html ]; then
  echo "apps/web/out/index.html is missing; embedding a placeholder page." >&2
  echo "Run 'mise run web:build' (or 'pnpm --filter @stackit/web build') for the real UI." >&2
  cat > apps/server/static/index.html <<'HTML'
<!doctype html>
<meta charset="utf-8">
<title>stackit — web UI not built</title>
<body style="font: 14px system-ui; margin: 3rem auto; max-width: 34rem">
  <h1>Web UI not built</h1>
  <p>This server was built without the web assets, so a placeholder is embedded
     in their place. The API is unaffected.</p>
  <p>To build the real UI:</p>
  <pre>mise run web:build
mise run web:sync-static</pre>
</body>
HTML
  exit 0
fi

cp -R apps/web/out/. apps/server/static/
