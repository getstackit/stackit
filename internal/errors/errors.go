// Package errors provides sentinel errors and custom error types for the stackit application.
// Use errors.Is() and errors.As() to check for specific error types.
package errors

import (
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

// Standard library error functions wrapped for convenience
var (
	Is     = errors.Is
	As     = errors.As
	New    = errors.New
	Unwrap = errors.Unwrap
)

// Sentinel errors for common conditions
var (
	// ErrNotOnBranch indicates that HEAD is not on a branch
	ErrNotOnBranch = errors.New("not on a branch")

	// ErrNotOnBranchNoBranchSpecified indicates HEAD is not on a branch and no branch was specified
	ErrNotOnBranchNoBranchSpecified = errors.New("not on a branch and no branch specified")

	// ErrBranchNotFound indicates that a branch does not exist
	ErrBranchNotFound = errors.New("branch not found")

	// ErrRebaseConflict indicates that a rebase operation encountered a conflict
	ErrRebaseConflict = errors.New("rebase conflict")

	// ErrRebaseNotInProgress indicates that no rebase is currently in progress
	ErrRebaseNotInProgress = errors.New("no rebase in progress")

	// ErrTrunkOperation indicates an invalid operation on the trunk branch
	ErrTrunkOperation = errors.New("invalid operation on trunk branch")

	// ErrBranchModificationRestricted indicates a branch cannot be modified due to its state (locked or frozen)
	ErrBranchModificationRestricted = errors.New("branch modification restricted")

	// ErrCanceled indicates that an interactive operation was canceled by the user
	ErrCanceled = errors.New("canceled")

	// ErrBack indicates that the user wants to go back to the previous step
	ErrBack = errors.New("back")
)

// BranchNotFoundError represents an error when a branch is not found
type BranchNotFoundError struct {
	BranchName string
}

func (e *BranchNotFoundError) Error() string {
	return fmt.Sprintf("branch %s does not exist", e.BranchName)
}

// Is returns true if the target error is ErrBranchNotFound
func (e *BranchNotFoundError) Is(target error) bool {
	return target == ErrBranchNotFound
}

// NewBranchNotFoundError creates a new BranchNotFoundError
func NewBranchNotFoundError(branchName string) *BranchNotFoundError {
	return &BranchNotFoundError{BranchName: branchName}
}

// BranchModificationError represents an error when a branch cannot be modified
type BranchModificationError struct {
	BranchName string
	LockReason git.LockReason
	IsFrozen   bool
}

func (e *BranchModificationError) Error() string {
	state := ""
	switch {
	case e.IsLocked() && e.IsFrozen:
		state = fmt.Sprintf("locked (%s) and frozen", e.LockReason)
	case e.IsLocked():
		state = fmt.Sprintf("locked (%s)", e.LockReason)
	case e.IsFrozen:
		state = "frozen"
	}
	return fmt.Sprintf("branch %s is %s", e.BranchName, state)
}

// IsLocked returns true if the branch is locked
func (e *BranchModificationError) IsLocked() bool {
	return e.LockReason.IsLocked()
}

// Is returns true if the target error is ErrBranchModificationRestricted
func (e *BranchModificationError) Is(target error) bool {
	return target == ErrBranchModificationRestricted
}

// NewBranchModificationError creates a new BranchModificationError
func NewBranchModificationError(branchName string, lockReason git.LockReason, frozen bool) *BranchModificationError {
	return &BranchModificationError{
		BranchName: branchName,
		LockReason: lockReason,
		IsFrozen:   frozen,
	}
}

// RebaseConflictError represents an error when a rebase encounters a conflict
type RebaseConflictError struct {
	BranchName string
	Message    string
}

func (e *RebaseConflictError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("rebase conflict on branch %s: %s", e.BranchName, e.Message)
	}
	return fmt.Sprintf("rebase conflict on branch %s", e.BranchName)
}

// Is returns true if the target error is ErrRebaseConflict
func (e *RebaseConflictError) Is(target error) bool {
	return target == ErrRebaseConflict
}

// NewRebaseConflictError creates a new RebaseConflictError
func NewRebaseConflictError(branchName string, message string) *RebaseConflictError {
	return &RebaseConflictError{
		BranchName: branchName,
		Message:    message,
	}
}
