package config

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/getstackit/stackit/internal/git"
)

// GitConfig provides typed access to stackit configuration stored in git config.
// This replaces the JSON-based config storage with native git config.
// Configuration follows a layered system: personal git config > team project config > defaults.
type GitConfig struct {
	repoRoot string
	store    *git.ConfigStore
	project  *ProjectConfig // Team config from .stackit.yaml for fallback
}

// LoadGitConfig loads configuration from git config.
// If JSON config exists and needs migration, it will be migrated automatically.
// This function does NOT load project config (.stackit.yaml) - use LoadGitConfigWithProject for that.
func LoadGitConfig(repoRoot string) (*GitConfig, error) {
	store := git.NewConfigStore(repoRoot)

	cfg := &GitConfig{
		repoRoot: repoRoot,
		store:    store,
	}

	// Check if we need to migrate from JSON
	if needsMigration(repoRoot) {
		if err := migrateFromJSON(repoRoot, store); err != nil {
			return nil, fmt.Errorf("config migration failed: %w", err)
		}
	}

	return cfg, nil
}

// LoadGitConfigWithProject loads configuration from git config with project config fallback.
// The layered system follows: personal git config > team project config (.stackit.yaml) > defaults.
func LoadGitConfigWithProject(repoRoot string) (*GitConfig, error) {
	cfg, err := LoadGitConfig(repoRoot)
	if err != nil {
		return nil, err
	}

	// Load project config for fallback
	project, err := LoadProjectConfig(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load project config: %w", err)
	}

	cfg.project = project
	return cfg, nil
}

// IsInitialized checks if stackit has been initialized (trunk is set).
func (c *GitConfig) IsInitialized() bool {
	return c.store.Exists(KeyTrunk)
}

// Trunk returns the primary trunk branch name.
// Priority: personal git config > team project config > default.
func (c *GitConfig) Trunk() string {
	// Check personal git config first
	trunk, _ := c.store.Get(KeyTrunk)
	if trunk != "" {
		return trunk
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasTrunk() {
		return c.project.Trunk
	}
	// Return default
	return DefaultTrunk
}

// SetTrunk sets the primary trunk branch name.
func (c *GitConfig) SetTrunk(trunk string) error {
	return c.store.Set(KeyTrunk, trunk)
}

// AllTrunks returns all configured trunk branches (primary + additional).
// Merges trunks from git config and project config (deduplicated).
func (c *GitConfig) AllTrunks() []string {
	trunks := []string{c.Trunk()}

	// Add additional trunks from git config
	additional, _ := c.store.GetAll(KeyTrunks)
	for _, t := range additional {
		if !slices.Contains(trunks, t) {
			trunks = append(trunks, t)
		}
	}

	// Add additional trunks from project config
	if c.project != nil && c.project.HasTrunks() {
		for _, t := range c.project.Trunks {
			if !slices.Contains(trunks, t) {
				trunks = append(trunks, t)
			}
		}
	}

	return trunks
}

// IsTrunk checks if a branch is configured as a trunk.
func (c *GitConfig) IsTrunk(branch string) bool {
	return slices.Contains(c.AllTrunks(), branch)
}

// AddTrunk adds an additional trunk branch.
func (c *GitConfig) AddTrunk(trunk string) error {
	if c.IsTrunk(trunk) {
		return fmt.Errorf("'%s' is already configured as a trunk", trunk)
	}
	return c.store.Add(KeyTrunks, trunk)
}

// ClearTrunks removes all additional trunks from personal git config.
// Note: Trunks from team config (.stackit.yaml) will still be visible in AllTrunks().
func (c *GitConfig) ClearTrunks() error {
	return c.store.Unset(KeyTrunks)
}

// RemoveTrunk removes a trunk from the additional trunks list.
// Cannot remove the primary trunk (use SetTrunk to change it).
func (c *GitConfig) RemoveTrunk(trunk string) error {
	// Check if it's the primary trunk
	if trunk == c.Trunk() {
		return fmt.Errorf("cannot remove primary trunk '%s'; use 'config set trunk <new-trunk>' to change it", trunk)
	}

	// Get current additional trunks from git config
	currentTrunks, _ := c.store.GetAll(KeyTrunks)
	if !slices.Contains(currentTrunks, trunk) {
		// Check if it's from project config - give a better error message
		if c.project != nil && c.project.HasTrunks() && slices.Contains(c.project.Trunks, trunk) {
			return fmt.Errorf("'%s' is defined in .stackit.yaml (team config), not in your personal config; edit .stackit.yaml to remove it", trunk)
		}
		return fmt.Errorf("'%s' is not in the additional trunks list", trunk)
	}

	// Remove all and re-add without the specified trunk
	if err := c.store.Unset(KeyTrunks); err != nil {
		return fmt.Errorf("failed to remove trunk: %w", err)
	}

	for _, t := range currentTrunks {
		if t != trunk {
			if err := c.store.Add(KeyTrunks, t); err != nil {
				return fmt.Errorf("failed to restore trunks: %w", err)
			}
		}
	}
	return nil
}

// BranchNamePattern returns the branch name pattern.
// Priority: personal git config > team project config > default.
func (c *GitConfig) BranchNamePattern() string {
	// Check personal git config first
	pattern, _ := c.store.Get(KeyBranchPattern)
	if pattern != "" {
		// Validate
		if _, err := NewBranchPattern(pattern); err != nil {
			return DefaultBranchPattern.String()
		}
		return pattern
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasBranchPattern() {
		// Already validated during LoadProjectConfig
		return c.project.Branch.Pattern
	}
	// Return default
	return DefaultBranchPattern.String()
}

// SetBranchNamePattern sets the branch name pattern.
func (c *GitConfig) SetBranchNamePattern(pattern string) error {
	// Validate the pattern
	if _, err := NewBranchPattern(pattern); err != nil {
		return err
	}
	return c.store.Set(KeyBranchPattern, pattern)
}

// GetBranchPattern returns the branch pattern object.
func (c *GitConfig) GetBranchPattern() BranchPattern {
	pattern, err := NewBranchPattern(c.BranchNamePattern())
	if err != nil {
		return DefaultBranchPattern
	}
	return pattern.WithDefault()
}

// SubmitFooter returns whether to include PR footer.
// Priority: personal git config > team project config > default.
func (c *GitConfig) SubmitFooter() bool {
	// Check personal git config first
	if c.store.Exists(KeySubmitFooter) {
		return c.store.GetBoolWithDefault(KeySubmitFooter, DefaultSubmitFooter)
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasSubmitFooter() {
		return c.project.GetSubmitFooter()
	}
	// Return default
	return DefaultSubmitFooter
}

// SetSubmitFooter sets whether to include PR footer.
func (c *GitConfig) SetSubmitFooter(enabled bool) error {
	return c.store.SetBool(KeySubmitFooter, enabled)
}

// UndoStackDepth returns the max undo depth.
// Priority: personal git config > team project config > default.
func (c *GitConfig) UndoStackDepth() int {
	// Check personal git config first
	if c.store.Exists(KeyUndoDepth) {
		depth := c.store.GetIntWithDefault(KeyUndoDepth, DefaultUndoDepth)
		if depth < 1 {
			return DefaultUndoDepth
		}
		return depth
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasUndoDepth() {
		if c.project.Undo.Depth < 1 {
			return DefaultUndoDepth
		}
		return c.project.Undo.Depth
	}
	// Return default
	return DefaultUndoDepth
}

// SetUndoStackDepth sets the max undo depth.
func (c *GitConfig) SetUndoStackDepth(depth int) error {
	if depth < 1 {
		return fmt.Errorf("undo depth must be at least 1")
	}
	return c.store.SetInt(KeyUndoDepth, depth)
}

// WorktreeBasePath returns the worktree base path.
// Priority: personal git config > team project config > empty (not set).
func (c *GitConfig) WorktreeBasePath() string {
	// Check personal git config first
	path, _ := c.store.Get(KeyWorktreeBasePath)
	if path != "" {
		return path
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasWorktreeBasePath() {
		return c.project.Worktree.BasePath
	}
	// Return empty (not set)
	return ""
}

// SetWorktreeBasePath sets the worktree base path.
func (c *GitConfig) SetWorktreeBasePath(path string) error {
	return c.store.Set(KeyWorktreeBasePath, path)
}

// WorktreeAutoClean returns whether to auto-clean worktrees.
// Priority: personal git config > team project config > default.
func (c *GitConfig) WorktreeAutoClean() bool {
	// Check personal git config first
	if c.store.Exists(KeyWorktreeAutoClean) {
		return c.store.GetBoolWithDefault(KeyWorktreeAutoClean, DefaultWorktreeAutoClean)
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasWorktreeAutoClean() {
		return c.project.GetWorktreeAutoClean()
	}
	// Return default
	return DefaultWorktreeAutoClean
}

// SetWorktreeAutoClean sets whether to auto-clean worktrees.
func (c *GitConfig) SetWorktreeAutoClean(enabled bool) error {
	return c.store.SetBool(KeyWorktreeAutoClean, enabled)
}

// MergeMethod returns the configured merge method (empty if not set).
// Priority: personal git config > team project config > empty (not set).
func (c *GitConfig) MergeMethod() string {
	// Check personal git config first
	method, _ := c.store.Get(KeyMergeMethod)
	if method != "" {
		return method
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasMergeMethod() {
		return c.project.Merge.Method
	}
	// Return empty (not set)
	return ""
}

// SetMergeMethod sets the merge method preference.
func (c *GitConfig) SetMergeMethod(method string) error {
	if !slices.Contains(ValidMergeMethods, method) {
		return fmt.Errorf("invalid merge method: %s (must be one of: %s)", method, strings.Join(ValidMergeMethods, ", "))
	}
	return c.store.Set(KeyMergeMethod, method)
}

// CICommand returns the CI validation command.
// Priority: personal git config > team project config > empty (not set).
func (c *GitConfig) CICommand() string {
	// Check personal git config first
	cmd, _ := c.store.Get(KeyCICommand)
	if cmd != "" {
		return cmd
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasCICommand() {
		return c.project.CI.Command
	}
	// Return empty (not set)
	return ""
}

// SetCICommand sets the CI validation command.
func (c *GitConfig) SetCICommand(cmd string) error {
	return c.store.Set(KeyCICommand, cmd)
}

// CITimeout returns the CI timeout in seconds.
// Priority: personal git config > team project config > default.
func (c *GitConfig) CITimeout() int {
	// Check personal git config first
	if c.store.Exists(KeyCITimeout) {
		timeout := c.store.GetIntWithDefault(KeyCITimeout, DefaultCITimeout)
		if timeout < 1 {
			return DefaultCITimeout
		}
		return timeout
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasCITimeout() {
		return c.project.CI.Timeout
	}
	// Return default
	return DefaultCITimeout
}

// SetCITimeout sets the CI timeout in seconds.
// Must be at least 1 second. To revert to the default timeout,
// use UnsetCITimeout() instead of setting to 0.
func (c *GitConfig) SetCITimeout(seconds int) error {
	if seconds < 1 {
		return fmt.Errorf("CI timeout must be at least 1 second; use 'config unset ci.timeout' to revert to default (%d seconds)", DefaultCITimeout)
	}
	return c.store.SetInt(KeyCITimeout, seconds)
}

// SplitHunkSelector returns the hunk selector mode.
// Priority: personal git config > team project config > default.
func (c *GitConfig) SplitHunkSelector() string {
	// Check personal git config first
	selector, _ := c.store.Get(KeySplitHunkSelector)
	if selector != "" {
		if !slices.Contains(ValidHunkSelectors, selector) {
			return DefaultSplitHunkSelector
		}
		return selector
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasSplitHunkSelector() {
		// Already validated during LoadProjectConfig
		return c.project.Split.HunkSelector
	}
	// Return default
	return DefaultSplitHunkSelector
}

// SetSplitHunkSelector sets the hunk selector mode.
func (c *GitConfig) SetSplitHunkSelector(selector string) error {
	if !slices.Contains(ValidHunkSelectors, selector) {
		return fmt.Errorf("invalid hunk selector: %s (must be one of: %s)", selector, strings.Join(ValidHunkSelectors, ", "))
	}
	return c.store.Set(KeySplitHunkSelector, selector)
}

// ApprovedPostWorktreeCreateHooks returns the list of approved hooks.
func (c *GitConfig) ApprovedPostWorktreeCreateHooks() []string {
	hooks, _ := c.store.GetAll(KeyApprovedHooks)
	return hooks
}

// IsPostWorktreeCreateHookApproved checks if a hook is approved.
func (c *GitConfig) IsPostWorktreeCreateHookApproved(hook string) bool {
	return slices.Contains(c.ApprovedPostWorktreeCreateHooks(), hook)
}

// AddApprovedPostWorktreeCreateHook adds a hook to the approved list.
func (c *GitConfig) AddApprovedPostWorktreeCreateHook(hook string) error {
	if c.IsPostWorktreeCreateHookApproved(hook) {
		return nil // Already approved
	}
	return c.store.Add(KeyApprovedHooks, hook)
}

// RemoveApprovedPostWorktreeCreateHook removes a hook from the approved list.
func (c *GitConfig) RemoveApprovedPostWorktreeCreateHook(hook string) error {
	// Get all current hooks
	hooks := c.ApprovedPostWorktreeCreateHooks()
	if !slices.Contains(hooks, hook) {
		return nil // Not in list
	}

	// Build the list of hooks to keep
	hooksToKeep := make([]string, 0, len(hooks)-1)
	for _, h := range hooks {
		if h != hook {
			hooksToKeep = append(hooksToKeep, h)
		}
	}

	// Remove all hooks first
	if err := c.store.Unset(KeyApprovedHooks); err != nil {
		return err
	}

	// Re-add the ones we want to keep
	// If this fails, try to restore the original state
	for _, h := range hooksToKeep {
		if err := c.store.Add(KeyApprovedHooks, h); err != nil {
			// Try to restore original hooks
			for _, original := range hooks {
				_ = c.store.Add(KeyApprovedHooks, original)
			}
			return fmt.Errorf("failed to update hooks, attempted recovery: %w", err)
		}
	}
	return nil
}

// ClearApprovedPostWorktreeCreateHooks removes all hook approvals.
func (c *GitConfig) ClearApprovedPostWorktreeCreateHooks() error {
	return c.store.Unset(KeyApprovedHooks)
}

// MaxConcurrency returns the maximum number of concurrent validation operations.
// Priority: personal git config > team project config > default (0 = auto based on CPU count).
func (c *GitConfig) MaxConcurrency() int {
	// Check personal git config first
	if c.store.Exists(KeyMaxConcurrency) {
		concurrency, _ := c.store.GetInt(KeyMaxConcurrency)
		if concurrency >= 0 {
			return concurrency
		}
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasMaxConcurrency() {
		return c.project.GetMaxConcurrency()
	}
	return DefaultMaxConcurrency
}

// SetMaxConcurrency sets the maximum number of concurrent validation operations.
func (c *GitConfig) SetMaxConcurrency(n int) error {
	if n < 0 {
		return fmt.Errorf("max concurrency must be >= 0")
	}
	return c.store.SetInt(KeyMaxConcurrency, n)
}

// NavigationWhen returns when navigation should be displayed.
// Priority: personal git config > team project config > default.
func (c *GitConfig) NavigationWhen() string {
	// Check personal git config first
	when, _ := c.store.Get(KeyNavigationWhen)
	if when != "" {
		if !slices.Contains(ValidNavigationWhen, when) {
			return DefaultNavigationWhen
		}
		return when
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasNavigationWhen() {
		return c.project.Navigation.When
	}
	// Return default
	return DefaultNavigationWhen
}

// SetNavigationWhen sets when navigation should be displayed.
func (c *GitConfig) SetNavigationWhen(when string) error {
	if !slices.Contains(ValidNavigationWhen, when) {
		return fmt.Errorf("invalid navigation.when: %s (must be one of: %s)", when, strings.Join(ValidNavigationWhen, ", "))
	}
	return c.store.Set(KeyNavigationWhen, when)
}

// NavigationMarker returns the marker symbol for the current branch.
// Priority: personal git config > team project config > default.
func (c *GitConfig) NavigationMarker() string {
	// Check personal git config first
	marker, _ := c.store.Get(KeyNavigationMarker)
	if marker != "" {
		return marker
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasNavigationMarker() {
		return c.project.Navigation.Marker
	}
	// Return default
	return DefaultNavigationMarker
}

// SetNavigationMarker sets the marker symbol for the current branch.
func (c *GitConfig) SetNavigationMarker(marker string) error {
	// Check for newlines before trimming (since TrimSpace removes trailing newlines)
	if strings.ContainsAny(marker, "\n\r") {
		return fmt.Errorf("navigation.marker cannot contain newlines")
	}
	marker = strings.TrimSpace(marker)
	if marker == "" {
		return fmt.Errorf("navigation.marker cannot be empty")
	}
	if utf8.RuneCountInString(marker) > 10 {
		return fmt.Errorf("navigation.marker cannot exceed 10 characters")
	}
	return c.store.Set(KeyNavigationMarker, marker)
}

// NavigationLocation returns where navigation should appear.
// Priority: personal git config > team project config > default.
func (c *GitConfig) NavigationLocation() string {
	// Check personal git config first
	location, _ := c.store.Get(KeyNavigationLocation)
	if location != "" {
		if !slices.Contains(ValidNavigationLocation, location) {
			return DefaultNavigationLocation
		}
		return location
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasNavigationLocation() {
		return c.project.Navigation.Location
	}
	// Return default
	return DefaultNavigationLocation
}

// SetNavigationLocation sets where navigation should appear.
func (c *GitConfig) SetNavigationLocation(location string) error {
	if !slices.Contains(ValidNavigationLocation, location) {
		return fmt.Errorf("invalid navigation.location: %s (must be one of: %s)", location, strings.Join(ValidNavigationLocation, ", "))
	}
	return c.store.Set(KeyNavigationLocation, location)
}

// NavigationShowMerged returns whether to show merged branch history.
// Priority: personal git config > team project config > default.
func (c *GitConfig) NavigationShowMerged() bool {
	// Check personal git config first
	if c.store.Exists(KeyNavigationShowMerged) {
		return c.store.GetBoolWithDefault(KeyNavigationShowMerged, DefaultNavigationShowMerged)
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasNavigationShowMerged() {
		return c.project.GetNavigationShowMerged()
	}
	// Return default
	return DefaultNavigationShowMerged
}

// SetNavigationShowMerged sets whether to show merged branch history.
func (c *GitConfig) SetNavigationShowMerged(show bool) error {
	return c.store.SetBool(KeyNavigationShowMerged, show)
}

// SubmitDraft returns whether to create PRs as drafts by default.
// Priority: personal git config > team project config > default.
func (c *GitConfig) SubmitDraft() bool {
	// Check personal git config first
	if c.store.Exists(KeySubmitDraft) {
		return c.store.GetBoolWithDefault(KeySubmitDraft, DefaultSubmitDraft)
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasSubmitDraft() {
		return c.project.GetSubmitDraft()
	}
	// Return default
	return DefaultSubmitDraft
}

// SetSubmitDraft sets whether to create PRs as drafts by default.
func (c *GitConfig) SetSubmitDraft(draft bool) error {
	return c.store.SetBool(KeySubmitDraft, draft)
}

// SubmitWeb returns when to open PRs in browser.
// Priority: personal git config > team project config > default.
func (c *GitConfig) SubmitWeb() string {
	// Check personal git config first
	web, _ := c.store.Get(KeySubmitWeb)
	if web != "" {
		if !slices.Contains(ValidSubmitWeb, web) {
			return DefaultSubmitWeb
		}
		return web
	}
	// Fall back to team project config
	if c.project != nil && c.project.HasSubmitWeb() {
		return c.project.Submit.Web
	}
	// Return default
	return DefaultSubmitWeb
}

// SetSubmitWeb sets when to open PRs in browser.
func (c *GitConfig) SetSubmitWeb(web string) error {
	if !slices.Contains(ValidSubmitWeb, web) {
		return fmt.Errorf("invalid submit.web: %s (must be one of: %s)", web, strings.Join(ValidSubmitWeb, ", "))
	}
	return c.store.Set(KeySubmitWeb, web)
}

// SubmitLabels returns the default labels for PRs.
// Merges labels from git config and project config (deduplicated).
func (c *GitConfig) SubmitLabels() []string {
	labels := []string{}

	// Add labels from git config
	gitLabels, _ := c.store.GetAll(KeySubmitLabels)
	for _, l := range gitLabels {
		if !slices.Contains(labels, l) {
			labels = append(labels, l)
		}
	}

	// Add labels from project config
	if c.project != nil && c.project.HasSubmitLabels() {
		for _, l := range c.project.Submit.Labels {
			if !slices.Contains(labels, l) {
				labels = append(labels, l)
			}
		}
	}

	return labels
}

// SetSubmitLabels sets the default labels for PRs.
// This replaces all existing labels.
func (c *GitConfig) SetSubmitLabels(labels []string) error {
	// Clear existing labels
	if err := c.store.Unset(KeySubmitLabels); err != nil {
		return err
	}
	// Add new labels
	for _, label := range labels {
		if err := c.store.Add(KeySubmitLabels, label); err != nil {
			return err
		}
	}
	return nil
}

// AddSubmitLabel adds a label to the default labels.
func (c *GitConfig) AddSubmitLabel(label string) error {
	currentLabels, _ := c.store.GetAll(KeySubmitLabels)
	if slices.Contains(currentLabels, label) {
		return nil // Already exists
	}
	return c.store.Add(KeySubmitLabels, label)
}

// SubmitReviewers returns the default reviewers for PRs.
// Merges reviewers from git config and project config (deduplicated).
func (c *GitConfig) SubmitReviewers() []string {
	reviewers := []string{}

	// Add reviewers from git config
	gitReviewers, _ := c.store.GetAll(KeySubmitReviewers)
	for _, r := range gitReviewers {
		if !slices.Contains(reviewers, r) {
			reviewers = append(reviewers, r)
		}
	}

	// Add reviewers from project config
	if c.project != nil && c.project.HasSubmitReviewers() {
		for _, r := range c.project.Submit.Reviewers {
			if !slices.Contains(reviewers, r) {
				reviewers = append(reviewers, r)
			}
		}
	}

	return reviewers
}

// SetSubmitReviewers sets the default reviewers for PRs.
// This replaces all existing reviewers.
func (c *GitConfig) SetSubmitReviewers(reviewers []string) error {
	// Clear existing reviewers
	if err := c.store.Unset(KeySubmitReviewers); err != nil {
		return err
	}
	// Add new reviewers
	for _, reviewer := range reviewers {
		if err := c.store.Add(KeySubmitReviewers, reviewer); err != nil {
			return err
		}
	}
	return nil
}

// AddSubmitReviewer adds a reviewer to the default reviewers.
func (c *GitConfig) AddSubmitReviewer(reviewer string) error {
	currentReviewers, _ := c.store.GetAll(KeySubmitReviewers)
	if slices.Contains(currentReviewers, reviewer) {
		return nil // Already exists
	}
	return c.store.Add(KeySubmitReviewers, reviewer)
}

// SubmitAssignees returns the default assignees for PRs.
// Merges assignees from git config and project config (deduplicated).
func (c *GitConfig) SubmitAssignees() []string {
	assignees := []string{}

	// Add assignees from git config
	gitAssignees, _ := c.store.GetAll(KeySubmitAssignees)
	for _, a := range gitAssignees {
		if !slices.Contains(assignees, a) {
			assignees = append(assignees, a)
		}
	}

	// Add assignees from project config
	if c.project != nil && c.project.HasSubmitAssignees() {
		for _, a := range c.project.Submit.Assignees {
			if !slices.Contains(assignees, a) {
				assignees = append(assignees, a)
			}
		}
	}

	return assignees
}

// SetSubmitAssignees sets the default assignees for PRs.
// This replaces all existing assignees.
func (c *GitConfig) SetSubmitAssignees(assignees []string) error {
	// Clear existing assignees
	if err := c.store.Unset(KeySubmitAssignees); err != nil {
		return err
	}
	// Add new assignees
	for _, assignee := range assignees {
		if err := c.store.Add(KeySubmitAssignees, assignee); err != nil {
			return err
		}
	}
	return nil
}

// AddSubmitAssignee adds an assignee to the default assignees.
func (c *GitConfig) AddSubmitAssignee(assignee string) error {
	currentAssignees, _ := c.store.GetAll(KeySubmitAssignees)
	if slices.Contains(currentAssignees, assignee) {
		return nil // Already exists
	}
	return c.store.Add(KeySubmitAssignees, assignee)
}

// Deprecated methods for backwards compatibility during migration.

// CombineCICommand returns the CI command (deprecated, use CICommand).
func (c *GitConfig) CombineCICommand() string {
	return c.CICommand()
}

// SetCombineCICommand sets the CI command (deprecated, use SetCICommand).
func (c *GitConfig) SetCombineCICommand(cmd string) {
	_ = c.SetCICommand(cmd)
}

// CombineCITimeout returns the CI timeout (deprecated, use CITimeout).
func (c *GitConfig) CombineCITimeout() int {
	return c.CITimeout()
}

// SetCombineCITimeout sets the CI timeout (deprecated, use SetCITimeout).
func (c *GitConfig) SetCombineCITimeout(seconds int) {
	_ = c.SetCITimeout(seconds)
}

// Save is a no-op for GitConfig since git config writes are immediate.
// This method exists for API compatibility with the old Config type.
func (c *GitConfig) Save() error {
	return nil
}

// UnsetTrunk removes the personal trunk setting, reverting to project/default.
// Note: This only makes sense if there's a project config with a trunk set,
// otherwise the effective trunk will be the built-in default ("main").
func (c *GitConfig) UnsetTrunk() error {
	return c.store.Unset(KeyTrunk)
}

// UnsetBranchNamePattern removes the personal branch name pattern, reverting to project/default.
func (c *GitConfig) UnsetBranchNamePattern() error {
	return c.store.Unset(KeyBranchPattern)
}

// UnsetSubmitFooter removes the personal submit footer setting, reverting to project/default.
func (c *GitConfig) UnsetSubmitFooter() error {
	return c.store.Unset(KeySubmitFooter)
}

// UnsetMergeMethod removes the personal merge method setting, reverting to project/default.
func (c *GitConfig) UnsetMergeMethod() error {
	return c.store.Unset(KeyMergeMethod)
}

// UnsetWorktreeBasePath removes the personal worktree base path setting, reverting to project/default.
func (c *GitConfig) UnsetWorktreeBasePath() error {
	return c.store.Unset(KeyWorktreeBasePath)
}

// UnsetWorktreeAutoClean removes the personal worktree auto clean setting, reverting to project/default.
func (c *GitConfig) UnsetWorktreeAutoClean() error {
	return c.store.Unset(KeyWorktreeAutoClean)
}

// UnsetSplitHunkSelector removes the personal split hunk selector setting, reverting to project/default.
func (c *GitConfig) UnsetSplitHunkSelector() error {
	return c.store.Unset(KeySplitHunkSelector)
}

// UnsetUndoStackDepth removes the personal undo stack depth setting, reverting to project/default.
func (c *GitConfig) UnsetUndoStackDepth() error {
	return c.store.Unset(KeyUndoDepth)
}

// UnsetCICommand removes the personal CI command setting, reverting to project/default.
func (c *GitConfig) UnsetCICommand() error {
	return c.store.Unset(KeyCICommand)
}

// UnsetCITimeout removes the personal CI timeout setting, reverting to project/default.
func (c *GitConfig) UnsetCITimeout() error {
	return c.store.Unset(KeyCITimeout)
}

// UnsetMaxConcurrency removes the personal max concurrency setting, reverting to default.
func (c *GitConfig) UnsetMaxConcurrency() error {
	return c.store.Unset(KeyMaxConcurrency)
}

// UnsetNavigationWhen removes the personal navigation.when setting, reverting to project/default.
func (c *GitConfig) UnsetNavigationWhen() error {
	return c.store.Unset(KeyNavigationWhen)
}

// UnsetNavigationMarker removes the personal navigation.marker setting, reverting to project/default.
func (c *GitConfig) UnsetNavigationMarker() error {
	return c.store.Unset(KeyNavigationMarker)
}

// UnsetNavigationLocation removes the personal navigation.location setting, reverting to project/default.
func (c *GitConfig) UnsetNavigationLocation() error {
	return c.store.Unset(KeyNavigationLocation)
}

// UnsetNavigationShowMerged removes the personal navigation.showMerged setting, reverting to project/default.
func (c *GitConfig) UnsetNavigationShowMerged() error {
	return c.store.Unset(KeyNavigationShowMerged)
}

// UnsetSubmitDraft removes the personal submit.draft setting, reverting to project/default.
func (c *GitConfig) UnsetSubmitDraft() error {
	return c.store.Unset(KeySubmitDraft)
}

// UnsetSubmitWeb removes the personal submit.web setting, reverting to project/default.
func (c *GitConfig) UnsetSubmitWeb() error {
	return c.store.Unset(KeySubmitWeb)
}

// UnsetSubmitLabels removes all personal submit.labels, reverting to project/default.
func (c *GitConfig) UnsetSubmitLabels() error {
	return c.store.Unset(KeySubmitLabels)
}

// UnsetSubmitReviewers removes all personal submit.reviewers, reverting to project/default.
func (c *GitConfig) UnsetSubmitReviewers() error {
	return c.store.Unset(KeySubmitReviewers)
}

// UnsetSubmitAssignees removes all personal submit.assignees, reverting to project/default.
func (c *GitConfig) UnsetSubmitAssignees() error {
	return c.store.Unset(KeySubmitAssignees)
}

// ResetAllPersonal removes all personal configuration overrides, reverting to team/default values.
// This clears all stackit.* keys from the local git config.
func (c *GitConfig) ResetAllPersonal() error {
	keys := []string{
		KeyTrunk,
		KeyTrunks,
		KeyBranchPattern,
		KeySubmitFooter,
		KeySubmitDraft,
		KeySubmitWeb,
		KeySubmitLabels,
		KeySubmitReviewers,
		KeySubmitAssignees,
		KeyUndoDepth,
		KeyWorktreeBasePath,
		KeyWorktreeAutoClean,
		KeyMergeMethod,
		KeyCICommand,
		KeyCITimeout,
		KeySplitHunkSelector,
		KeyApprovedHooks,
		KeyMaxConcurrency,
		KeyNavigationWhen,
		KeyNavigationMarker,
		KeyNavigationLocation,
		KeyNavigationShowMerged,
	}

	var firstErr error
	for _, key := range keys {
		if err := c.store.Unset(key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
