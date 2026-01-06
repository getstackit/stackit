package git

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WorktreeRefPrefix is the prefix for Git refs where worktree metadata is stored (local-only)
const WorktreeRefPrefix = "refs/stackit/worktrees/"

// WorktreeMeta represents worktree tracking metadata stored in local Git refs
type WorktreeMeta struct {
	Path        string    `json:"path"`        // Absolute path to worktree
	StackRoot   string    `json:"stackRoot"`   // First branch created from trunk (stack root)
	CreatedAt   time.Time `json:"createdAt"`   // When worktree was created
	MainRepoDir string    `json:"mainRepoDir"` // Path to main repo (for detection)
}

func (r *runner) AddWorktree(ctx context.Context, path string, branch string, detach bool) error {
	args := []string{"worktree", "add"}
	if detach {
		args = append(args, "--detach")
	}
	args = append(args, path)
	if branch != "" {
		args = append(args, branch)
	}

	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to add worktree at %s: %w", path, err)
	}
	return nil
}

func (r *runner) RemoveWorktree(ctx context.Context, path string) error {
	_, err := r.RunGitCommandWithContext(ctx, "worktree", "remove", "--force", path)
	if err != nil {
		return fmt.Errorf("failed to remove worktree at %s: %w", path, err)
	}
	return nil
}

func (r *runner) ListWorktrees(ctx context.Context) ([]string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	if output == "" {
		return []string{}, nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	var worktrees []string
	for _, line := range lines {
		if len(line) > 9 && line[:9] == "worktree " {
			worktrees = append(worktrees, line[9:])
		}
	}
	return worktrees, nil
}

// ReadWorktreeMeta reads worktree metadata for a stack root from local git refs
func (r *runner) ReadWorktreeMeta(stackRoot string) (*WorktreeMeta, error) {
	refName := fmt.Sprintf("%s%s", WorktreeRefPrefix, stackRoot)

	sha, err := r.GetRef(refName)
	if err != nil {
		// If ref doesn't exist, it's not an error, just means no worktree registered
		return nil, nil //nolint:nilerr
	}

	content, err := r.ReadBlob(sha)
	if err != nil {
		return nil, fmt.Errorf("failed to read worktree metadata blob %s: %w", sha, err)
	}

	if content == "" {
		return nil, nil
	}

	var meta WorktreeMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal worktree metadata for %s: %w", stackRoot, err)
	}

	return &meta, nil
}

// WriteWorktreeMeta writes worktree metadata for a stack root to local git refs
func (r *runner) WriteWorktreeMeta(stackRoot string, meta *WorktreeMeta) error {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal worktree metadata: %w", err)
	}

	sha, err := r.CreateBlob(string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create worktree metadata blob: %w", err)
	}

	refName := fmt.Sprintf("%s%s", WorktreeRefPrefix, stackRoot)
	if err := r.UpdateRef(refName, sha); err != nil {
		return fmt.Errorf("failed to write worktree metadata ref: %w", err)
	}

	return nil
}

// DeleteWorktreeMeta deletes worktree metadata for a stack root
func (r *runner) DeleteWorktreeMeta(stackRoot string) error {
	refName := fmt.Sprintf("%s%s", WorktreeRefPrefix, stackRoot)
	return r.DeleteRef(refName)
}

// GetWorktreePathForBranch returns the worktree path where a branch is checked out.
// Returns empty string if the branch is not checked out in any worktree.
func (r *runner) GetWorktreePathForBranch(ctx context.Context, branchName string) (string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}

	if output == "" {
		return "", nil
	}

	// Parse porcelain output to find worktree with this branch
	// Format:
	// worktree /path/to/worktree
	// HEAD abc123
	// branch refs/heads/branchname
	// (blank line)
	lines := strings.Split(output, "\n")
	var currentWorktree string
	targetRef := "refs/heads/" + branchName

	for _, line := range lines {
		if strings.HasPrefix(line, "worktree ") {
			currentWorktree = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			branch := strings.TrimPrefix(line, "branch ")
			if branch == targetRef && currentWorktree != "" {
				return currentWorktree, nil
			}
		}
	}

	return "", nil
}

// ResetWorktreeWorkingDir resets a worktree's working directory to match HEAD.
// This is used after updating a branch ref to sync the worktree's working directory.
func (r *runner) ResetWorktreeWorkingDir(ctx context.Context, worktreePath string) error {
	_, err := r.RunGitCommandWithContext(ctx, "-C", worktreePath, "reset", "--hard", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to reset worktree at %s: %w", worktreePath, err)
	}
	return nil
}

// GetWorktreeCurrentBranch returns the name of the branch currently checked out in a worktree.
// Returns empty string if the worktree is in detached HEAD state.
func (r *runner) GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error) {
	output, err := r.RunGitCommandWithContext(ctx, "-C", worktreePath, "rev-parse", "--abbrev-ref", detachedHEAD)
	if err != nil {
		return "", fmt.Errorf("failed to get current branch in worktree %s: %w", worktreePath, err)
	}

	branch := strings.TrimSpace(output)
	if branch == detachedHEAD {
		// Detached HEAD state
		return "", nil
	}
	return branch, nil
}

// ListWorktreeMetas lists all registered worktree metadata
func (r *runner) ListWorktreeMetas() (map[string]*WorktreeMeta, error) {
	refs, err := r.ListRefs(WorktreeRefPrefix)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*WorktreeMeta)
	for refName, sha := range refs {
		stackRoot := strings.TrimPrefix(refName, WorktreeRefPrefix)

		content, err := r.ReadBlob(sha)
		if err != nil {
			continue // Skip unreadable entries
		}

		if content == "" {
			continue
		}

		var meta WorktreeMeta
		if err := json.Unmarshal([]byte(content), &meta); err != nil {
			continue // Skip invalid entries
		}

		result[stackRoot] = &meta
	}

	return result, nil
}
