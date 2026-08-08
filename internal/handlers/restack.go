// Package handlers provides shared handler interfaces for CLI output.
package handlers

import (
	"slices"
	"sync"

	"github.com/getstackit/stackit/internal/engine"
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
	// RestackBlocked indicates the branch was left untouched because another
	// branch in its stack conflicted
	RestackBlocked RestackResult = "blocked"
)

// RestackHandler abstracts TTY vs non-TTY output for restack operations
// This interface is shared between sync, get, and restack commands
type RestackHandler interface {
	// OnRestackStart is called at the beginning of restack with branch count
	OnRestackStart(branchCount int)

	// OnRestackBranch is called for each branch during restack.
	OnRestackBranch(event RestackBranchEvent)

	// OnRestackComplete is called when restack finishes. blocked lists
	// branches left untouched because their stack contained a conflict.
	OnRestackComplete(summary RestackSummary)
}

// RestackBranchEvent describes the outcome of restacking one branch.
// Keeping these facts together makes the shared presentation contract safe to
// extend without relying on positional arguments.
type RestackBranchEvent struct {
	Branch              string
	Result              RestackResult
	NewRevision         string
	PRNumber            *int
	LockReason          engine.LockReason
	Frozen              bool
	IsCurrent           bool
	Parent              string
	Reparented          bool
	OldParent           string
	NewParent           string
	RerereResolvedCount int
	StackRoot           string
}

// RestackSummary contains aggregate outcomes from a restack operation.
type RestackSummary struct {
	Restacked int
	Skipped   int
	Conflicts []string
	Blocked   []string
}

// NullRestackHandler is a no-op handler for testing or when output is not needed
type NullRestackHandler struct{}

// OnRestackStart implements RestackHandler.
func (h *NullRestackHandler) OnRestackStart(_ int) {}

// OnRestackBranch implements RestackHandler.
func (h *NullRestackHandler) OnRestackBranch(RestackBranchEvent) {}

// OnRestackComplete implements RestackHandler.
func (h *NullRestackHandler) OnRestackComplete(RestackSummary) {}

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
	Blocked       []string              `json:"blocked,omitempty"`     // Branches left untouched because their stack contained a conflict
	StackRoots    []string              `json:"stack_roots,omitempty"` // Deduped independent stack roots that were processed
	TotalCount    int                   `json:"total_count"`
	RestackCount  int                   `json:"restack_count"`
	ConflictCount int                   `json:"conflict_count"`
	BlockedCount  int                   `json:"blocked_count,omitempty"`
}

// RestackBranchInfo represents info about a restacked branch
type RestackBranchInfo struct {
	Name                string `json:"name"`
	Parent              string `json:"parent"`
	StackRoot           string `json:"stack_root,omitempty"` // Independent stack root this branch belongs to
	NewRev              string `json:"new_rev,omitempty"`
	PRNumber            *int   `json:"pr_number,omitempty"`
	RerereResolvedCount int    `json:"rerere_resolved_count,omitempty"`
}

// RestackConflictInfo represents a conflict during restack
type RestackConflictInfo struct {
	Branch    string `json:"branch"`
	Parent    string `json:"parent"`
	StackRoot string `json:"stack_root,omitempty"` // Independent stack root this branch belongs to
}

// JSONRestackHandler collects restack results for JSON output.
// All methods are mutex-protected so concurrent callers (parallel restack) are safe.
type JSONRestackHandler struct {
	mu     sync.Mutex
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
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Result.TotalCount = branchCount
}

// OnRestackBranch implements RestackHandler.
func (h *JSONRestackHandler) OnRestackBranch(event RestackBranchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch event.Result {
	case RestackDone:
		h.Result.Restacked = append(h.Result.Restacked, RestackBranchInfo{
			Name:                event.Branch,
			Parent:              event.Parent,
			StackRoot:           event.StackRoot,
			NewRev:              event.NewRevision,
			PRNumber:            event.PRNumber,
			RerereResolvedCount: event.RerereResolvedCount,
		})
	case RestackUnneeded:
		h.Result.Skipped = append(h.Result.Skipped, event.Branch)
	case RestackConflict:
		h.Result.Conflicts = append(h.Result.Conflicts, RestackConflictInfo{
			Branch:    event.Branch,
			Parent:    event.Parent,
			StackRoot: event.StackRoot,
		})
	case RestackBlocked:
		h.Result.Blocked = append(h.Result.Blocked, event.Branch)
	}

	if event.StackRoot != "" && !slices.Contains(h.Result.StackRoots, event.StackRoot) {
		h.Result.StackRoots = append(h.Result.StackRoots, event.StackRoot)
	}
}

// OnRestackComplete implements RestackHandler.
func (h *JSONRestackHandler) OnRestackComplete(summary RestackSummary) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Result.RestackCount = summary.Restacked
	h.Result.ConflictCount = len(h.Result.Conflicts)
	h.Result.BlockedCount = len(h.Result.Blocked)

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
		h.mu.Lock()
		defer h.mu.Unlock()
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
