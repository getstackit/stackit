// Package engine provides the core branch state management interface and implementation.
// It tracks branch relationships, metadata, and provides operations for querying
// and manipulating the branch stack.
// Package engine manages the state and relationships of stacked branches.
//
// It is the core of stackit, responsible for:
//   - Tracking parent-child relationships between branches
//   - Storing and retrieving branch metadata (PR info, status, etc.)
//   - Managing the branch stack structure
//   - Coordinating branch operations like splitting, squashing, and restacking
//
// The engine abstracts the underlying storage (git refs, notes) and provides
// a high-level interface for branch management.
package engine

import (
	"context"

	"github.com/getstackit/stackit/internal/git"
)

// PRManager provides operations for managing pull request information
// Thread-safe: All methods are safe for concurrent use
type PRManager interface {
	UpsertPrInfo(ctx context.Context, branch Branch, prInfo *PrInfo) error
	ReadBranchRemoteStatuses(ctx context.Context, branches Branches) BranchRemoteStatuses
	PushBranch(ctx context.Context, branch Branch, remote string, opts git.PushOptions) error
	// Navigation comment ID caching (stored in local metadata)
	GetNavigationCommentID(branch Branch) (int64, error)
	SetNavigationCommentID(branch Branch, commentID int64) error
	ClearNavigationCommentID(branch Branch) error
}

// SyncManager provides operations for syncing and restacking branches
// Thread-safe: All methods are safe for concurrent use
type SyncManager interface {
	// Sync operations
	PullTrunk(ctx context.Context) (PullResult, error)
	UpdateTrunkFromRemote(ctx context.Context) (PullResult, error)
	ResetTrunkToRemote(ctx context.Context) error
	PlanRestack(ctx context.Context, branches Branches) (*RestackPlan, error)
	RestackBranches(ctx context.Context, branches Branches) (RestackBatchResult, error)
	RestackBranchesWithProgress(ctx context.Context, branches Branches, progress RestackBranchProgressFunc) (RestackBatchResult, error)
	RestackBranchesWithValidatedRebases(ctx context.Context, branches Branches, validation *RebaseValidation, progress RestackBranchProgressFunc) (RestackBatchResult, error)
	RestackBranchesWithValidatedPlan(ctx context.Context, branches Branches, validation *RebaseValidation, plan *RestackPlan, progress RestackBranchProgressFunc) (RestackBatchResult, error)
	ContinueRebase(ctx context.Context, branchName string, rebasedBranchBase string) (ContinueRebaseResult, error)
	Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RestackResult, error)

	// Validation
	ValidateRebases(ctx context.Context, specs []RebaseSpec) (*RebaseValidation, error)
	ValidateRebasesParallel(ctx context.Context, specs []RebaseSpec) (*RebaseValidation, error)
}

// StackRewriter provides operations for modifying commit history and branch structure
// Thread-safe: All methods are safe for concurrent use
type StackRewriter interface {
	// SquashCurrentBranch squashes commits on the current branch
	SquashCurrentBranch(ctx context.Context, opts SquashOptions) error

	// ApplySplitToCommits creates branches at specified commit points
	ApplySplitToCommits(ctx context.Context, opts ApplySplitOptions) error

	// Detach detaches HEAD to a specific revision
	Detach(ctx context.Context, revision string) error

	// DetachAndResetBranchChanges detaches and resets branch changes
	DetachAndResetBranchChanges(ctx context.Context, branchName string) error

	// ForceCheckoutBranch force checks out a branch
	ForceCheckoutBranch(ctx context.Context, branch Branch) error
}

// RemoteMetadataManager provides operations for syncing branch metadata with remote
type RemoteMetadataManager interface {
	IsRemoteSyncEnabled() bool
	SetRemoteSyncEnabled(enabled bool)
	BatchSetLastModifiedBy(branchNames []string) error
	LoadRemoteMetadataCache() error
	ApplyRemoteMetadataIfExists(branchName string) error
	ApplyRemoteMetadataForBranches(ctx context.Context, branchNames []string) error
	GetRemoteMetadataCache() RemoteMetadataView
	ComputeMetadataDiff(branch string) (*MetadataDiff, error)
	ComputeAllMetadataDiffs() ([]*MetadataDiff, error)
	AcceptRemoteMetadata(branch string) error
	RejectRemoteMetadata(branch string)
	HasLocalModifications(branch string) bool
	FindOrphanedLocalMetadata() ([]OrphanedMetadataInfo, error)
	DeleteLocalMetadataHash(branchName string) error
	DeleteMetadata(ctx context.Context, branchName string) error
	FetchRemoteMetadata(ctx context.Context) error
	ConfigureRemoteMetadataSync(ctx context.Context) error
	// TestRemoteMetadataCompatibility probes the configured remote to verify it
	// accepts the metadata-ref namespace. Adapter code should call this instead
	// of reaching to the git runner directly.
	TestRemoteMetadataCompatibility(ctx context.Context) error
	// PrepareRemoteMetadataPush verifies remote metadata refs are supported and
	// enables local metadata sync state before pushing metadata refs.
	PrepareRemoteMetadataPush(ctx context.Context) error
	// PushMetadataForBranches pushes the metadata refs for the given branch
	// names to origin.
	PushMetadataForBranches(ctx context.Context, branchNames []string) error
	// ConfigureStackMetadataSync adds the stack-metadata refspec to the configured remote.
	ConfigureStackMetadataSync(ctx context.Context) error
	// FetchStackMetadata fetches stack-metadata refs from the configured remote.
	FetchStackMetadata(ctx context.Context) error
	// ListStackMetadata returns a map of local stack IDs to their ref SHAs.
	ListStackMetadata() (map[string]string, error)
	// DeleteStackMetadata removes a single local stack-metadata ref.
	DeleteStackMetadata(ctx context.Context, stackID string) error
	// DeleteRemoteStackMetadata pushes ref-deletions for the given stack IDs.
	DeleteRemoteStackMetadata(ctx context.Context, stackIDs []string) error
	// GetStackIDsForBranches returns the unique stack IDs for the given branches.
	// This is used to determine which stack refs need to be pushed to remote.
	GetStackIDsForBranches(branches Branches) []string
}

// RemoteFetchRequest describes a single remote fetch that may update several
// branch and Stackit metadata namespaces in one network round trip.
type RemoteFetchRequest struct {
	Remote               string
	Branches             []string
	IncludeMetadata      bool
	IncludeStackMetadata bool
}

// RemoteFetcher provides batched remote fetch operations.
type RemoteFetcher interface {
	FetchRemote(ctx context.Context, req RemoteFetchRequest) error
}

// ApplySplitOptions contains options for applying a split
type ApplySplitOptions struct {
	BranchToSplit string   // The branch being split
	BranchNames   []string // Branch names from oldest to newest
	BranchPoints  []int    // Commit indices (0 = HEAD, 1 = HEAD~1, etc.)
	// AsSibling creates all split branches as siblings on the same parent,
	// instead of creating a linear chain. When true:
	// - All new branches share the same parent (the original branch's parent)
	// - Branches are independent rather than stacked on each other
	AsSibling bool
}

// LoadMode controls how much state the engine populates at construction time.
// Lighter modes defer metadata reads until the first accessor that needs them
// triggers a promotion via ensureSharedLoaded / ensureLocalLoaded.
type LoadMode int

const (
	// LoadModeFull reads both shared and local metadata at construction.
	// This is today's behavior and the default. Used by tests and any
	// command that needs lock/frozen/scope/PR data for every branch.
	LoadModeFull LoadMode = iota

	// LoadModeShared reads shared metadata at construction but defers local
	// metadata (Frozen, NeedsPRBodyUpdate, NavigationCommentID) until the
	// first local-only accessor is called. Saves the BatchReadLocalMetadata
	// pass for commands that never touch frozen/needs-PR-update state.
	LoadModeShared

	// LoadModeBranchesOnly reads only the branch list at construction.
	// Shared metadata is loaded on the first state access; local metadata on
	// the first local-only accessor. Intended for read-only navigation
	// commands (parent, children, trunk, co <exact>) that only need a
	// single branch's metadata, not every branch in the repo.
	LoadModeBranchesOnly
)

// Options contains configuration options for creating an Engine
type Options struct {
	// RepoRoot is the root directory of the Git repository
	RepoRoot string

	// Trunk is the primary trunk branch name (e.g., "main", "master")
	Trunk string

	// MaxUndoStackDepth is the maximum number of undo snapshots to keep.
	// If zero or negative, defaults to DefaultMaxUndoStackDepth (10).
	MaxUndoStackDepth int

	// MaxConcurrency is the maximum number of concurrent validation operations.
	// If zero or negative, defaults to min(NumCPU, 8).
	MaxConcurrency int

	// Git is the git runner to use. If nil, a default real git runner is used.
	Git git.Runner

	// LoadMode controls how much metadata is read at construction time.
	// Zero value (LoadModeFull) matches the pre-lite behavior — readers see
	// all metadata populated synchronously after NewEngine returns.
	LoadMode LoadMode
}

// UndoManager provides operations for undo/redo functionality
// Thread-safe: All methods are safe for concurrent use
type UndoManager interface {
	TakeSnapshot(opts SnapshotOptions) error
	GetSnapshots() ([]SnapshotInfo, error)
	LoadSnapshot(snapshotID string) (*Snapshot, error)
	RestoreSnapshot(ctx context.Context, snapshotID string) error
}

// Engine is the core interface for branch state management
// It composes BranchReader, BranchWriter, PRManager, SyncManager, HistoryRewriter, and UndoManager
// for backward compatibility. New code should prefer using the smaller interfaces.
// Thread-safe: All methods are safe for concurrent use
type Engine interface {
	BranchReader
	BranchWriter
	PRManager
	SyncManager
	StackRewriter
	Absorber
	UndoManager
	RemoteMetadataManager
	RemoteFetcher
	WorktreeRegistry
	Git() git.Runner

	// SnapshotForWorktree creates a deep copy of engine state for initializing
	// worktree engines without the cost of rebuildInternal.
	SnapshotForWorktree() WorktreeSnapshot
}
