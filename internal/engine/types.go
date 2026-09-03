package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

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

// MetaMap is branch name -> metadata, as returned by the batch metadata readers.
type MetaMap map[string]*git.Meta

// Get returns the metadata for a branch, or nil if the map is nil or the
// branch has no entry.
func (m MetaMap) Get(branchName string) *git.Meta {
	if m == nil {
		return nil
	}
	return m[branchName]
}

// RevisionMap is branch name -> commit SHA, as returned by the batch revision readers.
type RevisionMap map[string]string

// Rev returns the revision for a branch and whether it was present. Safe to
// call on a nil map.
func (r RevisionMap) Rev(name string) (string, bool) {
	rev, ok := r[name]
	return rev, ok
}

// BranchNameSet is a set of branch names.
type BranchNameSet map[string]bool

// Contains reports whether the set includes the branch. Safe to call on a
// nil set.
func (s BranchNameSet) Contains(name string) bool {
	return s[name]
}

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
	// CommitFormatSHASubject pairs the full SHA and subject on one NUL-separated
	// record per commit ("<full-sha>\x00<subject>"), so callers get both from a
	// single walk without index-aligning two separate lists. The subject may be
	// empty; the record is never blank because the SHA is always present.
	CommitFormatSHASubject CommitFormat = "SHA_SUBJECT"
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

// DeletionStatuses maps branch name -> deletion status, as returned by
// GetDeletionStatuses.
type DeletionStatuses map[string]DeletionStatus

// For returns the deletion status for a branch. A branch absent from the map
// returns the zero DeletionStatus (SafeToDelete=false), so an unknown branch is
// never treated as safe to delete. Safe to call on a nil map.
func (s DeletionStatuses) For(name string) DeletionStatus {
	return s[name]
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

// Unknown reports that the local/remote relationship could not be determined:
// both sides exist and differ, but no merge base was resolvable. That happens
// when the remote object is not in the local object database — the remote SHA
// came from a listing (ls-remote) and nothing has fetched the objects yet.
//
// Behind, Ahead and Diverged all return false in that state, which is
// indistinguishable from "in sync". Callers that act on being up to date must
// check this first rather than treating a silent false as a clean bill.
func (s BranchRemoteStatus) Unknown() bool {
	if s.Matches() || s.LocalSha == "" || s.RemoteSha == "" {
		return false
	}
	return s.CommonAncestor == ""
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

// TrunkRemoteState describes how the local trunk relates to its remote-tracking
// branch (e.g. origin/main) using only already-fetched local refs — never the
// network. It is safe on the restack path and offline.
type TrunkRemoteState struct {
	// HasRemoteRef is false when no remote-tracking trunk ref exists locally: a
	// local-only repo, a never-fetched remote, or a fresh clone without
	// origin/<trunk>. Callers treat this as "unknown — do not guard".
	HasRemoteRef bool
	// AheadOrDiverged is true when the local trunk has commits that are not
	// present on the remote-tracking trunk (local trunk is NOT an ancestor of
	// origin/<trunk>). Equal or behind is false.
	AheadOrDiverged bool
	// LocalSha is the local trunk tip; RemoteSha is the remote-tracking tip.
	LocalSha  string
	RemoteSha string
	// RemoteRef is the remote-tracking ref name, e.g. "origin/main", for
	// user-facing messages.
	RemoteRef string
}

// BranchRemoteStatuses maps branch names to their remote sync status.
type BranchRemoteStatuses map[string]BranchRemoteStatus

// ForBranch returns the remote status for the given branch.
func (s BranchRemoteStatuses) ForBranch(branch Branch) BranchRemoteStatus {
	return s[branch.GetName()]
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
	// RestackBlocked indicates the branch was not restacked because another
	// branch in its stack conflicted; it is untouched and needs a later pass
	RestackBlocked
)

// RestackBranchResult represents the result of restacking a branch, including the rebased branch base
type RestackBranchResult struct {
	Result              RestackResult
	RebasedBranchBase   string     // The new parent revision after successful rebase (only set if Result is RestackDone or RestackConflict)
	NewRev              string     // The new branch revision after this restack (set when Result is RestackDone; empty for Unneeded / Conflict)
	Reparented          bool       // True if the branch was reparented due to merged/deleted parent
	OldParent           string     // The old parent branch name (only set if Reparented is true)
	NewParent           string     // The new parent branch name (only set if Reparented is true)
	LockReason          LockReason // Reason why the branch is locked
	Frozen              bool       // True if the branch is frozen
	HeldBy              string     // Why a worktree held this branch back (empty if it was not held); Result is RestackUnneeded
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
	// RestackPlanApplyMetadataRefresh corrects a branch's recorded parent
	// revision without moving its branch ref — used when the branch is
	// already correctly based on its parent but the recorded revision has
	// drifted.
	RestackPlanApplyMetadataRefresh
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
	BranchMap      BranchNameSet
	ApplyMap       BranchNameSet
	PlannedResults map[string]RestackBranchResult
	Items          map[string]RestackPlanItem
}

// ContinueRebaseResult represents the result of continuing a rebase
type ContinueRebaseResult struct {
	Result              int    // git.RebaseResult value (0 = RebaseDone, 1 = RebaseConflict)
	BranchName          string // Only set if Result is RebaseDone
	RerereResolvedCount int    // Number of rebase continuations handled by git rerere

	// UnresetWorktree is a worktree holding BranchName that was left on
	// pre-rebase content because resetting it would have discarded work. Its ref
	// moved anyway — refusing that would throw away the conflict resolution the
	// user just performed — so the divergence is real and only the user can
	// clear it. Empty when nothing was held.
	UnresetWorktree string
	UnresetReason   string
}

// SubmitAction says whether submitting a branch will create a new PR or
// update the existing one.
type SubmitAction string

const (
	SubmitActionCreate SubmitAction = "create"
	SubmitActionUpdate SubmitAction = "update"
)

// PRSubmissionStatus represents the submission status of a branch
type PRSubmissionStatus struct {
	Action      SubmitAction
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
