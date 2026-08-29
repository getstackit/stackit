package submit

import (
	"net/url"
	"path"
	"strconv"
	"sync"
)

// JSONResult is the machine-readable summary of a submit run.
type JSONResult struct {
	Outcome  CompletionOutcome  `json:"outcome"`
	Message  string             `json:"message,omitempty"`
	Error    string             `json:"error,omitempty"`
	Branches []JSONBranchResult `json:"branches"`
	// GitHubStacks contains every native GitHub Stack reconciled during submit.
	GitHubStacks []JSONGitHubStackResult `json:"github_stacks,omitempty"`
	// GitHubStackSkips contains every reason configured native Stack sync skipped
	// an otherwise submitted component.
	GitHubStackSkips []string `json:"github_stack_skips,omitempty"`
	// GitHubStack is retained for backwards compatibility when exactly one
	// native Stack was reconciled. Use GitHubStacks for new consumers.
	GitHubStack *JSONGitHubStackResult `json:"github_stack,omitempty"`
	// GitHubStackSkipped is retained for backwards compatibility when exactly
	// one component was skipped. Use GitHubStackSkips for new consumers.
	GitHubStackSkipped string `json:"github_stack_skipped,omitempty"`
}

// JSONGitHubStackResult identifies native GitHub Stack metadata reconciled by submit.
type JSONGitHubStackResult struct {
	Number       int    `json:"number"`
	PullRequests []int  `json:"pull_requests"`
	Action       string `json:"action"`
}

// JSONBranchResult is one branch's plan and result in submit JSON output.
type JSONBranchResult struct {
	Branch     string   `json:"branch"`
	Action     string   `json:"action,omitempty"` // "create" or "update"
	Status     string   `json:"status"`           // pending, done, error, skipped
	PR         *int     `json:"pr,omitempty"`
	URL        string   `json:"url,omitempty"`
	SkipReason string   `json:"skip_reason,omitempty"`
	Error      string   `json:"error,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

// JSONHandler collects submit events into a JSONResult.
type JSONHandler struct {
	mu     sync.Mutex
	Result *JSONResult
	index  map[string]int // branch name -> position in Result.Branches
}

// NewJSONHandler creates a handler that collects submit results as JSON data.
func NewJSONHandler() *JSONHandler {
	return &JSONHandler{
		Result: &JSONResult{Branches: []JSONBranchResult{}},
		index:  map[string]int{},
	}
}

// SetError records a run-level error on the result.
func (h *JSONHandler) SetError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Result.Error = err.Error()
	if h.Result.Outcome == "" {
		h.Result.Outcome = OutcomeFailed
	}
}

// OnEvent implements Handler.
func (h *JSONHandler) OnEvent(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch ev := e.(type) {
	case BranchPlanEvent:
		b := h.branch(ev.BranchName)
		b.Action = string(ev.Action)
		b.PR = ev.PRNumber
		if ev.Skipped {
			b.Status = string(StatusSkipped)
			b.SkipReason = ev.SkipReason
		} else {
			b.Status = string(StatusPending)
		}

	case BranchProgressEvent:
		b := h.branch(ev.BranchName)
		// The footer sync re-emits done without a URL; never clobber fields
		// already learned.
		if ev.Status != "" {
			b.Status = string(ev.Status)
		}
		if ev.URL != "" {
			b.URL = ev.URL
			if b.PR == nil {
				b.PR = prNumberFromPullURL(ev.URL)
			}
		}
		if ev.Error != nil {
			b.Error = ev.Error.Error()
		}

	case BranchWarningEvent:
		b := h.branch(ev.BranchName)
		b.Warnings = append(b.Warnings, ev.Warning)

	case GitHubStackSyncedEvent:
		stack := JSONGitHubStackResult{Number: ev.Number, PullRequests: ev.PullRequests, Action: string(ev.Action)}
		h.Result.GitHubStacks = append(h.Result.GitHubStacks, stack)
		if len(h.Result.GitHubStacks) == 1 {
			h.Result.GitHubStack = &h.Result.GitHubStacks[0]
		} else {
			h.Result.GitHubStack = nil
		}

	case GitHubStackSkippedEvent:
		h.Result.GitHubStackSkips = append(h.Result.GitHubStackSkips, ev.Reason)
		if len(h.Result.GitHubStackSkips) == 1 {
			h.Result.GitHubStackSkipped = ev.Reason
		} else {
			h.Result.GitHubStackSkipped = ""
		}

	case CompletionEvent:
		h.Result.Outcome = ev.Outcome
		h.Result.Message = ev.Message
	}
}

// branch returns the result entry for name, creating it on first sight so
// event order never drops data.
func (h *JSONHandler) branch(name string) *JSONBranchResult {
	if i, ok := h.index[name]; ok {
		return &h.Result.Branches[i]
	}
	h.index[name] = len(h.Result.Branches)
	h.Result.Branches = append(h.Result.Branches, JSONBranchResult{Branch: name})
	return &h.Result.Branches[len(h.Result.Branches)-1]
}

// prNumberFromPullURL extracts the PR number from a GitHub pull URL.
func prNumberFromPullURL(raw string) *int {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	n, err := strconv.Atoi(path.Base(u.Path))
	if err != nil {
		return nil
	}
	return &n
}

// Confirm auto-confirms with the default value — JSON output is non-interactive.
func (h *JSONHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

// IsInteractive returns false — JSON handlers never prompt.
func (h *JSONHandler) IsInteractive() bool {
	return false
}
