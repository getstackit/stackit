package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/api/reqid"
)

// requestIDHeader is the response header that carries the per-request ID. The
// same value is stashed on the request context (via package reqid) so
// handlers and the logger can reference it.
const requestIDHeader = reqid.HeaderName

// maxRequestBodyBytes caps every request body. POST /submit takes no body
// today; this is defense against runaway uploads against any endpoint.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// RequestIDFromContext is kept here as a re-export for callers in the api
// package; handlers in other packages import internal/api/reqid directly.
func RequestIDFromContext(ctx context.Context) string {
	return reqid.FromContext(ctx)
}

// recoverMiddleware catches panics in downstream handlers, logs them with a
// stack trace, and returns a 500. Without this, a single panicking handler
// kills the whole server process.
func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			rid := RequestIDFromContext(r.Context())
			log.Printf("panic recovered request_id=%s %s %s: %v\n%s", rid, r.Method, r.URL.Path, rec, debug.Stack()) //nolint:gosec // method and path are safe to log
			// If the handler already wrote a status, we can't change it.
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// requestIDMiddleware assigns each request an opaque ID, echoes it as a
// response header, and stashes it on the context for handlers/logging. If
// the client supplied an X-Request-ID we adopt it (so traces can be
// stitched across a proxy), capped at 64 chars and ASCII-only.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := sanitizeIncomingRequestID(r.Header.Get(requestIDHeader))
		if rid == "" {
			rid = newRequestID()
		}
		w.Header().Set(requestIDHeader, rid)
		ctx := reqid.WithValue(r.Context(), rid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// rand.Read on modern OSes only fails in pathological conditions.
		// Fall back to a timestamp so we still produce *something*.
		return "rid-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(buf[:])
}

// sanitizeIncomingRequestID accepts a caller-supplied ID only if it's short
// and ASCII-printable. Anything else gets dropped so we don't reflect attacker
// input into headers or log lines.
func sanitizeIncomingRequestID(s string) string {
	if s == "" || len(s) > 64 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return s
}

// securityHeadersMiddleware sets the baseline browser security headers we want
// on every response. CSP here matches the embedded Next.js shell; if the
// frontend starts pulling third-party scripts/images, tighten or extend
// connect-src/script-src to match.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"img-src 'self' data: https:; "+
				"style-src 'self' 'unsafe-inline'; "+
				"script-src 'self'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// maxBodyMiddleware caps each request body at maxRequestBodyBytes. Reads past
// the limit return an error from the handler's Read call, and http.MaxBytesReader
// surfaces a 413 if the handler doesn't handle it explicitly.
func maxBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers for the given allowed origins. Origins
// must be configured explicitly via -cors; there is no implicit loopback
// allowance, so the server is safe to run on a host shared with untrusted
// processes.
func corsMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.TrimRight(o, "/")] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		if _, ok := originSet[origin]; ok {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, X-Stackit-CSRF")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Max-Age", "86400")
			h.Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request with method, path, status, duration,
// and request ID.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		rid := RequestIDFromContext(r.Context())
		log.Printf("%s %s %d %s rid=%s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond), rid) //nolint:gosec // method and path are safe to log
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap allows http.ResponseController to access the underlying ResponseWriter
// for features like Flush and SetWriteDeadline (SSE).
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush forwards flush calls when the wrapped writer supports streaming.
func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
