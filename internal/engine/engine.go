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

	"stackit.dev/stackit/internal/git"
)

// PRManager provides operations for managing pull request information
// Thread-safe: All methods are safe for concurrent use
type PRManager interface {
	UpsertPrInfo(branch Branch, prInfo *PrInfo) error
	BranchMatchesRemote(branchName string) (bool, error)
	PopulateRemoteShas() error
	PushBranch(ctx context.Context, branchName string, remote string, opts git.PushOptions) error
}

// SyncManager provides operations for syncing and restacking branches
// Thread-safe: All methods are safe for concurrent use
type SyncManager interface {
	// Sync operations
	PullTrunk(ctx context.Context) (PullResult, error)
	ResetTrunkToRemote(ctx context.Context) error
	RestackBranches(ctx context.Context, branches []Branch) (RestackBatchResult, error)
	ContinueRebase(ctx context.Context, branchName string, rebasedBranchBase string) (ContinueRebaseResult, error)
	Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RestackResult, error)
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
	SetLastModifiedBy(branchName string) error
	LoadRemoteMetadataCache() error
	ApplyRemoteMetadataIfExists(branchName string) error
	GetRemoteMetadataCache() map[string]*git.Meta
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
}

// ApplySplitOptions contains options for applying a split
type ApplySplitOptions struct {
	BranchToSplit string   // The branch being split
	BranchNames   []string // Branch names from oldest to newest
	BranchPoints  []int    // Commit indices (0 = HEAD, 1 = HEAD~1, etc.)
}

// Options contains configuration options for creating an Engine
type Options struct {
	// RepoRoot is the root directory of the Git repository
	RepoRoot string

	// Trunk is the primary trunk branch name (e.g., "main", "master")
	Trunk string

	// MaxUndoStackDepth is the maximum number of undo snapshots to keep.
	// If zero or negative, defaults to DefaultMaxUndoStackDepth (10).
	MaxUndoStackDepth int

	// Git is the git runner to use. If nil, a default real git runner is used.
	Git git.Runner
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
	Git() git.Runner
}
