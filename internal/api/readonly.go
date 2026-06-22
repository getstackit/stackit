package api

import "net/http"

// readOnlyWriteHandler refuses mutating requests on a read-only server. It
// replaces the submit handler when ServerConfig.ReadOnly is set, so the
// write path is gone by construction — there is no code path that can push
// branches or update GitHub, regardless of auth or who is calling.
//
// It returns 405 (the method is disabled on this server) with a JSON body
// so clients can distinguish a read-only refusal from a missing route (404)
// or a failed auth/CSRF check (401/403).
func readOnlyWriteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"server is read-only"}`))
	})
}
