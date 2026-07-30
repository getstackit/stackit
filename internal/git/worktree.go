package git

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// WorktreeRefPrefix is the prefix for Git refs where worktree metadata is stored (local-only)
const WorktreeRefPrefix = "refs/stackit/worktrees/"

// Worktree represents a single entry from `git worktree list`.
type Worktree struct {
	Path   string
	Branch string // empty when the worktree is at detached HEAD
}

// WorktreeList is the parsed result of one `git worktree list --porcelain`
// invocation. Callers should pass it down per-batch instead of calling
// ListWorktrees per branch — see PathForBranch for the common lookup.
type WorktreeList []Worktree

// IsMainWorktree reports whether worktreePath is the repo's main worktree
// (repoRoot), resolving symlinks on both sides for comparison (e.g. /var vs
// /private/var on macOS).
func IsMainWorktree(worktreePath, repoRoot string) bool {
	resolvedWorktree, _ := filepath.EvalSymlinks(worktreePath)
	resolvedRoot, _ := filepath.EvalSymlinks(repoRoot)
	return resolvedWorktree == resolvedRoot
}

// PathForBranch returns the worktree path where branchName is checked out,
// or "" if no worktree currently has it.
func (l WorktreeList) PathForBranch(branchName string) string {
	for _, w := range l {
		if w.Branch == branchName {
			return w.Path
		}
	}
	return ""
}

// Paths returns just the worktree paths.
func (l WorktreeList) Paths() []string {
	out := make([]string, len(l))
	for i, w := range l {
		out[i] = w.Path
	}
	return out
}

func (r *runner) ensureRefBranchesNotCheckedOut(ctx context.Context, refNames []string) error {
	branchNames := make([]string, 0, len(refNames))
	for _, refName := range refNames {
		if branchName, ok := strings.CutPrefix(refName, "refs/heads/"); ok {
			branchNames = append(branchNames, branchName)
		}
	}
	return r.ensureBranchesNotCheckedOut(ctx, branchNames)
}

func (r *runner) ensureBranchesNotCheckedOut(ctx context.Context, branchNames []string) error {
	if len(branchNames) == 0 {
		return nil
	}
	branchNames = slices.Compact(slices.Sorted(slices.Values(branchNames)))

	worktrees, err := r.ListWorktrees(ctx)
	if err != nil {
		return fmt.Errorf("failed to check worktrees: %w", err)
	}

	for _, worktree := range worktrees {
		if slices.Contains(branchNames, worktree.Branch) {
			return fmt.Errorf("branch %s is checked out in worktree %s", worktree.Branch, worktree.Path)
		}
	}
	return nil
}

// WorktreeMeta represents worktree tracking metadata stored in local Git refs
type WorktreeMeta struct {
	Name         string    `json:"name,omitempty"` // User-provided name for display (new worktrees only)
	Path         string    `json:"path"`           // Absolute path to worktree
	AnchorBranch string    `json:"stackRoot"`      // Anchor branch for worktree (JSON: stackRoot for backwards compat)
	CreatedAt    time.Time `json:"createdAt"`      // When worktree was created
	MainRepoDir  string    `json:"mainRepoDir"`    // Path to main repo (for detection)
}

// WorktreeDetachMode controls whether a new worktree checks out its branch
// normally or at a detached HEAD (`git worktree add --detach`).
type WorktreeDetachMode int

const (
	// WorktreeAttached checks out the branch normally (HEAD attached to it).
	WorktreeAttached WorktreeDetachMode = iota
	// WorktreeDetached adds the worktree at a detached HEAD.
	WorktreeDetached
)

func (r *runner) AddWorktree(ctx context.Context, path string, branch string, detach WorktreeDetachMode) error {
	return r.AddWorktreeWithOptions(ctx, path, branch, detach, false)
}

// AddWorktreeWithOptions adds a worktree with additional options
func (r *runner) AddWorktreeWithOptions(ctx context.Context, path string, branch string, detach WorktreeDetachMode, noCheckout bool) error {
	args := []string{"worktree", "add"}
	if detach == WorktreeDetached {
		args = append(args, "--detach")
	}
	if noCheckout {
		args = append(args, "--no-checkout")
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
	_, err := r.RunGitCommandWithContext(ctx, "worktree", "remove", path)
	if err != nil {
		return fmt.Errorf("failed to remove worktree at %s: %w", path, err)
	}
	return nil
}

func (r *runner) ForceRemoveWorktree(ctx context.Context, path string) error {
	_, err := r.RunGitCommandWithContext(ctx, "worktree", "remove", "--force", path)
	if err != nil {
		return fmt.Errorf("failed to remove worktree at %s: %w", path, err)
	}
	return nil
}

// ListWorktrees returns every working tree registered with the repo,
// including the branch (if any) checked out in each. Callers needing to look
// up many branches should call this once and reuse the result rather than
// invoking git per branch — see WorktreeList.PathForBranch.
func (r *runner) ListWorktrees(ctx context.Context) (WorktreeList, error) {
	output, err := r.RunGitCommandWithContext(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	if output == "" {
		return WorktreeList{}, nil
	}

	var result WorktreeList
	var current Worktree
	flush := func() {
		if current.Path != "" {
			result = append(result, current)
		}
		current = Worktree{}
	}
	for line := range strings.SplitSeq(output, "\n") {
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return result, nil
}

// PruneWorktrees removes stale worktree entries from .git/worktrees.
// This cleans up worktree information for worktrees whose working directory
// has been deleted or is otherwise unavailable. This should be called before
// creating new worktrees to prevent "failed to read commondir" errors caused
// by incomplete cleanup from previous operations.
func (r *runner) PruneWorktrees(ctx context.Context) error {
	_, err := r.RunGitCommandWithContext(ctx, "worktree", "prune")
	if err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}
	return nil
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
func (r *runner) DeleteWorktreeMeta(ctx context.Context, stackRoot string) error {
	refName := fmt.Sprintf("%s%s", WorktreeRefPrefix, stackRoot)
	return r.DeleteRef(ctx, refName)
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
		if after, ok := strings.CutPrefix(line, "worktree "); ok {
			currentWorktree = after
		} else if after, ok := strings.CutPrefix(line, "branch "); ok {
			branch := after
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

// WorktreeHasUncommittedChanges checks if a worktree has uncommitted changes.
// "Clean" means: no staged, unstaged, or untracked entries — equivalent to
// `git status --porcelain --untracked-files=normal` returning empty output.
func (r *runner) WorktreeHasUncommittedChanges(ctx context.Context, worktreePath string) (bool, error) {
	out, err := r.RunGitCommandRawWithContext(ctx,
		"-C", worktreePath,
		"status", "--porcelain", "--untracked-files=normal",
	)
	if err != nil {
		return false, fmt.Errorf("failed to check status in worktree %s: %w", worktreePath, err)
	}
	return strings.TrimSpace(out) != "", nil
}

// WorktreeHasTrackedChanges reports whether a worktree has staged or unstaged
// changes to tracked files, ignoring untracked ones — equivalent to
// `git status --porcelain --untracked-files=no` returning empty output.
//
// This is the right question when deciding whether a `git reset --hard` would
// destroy something: reset never touches untracked files, so counting them as
// "dirty" suppresses a reset that could not have harmed them. Use
// WorktreeHasUncommittedChanges instead when deciding whether it is polite to
// operate on someone's worktree at all, where a stray untracked file still
// signals work in progress.
func (r *runner) WorktreeHasTrackedChanges(ctx context.Context, worktreePath string) (bool, error) {
	out, err := r.RunGitCommandRawWithContext(ctx,
		"-C", worktreePath,
		"status", "--porcelain", "--untracked-files=no",
	)
	if err != nil {
		return false, fmt.Errorf("failed to check tracked status in worktree %s: %w", worktreePath, err)
	}
	return strings.TrimSpace(out) != "", nil
}

// GetWorktreeCurrentBranch returns the name of the branch currently checked out in a worktree.
// Returns empty string if the worktree is in detached HEAD state. Real failures
// (worktree missing, repo corruption, context cancellation) are surfaced — git
// uses exit code 1 specifically for "not a symbolic ref" when `-q` is passed,
// reserving 128 (and others) for genuine errors.
func (r *runner) GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error) {
	out, err := r.RunGitCommandWithContext(ctx, "-C", worktreePath, "symbolic-ref", "-q", "--short", "HEAD")
	if err != nil {
		if isExitCode(err, 1) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read HEAD of worktree %s: %w", worktreePath, err)
	}
	return strings.TrimSpace(out), nil
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
