package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// SortStrategy specifies how branches should be sorted in displays
type SortStrategy string

const (
	// SortStrategyAlphabetical sorts branches by name ascending (A-Z)
	SortStrategyAlphabetical SortStrategy = "ALPHABETICAL"
	// SortStrategySmart sorts branches by name descending (newest first) and hoists the active path
	SortStrategySmart SortStrategy = "SMART"
)

// LockReason is re-exported from git package
type LockReason = git.LockReason

const (
	// LockReasonNone indicates the branch is not locked
	LockReasonNone LockReason = git.LockReasonNone
	// LockReasonUser indicates the branch was manually locked by the user
	LockReasonUser LockReason = git.LockReasonUser
	// LockReasonConsolidating indicates the branch is being consolidated
	LockReasonConsolidating LockReason = git.LockReasonConsolidating
	// LockReasonDraining indicates the branch is being drained (merge drain in progress)
	LockReasonDraining LockReason = git.LockReasonDraining
)

// StackRange specifies the range of branches to include in stack operations
type StackRange struct {
	RecursiveParents  bool
	IncludeCurrent    bool
	RecursiveChildren bool
}

// CommitFormat specifies the format for commit output
type CommitFormat string

const (
	// CommitFormatSHA is the full commit SHA
	CommitFormatSHA CommitFormat = "SHA" // Full SHA
	// CommitFormatReadable is a readable one-line format
	CommitFormatReadable CommitFormat = "READABLE" // Oneline format: "abc123 Commit message"
	// CommitFormatReadableWithDate includes an ISO date: "abc123\t2024-01-15T10:30:00Z\tCommit message"
	CommitFormatReadableWithDate CommitFormat = "READABLE_WITH_DATE"
	// CommitFormatMessage is the full commit message
	CommitFormatMessage CommitFormat = "MESSAGE" // Full commit message
	// CommitFormatSubject is the first line of the commit message
	CommitFormatSubject CommitFormat = "SUBJECT" // First line of commit message
)

// Scope represents a branch scope that can be empty, a regular scope, or an inheritance breaker
type Scope struct {
	value string
}

// NewScope creates a new scope with the given value
func NewScope(value string) Scope {
	return Scope{value: value}
}

// Empty returns an empty scope
func Empty() Scope {
	return Scope{value: ""}
}

// None returns a scope that breaks inheritance
func None() Scope {
	return Scope{value: "none"}
}

// String returns the string representation of the scope
func (s Scope) String() string {
	return s.value
}

// IsEmpty returns true if the scope is empty
func (s Scope) IsEmpty() bool {
	return s.value == ""
}

// IsNone returns true if the scope breaks inheritance
func (s Scope) IsNone() bool {
	return s.value == "none" || s.value == "clear"
}

// IsDefined returns true if the scope has a meaningful value (not empty and not none)
func (s Scope) IsDefined() bool {
	return !s.IsEmpty() && !s.IsNone()
}

// Equal checks if two scopes are equal
func (s Scope) Equal(other Scope) bool {
	return s.value == other.value
}

// MarshalJSON implements json.Marshaler
func (s Scope) MarshalJSON() ([]byte, error) {
	if s.IsEmpty() {
		return []byte("null"), nil
	}
	return json.Marshal(s.value)
}

// UnmarshalJSON implements json.Unmarshaler
func (s *Scope) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = Empty()
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*s = NewScope(str)
	return nil
}

// scopeRegex matches scope prefixes like "[PROJ-123]" at the start of titles.
var scopeRegex = regexp.MustCompile(`^\[[^\]]+\]\s*`)

// ApplyToTitle adds or replaces a scope prefix in a title.
// If the title already has a scope prefix and it differs from this scope, it's replaced.
// If no scope prefix exists, this scope is prepended.
// Returns the original title if this scope is empty.
func (s Scope) ApplyToTitle(title string) string {
	if s.IsEmpty() {
		return title
	}

	if scopeRegex.MatchString(title) {
		// Title already has a scope prefix - replace if different
		if !strings.HasPrefix(strings.ToUpper(title), "["+strings.ToUpper(s.value)+"]") {
			return scopeRegex.ReplaceAllString(title, "["+s.value+"] ")
		}
		return title
	}

	// No scope prefix, add it
	return fmt.Sprintf("[%s] %s", s.value, title)
}

// TitleNeedsUpdate checks if a title needs to be updated due to this scope.
func (s Scope) TitleNeedsUpdate(title string) bool {
	if title == "" || s.IsEmpty() {
		return false
	}
	return s.ApplyToTitle(title) != title
}

// DeletionStatus represents the deletion status of a branch
type DeletionStatus struct {
	SafeToDelete       bool               // True if the branch is merged, closed, or empty (with PR)
	Reason             string             // Human-readable reason why it's safe (or not) to delete
	Kind               DeletionReasonKind // Machine-readable deletion reason
	HasUnpushedChanges bool               // Local branch is ahead of or diverged from remote
}

// DeletionReasonKind is a machine-readable reason for branch deletion eligibility.
type DeletionReasonKind string

const (
	DeletionReasonNone            DeletionReasonKind = "none"
	DeletionReasonClosedPR        DeletionReasonKind = "closed_pr"
	DeletionReasonMergedPR        DeletionReasonKind = "merged_pr"
	DeletionReasonMergedIntoTrunk DeletionReasonKind = "merged_into_trunk"
	DeletionReasonEmptyWithPR     DeletionReasonKind = "empty_with_pr"
	// DeletionReasonGhost is set for branches whose metadata is still on
	// disk but whose git ref has been deleted out-of-band (e.g., the user
	// ran `git branch -D` directly). Sync synthesizes this so the metadata
	// gets cleaned up and surviving children get reparented past the gap.
	DeletionReasonGhost DeletionReasonKind = "ghost"
)

// PendingChange represents a changed file in the working directory
type PendingChange struct {
	Path   string
	Status string // "A", "M", "D", "??", etc.
	Staged bool
}

// BranchRemoteStatus represents the relationship between a local branch and its remote counterpart
type BranchRemoteStatus struct {
	LocalSha       string
	RemoteSha      string
	CommonAncestor string
}

// Matches returns true if local and remote SHAs are identical
func (s BranchRemoteStatus) Matches() bool {
	return s.LocalSha != "" && s.LocalSha == s.RemoteSha
}

// Ahead returns true if the local branch has commits not yet on remote
func (s BranchRemoteStatus) Ahead() bool {
	if s.Matches() || s.LocalSha == "" || s.RemoteSha == "" {
		return false
	}
	return s.CommonAncestor == s.RemoteSha
}

// Behind returns true if the remote branch has commits not yet on local
func (s BranchRemoteStatus) Behind() bool {
	if s.Matches() || s.LocalSha == "" || s.RemoteSha == "" {
		return false
	}
	return s.CommonAncestor == s.LocalSha
}

// Diverged returns true if both local and remote have unique commits
func (s BranchRemoteStatus) Diverged() bool {
	if s.Matches() || s.LocalSha == "" || s.RemoteSha == "" {
		return false
	}
	return s.CommonAncestor != s.LocalSha && s.CommonAncestor != s.RemoteSha
}

// MissingRemote returns true if the branch does not exist on the remote
func (s BranchRemoteStatus) MissingRemote() bool {
	return s.RemoteSha == ""
}

// Branch represents a branch in the stack
type Branch struct {
	name   string
	reader branchReader
}

type branchReader interface {
	BranchReader
	GetParent(branch Branch) *Branch
	getParent(branch Branch) *Branch
	getPrInfo(branch Branch) (*PrInfo, error)
	getMergedDownstack(branch Branch) []git.MergedParent
	getExplicitScope(branch Branch) Scope
	getPRSubmissionStatus(branch Branch) (PRSubmissionStatus, error)
}

// NewBranch creates a new immutable Branch
func NewBranch(name string, reader branchReader) Branch {
	return Branch{
		name:   name,
		reader: reader,
	}
}

// GetName returns the branch name. This method allows Branch to implement
// the engine.Branch interface without creating circular dependencies.
func (b Branch) GetName() string {
	return b.name
}

// Equal checks if two branches are equal by comparing their names
func (b Branch) Equal(other Branch) bool {
	return b.name == other.name
}

// IsTrunk checks if this branch is the trunk
func (b Branch) IsTrunk() bool {
	return b.reader.IsTrunk(b)
}

// IsTracked checks if this branch is tracked (has metadata)
func (b Branch) IsTracked() bool {
	return b.reader.IsTracked(b)
}

// GetScope returns the scope for this branch, inheriting from parent if not set
func (b Branch) GetScope() Scope {
	return b.reader.GetScope(b)
}

// GetParentOrTrunk returns the parent branch name, or trunk if no parent
// This is used for validation where we expect a parent to exist
func (b Branch) GetParentOrTrunk() string {
	parent := b.reader.GetParent(b)
	if parent == nil {
		return b.reader.Trunk().GetName()
	}
	return parent.GetName()
}

// IsBranchUpToDate checks if this branch is up to date with its parent
// A branch is up to date if its parent revision matches the stored parent revision
func (b Branch) IsBranchUpToDate() bool {
	return b.reader.IsUpToDate(b)
}

// NeedsRestack returns true if the branch needs to be restacked onto its parent.
// This is the inverse of IsBranchUpToDate - a branch needs restacking when its
// parent has moved and the branch is no longer based on the current parent tip.
func (b Branch) NeedsRestack() bool {
	return !b.IsBranchUpToDate()
}

// GetCommitDate returns the commit date for this branch
func (b Branch) GetCommitDate() (time.Time, error) {
	return b.reader.GetCommitDate(b)
}

// GetCommitAuthor returns the commit author for this branch
func (b Branch) GetCommitAuthor() (string, error) {
	return b.reader.GetCommitAuthor(b)
}

// GetRevision returns the SHA of this branch
func (b Branch) GetRevision() (string, error) {
	return b.reader.GetRevision(b)
}

// GetCommitCount returns the number of commits for this branch
func (b Branch) GetCommitCount() (int, error) {
	return b.reader.GetCommitCount(b)
}

// GetDiffStats returns the number of lines added and deleted for this branch
func (b Branch) GetDiffStats() (added int, deleted int, err error) {
	return b.reader.GetDiffStats(b)
}

// GetAllCommits returns commits for this branch in various formats
func (b Branch) GetAllCommits(format CommitFormat) ([]string, error) {
	return b.reader.GetAllCommits(b, format)
}

// GetParent returns the parent branch (nil if no parent)
func (b Branch) GetParent() *Branch {
	return b.reader.getParent(b)
}

// GetPrInfo returns PR information for this branch
func (b Branch) GetPrInfo() (*PrInfo, error) {
	return b.reader.getPrInfo(b)
}

// HasPR reports whether this branch has a submitted PR number recorded.
func (b Branch) HasPR() bool {
	prInfo, err := b.GetPrInfo()
	return err == nil && prInfo != nil && prInfo.Number() != nil
}

// GetMergedDownstack returns the merged downstack history for this branch
func (b Branch) GetMergedDownstack() []git.MergedParent {
	return b.reader.getMergedDownstack(b)
}

// GetExplicitScope returns the explicit scope set for this branch (no inheritance)
func (b Branch) GetExplicitScope() Scope {
	return b.reader.getExplicitScope(b)
}

// IsLocked checks if the branch is locked for modifications
func (b Branch) IsLocked() bool {
	return b.reader.IsLocked(b)
}

// GetLockReason returns why the branch is locked
func (b Branch) GetLockReason() LockReason {
	return b.reader.GetLockReason(b)
}

// IsFrozen checks if the branch is frozen locally
func (b Branch) IsFrozen() bool {
	return b.reader.IsFrozen(b)
}

// IsWorktreeAnchor checks if the branch is a worktree anchor branch
func (b Branch) IsWorktreeAnchor() bool {
	return b.reader.IsWorktreeAnchor(b)
}

// CanModify checks if the branch can be modified (not locked, frozen, or a worktree anchor)
func (b Branch) CanModify() bool {
	return !b.IsLocked() && !b.IsFrozen() && !b.IsWorktreeAnchor()
}

// ModificationBlocker returns a human-readable reason why the branch cannot be modified,
// or an empty string if the branch can be modified.
// This is useful for displaying status in UIs or logs without throwing errors.
func (b Branch) ModificationBlocker() string {
	switch {
	case b.IsWorktreeAnchor():
		return "worktree anchor"
	case b.IsLocked() && b.IsFrozen():
		return fmt.Sprintf("locked (%s) and frozen", b.GetLockReason())
	case b.IsLocked():
		return fmt.Sprintf("locked (%s)", b.GetLockReason())
	case b.IsFrozen():
		return "frozen"
	default:
		return ""
	}
}

// EnsureCanModify checks if the branch can be modified and returns an error if not
func (b Branch) EnsureCanModify() error {
	if b.IsWorktreeAnchor() {
		return fmt.Errorf("cannot modify worktree anchor branch %s; use 'stackit create' to add commits to this stack", b.name)
	}
	if b.CanModify() {
		return nil
	}
	return errors.NewBranchModificationError(b.name, b.GetLockReason(), b.IsFrozen())
}

// GetPRSubmissionStatus returns the PR submission status for this branch
func (b Branch) GetPRSubmissionStatus() (PRSubmissionStatus, error) {
	return b.reader.getPRSubmissionStatus(b)
}

// DefaultPRTitle returns the default PR title for this branch.
// Uses the oldest commit subject, falling back to the branch name.
func (b Branch) DefaultPRTitle() string {
	commits, err := b.GetAllCommits(CommitFormatSubject)
	if err != nil || len(commits) == 0 {
		return b.name
	}
	// GetAllCommits returns newest to oldest, so oldest is last
	return commits[len(commits)-1]
}

// DefaultPRBody returns the default PR body for this branch.
// For single commit: uses the commit body (skips subject line).
// For multiple commits: creates a bulleted list of subjects in chronological order.
func (b Branch) DefaultPRBody() string {
	messages, err := b.GetAllCommits(CommitFormatMessage)
	if err != nil || len(messages) == 0 {
		return ""
	}

	if len(messages) == 1 {
		// Use body (skip first line which is subject)
		lines := strings.Split(messages[0], "\n")
		if len(lines) > 1 {
			return strings.Join(lines[1:], "\n")
		}
		return ""
	}

	// Format as a bulleted list of subjects in chronological order
	var sb strings.Builder
	// GetAllCommits returns newest to oldest, so iterate in reverse
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		subject := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])
		if subject != "" {
			sb.WriteString("- " + subject + "\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// PrInfo represents PR information for a branch
// PrInfo is immutable - use With* methods to create modified copies
type PrInfo struct {
	number      *int
	title       string
	body        string
	isDraft     bool
	state       string     // MERGED, CLOSED, OPEN
	base        string     // Base branch name
	url         string     // PR URL
	lockReason  LockReason // Why the PR is locked (empty if not locked)
	mergeBranch string     // Name of the merge branch this PR is part of
}

// NewPrInfo creates a new PrInfo instance
func NewPrInfo(number *int, title, body, state, base, url string, isDraft bool) *PrInfo {
	return &PrInfo{
		number:  number,
		title:   title,
		body:    body,
		isDraft: isDraft,
		state:   state,
		base:    base,
		url:     url,
	}
}

// NewPrInfoWithLockReason creates a new PrInfo instance including lock reason
func NewPrInfoWithLockReason(number *int, title, body, state, base, url string, isDraft bool, lockReason LockReason) *PrInfo {
	return &PrInfo{
		number:     number,
		title:      title,
		body:       body,
		isDraft:    isDraft,
		state:      state,
		base:       base,
		url:        url,
		lockReason: lockReason,
	}
}

// NewPrInfoFull creates a new PrInfo instance with all fields
func NewPrInfoFull(number *int, title, body, state, base, url string, isDraft bool, lockReason LockReason, mergeBranch string) *PrInfo {
	return &PrInfo{
		number:      number,
		title:       title,
		body:        body,
		isDraft:     isDraft,
		state:       state,
		base:        base,
		url:         url,
		lockReason:  lockReason,
		mergeBranch: mergeBranch,
	}
}

// Number returns the PR number
func (p *PrInfo) Number() *int {
	return p.number
}

// Title returns the PR title
func (p *PrInfo) Title() string {
	return p.title
}

// Body returns the PR body
func (p *PrInfo) Body() string {
	return p.body
}

// IsDraft returns whether the PR is a draft
func (p *PrInfo) IsDraft() bool {
	return p.isDraft
}

// State returns the PR state (MERGED, CLOSED, OPEN)
func (p *PrInfo) State() string {
	return p.state
}

// Base returns the base branch name
func (p *PrInfo) Base() string {
	return p.base
}

// URL returns the PR URL
func (p *PrInfo) URL() string {
	return p.url
}

// IsLocked returns whether the PR footer shows it as locked
func (p *PrInfo) IsLocked() bool {
	return p.lockReason.IsLocked()
}

// LockReason returns the reason why the PR is locked
func (p *PrInfo) LockReason() LockReason {
	return p.lockReason
}

// MergeBranch returns the name of the merge branch this PR is part of
func (p *PrInfo) MergeBranch() string {
	return p.mergeBranch
}

// MarshalJSON implements json.Marshaler for PrInfo
func (p *PrInfo) MarshalJSON() ([]byte, error) {
	type Alias struct {
		Number      *int   `json:"number,omitempty"`
		Base        string `json:"base,omitempty"`
		URL         string `json:"url,omitempty"`
		Title       string `json:"title,omitempty"`
		Body        string `json:"body,omitempty"`
		State       string `json:"state,omitempty"`
		IsDraft     bool   `json:"is_draft"`
		LockReason  string `json:"lock_reason,omitempty"`
		MergeBranch string `json:"merge_branch,omitempty"`
	}
	return json.Marshal(&Alias{
		Number:      p.number,
		Base:        p.base,
		URL:         p.url,
		Title:       p.title,
		Body:        p.body,
		State:       p.state,
		IsDraft:     p.isDraft,
		LockReason:  string(p.lockReason),
		MergeBranch: p.mergeBranch,
	})
}

// WithNumber returns a new PrInfo with the number field updated
func (p *PrInfo) WithNumber(number *int) *PrInfo {
	return &PrInfo{
		number:     number,
		title:      p.title,
		body:       p.body,
		isDraft:    p.isDraft,
		state:      p.state,
		base:       p.base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithTitle returns a new PrInfo with the title field updated
func (p *PrInfo) WithTitle(title string) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      title,
		body:       p.body,
		isDraft:    p.isDraft,
		state:      p.state,
		base:       p.base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithBody returns a new PrInfo with the body field updated
func (p *PrInfo) WithBody(body string) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      p.title,
		body:       body,
		isDraft:    p.isDraft,
		state:      p.state,
		base:       p.base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithTitleAndBody returns a new PrInfo with both title and body fields updated
// This is more efficient than chaining WithTitle().WithBody() as it only creates one copy
func (p *PrInfo) WithTitleAndBody(title, body string) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      title,
		body:       body,
		isDraft:    p.isDraft,
		state:      p.state,
		base:       p.base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithIsDraft returns a new PrInfo with the isDraft field updated
func (p *PrInfo) WithIsDraft(isDraft bool) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      p.title,
		body:       p.body,
		isDraft:    isDraft,
		state:      p.state,
		base:       p.base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithState returns a new PrInfo with the state field updated
func (p *PrInfo) WithState(state string) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      p.title,
		body:       p.body,
		isDraft:    p.isDraft,
		state:      state,
		base:       p.base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithBase returns a new PrInfo with the base field updated
func (p *PrInfo) WithBase(base string) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      p.title,
		body:       p.body,
		isDraft:    p.isDraft,
		state:      p.state,
		base:       base,
		url:        p.url,
		lockReason: p.lockReason,
	}
}

// WithURL returns a new PrInfo with the url field updated
func (p *PrInfo) WithURL(url string) *PrInfo {
	return &PrInfo{
		number:     p.number,
		title:      p.title,
		body:       p.body,
		isDraft:    p.isDraft,
		state:      p.state,
		base:       p.base,
		url:        url,
		lockReason: p.lockReason,
	}
}

// WithLockReason returns a new PrInfo with the lockReason field updated
func (p *PrInfo) WithLockReason(reason LockReason) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		url:         p.url,
		lockReason:  reason,
		mergeBranch: p.mergeBranch,
	}
}

// WithMergeBranch returns a new PrInfo with the mergeBranch field updated
func (p *PrInfo) WithMergeBranch(branch string) *PrInfo {
	return &PrInfo{
		number:      p.number,
		title:       p.title,
		body:        p.body,
		isDraft:     p.isDraft,
		state:       p.state,
		base:        p.base,
		url:         p.url,
		lockReason:  p.lockReason,
		mergeBranch: branch,
	}
}

// ValidationResult represents the validation state of a branch
type ValidationResult int

const (
	// ValidationResultValid indicates the branch is valid
	ValidationResultValid ValidationResult = iota
	// ValidationResultInvalidParent indicates the branch has an invalid parent
	ValidationResultInvalidParent
	// ValidationResultBadParentRevision indicates the branch has a bad parent revision
	ValidationResultBadParentRevision
	// ValidationResultBadParentName indicates the branch has a bad parent name
	ValidationResultBadParentName
	// ValidationResultTrunk indicates the branch is a trunk
	ValidationResultTrunk
)

// PullResult represents the result of pulling trunk
type PullResult int

const (
	// PullDone indicates the pull was successful
	PullDone PullResult = iota
	// PullUnneeded indicates no pull was needed
	PullUnneeded
	// PullConflict indicates a conflict occurred during pull
	PullConflict
)

// RestackResult represents the result of restacking a branch
type RestackResult int

const (
	// RestackDone indicates the restack was successful
	RestackDone RestackResult = iota
	// RestackUnneeded indicates no restack was needed
	RestackUnneeded
	// RestackConflict indicates a conflict occurred during restack
	RestackConflict
)

// RestackBranchResult represents the result of restacking a branch, including the rebased branch base
type RestackBranchResult struct {
	Result              RestackResult
	RebasedBranchBase   string     // The new parent revision after successful rebase (only set if Result is RestackDone or RestackConflict)
	Reparented          bool       // True if the branch was reparented due to merged/deleted parent
	OldParent           string     // The old parent branch name (only set if Reparented is true)
	NewParent           string     // The new parent branch name (only set if Reparented is true)
	LockReason          LockReason // Reason why the branch is locked
	Frozen              bool       // True if the branch is frozen
	RerereResolvedCount int        // Number of rebase continuations handled by git rerere
}

// RestackBranchProgressFunc is called after a branch has been processed during
// a batch restack.
type RestackBranchProgressFunc func(branch Branch, result RestackBranchResult)

// IsLocked returns true if the branch is locked
func (r RestackBranchResult) IsLocked() bool {
	return r.LockReason.IsLocked()
}

// RestackBatchResult represents the result of restacking multiple branches
type RestackBatchResult struct {
	ConflictBranch    string                         // The branch that hit a conflict
	RebasedBranchBase string                         // The parent revision for the conflict
	RemainingBranches []string                       // Branches that weren't reached
	Results           map[string]RestackBranchResult // Results for each branch attempted
}

// RestackPlanAction describes how a planned branch should be applied.
type RestackPlanAction int

const (
	// RestackPlanApplyValidated applies a SHA produced by rebase validation.
	RestackPlanApplyValidated RestackPlanAction = iota
	// RestackPlanApplyFrozen updates a frozen branch from its remote ref.
	RestackPlanApplyFrozen
	// RestackPlanApplyAnchor updates a worktree anchor to trunk.
	RestackPlanApplyAnchor
)

// RestackPlanItem describes how a branch should be handled during restack.
type RestackPlanItem struct {
	Branch      string
	NewParent   string
	ParentRev   string
	OldUpstream string
	TargetRev   string
	Action      RestackPlanAction
	Skip        bool
	SkipResult  RestackBranchResult
	Reparented  bool
	OldParent   string
}

// RestackPlan describes validation specs, pre-skipped branch results, and
// per-branch apply decisions for a restack operation.
type RestackPlan struct {
	Specs          []RebaseSpec
	BranchMap      map[string]bool
	ApplyMap       map[string]bool
	PlannedResults map[string]RestackBranchResult
	Items          map[string]RestackPlanItem
}

// ContinueRebaseResult represents the result of continuing a rebase
type ContinueRebaseResult struct {
	Result              int    // git.RebaseResult value (0 = RebaseDone, 1 = RebaseConflict)
	BranchName          string // Only set if Result is RebaseDone
	RerereResolvedCount int    // Number of rebase continuations handled by git rerere
}

// PRSubmissionStatus represents the submission status of a branch
type PRSubmissionStatus struct {
	Action      string // "create", "update", or "skip"
	NeedsUpdate bool   // True if the branch has changes or metadata needs update
	Reason      string // Reason for the status
	PRNumber    *int
	PRInfo      *PrInfo
}

const (
	// ReasonNoChanges indicates there are no changes to submit
	ReasonNoChanges = "no changes"
)

// SquashOptions contains options for squashing commits
type SquashOptions struct {
	Message  string
	NoEdit   bool
	NoVerify bool
}

// MergeOptions contains options for merging branches
type MergeOptions struct {
	FFOnly  bool
	NoEdit  bool
	NoFF    bool
	Message string
}

// BatchLockResult represents the result of a batch lock/unlock operation
type BatchLockResult struct {
	AffectedBranches []string
	Errors           map[string]error
}

// BatchFreezeResult represents the result of a batch freeze/unfreeze operation
type BatchFreezeResult struct {
	AffectedBranches []string
	Errors           map[string]error
}
