package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// Branch represents a branch in the stack
type Branch struct {
	name   string
	reader branchReader
}

type branchReader interface {
	BranchReader
	// Branch-internal accessors. These complement the public BranchReader
	// methods that operate on branch names with versions that take a Branch
	// value, used by Branch's own getters. Engine is the only implementor.
	GetParent(branch Branch) *Branch
	GetMergedDownstack(branch Branch) []git.MergedParent
	GetExplicitScope(branch Branch) Scope
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

// GetAllCommits returns commits for this branch in various formats
func (b Branch) GetAllCommits(format CommitFormat) ([]string, error) {
	return b.reader.GetAllCommits(b, format)
}

// GetParent returns the parent branch (nil if no parent)
func (b Branch) GetParent() *Branch {
	return b.reader.GetParent(b)
}

// GetPrInfo returns PR information for this branch
func (b Branch) GetPrInfo() (*PrInfo, error) {
	return b.reader.GetPrInfo(b)
}

// HasPR reports whether this branch has a submitted PR number recorded.
func (b Branch) HasPR() bool {
	prInfo, err := b.GetPrInfo()
	return err == nil && prInfo != nil && prInfo.Number() != nil
}

// GetMergedDownstack returns the merged downstack history for this branch
func (b Branch) GetMergedDownstack() []git.MergedParent {
	return b.reader.GetMergedDownstack(b)
}

// GetExplicitScope returns the explicit scope set for this branch (no inheritance)
func (b Branch) GetExplicitScope() Scope {
	return b.reader.GetExplicitScope(b)
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
