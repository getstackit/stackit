package git

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

var (
	defaultRepo   *Repository
	defaultRepoMu sync.RWMutex
	// goGitMu synchronizes go-git operations that access packfiles to prevent
	// "concurrent map iteration and map write" panics
	goGitMu sync.Mutex
)

// InitDefaultRepo initializes the default repository from the current directory
func InitDefaultRepo() error {
	return InitDefaultRepoInDir("")
}

// InitDefaultRepoInDir initializes the default repository from a specific directory.
// If dir is empty, uses the current working directory.
func InitDefaultRepoInDir(dir string) error {
	// Fast path: check without write lock
	defaultRepoMu.RLock()
	if defaultRepo != nil {
		defaultRepoMu.RUnlock()
		return nil // Already initialized
	}
	defaultRepoMu.RUnlock()

	// Slow path: acquire write lock
	defaultRepoMu.Lock()
	defer defaultRepoMu.Unlock()

	// Double-check after acquiring lock
	if defaultRepo != nil {
		return nil // Already initialized by another goroutine
	}

	var repoRoot string
	var err error
	if dir == "" {
		repoRoot, err = GetRepoRoot()
		if err != nil {
			return err
		}
	} else {
		repoRoot = dir
	}

	repo, err := OpenRepository(repoRoot)
	if err != nil {
		return err
	}

	defaultRepo = repo
	return nil
}

// ResetDefaultRepo clears the default repository.
// This should be called in tests to ensure each test gets a fresh repository.
func ResetDefaultRepo() {
	defaultRepoMu.Lock()
	defer defaultRepoMu.Unlock()
	defaultRepo = nil
}

// GetDefaultRepo returns the default repository (must call InitDefaultRepo first)
func GetDefaultRepo() (*Repository, error) {
	defaultRepoMu.RLock()
	defer defaultRepoMu.RUnlock()
	if defaultRepo == nil {
		return nil, fmt.Errorf("repository not initialized, call InitDefaultRepo first")
	}
	return defaultRepo, nil
}

// GetAllBranchNames returns all branch names in the repository
func GetAllBranchNames() ([]string, error) {
	repo, err := GetDefaultRepo()
	if err != nil {
		return nil, err
	}
	return repo.GetBranchNames()
}

// GetCurrentBranch returns the current branch name
func GetCurrentBranch() (string, error) {
	repo, err := GetDefaultRepo()
	if err != nil {
		return "", err
	}

	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	if !head.Name().IsBranch() {
		return "", fmt.Errorf("HEAD is not on a branch")
	}

	return head.Name().Short(), nil
}

// FindRemoteBranch finds the branch that tracks a remote branch
// Returns the local branch name if found, empty string otherwise
func FindRemoteBranch(ctx context.Context, remote string) (string, error) {
	// Get all branch configs that have this remote
	// Format: "branch.<name>.remote <remote>"
	output, err := RunGitCommandWithContext(ctx, "config", "--get-regexp", "^branch\\..*\\.remote$")
	if err != nil {
		return "", nil //nolint:nilerr // git config returns 1 if no branches match
	}

	if output == "" {
		return "", nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		// Line format: "branch.<name>.remote <remote>"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			remoteValue := parts[1]
			if remoteValue == remote {
				// Extract branch name from "branch.<name>.remote"
				branchPart := parts[0]
				if strings.HasPrefix(branchPart, "branch.") && strings.HasSuffix(branchPart, ".remote") {
					// Remove "branch." prefix and ".remote" suffix
					branchName := branchPart[7 : len(branchPart)-7]
					return branchName, nil
				}
			}
		}
	}
	return "", nil
}
