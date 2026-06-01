// Package worktree provides utilities for executing operations in Git worktrees.
package worktree

import (
	"context"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
)

// Session represents an active worktree session that can be used to execute operations.
// The caller must call Close() when done to clean up the worktree.
type Session struct {
	Path    string        // Path to the worktree directory
	Engine  engine.Engine // Engine initialized for the worktree
	cleanup func()        // Internal cleanup function
	output  output.Output
}

// Close cleans up the worktree session. This should be called when done with the session.
func (s *Session) Close() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

// Executor creates and manages temporary worktrees for executing operations.
type Executor struct {
	eng    engine.Engine
	output output.Output
}

// NewExecutor creates a new worktree executor.
func NewExecutor(eng engine.Engine, out output.Output) *Executor {
	return &Executor{
		eng:    eng,
		output: out,
	}
}

// CreateSessionOptions configures how a worktree session is created.
type CreateSessionOptions struct {
	// Ref is the git ref (branch, commit, tag) to checkout in the worktree.
	// If empty, defaults to trunk.
	Ref string

	// NamePattern is the pattern for the worktree directory name.
	// Uses Go's os.MkdirTemp pattern (e.g., "stackit-work-*").
	NamePattern string

	// PullTrunk determines whether to pull the latest trunk before returning.
	// Only applicable when Ref is trunk or empty.
	PullTrunk bool
}

// CreateSession creates a new worktree session.
// The caller is responsible for calling Close() on the returned session.
func (e *Executor) CreateSession(ctx context.Context, opts CreateSessionOptions) (*Session, error) {
	ref := opts.Ref
	if ref == "" {
		ref = e.eng.Trunk().GetName()
	}

	pattern := opts.NamePattern
	if pattern == "" {
		pattern = "stackit-worktree-*"
	}

	// Create temporary worktree
	worktreePath, cleanup, err := e.eng.CreateTemporaryWorktree(ctx, ref, pattern)
	if err != nil {
		return nil, errors.FailedTo("create", "worktree", err)
	}

	e.output.Debug("Created worktree at %s", worktreePath)

	// Create engine for worktree using parent's snapshot to skip rebuildInternal
	worktreeEng, err := engine.NewEngineForWorktree(engine.WorktreeEngineOptions{
		WorktreePath: worktreePath,
		Snapshot:     e.eng.SnapshotForWorktree(),
	})
	if err != nil {
		cleanup()
		return nil, errors.FailedTo("initialize", "worktree engine", err)
	}

	session := &Session{
		Path:    worktreePath,
		Engine:  worktreeEng,
		cleanup: cleanup,
		output:  e.output,
	}

	// Optionally pull trunk to ensure we're up to date
	if opts.PullTrunk {
		if err := session.pullTrunk(ctx); err != nil {
			session.Close()
			return nil, err
		}
	}

	return session, nil
}

// pullTrunk updates trunk in the worktree to match remote.
func (s *Session) pullTrunk(ctx context.Context) error {
	pullResult, err := s.Engine.PullTrunk(ctx)
	if err != nil {
		return errors.FailedTo("update", "trunk in worktree", err)
	}
	if pullResult == engine.PullConflict {
		return errors.New("trunk could not be fast-forwarded (diverged from remote)")
	}

	// IMPORTANT: Use ResetHard instead of CheckoutBranch to stay at detached HEAD.
	// CheckoutBranch would put us ON the trunk branch, and any subsequent commits
	// (especially merges) would update refs/heads/main globally, affecting the
	// user's main workspace. ResetHard updates the working tree to match trunk
	// while keeping HEAD detached (since the worktree was created with --detach).
	trunk := s.Engine.Trunk()
	if err := s.Engine.ResetHard(ctx, trunk.GetName()); err != nil {
		return errors.FailedTo("reset to", "trunk in worktree", err)
	}

	return nil
}

// ResetToRef resets the worktree to the specified ref.
func (s *Session) ResetToRef(ctx context.Context, ref string) error {
	return s.Engine.ResetHard(ctx, ref)
}

// ResetToTrunk resets the worktree to trunk.
func (s *Session) ResetToTrunk(ctx context.Context) error {
	return s.Engine.ResetHard(ctx, s.Engine.Trunk().GetName())
}

// GetCurrentRevision returns the current commit SHA of the worktree.
func (s *Session) GetCurrentRevision(ctx context.Context) (string, error) {
	return s.Engine.GetCurrentRevision(ctx)
}
