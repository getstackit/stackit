package auth

import (
	"net/http"
)

// RequireSession returns a middleware that ensures the request carries a
// valid session cookie before forwarding to next. Failed checks emit
// 401 JSON; passed checks attach the Session to the request context for
// downstream handlers to read via SessionFromContext.
//
// This middleware does *not* set cookie attributes or talk to GitHub —
// it's a pure store lookup. Sessions are created elsewhere (the OAuth
// callback) and revoked elsewhere (logout, TTL sweep).
func RequireSession(store Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := readCookie(r, sessionCookieName)
		if cookie == "" {
			writeUnauthorized(w)
			return
		}
		sess, ok := store.Get(cookie)
		if !ok {
			writeUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithSession(r.Context(), sess)))
	})
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
}

// CSRFHeader is the custom request header the web client adds on every
// non-safe request. Its presence — not value — is what matters: browsers
// can't set a custom header on a cross-origin request without a CORS
// preflight, and the server's CORS allowlist is exact-match. That means
// only configured origins can satisfy this check, which is enough to
// block cookie-based CSRF without a per-request token.
const CSRFHeader = "X-Stackit-CSRF"

// safeMethods are the HTTP methods that don't change server state and so
// don't need CSRF protection. Everything else does.
var safeMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodOptions: {},
}

// RequireCSRFHeader gates non-safe methods on the presence of a custom
// CSRFHeader. Safe methods pass through untouched. Failures emit a 403
// JSON response so clients can distinguish "your auth is bad" (401) from
// "your call shape is wrong" (403).
//
// Pair this with RequireSession on /api/*: RequireSession says "you must
// be logged in", RequireCSRFHeader says "and this isn't a cross-site
// drive-by".
func RequireCSRFHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := safeMethods[r.Method]; ok {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(CSRFHeader) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"missing csrf header"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
