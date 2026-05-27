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

		// Return 404 for unknown files with extensions, but fallback to SPA index
		// for extension-less client routes (e.g. /stacks/main).
		if path.Ext(cleanPath) != "" {
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
