package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/git"
)

// stackFallbackName is the fallback ID used when a branch name produces only special characters.
const stackFallbackName = "stack"

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
		result = stackFallbackName
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

	// Need to create a new stack ID.
	// Find the stack root to generate the ID.
	rootName := e.GetStackRootForBranch(branch)
	if rootName == "" {
		return "", fmt.Errorf("cannot determine stack root for branch %s", branch.GetName())
	}

	stackID := e.GenerateStackID(rootName)
	if err := e.writeNewStackMeta(ctx, stackID); err != nil {
		return "", fmt.Errorf("failed to create stack ref: %w", err)
	}

	// Set the stack ID on all branches in the stack
	if err := e.propagateStackID(ctx, rootName, stackID); err != nil {
		return "", fmt.Errorf("failed to propagate stack ID: %w", err)
	}

	return stackID, nil
}

// collectSubtree returns all branch names in the subtree rooted at branchName,
// including branchName itself, via depth-first traversal of childrenMap.
func (e *engineImpl) collectSubtree(branchName string) []string {
	var names []string
	var visit func(name string)
	visit = func(name string) {
		names = append(names, name)
		e.mu.RLock()
		children := make([]string, len(e.state.childrenMap[name]))
		copy(children, e.state.childrenMap[name])
		e.mu.RUnlock()
		for _, child := range children {
			visit(child)
		}
	}
	visit(branchName)
	return names
}

// batchSetStackID sets the stack ID on all given branches in a single atomic transaction.
func (e *engineImpl) batchSetStackID(ctx context.Context, branchNames []string, stackID string) error {
	if len(branchNames) == 0 {
		return nil
	}

	return e.WithRetry(ctx, func() error {
		metas, _ := e.batchReadMetadata(branchNames)

		tx := e.BeginTx(fmt.Sprintf("set stack ID: %d branches -> %s", len(branchNames), stackID))
		for _, name := range branchNames {
			meta := metas[name]
			if meta == nil {
				meta = git.NewMeta()
			}
			meta = meta.WithStackID(&stackID)
			if err := tx.UpdateMeta(name, meta); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	})
}

// propagateStackID sets the stack ID on a branch and all its descendants in a single transaction.
func (e *engineImpl) propagateStackID(ctx context.Context, branchName string, stackID string) error {
	return e.batchSetStackID(ctx, e.collectSubtree(branchName), stackID)
}

func (e *engineImpl) writeNewStackMeta(ctx context.Context, stackID string) error {
	stackMeta := &git.StackMeta{
		ID:        stackID,
		CreatedAt: timeNow(),
	}
	if userName, err := e.git.GetUserName(ctx); err == nil {
		stackMeta.CreatedBy = userName
	}
	return e.git.WriteStackMeta(stackID, stackMeta)
}

// AssignBranchesToNewStack creates a new stack metadata ref and assigns its ID
// to the provided branches. Use this for operations that intentionally extract
// a branch or subtree into an independent stack; plain reparenting preserves
// existing stack membership unless the new parent belongs to another stack.
func (e *engineImpl) AssignBranchesToNewStack(ctx context.Context, root Branch, branches Branches) (string, error) {
	if len(branches) == 0 {
		return "", nil
	}

	stackID := e.GenerateStackID(root.GetName())
	if err := e.writeNewStackMeta(ctx, stackID); err != nil {
		return "", fmt.Errorf("failed to create stack ref: %w", err)
	}
	if err := e.SetStackID(ctx, branches, stackID); err != nil {
		return "", fmt.Errorf("failed to assign stack ID: %w", err)
	}
	return stackID, nil
}

// SetStackID sets the stack ID on multiple branches atomically in a single transaction.
func (e *engineImpl) SetStackID(ctx context.Context, branches Branches, stackID string) error {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.GetName()
	}
	return e.batchSetStackID(ctx, names, stackID)
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

// syncStackIDFromParent updates a branch's stack ID to match its parent's and
// propagates that stack ID to all descendants. Called automatically at the end
// of ReparentBranch/ReparentBranches so callers don't need to remember; not
// exported because external callers should reparent via ReparentBranch instead
// of poking the stack ID directly.
//
// Returns nil if the parent is trunk (keeps existing stack membership), the
// parent has no stack ID (legacy branch), or the branch is already in sync.
func (e *engineImpl) syncStackIDFromParent(ctx context.Context, branch Branch) error {
	parent := branch.GetParent()
	if parent == nil || e.IsTrunk(*parent) {
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

	// Propagate to descendants so a cross-stack reparent moves the whole
	// subtree under the new stack, not just the reparented branch.
	return e.propagateStackID(ctx, branch.GetName(), parentStackID)
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
