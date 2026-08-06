package git

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// WorktreeRefPrefix is the prefix for Git refs where worktree metadata is stored (local-only)
const WorktreeRefPrefix = "refs/stackit/worktrees/"

// WorktreePathRefPrefix is a reverse index from canonical worktree path to
// its anchor registration. Keeping this alongside WorktreeRefPrefix lets a
// single update-ref transaction enforce both directions of the ownership
// invariant.
const WorktreePathRefPrefix = "refs/stackit/worktree-paths/"

func worktreePathRef(path string) string {
	if canonicalPath, err := CanonicalWorktreePath(path); err == nil {
		path = canonicalPath
	}
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("%s%x", WorktreePathRefPrefix, hash)
}

// CanonicalWorktreePath returns an absolute path with every existing path
// component resolved. It deliberately resolves the deepest existing ancestor
// instead of requiring the final worktree directory to exist: registration
// happens after creation but unregistering often happens after removal. Using
// the same canonical spelling in both cases keeps the reverse registration
// ref addressable through symlinked bases such as /tmp on macOS.
func CanonicalWorktreePath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	for existing := absPath; ; existing = filepath.Dir(existing) {
		resolvedPath, evalErr := filepath.EvalSymlinks(existing)
		if evalErr == nil {
			remainder, relErr := filepath.Rel(existing, absPath)
			if relErr != nil {
				return "", relErr
			}
			return filepath.Clean(filepath.Join(resolvedPath, remainder)), nil
		}
		if !os.IsNotExist(evalErr) {
			return "", evalErr
		}
		if parent := filepath.Dir(existing); parent == existing {
			return "", evalErr
		}
	}
}

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
//
// Only use this when repoRoot is known to be the main working tree. An engine
// constructed inside a temporary worktree reports that worktree as its repo
// root, which makes this answer "no" for the user's actual main checkout —
// prefer WorktreeList.IsMain, which reads the answer from git instead of
// trusting the caller's idea of where the repo lives.
func IsMainWorktree(worktreePath, repoRoot string) bool {
	equal, known := samePath(worktreePath, repoRoot)
	return known && equal
}

// samePath reports whether two paths name the same directory, and whether that
// question could be answered at all. Resolving symlinks on both sides matters
// on macOS, where /var and /private/var name the same place.
//
// The second return value exists because callers guard destructive operations
// with this: previously both EvalSymlinks errors were discarded, so two
// unresolvable paths yielded "" == "" and compared equal. Reporting "could not
// tell" lets each caller pick the safe answer for its own operation.
func samePath(a, b string) (equal, known bool) {
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false, false
	}
	return resolvedA == resolvedB, true
}

// MainPath returns the main working tree's path. `git worktree list` always
// reports the main worktree first and linked worktrees after it, so the first
// entry is authoritative regardless of which worktree the command ran from.
func (l WorktreeList) MainPath() string {
	if len(l) == 0 {
		return ""
	}
	return l[0].Path
}

// IsMain reports whether path is the repo's main working tree, which can never
// be removed with `git worktree remove`.
//
// Prefer this over IsMainWorktree for removal guards. IsMainWorktree compares
// against whatever the caller believes the repo root to be, and an engine built
// inside a temporary worktree believes it is that worktree — so the main
// checkout does not match, the guard passes, and git is asked to remove the
// user's working tree.
//
// Uncertainty resolves to true: with no list to compare against, or paths that
// cannot be resolved, the caller must not proceed to remove.
func (l WorktreeList) IsMain(path string) bool {
	main := l.MainPath()
	if main == "" {
		return true
	}
	equal, known := samePath(path, main)
	if !known {
		return true
	}
	return equal
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
	// Git requires --force twice when the worktree is locked, in addition to
	// using it to discard local changes.
	_, err := r.RunGitCommandWithContext(ctx, "worktree", "remove", "--force", "--force", path)
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
	// -z is required because worktree paths may legally contain newlines.
	output, err := r.RunGitCommandWithContext(ctx, "worktree", "list", "--porcelain", "-z")
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
	for line := range strings.SplitSeq(output, "\x00") {
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
func (r *runner) WriteWorktreeMeta(ctx context.Context, stackRoot string, meta *WorktreeMeta) error {
	jsonData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal worktree metadata: %w", err)
	}

	sha, err := r.CreateBlob(string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create worktree metadata blob: %w", err)
	}

	// Both refs are created only when absent. The forward ref protects one
	// anchor from receiving two registrations; the reverse ref protects one
	// path from being claimed by two anchors. update-ref applies the pair
	// atomically, so a competing registration loses cleanly instead of creating
	// an ambiguous ownership state.
	zeroSHA := strings.Repeat("0", len(sha))
	updates := []RefUpdate{
		{RefName: fmt.Sprintf("%s%s", WorktreeRefPrefix, stackRoot), NewSHA: sha, OldSHA: zeroSHA},
		{RefName: worktreePathRef(meta.Path), NewSHA: sha, OldSHA: zeroSHA},
	}
	if err := r.UpdateRefsBatch(ctx, updates); err != nil {
		return fmt.Errorf("failed to register worktree metadata refs: %w", err)
	}

	return nil
}

// DeleteWorktreeMeta deletes worktree metadata for a stack root
func (r *runner) DeleteWorktreeMeta(ctx context.Context, stackRoot string) error {
	refName := fmt.Sprintf("%s%s", WorktreeRefPrefix, stackRoot)
	sha, err := r.GetRef(refName)
	if err != nil {
		// Preserve DeleteRef's idempotent behavior for absent legacy metadata.
		return r.DeleteRef(ctx, refName)
	}

	meta, err := r.ReadWorktreeMeta(stackRoot)
	if err != nil {
		return err
	}
	updates := []RefUpdate{{RefName: refName, OldSHA: sha, IsDelete: true}}
	if meta != nil {
		pathRef := worktreePathRef(meta.Path)
		if pathSHA, pathErr := r.GetRef(pathRef); pathErr == nil {
			if pathSHA == sha {
				updates = append(updates, RefUpdate{RefName: pathRef, OldSHA: sha, IsDelete: true})
			}
		}
	}
	if err := r.UpdateRefsBatch(ctx, updates); err != nil {
		return fmt.Errorf("failed to unregister worktree metadata refs: %w", err)
	}
	return nil
}

// GetWorktreePathForBranch returns the worktree path where a branch is checked out.
// Returns empty string if the branch is not checked out in any worktree.
func (r *runner) GetWorktreePathForBranch(ctx context.Context, branchName string) (string, error) {
	worktrees, err := r.ListWorktrees(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}
	return worktrees.PathForBranch(branchName), nil
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

// ListIgnoredFiles returns every ignored, untracked file in worktreePath as a
// repository-relative path. --exclude-standard ensures this has Git's actual
// ignore semantics, rather than treating a warm-start include file as another
// source of ignored paths.
func (r *runner) ListIgnoredFiles(ctx context.Context, worktreePath string) ([]string, error) {
	out, err := r.RunGitCommandRawWithContext(ctx,
		"-C", worktreePath,
		"ls-files", "--others", "--ignored", "--exclude-standard", "-z",
	)
	if err != nil {
		return nil, fmt.Errorf("list ignored files in worktree %s: %w", worktreePath, err)
	}

	paths := strings.Split(out, "\x00")
	paths = slices.DeleteFunc(paths, func(path string) bool { return path == "" })
	slices.Sort(paths)
	return paths, nil
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
