package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// ProjectConfigFileName is the name of the project configuration file
const ProjectConfigFileName = ".stackit.yaml"

// BranchConfig contains branch naming configuration
type BranchConfig struct {
	Pattern string `yaml:"pattern,omitempty"`
}

// StackConfig contains topology constraints for Stackit-managed branches.
type StackConfig struct {
	// Shape is "tree" (the default) or "linear". Linear mode prevents a
	// non-trunk branch from gaining a second child.
	Shape string `yaml:"shape,omitempty"`
}

// SubmitConfig contains PR submission configuration
type SubmitConfig struct {
	Footer      *bool    `yaml:"footer,omitempty"`      // Pointer to distinguish unset from false
	Draft       *bool    `yaml:"draft,omitempty"`       // Pointer to distinguish unset from false
	GitHubStack *bool    `yaml:"githubStack,omitempty"` // Sync native GitHub Stack metadata
	Web         string   `yaml:"web,omitempty"`         // "always", "created", "never"
	Labels      []string `yaml:"labels,omitempty"`      // Default labels for PRs
	Reviewers   []string `yaml:"reviewers,omitempty"`   // Default reviewers for PRs
	Assignees   []string `yaml:"assignees,omitempty"`   // Default assignees for PRs
}

// MergeConfig contains merge method configuration
type MergeConfig struct {
	Method string `yaml:"method,omitempty"`
}

// CIConfig contains CI validation configuration
type CIConfig struct {
	Command string `yaml:"command,omitempty"`
	Timeout int    `yaml:"timeout,omitempty"`
}

// UndoConfig contains undo settings
type UndoConfig struct {
	Depth   int   `yaml:"depth,omitempty"`
	Enabled *bool `yaml:"enabled,omitempty"`
}

// WorktreeConfig contains worktree settings
type WorktreeConfig struct {
	BasePath  string `yaml:"basePath,omitempty"`
	AutoClean *bool  `yaml:"autoClean,omitempty"` // Pointer to distinguish unset from false
}

// SplitConfig contains split settings
type SplitConfig struct {
	HunkSelector string `yaml:"hunkSelector,omitempty"`
}

// NavigationConfig contains PR navigation display settings
type NavigationConfig struct {
	When       string `yaml:"when,omitempty"`       // always/never/multiple
	Marker     string `yaml:"marker,omitempty"`     // custom marker symbol
	Location   string `yaml:"location,omitempty"`   // body/comment
	ShowMerged *bool  `yaml:"showMerged,omitempty"` // show merged history
}

// ProjectConfig represents the project-level configuration stored in .stackit.yaml
// This file is committed to the repository and shared across the team.
// Team settings can be overridden by personal git config (git config > project config > defaults).
type ProjectConfig struct {
	Trunk  string   `yaml:"trunk,omitempty"`
	Trunks []string `yaml:"trunks,omitempty"`

	Branch         BranchConfig     `yaml:"branch,omitempty"`
	Stack          StackConfig      `yaml:"stack,omitempty"`
	Submit         SubmitConfig     `yaml:"submit,omitempty"`
	Merge          MergeConfig      `yaml:"merge,omitempty"`
	CI             CIConfig         `yaml:"ci,omitempty"`
	Undo           UndoConfig       `yaml:"undo,omitempty"`
	Worktree       WorktreeConfig   `yaml:"worktree,omitempty"`
	Split          SplitConfig      `yaml:"split,omitempty"`
	Hooks          HooksConfig      `yaml:"hooks,omitempty"`
	MaxConcurrency *int             `yaml:"maxConcurrency,omitempty"` // Pointer to distinguish unset from 0
	Navigation     NavigationConfig `yaml:"navigation,omitempty"`
}

// HooksConfig contains hook configurations keyed by lifecycle phase.
// Keys are phase names like "pre-modify" or "post-worktree-create"; values
// are shell command lists.
type HooksConfig map[string][]string

// For returns the hook command list configured for the given phase.
// Returns nil for an unknown phase.
func (h HooksConfig) For(phase string) []string {
	return h[phase]
}

// knownTopLevelKeys contains all valid top-level keys in .stackit.yaml
var knownTopLevelKeys = map[string]bool{
	SectionTrunk:      true,
	SectionTrunks:     true,
	SectionBranch:     true,
	SectionStack:      true,
	SectionSubmit:     true,
	SectionMerge:      true,
	SectionCI:         true,
	SectionUndo:       true,
	SectionWorktree:   true,
	SectionSplit:      true,
	SectionHooks:      true,
	"maxConcurrency":  true,
	SectionNavigation: true,
}

// LoadProjectConfig reads the project configuration from .stackit.yaml in the repo root.
// Returns an empty config (not an error) if the file doesn't exist.
func LoadProjectConfig(repoRoot string) (*ProjectConfig, error) {
	configPath := filepath.Join(repoRoot, ProjectConfigFileName)

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - return empty config
			return &ProjectConfig{}, nil
		}
		return nil, err
	}

	var config ProjectConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w (content: %q)", ProjectConfigFileName, err, string(data))
	}

	// Check for unknown top-level keys (likely typos)
	var rawConfig map[string]any
	if err := yaml.Unmarshal(data, &rawConfig); err == nil {
		for key := range rawConfig {
			if !knownTopLevelKeys[key] {
				return nil, fmt.Errorf("unknown key %q in %s (check for typos)", key, ProjectConfigFileName)
			}
		}
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// HasPostWorktreeCreateHooks returns true if there are any post-worktree-create hooks configured
func (c *ProjectConfig) HasPostWorktreeCreateHooks() bool {
	return len(c.Hooks.For(PhasePostWorktreeCreate)) > 0
}

// HasTrunk returns true if a trunk branch is configured
func (c *ProjectConfig) HasTrunk() bool {
	return c.Trunk != ""
}

// HasTrunks returns true if additional trunk branches are configured
func (c *ProjectConfig) HasTrunks() bool {
	return len(c.Trunks) > 0
}

// HasBranchPattern returns true if a branch naming pattern is configured
func (c *ProjectConfig) HasBranchPattern() bool {
	return c.Branch.Pattern != ""
}

// HasStackShape returns true if stack.shape is configured.
func (c *ProjectConfig) HasStackShape() bool {
	return c.Stack.Shape != ""
}

// HasSubmitFooter returns true if the submit footer setting is configured
func (c *ProjectConfig) HasSubmitFooter() bool {
	return c.Submit.Footer != nil
}

// GetSubmitFooter returns the submit footer value (caller should check HasSubmitFooter first)
func (c *ProjectConfig) GetSubmitFooter() bool {
	if c.Submit.Footer == nil {
		return true // Default
	}
	return *c.Submit.Footer
}

// HasSubmitDraft returns true if the submit draft setting is configured
func (c *ProjectConfig) HasSubmitDraft() bool {
	return c.Submit.Draft != nil
}

// GetSubmitDraft returns the submit draft value (caller should check HasSubmitDraft first)
func (c *ProjectConfig) GetSubmitDraft() bool {
	if c.Submit.Draft == nil {
		return false // Default
	}
	return *c.Submit.Draft
}

// HasSubmitGitHubStack returns true if native GitHub Stack sync is configured.
func (c *ProjectConfig) HasSubmitGitHubStack() bool {
	return c.Submit.GitHubStack != nil
}

// GetSubmitGitHubStack returns the native GitHub Stack sync value.
func (c *ProjectConfig) GetSubmitGitHubStack() bool {
	if c.Submit.GitHubStack == nil {
		return DefaultSubmitGitHubStack
	}
	return *c.Submit.GitHubStack
}

// HasSubmitWeb returns true if the submit web setting is configured
func (c *ProjectConfig) HasSubmitWeb() bool {
	return c.Submit.Web != ""
}

// HasSubmitLabels returns true if default labels are configured
func (c *ProjectConfig) HasSubmitLabels() bool {
	return len(c.Submit.Labels) > 0
}

// HasSubmitReviewers returns true if default reviewers are configured
func (c *ProjectConfig) HasSubmitReviewers() bool {
	return len(c.Submit.Reviewers) > 0
}

// HasSubmitAssignees returns true if default assignees are configured
func (c *ProjectConfig) HasSubmitAssignees() bool {
	return len(c.Submit.Assignees) > 0
}

// HasMergeMethod returns true if a merge method is configured
func (c *ProjectConfig) HasMergeMethod() bool {
	return c.Merge.Method != ""
}

// HasCICommand returns true if a CI command is configured
func (c *ProjectConfig) HasCICommand() bool {
	return c.CI.Command != ""
}

// HasCITimeout returns true if a CI timeout is configured
func (c *ProjectConfig) HasCITimeout() bool {
	return c.CI.Timeout > 0
}

// HasUndoDepth returns true if undo depth is configured
func (c *ProjectConfig) HasUndoDepth() bool {
	return c.Undo.Depth > 0
}

// HasUndoEnabled returns true if undo enabled is configured
func (c *ProjectConfig) HasUndoEnabled() bool {
	return c.Undo.Enabled != nil
}

// HasWorktreeBasePath returns true if worktree base path is configured
func (c *ProjectConfig) HasWorktreeBasePath() bool {
	return c.Worktree.BasePath != ""
}

// HasWorktreeAutoClean returns true if worktree auto clean is configured
func (c *ProjectConfig) HasWorktreeAutoClean() bool {
	return c.Worktree.AutoClean != nil
}

// GetWorktreeAutoClean returns the worktree auto clean value (caller should check HasWorktreeAutoClean first)
func (c *ProjectConfig) GetWorktreeAutoClean() bool {
	if c.Worktree.AutoClean == nil {
		return true // Default
	}
	return *c.Worktree.AutoClean
}

// HasSplitHunkSelector returns true if split hunk selector is configured
func (c *ProjectConfig) HasSplitHunkSelector() bool {
	return c.Split.HunkSelector != ""
}

// HasMaxConcurrency returns true if max concurrency is configured
func (c *ProjectConfig) HasMaxConcurrency() bool {
	return c.MaxConcurrency != nil
}

// GetMaxConcurrency returns the max concurrency value (caller should check HasMaxConcurrency first)
func (c *ProjectConfig) GetMaxConcurrency() int {
	if c.MaxConcurrency == nil {
		return DefaultMaxConcurrency
	}
	return *c.MaxConcurrency
}

// HasNavigationWhen returns true if navigation.when is configured
func (c *ProjectConfig) HasNavigationWhen() bool {
	return c.Navigation.When != ""
}

// HasNavigationMarker returns true if navigation.marker is configured
func (c *ProjectConfig) HasNavigationMarker() bool {
	return c.Navigation.Marker != ""
}

// HasNavigationLocation returns true if navigation.location is configured
func (c *ProjectConfig) HasNavigationLocation() bool {
	return c.Navigation.Location != ""
}

// HasNavigationShowMerged returns true if navigation.showMerged is configured
func (c *ProjectConfig) HasNavigationShowMerged() bool {
	return c.Navigation.ShowMerged != nil
}

// GetNavigationShowMerged returns the navigation.showMerged value (caller should check HasNavigationShowMerged first)
func (c *ProjectConfig) GetNavigationShowMerged() bool {
	if c.Navigation.ShowMerged == nil {
		return DefaultNavigationShowMerged
	}
	return *c.Navigation.ShowMerged
}

// Validate checks the configuration for invalid values
func (c *ProjectConfig) Validate() error {
	// Validate trunks doesn't duplicate the primary trunk
	if c.HasTrunk() && c.HasTrunks() && slices.Contains(c.Trunks, c.Trunk) {
		return fmt.Errorf("duplicate trunk in %s: %q appears in both 'trunk' and 'trunks'", ProjectConfigFileName, c.Trunk)
	}

	// Validate branch pattern if set
	if c.HasBranchPattern() {
		if _, err := NewBranchPattern(c.Branch.Pattern); err != nil {
			return fmt.Errorf("invalid branch.pattern in %s: %w", ProjectConfigFileName, err)
		}
	}

	if c.HasStackShape() && !slices.Contains(ValidStackShapes, c.Stack.Shape) {
		return fmt.Errorf("invalid stack.shape in %s: %q (must be one of: %s)", ProjectConfigFileName, c.Stack.Shape, strings.Join(ValidStackShapes, ", "))
	}

	// Validate merge method if set
	if c.HasMergeMethod() {
		if !slices.Contains(ValidMergeMethods, c.Merge.Method) {
			return fmt.Errorf("invalid merge.method in %s: %q (must be one of: %s)", ProjectConfigFileName, c.Merge.Method, strings.Join(ValidMergeMethods, ", "))
		}
	}

	// Validate submit.web if set
	if c.HasSubmitWeb() {
		if !slices.Contains(ValidSubmitWeb, c.Submit.Web) {
			return fmt.Errorf("invalid submit.web in %s: %q (must be one of: %s)", ProjectConfigFileName, c.Submit.Web, strings.Join(ValidSubmitWeb, ", "))
		}
	}

	// Validate CI timeout if set (0 means not set in YAML, which is fine)
	if c.CI.Timeout < 0 {
		return fmt.Errorf("invalid ci.timeout in %s: must be a positive number", ProjectConfigFileName)
	}

	// Validate undo depth if set
	if c.Undo.Depth < 0 {
		return fmt.Errorf("invalid undo.depth in %s: must be >= 0", ProjectConfigFileName)
	}

	// Validate split hunk selector if set
	if c.HasSplitHunkSelector() {
		if !slices.Contains(ValidHunkSelectors, c.Split.HunkSelector) {
			return fmt.Errorf("invalid split.hunkSelector in %s: %q (must be one of: %s)", ProjectConfigFileName, c.Split.HunkSelector, strings.Join(ValidHunkSelectors, ", "))
		}
	}

	// Validate maxConcurrency if set
	if c.HasMaxConcurrency() && *c.MaxConcurrency < 0 {
		return fmt.Errorf("invalid maxConcurrency in %s: must be >= 0", ProjectConfigFileName)
	}

	// Validate navigation.when if set
	if c.HasNavigationWhen() {
		if !slices.Contains(ValidNavigationWhen, c.Navigation.When) {
			return fmt.Errorf("invalid navigation.when in %s: %q (must be one of: %s)", ProjectConfigFileName, c.Navigation.When, strings.Join(ValidNavigationWhen, ", "))
		}
	}

	// Validate navigation.location if set
	if c.HasNavigationLocation() {
		if !slices.Contains(ValidNavigationLocation, c.Navigation.Location) {
			return fmt.Errorf("invalid navigation.location in %s: %q (must be one of: %s)", ProjectConfigFileName, c.Navigation.Location, strings.Join(ValidNavigationLocation, ", "))
		}
	}

	// Validate navigation.marker if set
	if c.HasNavigationMarker() {
		marker := c.Navigation.Marker
		if strings.ContainsAny(marker, "\n\r") {
			return fmt.Errorf("invalid navigation.marker in %s: cannot contain newlines", ProjectConfigFileName)
		}
		if utf8.RuneCountInString(marker) > 10 {
			return fmt.Errorf("invalid navigation.marker in %s: cannot exceed 10 characters", ProjectConfigFileName)
		}
	}

	return nil
}
