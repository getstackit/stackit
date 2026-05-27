package auth

import (
	"net/http"
	"time"
)

// Cookie names. Kept short, namespaced under "stackit_" so they're easy to
// recognize in browser devtools and don't collide with anything the
// embedded Next app might want for its own state.
const (
	sessionCookieName = "stackit_session"
	stateCookieName   = "stackit_oauth_state"
	returnCookieName  = "stackit_return"
)

// CookieOptions controls the attributes the auth package puts on cookies.
// Secure should be true in production (we're behind HTTPS at the proxy),
// and false in tests or local dev where the browser refuses Secure
// cookies on http://localhost.
type CookieOptions struct {
	Secure bool
}

func (o CookieOptions) sameSite() http.SameSite {
	// Lax is the right default for session cookies: cookies travel on
	// top-level navigations from another origin (so clicking a PR link in
	// Slack drops you into the app authenticated) but not on cross-site
	// XHR/form POSTs (so OAuth state isn't compromised). Mutating
	// endpoints rely on the CSRF header layer (PR 3) for the extra factor.
	return http.SameSiteLaxMode
}

func (o CookieOptions) setSession(w http.ResponseWriter, sessionID string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: o.sameSite(),
	})
}

func (o CookieOptions) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: o.sameSite(),
	})
}

func (o CookieOptions) setState(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/auth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: o.sameSite(),
	})
}

func (o CookieOptions) clearState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/auth/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: o.sameSite(),
	})
}

func (o CookieOptions) setReturn(w http.ResponseWriter, target string) {
	http.SetCookie(w, &http.Cookie{
		Name:     returnCookieName,
		Value:    target,
		Path:     "/auth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: o.sameSite(),
	})
}

func (o CookieOptions) clearReturn(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     returnCookieName,
		Value:    "",
		Path:     "/auth/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   o.Secure,
		SameSite: o.sameSite(),
	})
}

func readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
