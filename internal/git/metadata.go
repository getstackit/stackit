package git

import (
	"fmt"
	"time"
)

// LockReason is an enum for the reason why a branch is locked
type LockReason string

const (
	// LockReasonNone indicates the branch is not locked
	LockReasonNone LockReason = ""
	// LockReasonUser indicates the branch was manually locked by the user
	LockReasonUser LockReason = "user"
	// LockReasonConsolidating indicates the branch is being consolidated
	LockReasonConsolidating LockReason = "consolidating"
	// LockReasonDraining indicates the branch is being drained (merge drain in progress)
	LockReasonDraining LockReason = "draining"
)

// IsLocked returns true if the lock reason indicates the branch is locked
func (r LockReason) IsLocked() bool {
	return r != LockReasonNone
}

// BranchType indicates the type of branch
type BranchType string

// Branch types
const (
	BranchTypeUser           BranchType = "user"            // Normal stacked branch
	BranchTypeUtility        BranchType = "utility"         // Created by st merge --consolidate or other internal tasks
	BranchTypeWorktreeAnchor BranchType = "worktree-anchor" // Anchor branch for worktree, has no commits
)

// Meta represents branch metadata stored in Git refs.
// Fields are unexported to enforce immutability — use getters to read
// and With* methods to create modified copies. Construct via NewMeta()
// or NewMetaFrom(MetaFields{...}).
type Meta struct {
	parentBranchName     *string
	parentBranchRevision *string
	prInfo               *PrInfoPersistence
	scope                *string
	lockReason           LockReason

	// Fields for remote sync
	branchType     BranchType
	lastModifiedBy *ModifiedBy
	lastModifiedAt *time.Time
	localOnlyHash  *string

	// mergedDownstack preserves historical parent relationships when branches are reparented
	// due to merge/deletion. Ordered oldest to newest, limited to 5 entries max.
	mergedDownstack []MergedParent

	// stackID links this branch to a stack ref (refs/stackit/stacks/{stack-id}).
	stackID *string
}

// StackDescription holds stack-level title and description.
// This is stored on the root branch of a stack.
type StackDescription struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// IsEmpty returns true if both title and description are empty.
func (sd *StackDescription) IsEmpty() bool {
	return sd == nil || (sd.Title == "" && sd.Description == "")
}

// PRState is a GitHub pull-request state as reported by the API
// (GraphQL uppercase form). Empty means unknown.
type PRState string

const (
	PRStateOpen   PRState = "OPEN"
	PRStateMerged PRState = "MERGED"
	PRStateClosed PRState = "CLOSED"
)

// MergedParent represents a historical parent that was merged or deleted
type MergedParent struct {
	BranchName string   `json:"branchName"`
	PRNumber   *int     `json:"prNumber,omitempty"`
	PRState    *PRState `json:"prState,omitempty"` // MERGED or CLOSED
}

// LocalMeta represents branch metadata that is strictly local and never pushed
type LocalMeta struct {
	Frozen              bool   `json:"frozen,omitempty"`
	NeedsPRBodyUpdate   bool   `json:"needsPRBodyUpdate,omitempty"`
	NavigationCommentID *int64 `json:"navigationCommentId,omitempty"`
}

// LocalMetaMap is branch name -> local metadata, as returned by the batch
// local-metadata readers.
type LocalMetaMap map[string]*LocalMeta

// Get returns the local metadata for a branch, or nil if absent.
// Safe to call on a nil map.
func (m LocalMetaMap) Get(branchName string) *LocalMeta {
	return m[branchName]
}

// ModifiedBy represents information about who last modified the metadata
type ModifiedBy struct {
	GitName        string  `json:"gitName"`
	GitEmail       string  `json:"gitEmail"`
	GitHubUsername *string `json:"githubUsername,omitempty"`
}

// PrInfoPersistence represents PR information for persistence
type PrInfoPersistence struct {
	Number      *int        `json:"number,omitempty"`
	Base        *string     `json:"base,omitempty"`
	BaseSHA     *string     `json:"baseSHA,omitempty"`
	URL         *string     `json:"url,omitempty"`
	Title       *string     `json:"title,omitempty"`
	Body        *string     `json:"body,omitempty"`
	State       *PRState    `json:"state,omitempty"`
	IsDraft     *bool       `json:"isDraft,omitempty"`
	LockReason  *LockReason `json:"lockReason,omitempty"`
	MergeBranch *string     `json:"mergeBranch,omitempty"`
}

const (
	// MetadataRefPrefix is the prefix for Git refs where branch metadata is stored
	MetadataRefPrefix = "refs/stackit/metadata/"
	// LocalMetadataRefPrefix is the prefix for Git refs where local-only branch metadata is stored
	LocalMetadataRefPrefix = "refs/stackit/local-metadata/"
)

// MetadataRefName returns the full ref name for a branch's metadata.
func MetadataRefName(branchName string) string {
	return fmt.Sprintf("%s%s", MetadataRefPrefix, branchName)
}

// LocalMetadataRefName returns the full ref name for a branch's local metadata.
func LocalMetadataRefName(branchName string) string {
	return fmt.Sprintf("%s%s", LocalMetadataRefPrefix, branchName)
}
