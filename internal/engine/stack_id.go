package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// timeNow is a variable for time.Now to allow mocking in tests.
// Used by stack-ID generation, stack metadata creation, and worktree registration.
var timeNow = time.Now

// GetStackDescription returns the stack description for a branch's stack.
// It reads from the stack ref using the branch's StackID.
// Returns nil for untracked branches or if the stack has no description.
func (e *engineImpl) GetStackDescription(branch Branch) *git.StackDescription {
	stackID := e.GetStackID(branch)
	if stackID == "" {
		return nil
	}

	stackMeta, err := e.git.ReadStackMeta(stackID)
	if err != nil || stackMeta == nil {
		return nil
	}

	return stackMeta.StackDescription()
}

// SetStackDescription sets the stack description in the stack ref for a branch.
// Creates stack metadata lazily if it doesn't exist.
// Returns an error if the branch is not tracked.
func (e *engineImpl) SetStackDescription(ctx context.Context, branch Branch, desc *git.StackDescription) error {
	if !e.IsTracked(branch) {
		return fmt.Errorf("branch %s is not tracked", branch.GetName())
	}

	// Ensure stack ID exists (creates lazily if needed)
	stackID, err := e.EnsureStackID(ctx, branch)
	if err != nil {
		return fmt.Errorf("failed to ensure stack ID: %w", err)
	}

	// Read existing stack meta or create new one
	stackMeta, err := e.git.ReadStackMeta(stackID)
	if err != nil {
		return fmt.Errorf("failed to read stack metadata for %s: %w", stackID, err)
	}

	if stackMeta == nil {
		stackMeta = &git.StackMeta{
			ID:        stackID,
			CreatedAt: timeNow(),
		}
	}

	// Update title and description
	if desc != nil {
		stackMeta.Title = desc.Title
		stackMeta.Description = desc.Description
	} else {
		stackMeta.Title = ""
		stackMeta.Description = ""
	}

	if err := e.git.WriteStackMeta(stackID, stackMeta); err != nil {
		return fmt.Errorf("failed to write stack metadata for %s: %w", stackID, err)
	}

	return nil
}

// ClearStackDescription removes the stack description from the stack ref.
func (e *engineImpl) ClearStackDescription(ctx context.Context, branch Branch) error {
	return e.SetStackDescription(ctx, branch, nil)
}

// GenerateStackID creates a new stack ID for a new stack.
// Format: {timestamp-nanos}-{sanitized-root-branch}
func (e *engineImpl) GenerateStackID(rootBranch string) string {
	timestamp := timeNow().UnixNano()
	sanitized := sanitizeBranchNameForStackID(rootBranch)
	return fmt.Sprintf("%d-%s", timestamp, sanitized)
}

// sanitizeBranchNameForStackID converts a branch name into a safe suffix for stack IDs.
// Only allows alphanumeric characters and hyphens to ensure cross-platform compatibility
// with Git ref names. Limited to 50 chars to keep ref names reasonable and avoid
// filesystem path length issues on Windows.
func sanitizeBranchNameForStackID(branchName string) string {
	// Single-pass: replace unsafe chars with hyphens and collapse consecutive hyphens
	var b strings.Builder
	b.Grow(len(branchName))
	prevHyphen := true // Start true to skip leading hyphens

	for _, r := range branchName {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}

	result := b.String()

	// Limit length first, then trim trailing hyphen once
	if len(result) > 50 {
		result = result[:50]
	}
	result = strings.TrimSuffix(result, "-")

	// Fallback for empty result (branch name was all special chars)
	if result == "" {
		result = "stack"
	}

	return result
}

// GetStackID returns the stack ID for a branch.
// Returns empty string for untracked branches or trunk.
// For legacy branches without StackID, derives it from the stack root.
func (e *engineImpl) GetStackID(branch Branch) string {
	branchName := branch.GetName()

	// Trunk has no stack ID
	if branchName == e.trunk {
		return ""
	}

	// Check if branch is tracked
	if !e.IsTracked(branch) {
		return ""
	}

	// Read branch metadata
	meta, err := e.readMetadata(branchName)
	if err != nil {
		return ""
	}

	// Return explicit StackID if present
	if meta.GetStackID() != nil && *meta.GetStackID() != "" {
		return *meta.GetStackID()
	}

	// Legacy fallback: derive stack ID from stack root
	rootName := e.GetStackRootForBranch(branch)
	if rootName == "" {
		return ""
	}

	rootMeta, err := e.readMetadata(rootName)
	if err != nil || rootMeta == nil || rootMeta.GetStackID() == nil {
		return ""
	}

	return *rootMeta.GetStackID()
}

// EnsureStackID returns the stack ID for a branch, creating one if it doesn't exist.
// This is used for lazy creation of stack metadata when setting descriptions or scopes.
func (e *engineImpl) EnsureStackID(ctx context.Context, branch Branch) (string, error) {
	// First check if stack ID already exists
	existingID := e.GetStackID(branch)
	if existingID != "" {
		return existingID, nil
	}

	// Need to create a new stack ID
	// Find the stack root to generate the ID
	rootName := e.GetStackRootForBranch(branch)
	if rootName == "" {
		return "", fmt.Errorf("cannot determine stack root for branch %s", branch.GetName())
	}

	// Generate new stack ID based on the root branch
	stackID := e.GenerateStackID(rootName)

	// Create the stack metadata ref
	stackMeta := &git.StackMeta{
		ID:        stackID,
		CreatedAt: timeNow(),
	}

	// Try to get user name for CreatedBy
	if userName, err := e.git.GetUserName(ctx); err == nil {
		stackMeta.CreatedBy = userName
	}

	if err := e.git.WriteStackMeta(stackID, stackMeta); err != nil {
		return "", fmt.Errorf("failed to create stack ref: %w", err)
	}

	// Set the stack ID on all branches in the stack
	if err := e.propagateStackID(ctx, rootName, stackID); err != nil {
		return "", fmt.Errorf("failed to propagate stack ID: %w", err)
	}

	return stackID, nil
}

// propagateStackID sets the stack ID on a branch and all its descendants.
func (e *engineImpl) propagateStackID(ctx context.Context, branchName string, stackID string) error {
	branch := e.GetBranch(branchName)
	if err := e.SetStackID(ctx, branch, stackID); err != nil {
		return err
	}

	// Get children from the map
	e.mu.RLock()
	children := make([]string, len(e.state.childrenMap[branchName]))
	copy(children, e.state.childrenMap[branchName])
	e.mu.RUnlock()

	// Recursively set on children
	for _, childName := range children {
		if err := e.propagateStackID(ctx, childName, stackID); err != nil {
			return err
		}
	}

	return nil
}

// SetStackID sets the stack ID on a branch's metadata.
func (e *engineImpl) SetStackID(ctx context.Context, branch Branch, stackID string) error {
	branchName := branch.GetName()

	return e.WithRetry(ctx, func() error {
		meta, err := e.readMetadata(branchName)
		if err != nil {
			return fmt.Errorf("failed to read metadata: %w", err)
		}
		if meta == nil {
			meta = git.NewMeta()
		}

		meta = meta.WithStackID(&stackID)

		tx := e.BeginTx(fmt.Sprintf("set stack ID: %s -> %s", branchName, stackID))
		if err := tx.UpdateMeta(branchName, meta); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
}

// CreateStackRef creates a new stack ref with the given metadata.
func (e *engineImpl) CreateStackRef(stackID string, meta *git.StackMeta) error {
	if meta == nil {
		meta = &git.StackMeta{
			ID:        stackID,
			CreatedAt: timeNow(),
		}
	}
	return e.git.WriteStackMeta(stackID, meta)
}

// GetStackMeta returns the stack metadata for a stack ID.
func (e *engineImpl) GetStackMeta(stackID string) (*git.StackMeta, error) {
	return e.git.ReadStackMeta(stackID)
}

// SyncStackIDFromParent updates a branch's stack ID to match its parent's.
// This should be called after reparenting operations to keep stack IDs consistent.
// Returns nil if the parent is trunk (keeps existing stack ID) or if no change is needed.
func (e *engineImpl) SyncStackIDFromParent(ctx context.Context, branch Branch) error {
	parent := branch.GetParent()
	if parent == nil {
		// Parent is trunk - keep existing stack ID
		return nil
	}

	parentStackID := e.GetStackID(*parent)
	if parentStackID == "" {
		// Parent has no stack ID (legacy branch) - no action needed
		return nil
	}

	currentStackID := e.GetStackID(branch)
	if currentStackID == parentStackID {
		// Already in sync - no change needed
		return nil
	}

	return e.SetStackID(ctx, branch, parentStackID)
}

// GetStackIDsForBranches returns the unique stack IDs for the given branches.
// This is used to determine which stack refs need to be pushed to remote.
func (e *engineImpl) GetStackIDsForBranches(branches Branches) []string {
	seen := make(map[string]bool)
	var stackIDs []string

	for _, branch := range branches {
		stackID := e.GetStackID(branch)
		if stackID != "" && !seen[stackID] {
			seen[stackID] = true
			stackIDs = append(stackIDs, stackID)
		}
	}

	return stackIDs
}
