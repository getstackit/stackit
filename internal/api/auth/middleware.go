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
