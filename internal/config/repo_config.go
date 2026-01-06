// Package config provides repository configuration management,
// including reading and writing stackit configuration files.
// Package config manages stackit configuration and state persistence.
//
// It handles:
//   - Repository-specific configuration
//   - Global user configuration
//   - Continuation state for interrupted operations (like merge conflicts)
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Config represents a repository configuration with getters and setters
type Config struct {
	repoRoot string
	data     *RepoConfig
}

// resolveGitDir returns the path to the shared .git directory.
// For regular repos this is repoRoot/.git, but for worktrees it returns
// the main repository's .git directory (where config should be stored).
func resolveGitDir(repoRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		// Fallback to traditional path if git command fails
		return filepath.Join(repoRoot, ".git")
	}
	gitDir := strings.TrimSpace(string(out))
	// git may return a relative path, make it absolute
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoRoot, gitDir)
	}
	return gitDir
}

// LoadConfig creates a new Config instance from a repository root
func LoadConfig(repoRoot string) (*Config, error) {
	data, err := GetRepoConfig(repoRoot)
	if err != nil {
		return nil, err
	}

	return &Config{
		repoRoot: repoRoot,
		data:     data,
	}, nil
}

// Save persists the configuration to disk
func (c *Config) Save() error {
	gitDir := resolveGitDir(c.repoRoot)
	configPath := filepath.Join(gitDir, ".stackit_config")

	configJSON, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(configPath, configJSON, 0600)
}

// Trunk returns the primary trunk branch name, or "main" as default
func (c *Config) Trunk() string {
	if c.data.Trunk != nil && *c.data.Trunk != "" {
		return *c.data.Trunk
	}
	return "main"
}

// SetTrunk sets the primary trunk branch name
func (c *Config) SetTrunk(trunkName string) {
	c.data.Trunk = &trunkName
	if c.data.IsGithubIntegrationEnabled == nil {
		enabled := false
		c.data.IsGithubIntegrationEnabled = &enabled
	}
}

// AllTrunks returns all configured trunk branches
func (c *Config) AllTrunks() []string {
	var trunks []string
	if c.data.Trunk != nil && *c.data.Trunk != "" {
		trunks = append(trunks, *c.data.Trunk)
	}

	// Add additional trunks (avoiding duplicates)
	for _, t := range c.data.Trunks {
		if !slices.Contains(trunks, t) {
			trunks = append(trunks, t)
		}
	}

	// Default to "main" if no trunks configured
	if len(trunks) == 0 {
		return []string{"main"}
	}

	return trunks
}

// IsTrunk checks if a branch is configured as a trunk
func (c *Config) IsTrunk(branchName string) bool {
	trunks := c.AllTrunks()
	return slices.Contains(trunks, branchName)
}

// AddTrunk adds an additional trunk branch to the config
func (c *Config) AddTrunk(trunkName string) error {
	// Check if already a trunk
	if c.data.Trunk != nil && *c.data.Trunk == trunkName {
		return fmt.Errorf("'%s' is already the primary trunk", trunkName)
	}
	if slices.Contains(c.data.Trunks, trunkName) {
		return fmt.Errorf("'%s' is already configured as a trunk", trunkName)
	}

	// Add to trunks list
	c.data.Trunks = append(c.data.Trunks, trunkName)
	return nil
}

// IsInitialized checks if Stackit has been initialized
func (c *Config) IsInitialized() bool {
	return c.data.Trunk != nil && *c.data.Trunk != ""
}

// BranchNamePattern returns the branch name pattern from config, or default if not set
func (c *Config) BranchNamePattern() string {
	return c.data.GetBranchPattern().String()
}

// SetBranchNamePattern sets the branch name pattern in the config
func (c *Config) SetBranchNamePattern(pattern string) error {
	// Validate the pattern
	branchPattern, err := NewBranchPattern(pattern)
	if err != nil {
		return err
	}

	patternStr := branchPattern.String()
	c.data.BranchNamePattern = &patternStr
	return nil
}

// SubmitFooter returns whether PR footer is enabled, or true by default
func (c *Config) SubmitFooter() bool {
	if c.data.SubmitFooter != nil {
		return *c.data.SubmitFooter
	}
	return true
}

// SetSubmitFooter sets whether PR footer is enabled
func (c *Config) SetSubmitFooter(enabled bool) {
	c.data.SubmitFooter = &enabled
}

// UndoStackDepth returns the maximum number of undo snapshots to keep, or 10 by default
func (c *Config) UndoStackDepth() int {
	if c.data.UndoStackDepth != nil {
		return *c.data.UndoStackDepth
	}
	return 10
}

// SetUndoStackDepth sets the maximum number of undo snapshots to keep
func (c *Config) SetUndoStackDepth(depth int) {
	c.data.UndoStackDepth = &depth
}

// WorktreeBasePath returns the base path for worktrees, or empty string for default
// Default is "../{repo-name}-stacks" relative to the repository root
func (c *Config) WorktreeBasePath() string {
	if c.data.WorktreeBasePath != nil {
		return *c.data.WorktreeBasePath
	}
	return ""
}

// SetWorktreeBasePath sets the base path for worktrees
func (c *Config) SetWorktreeBasePath(path string) {
	c.data.WorktreeBasePath = &path
}

// WorktreeAutoClean returns whether worktrees should be auto-cleaned during sync, true by default
func (c *Config) WorktreeAutoClean() bool {
	if c.data.WorktreeAutoClean != nil {
		return *c.data.WorktreeAutoClean
	}
	return true
}

// SetWorktreeAutoClean sets whether worktrees should be auto-cleaned during sync
func (c *Config) SetWorktreeAutoClean(enabled bool) {
	c.data.WorktreeAutoClean = &enabled
}

// GetBranchPattern returns the branch name pattern as a BranchPattern type
func (c *Config) GetBranchPattern() BranchPattern {
	return c.data.GetBranchPattern()
}

// MergeMethod returns the configured merge method, or empty string if not set
func (c *Config) MergeMethod() string {
	if c.data.MergeMethod != nil {
		return *c.data.MergeMethod
	}
	return ""
}

// SetMergeMethod sets the merge method preference
func (c *Config) SetMergeMethod(method string) error {
	switch method {
	case "squash", "merge", "rebase":
		c.data.MergeMethod = &method
		return nil
	default:
		return fmt.Errorf("invalid merge method: %s (must be squash, merge, or rebase)", method)
	}
}

// CombineCICommand returns the CI command to run for combine validation
func (c *Config) CombineCICommand() string {
	if c.data.CombineCICommand != nil {
		return *c.data.CombineCICommand
	}
	return ""
}

// SetCombineCICommand sets the CI command for combine validation
func (c *Config) SetCombineCICommand(cmd string) {
	c.data.CombineCICommand = &cmd
}

// CombineCITimeout returns the timeout in seconds for combine CI validation (default: 600)
func (c *Config) CombineCITimeout() int {
	if c.data.CombineCITimeout != nil {
		return *c.data.CombineCITimeout
	}
	return 600 // 10 minutes default
}

// SetCombineCITimeout sets the timeout in seconds for combine CI validation
func (c *Config) SetCombineCITimeout(seconds int) {
	c.data.CombineCITimeout = &seconds
}

// RepoConfig represents the repository configuration
type RepoConfig struct {
	Trunk                      *string  `json:"trunk,omitempty"`
	Trunks                     []string `json:"trunks,omitempty"`
	IsGithubIntegrationEnabled *bool    `json:"isGithubIntegrationEnabled,omitempty"`
	BranchNamePattern          *string  `json:"branchNamePattern,omitempty"`
	SubmitFooter               *bool    `json:"submit.footer,omitempty"`
	UndoStackDepth             *int     `json:"undo.stackDepth,omitempty"`
	WorktreeBasePath           *string  `json:"worktree.basePath,omitempty"`
	WorktreeAutoClean          *bool    `json:"worktree.autoClean,omitempty"`
	MergeMethod                *string  `json:"merge.method,omitempty"`
	CombineCICommand           *string  `json:"combine.ciCommand,omitempty"`
	CombineCITimeout           *int     `json:"combine.ciTimeout,omitempty"`
}

// GetBranchPattern returns the branch name pattern as a BranchPattern type
// Always returns a valid pattern (default if not set or invalid)
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

// GetRepoConfig reads the repository configuration
func GetRepoConfig(repoRoot string) (*RepoConfig, error) {
	gitDir := resolveGitDir(repoRoot)
	configPath := filepath.Join(gitDir, ".stackit_config")

	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist - return default
		return &RepoConfig{}, nil //nolint:nilerr
	}

	var config RepoConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse repo config: %w", err)
	}

	return &config, nil
}
