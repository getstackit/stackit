package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func newStaticHandler(staticFS fs.FS) http.Handler {
	if staticFS == nil {
		return nil
	}

	indexHTML, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return nil
	}

	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if cleanPath == "" || cleanPath == "." {
			writeIndex(w, indexHTML)
			return
		}

		if _, err := fs.Stat(staticFS, cleanPath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
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
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(indexHTML)
}
