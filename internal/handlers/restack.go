// Package handlers provides shared handler interfaces for CLI output.
package handlers

import (
	"stackit.dev/stackit/internal/engine"
)

// RestackResult represents the outcome of a restack operation for a single branch
type RestackResult string

const (
	// RestackDone indicates the branch was successfully restacked
	RestackDone RestackResult = "done"
	// RestackUnneeded indicates the branch didn't need restacking
	RestackUnneeded RestackResult = "unneeded"
	// RestackConflict indicates the branch had a conflict
	RestackConflict RestackResult = "conflict"
)

// RestackHandler abstracts TTY vs non-TTY output for restack operations
// This interface is shared between sync, get, and restack commands
type RestackHandler interface {
	// OnRestackStart is called at the beginning of restack with branch count
	OnRestackStart(branchCount int)

	// OnRestackBranch is called for each branch during restack
	OnRestackBranch(branch string, result RestackResult, newRev string, prNumber *int, lockReason engine.LockReason, frozen bool, isCurrent bool, parent string, reparented bool, oldParent, newParent string)

	// OnRestackComplete is called when restack finishes
	OnRestackComplete(restacked, skipped int, conflicts []string)
}

// NullRestackHandler is a no-op handler for testing or when output is not needed
type NullRestackHandler struct{}

// OnRestackStart implements RestackHandler.
func (h *NullRestackHandler) OnRestackStart(_ int) {}

// OnRestackBranch implements RestackHandler.
func (h *NullRestackHandler) OnRestackBranch(_ string, _ RestackResult, _ string, _ *int, _ engine.LockReason, _ bool, _ bool, _ string, _ bool, _, _ string) {
}

// OnRestackComplete implements RestackHandler.
func (h *NullRestackHandler) OnRestackComplete(_, _ int, _ []string) {}

// RestackJSONStatus represents the aggregate outcome of a JSON restack operation.
type RestackJSONStatus string

const (
	RestackJSONStatusSuccess  RestackJSONStatus = "success"
	RestackJSONStatusConflict RestackJSONStatus = "conflict"
	RestackJSONStatusError    RestackJSONStatus = "error"
)

// RestackJSONResult represents the JSON output for restack operations
type RestackJSONResult struct {
	Status        RestackJSONStatus     `json:"status"`
	Error         string                `json:"error,omitempty"`
	Restacked     []RestackBranchInfo   `json:"restacked,omitempty"`
	Skipped       []string              `json:"skipped,omitempty"`
	Conflicts     []RestackConflictInfo `json:"conflicts,omitempty"`
	TotalCount    int                   `json:"total_count"`
	RestackCount  int                   `json:"restack_count"`
	ConflictCount int                   `json:"conflict_count"`
}

// RestackBranchInfo represents info about a restacked branch
type RestackBranchInfo struct {
	Name     string `json:"name"`
	Parent   string `json:"parent"`
	NewRev   string `json:"new_rev,omitempty"`
	PRNumber *int   `json:"pr_number,omitempty"`
}

// RestackConflictInfo represents a conflict during restack
type RestackConflictInfo struct {
	Branch string `json:"branch"`
	Parent string `json:"parent"`
}

// JSONRestackHandler collects restack results for JSON output
type JSONRestackHandler struct {
	Result *RestackJSONResult
}

// NewJSONRestackHandler creates a new JSON handler
func NewJSONRestackHandler() *JSONRestackHandler {
	return &JSONRestackHandler{
		Result: &RestackJSONResult{
			Restacked: []RestackBranchInfo{},
			Skipped:   []string{},
			Conflicts: []RestackConflictInfo{},
		},
	}
}

// OnRestackStart implements RestackHandler.
func (h *JSONRestackHandler) OnRestackStart(branchCount int) {
	h.Result.TotalCount = branchCount
}

// OnRestackBranch implements RestackHandler.
func (h *JSONRestackHandler) OnRestackBranch(branch string, result RestackResult, newRev string, prNumber *int, _ engine.LockReason, _ bool, _ bool, parent string, _ bool, _, _ string) {
	switch result {
	case RestackDone:
		h.Result.Restacked = append(h.Result.Restacked, RestackBranchInfo{
			Name:     branch,
			Parent:   parent,
			NewRev:   newRev,
			PRNumber: prNumber,
		})
	case RestackUnneeded:
		h.Result.Skipped = append(h.Result.Skipped, branch)
	case RestackConflict:
		h.Result.Conflicts = append(h.Result.Conflicts, RestackConflictInfo{
			Branch: branch,
			Parent: parent,
		})
	}
}

// OnRestackComplete implements RestackHandler.
func (h *JSONRestackHandler) OnRestackComplete(restacked, _ int, _ []string) {
	h.Result.RestackCount = restacked
	h.Result.ConflictCount = len(h.Result.Conflicts)

	if h.Result.ConflictCount > 0 {
		h.Result.Status = RestackJSONStatusConflict
	} else {
		h.Result.Status = RestackJSONStatusSuccess
	}
}

// SetError sets the error status and message on the result.
// Call this when the restack action returns an error.
func (h *JSONRestackHandler) SetError(err error) {
	if err != nil {
		// If we already observed restack conflicts, keep status as "conflict"
		// and attach the error details for debugging/context.
		if len(h.Result.Conflicts) > 0 {
			h.Result.Status = RestackJSONStatusConflict
			h.Result.Error = err.Error()
			return
		}

		h.Result.Status = RestackJSONStatusError
		h.Result.Error = err.Error()
	}
}
