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
		"index.html":     {Data: []byte("<html>index</html>")},
		"assets/app.js":  {Data: []byte("console.log('ok')")},
		"assets/app.css": {Data: []byte(".ok{}")},
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

	t.Run("nil fs returns nil handler", func(t *testing.T) {
		var nilFS fs.FS
		if got := newStaticHandler(nilFS); got != nil {
			t.Fatalf("expected nil handler, got %T", got)
		}
	})
}
