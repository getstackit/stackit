package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

var fallbackIndexHTML = []byte(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Stackit</title>
  </head>
  <body>
    <main>
      <h1>Stackit Web Assets Not Built</h1>
      <p>Build the frontend in <code>apps/web</code> and copy dist assets into <code>apps/server/static</code>.</p>
    </main>
  </body>
</html>
`)

// assetExts are the file extensions of real static build output (Next's hashed
// JS/CSS/media under _next/, plus root icons and manifests). A request whose
// path ends in one of these but isn't on disk is a stale or missing asset and
// must 404 — serving index.html for a <script>/<link>/<img> surfaces as a
// confusing "unexpected token '<'" (or a silently broken asset) in the browser.
// Any other dotted path is a client route (e.g. a branch named "v1.2.0") and
// falls through to the SPA shell.
var assetExts = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true, ".json": true,
	".ico": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".avif": true, ".woff": true, ".woff2": true,
	".ttf": true, ".otf": true, ".eot": true, ".txt": true, ".xml": true,
	".wasm": true, ".webmanifest": true,
}

func newStaticHandler(staticFS fs.FS) http.Handler {
	indexHTML := fallbackIndexHTML
	var fileServer http.Handler
	if staticFS != nil {
		if embeddedIndexHTML, err := fs.ReadFile(staticFS, "index.html"); err == nil {
			indexHTML = embeddedIndexHTML
		}
		fileServer = http.FileServer(http.FS(staticFS))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if cleanPath == "" || cleanPath == "." {
			writeIndex(w, indexHTML)
			return
		}

		if staticFS != nil {
			if _, err := fs.Stat(staticFS, cleanPath); err == nil {
				// Next emits hashed asset paths under _next/static/, which are
				// safe to cache aggressively. Everything else (HTML, manifests,
				// etc.) should re-validate so deploys take effect immediately.
				if strings.HasPrefix(cleanPath, "_next/static/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Not on disk: a missing static asset 404s, but every other unknown path
		// is a client-side route and gets the SPA shell. We key this on a known
		// set of asset extensions rather than "has any extension", because branch
		// routes carry dots too — /{owner}/{repo}/tree/release/v1.2.0 ends in
		// ".0", which must not be mistaken for a file and 404'd (see assetExts).
		if assetExts[strings.ToLower(path.Ext(cleanPath))] {
			http.NotFound(w, r)
			return
		}

		writeIndex(w, indexHTML)
	})
}

func writeIndex(w http.ResponseWriter, indexHTML []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}
