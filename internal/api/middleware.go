package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/url"
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
			slog.Error("panic recovered", "request_id", rid, "method", r.Method, "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack())) //nolint:gosec // values emitted as structured slog fields
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
// on every response. The CSP is computed once at server startup from the
// embedded static site (see buildCSP) so every web build is allowed without
// a manual hash paste-in.
func securityHeadersMiddleware(csp string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("Content-Security-Policy", csp)
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

// corsMiddleware adds CORS headers for the given allowed origins. Origins are
// configured explicitly via -cors. When allowLoopback is set (only on a
// loopback bind, never when the server is exposed), any loopback origin
// (localhost / 127.0.0.1 / [::1] on any port, http or https) is also accepted —
// local dev web servers get an environment-assigned port, so a fixed allowlist
// can't keep up. An exposed server keeps the strict explicit allowlist, so it
// stays safe on a host shared with untrusted processes.
func corsMiddleware(allowedOrigins []string, allowLoopback bool, next http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.TrimRight(o, "/")] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		_, allowed := originSet[origin]
		if !allowed && allowLoopback && isLoopbackOrigin(origin) {
			allowed = true
		}
		if allowed {
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

// isLoopbackOrigin reports whether origin is an http(s) URL whose host is a
// loopback address (localhost, 127.0.0.0/8, or ::1), on any port. Used to
// accept the environment-assigned port of a local dev web server without an
// explicit allowlist entry. Only consulted on a loopback bind.
func isLoopbackOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loggingMiddleware logs each request with method, path, status, duration,
// and request ID. The level reflects the response status so 5xx surfaces
// as error and 4xx as warn in log aggregators.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		level := slog.LevelInfo
		switch {
		case sw.status >= 500:
			level = slog.LevelError
		case sw.status >= 400:
			level = slog.LevelWarn
		}
		slog.LogAttrs(r.Context(), level, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("duration", time.Since(start).Round(time.Millisecond)),
			slog.String("request_id", RequestIDFromContext(r.Context())),
		)
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
