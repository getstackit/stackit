package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticHandler(t *testing.T) {
	staticFS := fstest.MapFS{
		"index.html":                    {Data: []byte("<html>index</html>")},
		"assets/app.js":                 {Data: []byte("console.log('ok')")},
		"assets/app.css":                {Data: []byte(".ok{}")},
		"_next/static/chunks/app.js":    {Data: []byte("console.log('next')")},
		"_next/static/media/font.woff2": {Data: []byte("fontdata")},
	}

	handler := newStaticHandler(staticFS)
	if handler == nil {
		t.Fatal("expected static handler")
	}

	t.Run("serves index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "index") {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})

	t.Run("serves known asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "console.log") {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})

	t.Run("serves underscore-prefixed next assets", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/_next/static/chunks/app.js", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "next") {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})

	t.Run("falls back to index for client route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/stacks/main", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "index") {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})

	t.Run("returns 404 for missing file with extension", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("want 404, got %d", rr.Code)
		}
	})

	// Branch names routinely contain dots (v1.2.0, 2026.01, fix.bug). Their URLs
	// must serve the SPA shell on a cold load, not 404 as if the trailing ".0"
	// were a missing asset — that would break deep links to dotted branches,
	// which is the whole point of the path-URL scheme.
	t.Run("falls back to index for dotted branch routes", func(t *testing.T) {
		for _, p := range []string{
			"/octo/widget/tree/release/v1.2.0",
			"/octo/widget/tree/2026.01",
			"/octo/widget/stack/fix.bug",
		} {
			req := httptest.NewRequest(http.MethodGet, p, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("%s: want 200, got %d", p, rr.Code)
			}
			if !strings.Contains(rr.Body.String(), "index") {
				t.Fatalf("%s: unexpected body: %q", p, rr.Body.String())
			}
		}
	})

	t.Run("nil fs serves fallback placeholder", func(t *testing.T) {
		var nilFS fs.FS
		handler := newStaticHandler(nilFS)
		if handler == nil {
			t.Fatal("expected fallback handler")
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "Stackit Web Assets Not Built") {
			t.Fatalf("unexpected body: %q", rr.Body.String())
		}
	})
}
