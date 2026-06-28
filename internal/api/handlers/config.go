package handlers

import (
	"net/http"

	httpcontract "github.com/getstackit/stackit/internal/contracts/http"
)

// ConfigHandler serves server capabilities at GET /api/v1/config. It carries
// no repo state — the values are fixed at server startup — and is served
// without a session so the web client can discover whether auth is required
// before attempting a login.
type ConfigHandler struct {
	readOnly     bool
	authRequired bool
	singleRepo   bool
}

// NewConfigHandler creates a handler that reports the given capabilities.
func NewConfigHandler(readOnly, authRequired, singleRepo bool) *ConfigHandler {
	return &ConfigHandler{readOnly: readOnly, authRequired: authRequired, singleRepo: singleRepo}
}

// ServeHTTP returns the capability payload.
func (h *ConfigHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, httpcontract.ConfigResponse{
		ReadOnly:     h.readOnly,
		AuthRequired: h.authRequired,
		SingleRepo:   h.singleRepo,
	})
}
