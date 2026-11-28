package stack

import (
	"slices"
	"sync"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/git"
)

// RestackJSONStatus represents the aggregate outcome of a JSON restack operation.
type RestackJSONStatus string

const (
	RestackJSONStatusSuccess  RestackJSONStatus = "success"
	RestackJSONStatusConflict RestackJSONStatus = "conflict"
	RestackJSONStatusError    RestackJSONStatus = "error"
)

// RestackJSONResult represents the JSON output for restack operations
type RestackJSONResult struct {
	Status        RestackJSONStatus         `json:"status"`
	Error         string                    `json:"error,omitempty"`
	Restacked     []RestackBranchInfo       `json:"restacked,omitempty"`
	Skipped       []string                  `json:"skipped,omitempty"`
	Conflicts     []RestackConflictInfo     `json:"conflicts,omitempty"`
	Held          []actions.RestackHeldInfo `json:"held,omitempty"`        // Branches a worktree held back; also present in Skipped
	Blocked       []string                  `json:"blocked,omitempty"`     // Branches left untouched because their stack contained a conflict
	StackRoots    []string                  `json:"stack_roots,omitempty"` // Deduped independent stack roots that were processed
	TotalCount    int                       `json:"total_count"`
	RestackCount  int                       `json:"restack_count"`
	ConflictCount int                       `json:"conflict_count"`
	BlockedCount  int                       `json:"blocked_count,omitempty"`
}

// RestackBranchInfo represents info about a restacked branch
type RestackBranchInfo struct {
	Name                string        `json:"name"`
	Parent              string        `json:"parent"`
	StackRoot           string        `json:"stack_root,omitempty"` // Independent stack root this branch belongs to
	NewRev              string        `json:"new_rev,omitempty"`
	PRNumber            *git.PRNumber `json:"pr_number,omitempty"`
	RerereResolvedCount int           `json:"rerere_resolved_count,omitempty"`
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
func (h *JSONRestackHandler) OnRestackBranch(event actions.RestackBranchEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch event.Result {
	case actions.RestackDone:
		h.Result.Restacked = append(h.Result.Restacked, RestackBranchInfo{
			Name:                event.Branch,
			Parent:              event.Parent,
			StackRoot:           event.StackRoot,
			NewRev:              event.NewRevision,
			PRNumber:            event.PRNumber,
			RerereResolvedCount: event.RerereResolvedCount,
		})
	case actions.RestackUnneeded:
		h.Result.Skipped = append(h.Result.Skipped, event.Branch)
	case actions.RestackConflict:
		h.Result.Conflicts = append(h.Result.Conflicts, RestackConflictInfo{
			Branch:    event.Branch,
			Parent:    event.Parent,
			StackRoot: event.StackRoot,
		})
	case actions.RestackBlocked:
		h.Result.Blocked = append(h.Result.Blocked, event.Branch)
	}

	if event.StackRoot != "" && !slices.Contains(h.Result.StackRoots, event.StackRoot) {
		h.Result.StackRoots = append(h.Result.StackRoots, event.StackRoot)
	}
}

// OnRestackComplete implements RestackHandler.
func (h *JSONRestackHandler) OnRestackComplete(summary actions.RestackSummary) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Result.RestackCount = summary.Restacked
	h.Result.ConflictCount = len(h.Result.Conflicts)
	h.Result.BlockedCount = len(h.Result.Blocked)
	// Holds come from the summary so branches pruned while planning (which
	// never produce a per-branch event) are reported alongside mid-run holds.
	// Held branches are skipped rather than restacked, so scripts must not read
	// an empty conflict list as "the whole stack moved".
	h.Result.Held = summary.Held
	for _, held := range summary.Held {
		if !slices.Contains(h.Result.Skipped, held.Branch) {
			h.Result.Skipped = append(h.Result.Skipped, held.Branch)
		}
	}

	// Status follows the summary alone so it never depends on whether the
	// caller also reports the error via SetError. Conflicts outrank a failure,
	// matching SetError.
	switch {
	case h.Result.ConflictCount > 0:
		h.Result.Status = RestackJSONStatusConflict
	case summary.Failed:
		h.Result.Status = RestackJSONStatusError
	default:
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
