// Package reqid holds the request-ID context plumbing shared between the
// middleware that mints the ID (internal/api) and the handlers that want
// to include it in audit log lines (internal/api/handlers). Lives in its
// own package so neither side has to import the other.
package reqid

import "context"

// HeaderName is the request/response header that carries the per-request
// ID. Kept here so the middleware and the http client agree.
const HeaderName = "X-Request-ID"

type ctxKey struct{}

// FromContext returns the request ID associated with ctx, or "" if none
// was attached (e.g. tests calling a handler directly without going
// through the middleware).
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

// WithValue attaches a request ID to ctx. Only the middleware should call
// this; handlers read via FromContext.
func WithValue(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}
