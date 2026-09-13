package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// restorePathBatchSize bounds how many paths are passed to a single git
// invocation, keeping the argument list well clear of the platform limit.
const restorePathBatchSize = 500

// CaptureUntracked records every untracked, non-ignored file as a commit so a
// caller can put those files back later. It reports an empty SHA when there is
// nothing untracked to capture.
//
// `git stash create` deliberately ignores untracked files, which is not enough
// for a rollback: a command that stages everything (`modify -a`, or `create`
// after `git add -A`) commits those files, and rolling that commit back deletes
// the only copy that exists anywhere — they were never in the index, so they
// have no reflog and no blob to recover.
//
// Nothing here touches the working tree or the repository index: the files are
// staged into a throwaway index and written out as a detached commit.
func (r *runner) CaptureUntracked(ctx context.Context, message string) (string, error) {
	paths, err := r.untrackedPaths(ctx)
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", nil
	}

	tmpIndex, err := os.CreateTemp("", "stackit-capture-index-")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary index: %w", err)
	}
	indexPath := tmpIndex.Name()
	// git writes the index itself; it only needs the path to be available.
	if err := tmpIndex.Close(); err != nil {
		return "", fmt.Errorf("failed to create temporary index: %w", err)
	}
	if err := os.Remove(indexPath); err != nil {
		return "", fmt.Errorf("failed to create temporary index: %w", err)
	}
	defer func() { _ = os.Remove(indexPath) }()

	env := []string{"GIT_INDEX_FILE=" + indexPath}
	for chunk := range slices.Chunk(paths, restorePathBatchSize) {
		addArgs := append([]string{"add", "--"}, chunk...)
		if _, err := r.RunGitCommandWithEnv(ctx, env, addArgs...); err != nil {
			return "", fmt.Errorf("failed to stage untracked files for capture: %w", err)
		}
	}

	tree, err := r.RunGitCommandWithEnv(ctx, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("failed to write capture tree: %w", err)
	}

	commit, err := r.RunGitCommandWithContext(ctx, "commit-tree", strings.TrimSpace(tree), "-m", message)
	if err != nil {
		return "", fmt.Errorf("failed to write capture commit: %w", err)
	}
	return strings.TrimSpace(commit), nil
}

// RestoreUntracked writes files from a capture back into the working tree,
// leaving the index alone so they come back untracked exactly as they were. It
// reports how many files it wrote.
//
// A file that already exists is left alone: the capture is a safety net for
// work that was destroyed, never a reason to overwrite content that is
// currently on disk.
func (r *runner) RestoreUntracked(ctx context.Context, commitSHA string) (int, error) {
	commitSHA = strings.TrimSpace(commitSHA)
	if commitSHA == "" {
		return 0, nil
	}

	output, err := r.RunGitCommandRawWithContext(ctx, "ls-tree", "-r", "--name-only", "-z", commitSHA)
	if err != nil {
		return 0, fmt.Errorf("failed to list captured files: %w", err)
	}

	root := r.getRepoRoot()
	var missing []string
	for _, path := range strings.Split(output, "\x00") {
		if path == "" {
			continue
		}
		if _, statErr := os.Lstat(filepath.Join(root, path)); statErr == nil {
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) == 0 {
		return 0, nil
	}

	// Chunked: a capture can hold an unbounded number of paths, and one
	// argv per file would eventually exceed the platform's argument limit.
	for chunk := range slices.Chunk(missing, restorePathBatchSize) {
		args := append([]string{"restore", "--source", commitSHA, "--worktree", "--"}, chunk...)
		if _, err := r.RunGitCommandWithContext(ctx, args...); err != nil {
			return 0, fmt.Errorf("failed to restore captured files: %w", err)
		}
	}
	return len(missing), nil
}

// untrackedPaths lists untracked files that Git is not ignoring, NUL-separated
// so paths containing newlines survive the round trip.
func (r *runner) untrackedPaths(ctx context.Context) ([]string, error) {
	output, err := r.RunGitCommandRawWithContext(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("failed to list untracked files: %w", err)
	}

	var paths []string
	for _, path := range strings.Split(output, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths, nil
}
