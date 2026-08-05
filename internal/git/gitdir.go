package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// GetGitDir resolves the actual git directory for a repository.
// In worktrees, .git is a file pointing to the real git directory, so we need
// to use git rev-parse to get the correct path.
//
// This returns the worktree-specific git directory (use GetGitCommonDir for shared config).
func GetGitDir(repoRoot string) string {
	return resolveGitDir(repoRoot)
}

// GetGitCommonDir resolves the shared git directory for a repository.
// In a linked worktree this follows the commondir pointer back to the main
// repository's .git directory.
func GetGitCommonDir(repoRoot string) string {
	return commonGitDir(resolveGitDir(repoRoot))
}

// getGitDir returns the runner's worktree-specific git directory.
func (r *runner) getGitDir(_ context.Context) string {
	root := r.getRepoRoot()
	if root == "" {
		root, _ = os.Getwd()
	}
	return resolveGitDir(root)
}

func resolveGitDir(repoRoot string) string {
	if repoRoot == "" {
		repoRoot, _ = os.Getwd()
	}
	repoRoot, _ = filepath.Abs(repoRoot)

	gitPath := findDotGit(repoRoot)
	info, err := os.Stat(gitPath)
	if err == nil && info.IsDir() {
		return gitPath
	}

	content, err := os.ReadFile(gitPath)
	if err != nil {
		return gitPath
	}

	line := strings.TrimSpace(string(content))
	gitDir, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return gitPath
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		// A relative gitdir is relative to the .git file, which may be in an
		// ancestor when the caller supplied a repository subdirectory.
		gitDir = filepath.Join(filepath.Dir(gitPath), gitDir)
	}
	return filepath.Clean(gitDir)
}

func findDotGit(path string) string {
	for {
		gitPath := filepath.Join(path, ".git")
		if _, err := os.Stat(gitPath); err == nil {
			return gitPath
		}
		parent := filepath.Dir(path)
		if parent == path {
			return gitPath
		}
		path = parent
	}
}

func commonGitDir(gitDir string) string {
	content, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	commonDir := strings.TrimSpace(string(content))
	if commonDir == "" {
		return gitDir
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitDir, commonDir)
	}
	return filepath.Clean(commonDir)
}
