// Package config provides repository configuration management.
//
// It handles:
//   - Repository-specific configuration (stored in git config)
//   - Migration from legacy JSON config to git config
//   - Continuation state for interrupted operations (like merge conflicts)
package config

import (
	"github.com/getstackit/stackit/internal/git"
)

// resolveGitDir returns the path to the shared .git directory.
// For regular repos this is repoRoot/.git, but for worktrees it returns
// the main repository's .git directory (where config should be stored).
func resolveGitDir(repoRoot string) string {
	return git.GetGitCommonDir(repoRoot)
}

// LoadConfig loads the repository configuration.
// This function delegates to LoadGitConfigWithProject for git-based config storage
// with project config (.stackit.yaml) fallback support.
// It automatically migrates any existing JSON config to git config.
func LoadConfig(repoRoot string) (*GitConfig, error) {
	return LoadGitConfigWithProject(repoRoot)
}

// RepoConfig represents the legacy JSON-based repository configuration.
// This struct is used only for migration from the old .stackit_config JSON format.
type RepoConfig struct {
	Trunk                           *string  `json:"trunk,omitempty"`
	Trunks                          []string `json:"trunks,omitempty"`
	IsGithubIntegrationEnabled      *bool    `json:"isGithubIntegrationEnabled,omitempty"`
	BranchNamePattern               *string  `json:"branchNamePattern,omitempty"`
	SubmitFooter                    *bool    `json:"submit.footer,omitempty"`
	UndoStackDepth                  *int     `json:"undo.stackDepth,omitempty"`
	MaxConcurrency                  *int     `json:"maxConcurrency,omitempty"` // Maximum concurrent validation operations
	WorktreeBasePath                *string  `json:"worktree.basePath,omitempty"`
	WorktreeAutoClean               *bool    `json:"worktree.autoClean,omitempty"`
	MergeMethod                     *string  `json:"merge.method,omitempty"`
	CombineCICommand                *string  `json:"combine.ciCommand,omitempty"` // Deprecated: use CICommand
	CombineCITimeout                *int     `json:"combine.ciTimeout,omitempty"` // Deprecated: use CITimeout
	CICommand                       *string  `json:"ci.command,omitempty"`        // Unified CI command for all validation
	CITimeout                       *int     `json:"ci.timeout,omitempty"`        // Unified CI timeout in seconds
	SplitHunkSelector               *string  `json:"split.hunkSelector,omitempty"`
	ApprovedPostWorktreeCreateHooks []string `json:"hooks.approvedPostWorktreeCreate,omitempty"`
}

// GetBranchPattern returns the branch name pattern as a BranchPattern type.
// Always returns a valid pattern (default if not set or invalid).
func (c *RepoConfig) GetBranchPattern() BranchPattern {
	if c.BranchNamePattern != nil && *c.BranchNamePattern != "" {
		pattern, err := NewBranchPattern(*c.BranchNamePattern)
		if err != nil {
			// If invalid, return default
			return DefaultBranchPattern
		}
		return pattern.WithDefault()
	}
	return DefaultBranchPattern
}
