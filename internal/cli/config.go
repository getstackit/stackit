package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	configtui "github.com/getstackit/stackit/internal/tui/config"
	"github.com/getstackit/stackit/internal/tui/style"
)

// Config key constants for CLI commands
const (
	keyTrunk                = "trunk"
	keyTrunks               = "trunks"
	keyTrunksAdd            = "trunks.add"
	keyTrunksRemove         = "trunks.remove"
	keyTrunksClear          = "trunks.clear"
	keyBranchPattern        = "branch.pattern"
	keyStackShape           = "stack.shape"
	keySubmitFooter         = "submit.footer"
	keySubmitDraft          = "submit.draft"
	keyGitHubStack          = "github.stack"
	keySubmitWeb            = "submit.web"
	keySubmitLabels         = "submit.labels"
	keySubmitLabelsAdd      = "submit.labels.add"
	keySubmitLabelsClear    = "submit.labels.clear"
	keySubmitReviewers      = "submit.reviewers"
	keySubmitReviewersAdd   = "submit.reviewers.add"
	keySubmitReviewersClear = "submit.reviewers.clear"
	keySubmitAssignees      = "submit.assignees"
	keySubmitAssigneesAdd   = "submit.assignees.add"
	keySubmitAssigneesClear = "submit.assignees.clear"
	keyMergeMethod          = "merge.method"
	keyWorktreeBasePath     = "worktree.basePath"
	keyWorktreeAutoClean    = "worktree.autoClean"
	keySplitHunkSelector    = "split.hunkSelector"
	keyUndoDepth            = "undo.depth"
	keyUndoEnabled          = "undo.enabled"
	keyCICommand            = "ci.command"
	keyCITimeout            = "ci.timeout"
	keyMaxConcurrency       = "maxConcurrency"
	keyNavigationWhen       = "navigation.when"
	keyNavigationMarker     = "navigation.marker"
	keyNavigationLocation   = "navigation.location"
	keyNavigationShowMerged = "navigation.showMerged"
	valueNotSet             = "(not set)"
	cmdReset                = "reset"
)

// newConfigCmd creates the config command
func newConfigCmd() *cobra.Command {
	var listFlag bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get and set repository configuration",
		Long: `Get and set repository configuration values.

When run without subcommands, opens an interactive TUI for editing configuration.
Use --list to print all configuration values instead.

Examples:
  stackit config                    # Interactive TUI
  stackit config --list             # Print all config values
  stackit config get branch.pattern
  stackit config set branch.pattern "{username}/{date}/{message}"
  stackit config get submit.footer
  stackit config set submit.footer false`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			// Get repo root
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			// If --list flag is set, or terminal is not interactive, show list
			if listFlag || !tui.IsTTY() {
				return actions.ConfigListAction(repoRoot, cmd.OutOrStdout())
			}

			// Otherwise, show interactive TUI
			return configtui.TUIAction(repoRoot)
		},
	}

	cmd.Flags().BoolVarP(&listFlag, "list", "l", false, "Print all configuration values instead of opening interactive TUI")

	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigUnsetCmd())
	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigResetCmd())
	cmd.AddCommand(newConfigInitCmd())

	return cmd
}

// newConfigGetCmd creates the config get command
func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "get <key>",
		Short:        "Get a configuration value",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			// Get repo root
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			key := args[0]
			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			switch key {
			case keyTrunk:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.Trunk())
			case keyTrunks:
				allTrunks := cfg.AllTrunks()
				if len(allTrunks) > 1 {
					// Show additional trunks (skip primary trunk)
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(allTrunks[1:], ", "))
				} else {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), valueNotSet)
				}
			case keyBranchPattern:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.BranchNamePattern())
			case keyStackShape:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.StackShape())
			case keySubmitFooter:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.SubmitFooter())
			case keySubmitDraft:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.SubmitDraft())
			case keyGitHubStack:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.GitHubStack())
			case keySubmitWeb:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.SubmitWeb())
			case keySubmitLabels:
				labels := cfg.SubmitLabels()
				if len(labels) > 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(labels, ", "))
				} else {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), valueNotSet)
				}
			case keySubmitReviewers:
				reviewers := cfg.SubmitReviewers()
				if len(reviewers) > 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(reviewers, ", "))
				} else {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), valueNotSet)
				}
			case keySubmitAssignees:
				assignees := cfg.SubmitAssignees()
				if len(assignees) > 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(assignees, ", "))
				} else {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), valueNotSet)
				}
			case keyMergeMethod:
				method := cfg.MergeMethod()
				if method == "" {
					method = valueNotSet
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), method)
			case keyWorktreeBasePath:
				basePath := cfg.WorktreeBasePath()
				if basePath == "" {
					basePath = valueNotSet
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), basePath)
			case keyWorktreeAutoClean:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.WorktreeAutoClean())
			case keySplitHunkSelector:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.SplitHunkSelector())
			case keyUndoDepth:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.UndoStackDepth())
			case keyUndoEnabled:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.UndoEnabled())
			case keyCICommand:
				ciCmd := cfg.CICommand()
				if ciCmd == "" {
					ciCmd = valueNotSet
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), ciCmd)
			case keyCITimeout:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.CITimeout())
			case keyMaxConcurrency:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.MaxConcurrency())
			case keyNavigationWhen:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.NavigationWhen())
			case keyNavigationMarker:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.NavigationMarker())
			case keyNavigationLocation:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.NavigationLocation())
			case keyNavigationShowMerged:
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.NavigationShowMerged())
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			return nil
		},
	}

	return cmd
}

// newConfigSetCmd creates the config set command
func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "set <key> <value>",
		Short:        "Set a configuration value",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			// Get repo root
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			key := args[0]
			value := args[1]

			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			splog := output.NewConsoleOutput(cmd.OutOrStdout(), false)

			switch key {
			case keyTrunk:
				if err := cfg.SetTrunk(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyTrunk, err)
				}
				splog.Info("Set %s to: %s", keyTrunk, value)
			case keyTrunksAdd:
				if err := cfg.AddTrunk(value); err != nil {
					return fmt.Errorf("failed to add trunk: %w", err)
				}
				splog.Info("Added '%s' to additional trunks", value)
			case keyTrunksRemove:
				if err := cfg.RemoveTrunk(value); err != nil {
					return fmt.Errorf("failed to remove trunk: %w", err)
				}
				splog.Info("Removed '%s' from additional trunks", value)
			case keyTrunksClear:
				if err := cfg.ClearTrunks(); err != nil {
					return fmt.Errorf("failed to clear trunks: %w", err)
				}
				splog.Info("Cleared all personal additional trunks")
			case keyBranchPattern:
				if err := cfg.SetBranchNamePattern(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyBranchPattern, err)
				}
				splog.Info("Set %s to: %s", keyBranchPattern, value)
			case keyStackShape:
				if err := cfg.SetStackShape(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyStackShape, err)
				}
				splog.Info("Set %s to: %s", keyStackShape, value)
			case keySubmitFooter:
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be 'true' or 'false')", keySubmitFooter, value)
				}
				if err := cfg.SetSubmitFooter(enabled); err != nil {
					return fmt.Errorf("failed to set %s: %w", keySubmitFooter, err)
				}
				splog.Info("Set %s to: %v", keySubmitFooter, enabled)
			case keySubmitDraft:
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be 'true' or 'false')", keySubmitDraft, value)
				}
				if err := cfg.SetSubmitDraft(enabled); err != nil {
					return fmt.Errorf("failed to set %s: %w", keySubmitDraft, err)
				}
				splog.Info("Set %s to: %v", keySubmitDraft, enabled)
			case keyGitHubStack:
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be 'true' or 'false')", keyGitHubStack, value)
				}
				if err := cfg.SetGitHubStack(enabled); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyGitHubStack, err)
				}
				splog.Info("Set %s to: %v", keyGitHubStack, enabled)
			case keySubmitWeb:
				if err := cfg.SetSubmitWeb(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keySubmitWeb, err)
				}
				splog.Info("Set %s to: %s", keySubmitWeb, value)
			case keySubmitLabelsAdd:
				if err := cfg.AddSubmitLabel(value); err != nil {
					return fmt.Errorf("failed to add label: %w", err)
				}
				splog.Info("Added '%s' to default labels", value)
			case keySubmitLabelsClear:
				if err := cfg.UnsetSubmitLabels(); err != nil {
					return fmt.Errorf("failed to clear labels: %w", err)
				}
				splog.Info("Cleared all personal default labels")
			case keySubmitReviewersAdd:
				if err := cfg.AddSubmitReviewer(value); err != nil {
					return fmt.Errorf("failed to add reviewer: %w", err)
				}
				splog.Info("Added '%s' to default reviewers", value)
			case keySubmitReviewersClear:
				if err := cfg.UnsetSubmitReviewers(); err != nil {
					return fmt.Errorf("failed to clear reviewers: %w", err)
				}
				splog.Info("Cleared all personal default reviewers")
			case keySubmitAssigneesAdd:
				if err := cfg.AddSubmitAssignee(value); err != nil {
					return fmt.Errorf("failed to add assignee: %w", err)
				}
				splog.Info("Added '%s' to default assignees", value)
			case keySubmitAssigneesClear:
				if err := cfg.UnsetSubmitAssignees(); err != nil {
					return fmt.Errorf("failed to clear assignees: %w", err)
				}
				splog.Info("Cleared all personal default assignees")
			case keyMergeMethod:
				if err := cfg.SetMergeMethod(github.MergeMethod(value)); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyMergeMethod, err)
				}
				splog.Info("Set %s to: %s", keyMergeMethod, value)
			case keyWorktreeBasePath:
				if err := cfg.SetWorktreeBasePath(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyWorktreeBasePath, err)
				}
				splog.Info("Set %s to: %s", keyWorktreeBasePath, value)
			case keyWorktreeAutoClean:
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be 'true' or 'false')", keyWorktreeAutoClean, value)
				}
				if err := cfg.SetWorktreeAutoClean(enabled); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyWorktreeAutoClean, err)
				}
				splog.Info("Set %s to: %v", keyWorktreeAutoClean, enabled)
			case keySplitHunkSelector:
				if err := cfg.SetSplitHunkSelector(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keySplitHunkSelector, err)
				}
				splog.Info("Set %s to: %s", keySplitHunkSelector, value)
			case keyUndoDepth:
				depth, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be a positive integer)", keyUndoDepth, value)
				}
				if err := cfg.SetUndoStackDepth(depth); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyUndoDepth, err)
				}
				splog.Info("Set %s to: %d", keyUndoDepth, depth)
			case keyUndoEnabled:
				enabled, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be 'true' or 'false')", keyUndoEnabled, value)
				}
				if err := cfg.SetUndoEnabled(enabled); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyUndoEnabled, err)
				}
				splog.Info("Set %s to: %v", keyUndoEnabled, enabled)
			case keyCICommand:
				if err := cfg.SetCICommand(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyCICommand, err)
				}
				splog.Info("Set %s to: %s", keyCICommand, value)
			case keyCITimeout:
				timeout, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be a positive integer)", keyCITimeout, value)
				}
				if err := cfg.SetCITimeout(timeout); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyCITimeout, err)
				}
				splog.Info("Set %s to: %d", keyCITimeout, timeout)
			case keyMaxConcurrency:
				concurrency, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be a non-negative integer)", keyMaxConcurrency, value)
				}
				if err := cfg.SetMaxConcurrency(concurrency); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyMaxConcurrency, err)
				}
				splog.Info("Set %s to: %d", keyMaxConcurrency, concurrency)
			case keyNavigationWhen:
				if err := cfg.SetNavigationWhen(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyNavigationWhen, err)
				}
				splog.Info("Set %s to: %s", keyNavigationWhen, value)
			case keyNavigationMarker:
				if err := cfg.SetNavigationMarker(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyNavigationMarker, err)
				}
				splog.Info("Set %s to: %s", keyNavigationMarker, value)
			case keyNavigationLocation:
				if err := cfg.SetNavigationLocation(value); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyNavigationLocation, err)
				}
				splog.Info("Set %s to: %s", keyNavigationLocation, value)
			case keyNavigationShowMerged:
				show, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("invalid value for %s: %s (must be 'true' or 'false')", keyNavigationShowMerged, value)
				}
				if err := cfg.SetNavigationShowMerged(show); err != nil {
					return fmt.Errorf("failed to set %s: %w", keyNavigationShowMerged, err)
				}
				splog.Info("Set %s to: %v", keyNavigationShowMerged, show)
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			return nil
		},
	}

	return cmd
}

// newConfigUnsetCmd creates the config unset command
func newConfigUnsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a personal configuration value (reverts to team/default)",
		Long: `Remove a personal configuration value, reverting to the team project config or default.

This removes your personal override for a setting, allowing the team default
(from .stackit.yaml) or the built-in default to take effect.

Examples:
  stackit config unset branch.pattern     # Revert to team/default pattern
  stackit config unset merge.method       # Revert to team/default merge method
  stackit config unset submit.footer      # Revert to team/default footer setting`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			key := args[0]
			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			splog := output.NewConsoleOutput(cmd.OutOrStdout(), false)

			switch key {
			case keyTrunk:
				if err := cfg.UnsetTrunk(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyTrunk, err)
				}
				splog.Info("Unset %s (now using: %s)", keyTrunk, cfg.Trunk())
			case keyBranchPattern:
				if err := cfg.UnsetBranchNamePattern(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyBranchPattern, err)
				}
				splog.Info("Unset %s (now using: %s)", keyBranchPattern, cfg.BranchNamePattern())
			case keyStackShape:
				if err := cfg.UnsetStackShape(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyStackShape, err)
				}
				splog.Info("Unset %s (now using: %s)", keyStackShape, cfg.StackShape())
			case keySubmitFooter:
				if err := cfg.UnsetSubmitFooter(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySubmitFooter, err)
				}
				splog.Info("Unset %s (now using: %v)", keySubmitFooter, cfg.SubmitFooter())
			case keySubmitDraft:
				if err := cfg.UnsetSubmitDraft(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySubmitDraft, err)
				}
				splog.Info("Unset %s (now using: %v)", keySubmitDraft, cfg.SubmitDraft())
			case keyGitHubStack:
				if err := cfg.UnsetGitHubStack(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyGitHubStack, err)
				}
				splog.Info("Unset %s (now using: %v)", keyGitHubStack, cfg.GitHubStack())
			case keySubmitWeb:
				if err := cfg.UnsetSubmitWeb(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySubmitWeb, err)
				}
				splog.Info("Unset %s (now using: %s)", keySubmitWeb, cfg.SubmitWeb())
			case keySubmitLabels:
				if err := cfg.UnsetSubmitLabels(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySubmitLabels, err)
				}
				labels := cfg.SubmitLabels()
				if len(labels) > 0 {
					splog.Info("Unset %s (now using: %s)", keySubmitLabels, strings.Join(labels, ", "))
				} else {
					splog.Info("Unset %s (now using: %s)", keySubmitLabels, valueNotSet)
				}
			case keySubmitReviewers:
				if err := cfg.UnsetSubmitReviewers(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySubmitReviewers, err)
				}
				reviewers := cfg.SubmitReviewers()
				if len(reviewers) > 0 {
					splog.Info("Unset %s (now using: %s)", keySubmitReviewers, strings.Join(reviewers, ", "))
				} else {
					splog.Info("Unset %s (now using: %s)", keySubmitReviewers, valueNotSet)
				}
			case keySubmitAssignees:
				if err := cfg.UnsetSubmitAssignees(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySubmitAssignees, err)
				}
				assignees := cfg.SubmitAssignees()
				if len(assignees) > 0 {
					splog.Info("Unset %s (now using: %s)", keySubmitAssignees, strings.Join(assignees, ", "))
				} else {
					splog.Info("Unset %s (now using: %s)", keySubmitAssignees, valueNotSet)
				}
			case keyMergeMethod:
				if err := cfg.UnsetMergeMethod(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyMergeMethod, err)
				}
				method := cfg.MergeMethod()
				if method == "" {
					method = valueNotSet
				}
				splog.Info("Unset %s (now using: %s)", keyMergeMethod, method)
			case keyWorktreeBasePath:
				if err := cfg.UnsetWorktreeBasePath(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyWorktreeBasePath, err)
				}
				basePath := cfg.WorktreeBasePath()
				if basePath == "" {
					basePath = valueNotSet
				}
				splog.Info("Unset %s (now using: %s)", keyWorktreeBasePath, basePath)
			case keyWorktreeAutoClean:
				if err := cfg.UnsetWorktreeAutoClean(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyWorktreeAutoClean, err)
				}
				splog.Info("Unset %s (now using: %v)", keyWorktreeAutoClean, cfg.WorktreeAutoClean())
			case keySplitHunkSelector:
				if err := cfg.UnsetSplitHunkSelector(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keySplitHunkSelector, err)
				}
				splog.Info("Unset %s (now using: %s)", keySplitHunkSelector, cfg.SplitHunkSelector())
			case keyUndoDepth:
				if err := cfg.UnsetUndoStackDepth(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyUndoDepth, err)
				}
				splog.Info("Unset %s (now using: %d)", keyUndoDepth, cfg.UndoStackDepth())
			case keyUndoEnabled:
				if err := cfg.UnsetUndoEnabled(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyUndoEnabled, err)
				}
				splog.Info("Unset %s (now using: %v)", keyUndoEnabled, cfg.UndoEnabled())
			case keyCICommand:
				if err := cfg.UnsetCICommand(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyCICommand, err)
				}
				ciCmd := cfg.CICommand()
				if ciCmd == "" {
					ciCmd = valueNotSet
				}
				splog.Info("Unset %s (now using: %s)", keyCICommand, ciCmd)
			case keyCITimeout:
				if err := cfg.UnsetCITimeout(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyCITimeout, err)
				}
				splog.Info("Unset %s (now using: %d)", keyCITimeout, cfg.CITimeout())
			case keyMaxConcurrency:
				if err := cfg.UnsetMaxConcurrency(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyMaxConcurrency, err)
				}
				splog.Info("Unset %s (now using: %d)", keyMaxConcurrency, cfg.MaxConcurrency())
			case keyNavigationWhen:
				if err := cfg.UnsetNavigationWhen(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyNavigationWhen, err)
				}
				splog.Info("Unset %s (now using: %s)", keyNavigationWhen, cfg.NavigationWhen())
			case keyNavigationMarker:
				if err := cfg.UnsetNavigationMarker(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyNavigationMarker, err)
				}
				splog.Info("Unset %s (now using: %s)", keyNavigationMarker, cfg.NavigationMarker())
			case keyNavigationLocation:
				if err := cfg.UnsetNavigationLocation(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyNavigationLocation, err)
				}
				splog.Info("Unset %s (now using: %s)", keyNavigationLocation, cfg.NavigationLocation())
			case keyNavigationShowMerged:
				if err := cfg.UnsetNavigationShowMerged(); err != nil {
					return fmt.Errorf("failed to unset %s: %w", keyNavigationShowMerged, err)
				}
				splog.Info("Unset %s (now using: %v)", keyNavigationShowMerged, cfg.NavigationShowMerged())
			default:
				return fmt.Errorf("unknown configuration key: %s", key)
			}

			return nil
		},
	}

	return cmd
}

// newConfigShowCmd creates the config show command
func newConfigShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show all configuration values with their sources",
		Long: `Show all configuration values with their sources (personal, team, or default).

This helps debug configuration by showing where each value comes from in the
layered configuration system:
  - personal: Set in your local git config (.git/config)
  - team:     Set in the project file (.stackit.yaml)
  - default:  Built-in default value`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			return showConfigWithSources(repoRoot, cmd.OutOrStdout())
		},
	}

	return cmd
}

// newConfigResetCmd creates the config reset command
func newConfigResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdReset,
		Short: "Reset all personal configuration overrides",
		Long: `Reset all personal configuration overrides, reverting to team project config or defaults.

This removes all your personal stackit settings from .git/config, allowing the team
defaults (from .stackit.yaml) or built-in defaults to take effect.

Use with caution - this will clear all personal configuration including:
  - trunk and additional trunks
  - branch pattern
  - submit footer
  - merge method
  - CI command and timeout
  - worktree settings
  - approved hooks
  - and all other personal overrides`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			cfg, err := config.LoadConfig(repoRoot)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Confirm with user if interactive
			if tui.IsTTY() {
				confirmed, err := tui.PromptConfirm("This will remove all personal configuration overrides. Continue?", false)
				if err != nil {
					return err
				}
				if !confirmed {
					return fmt.Errorf("reset canceled")
				}
			}

			if err := cfg.ResetAllPersonal(); err != nil {
				return fmt.Errorf("failed to reset config: %w", err)
			}

			splog := output.NewConsoleOutput(cmd.OutOrStdout(), false)
			splog.Info("Reset all personal configuration overrides")
			splog.Info("Run 'stackit config show' to see current effective values")
			return nil
		},
	}

	return cmd
}

// newConfigInitCmd creates the config init command
func newConfigInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create an example .stackit.yaml with documented options",
		Long: `Create a .stackit.yaml file with all available configuration options.

The generated file contains commented examples for all team-shared settings.
Uncomment and modify the options you want to use.

If .stackit.yaml already exists, use --force to overwrite it.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			repoRoot, err := runner.DiscoverRepoRoot()
			if err != nil {
				return fmt.Errorf("not a git repository: %w", err)
			}

			configPath := filepath.Join(repoRoot, ".stackit.yaml")

			// Check if file already exists
			if _, err := os.Stat(configPath); err == nil {
				if !force {
					return fmt.Errorf(".stackit.yaml already exists; use --force to overwrite")
				}
			}

			// Write the template
			template := config.GenerateConfigTemplate()
			if err := os.WriteFile(configPath, []byte(template), 0600); err != nil {
				return fmt.Errorf("failed to write .stackit.yaml: %w", err)
			}

			splog := output.NewConsoleOutput(cmd.OutOrStdout(), false)
			splog.Info("Created .stackit.yaml with documented configuration options")
			splog.Info("Edit the file to customize team settings, then commit it to share with your team")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing .stackit.yaml")
	return cmd
}

// configSource represents where a config value comes from
type configSource string

const (
	sourcePersonal configSource = "personal"
	sourceTeam     configSource = "team"
	sourceDefault  configSource = "default"
)

// showConfigWithSources displays all config values with their sources
func showConfigWithSources(repoRoot string, w io.Writer) error {
	// Load both git config and project config separately to determine sources
	store := git.NewConfigStore(repoRoot)
	projectCfg, _ := config.LoadProjectConfig(repoRoot)

	// Helper to format value and source
	formatLine := func(key, value string, source configSource) {
		sourceColor := style.ColorDim(fmt.Sprintf("(%s)", source))
		_, _ = fmt.Fprintf(w, "  %-20s = %-30s %s\n", key, value, sourceColor)
	}

	// Helper to determine source for string values
	getStringSource := func(gitKey string, hasProject bool) configSource {
		if val, _ := store.Get(gitKey); val != "" {
			return sourcePersonal
		}
		if hasProject {
			return sourceTeam
		}
		return sourceDefault
	}

	// Helper to determine source for bool values
	getBoolSource := func(gitKey string, hasProject bool) configSource {
		if store.Exists(gitKey) {
			return sourcePersonal
		}
		if hasProject {
			return sourceTeam
		}
		return sourceDefault
	}

	// Helper to determine source for int values
	getIntSource := func(gitKey string, hasProject bool) configSource {
		if store.Exists(gitKey) {
			return sourcePersonal
		}
		if hasProject {
			return sourceTeam
		}
		return sourceDefault
	}

	// Load config to get effective values
	cfg, err := config.LoadConfig(repoRoot)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	_, _ = fmt.Fprintln(w, "Configuration values with sources:")
	_, _ = fmt.Fprintln(w, "")

	// trunk
	trunkSource := getStringSource(config.KeyTrunk, projectCfg != nil && projectCfg.HasTrunk())
	formatLine("trunk", cfg.Trunk(), trunkSource)

	// additional trunks (merged from git config and project config)
	allTrunks := cfg.AllTrunks()
	if len(allTrunks) > 1 {
		additionalTrunks := allTrunks[1:] // Skip primary trunk
		// Determine source - if any are in git config, mark as personal; if in project, mark as team
		trunksFromGit, _ := store.GetAll(config.KeyTrunks)
		var trunksSource configSource
		switch {
		case len(trunksFromGit) > 0:
			trunksSource = sourcePersonal
		case projectCfg != nil && projectCfg.HasTrunks():
			trunksSource = sourceTeam
		default:
			trunksSource = sourceDefault
		}
		formatLine("trunks", strings.Join(additionalTrunks, ", "), trunksSource)
	}

	// branch.pattern
	patternSource := getStringSource(config.KeyBranchPattern, projectCfg != nil && projectCfg.HasBranchPattern())
	formatLine("branch.pattern", cfg.BranchNamePattern(), patternSource)

	// stack.shape
	stackShapeSource := getStringSource(config.KeyStackShape, projectCfg != nil && projectCfg.HasStackShape())
	formatLine(keyStackShape, cfg.StackShape(), stackShapeSource)

	// submit.footer
	footerSource := getBoolSource(config.KeySubmitFooter, projectCfg != nil && projectCfg.HasSubmitFooter())
	formatLine("submit.footer", strconv.FormatBool(cfg.SubmitFooter()), footerSource)

	// submit.draft
	draftSource := getBoolSource(config.KeySubmitDraft, projectCfg != nil && projectCfg.HasSubmitDraft())
	formatLine("submit.draft", strconv.FormatBool(cfg.SubmitDraft()), draftSource)

	// github.stack
	githubStackSource := getBoolSource(config.KeyGitHubStack, projectCfg != nil && projectCfg.HasGitHubStack())
	formatLine(keyGitHubStack, strconv.FormatBool(cfg.GitHubStack()), githubStackSource)

	// submit.web
	webSource := getStringSource(config.KeySubmitWeb, projectCfg != nil && projectCfg.HasSubmitWeb())
	formatLine("submit.web", cfg.SubmitWeb(), webSource)

	// submit.labels
	labels := cfg.SubmitLabels()
	labelsValue := valueNotSet
	if len(labels) > 0 {
		labelsValue = strings.Join(labels, ", ")
	}
	labelsFromGit, _ := store.GetAll(config.KeySubmitLabels)
	var labelsSource configSource
	switch {
	case len(labelsFromGit) > 0:
		labelsSource = sourcePersonal
	case projectCfg != nil && projectCfg.HasSubmitLabels():
		labelsSource = sourceTeam
	default:
		labelsSource = sourceDefault
	}
	formatLine("submit.labels", labelsValue, labelsSource)

	// submit.reviewers
	reviewers := cfg.SubmitReviewers()
	reviewersValue := valueNotSet
	if len(reviewers) > 0 {
		reviewersValue = strings.Join(reviewers, ", ")
	}
	reviewersFromGit, _ := store.GetAll(config.KeySubmitReviewers)
	var reviewersSource configSource
	switch {
	case len(reviewersFromGit) > 0:
		reviewersSource = sourcePersonal
	case projectCfg != nil && projectCfg.HasSubmitReviewers():
		reviewersSource = sourceTeam
	default:
		reviewersSource = sourceDefault
	}
	formatLine("submit.reviewers", reviewersValue, reviewersSource)

	// submit.assignees
	assignees := cfg.SubmitAssignees()
	assigneesValue := valueNotSet
	if len(assignees) > 0 {
		assigneesValue = strings.Join(assignees, ", ")
	}
	assigneesFromGit, _ := store.GetAll(config.KeySubmitAssignees)
	var assigneesSource configSource
	switch {
	case len(assigneesFromGit) > 0:
		assigneesSource = sourcePersonal
	case projectCfg != nil && projectCfg.HasSubmitAssignees():
		assigneesSource = sourceTeam
	default:
		assigneesSource = sourceDefault
	}
	formatLine("submit.assignees", assigneesValue, assigneesSource)

	// merge.method
	mergeMethod := string(cfg.MergeMethod())
	if mergeMethod == "" {
		mergeMethod = valueNotSet
	}
	mergeSource := getStringSource(config.KeyMergeMethod, projectCfg != nil && projectCfg.HasMergeMethod())
	formatLine(keyMergeMethod, mergeMethod, mergeSource)

	// ci.command
	ciCmd := cfg.CICommand()
	if ciCmd == "" {
		ciCmd = valueNotSet
	}
	ciCmdSource := getStringSource(config.KeyCICommand, projectCfg != nil && projectCfg.HasCICommand())
	formatLine(keyCICommand, ciCmd, ciCmdSource)

	// ci.timeout
	ciTimeoutSource := getIntSource(config.KeyCITimeout, projectCfg != nil && projectCfg.HasCITimeout())
	formatLine("ci.timeout", fmt.Sprintf("%d", cfg.CITimeout()), ciTimeoutSource)

	// undo.depth
	undoSource := getIntSource(config.KeyUndoDepth, projectCfg != nil && projectCfg.HasUndoDepth())
	formatLine("undo.depth", fmt.Sprintf("%d", cfg.UndoStackDepth()), undoSource)

	// undo.enabled
	undoEnabledSource := getBoolSource(config.KeyUndoEnabled, projectCfg != nil && projectCfg.HasUndoEnabled())
	formatLine("undo.enabled", strconv.FormatBool(cfg.UndoEnabled()), undoEnabledSource)

	// worktree.basePath
	basePath := cfg.WorktreeBasePath()
	if basePath == "" {
		basePath = valueNotSet
	}
	basePathSource := getStringSource(config.KeyWorktreeBasePath, projectCfg != nil && projectCfg.HasWorktreeBasePath())
	formatLine(keyWorktreeBasePath, basePath, basePathSource)

	// worktree.autoClean
	autoCleanSource := getBoolSource(config.KeyWorktreeAutoClean, projectCfg != nil && projectCfg.HasWorktreeAutoClean())
	formatLine("worktree.autoClean", strconv.FormatBool(cfg.WorktreeAutoClean()), autoCleanSource)

	// split.hunkSelector
	hunkSource := getStringSource(config.KeySplitHunkSelector, projectCfg != nil && projectCfg.HasSplitHunkSelector())
	formatLine("split.hunkSelector", cfg.SplitHunkSelector(), hunkSource)

	// maxConcurrency
	maxConcurrencySource := getIntSource(config.KeyMaxConcurrency, projectCfg != nil && projectCfg.HasMaxConcurrency())
	formatLine("maxConcurrency", fmt.Sprintf("%d", cfg.MaxConcurrency()), maxConcurrencySource)

	// navigation.when
	navWhenSource := getStringSource(config.KeyNavigationWhen, projectCfg != nil && projectCfg.HasNavigationWhen())
	formatLine("navigation.when", cfg.NavigationWhen(), navWhenSource)

	// navigation.marker
	navMarkerSource := getStringSource(config.KeyNavigationMarker, projectCfg != nil && projectCfg.HasNavigationMarker())
	formatLine("navigation.marker", cfg.NavigationMarker(), navMarkerSource)

	// navigation.location
	navLocationSource := getStringSource(config.KeyNavigationLocation, projectCfg != nil && projectCfg.HasNavigationLocation())
	formatLine("navigation.location", cfg.NavigationLocation(), navLocationSource)

	// navigation.showMerged
	navShowMergedSource := getBoolSource(config.KeyNavigationShowMerged, projectCfg != nil && projectCfg.HasNavigationShowMerged())
	formatLine("navigation.showMerged", strconv.FormatBool(cfg.NavigationShowMerged()), navShowMergedSource)

	// approved hooks (personal only, no team fallback)
	approvedHooks := cfg.ApprovedHooks(config.PhasePostWorktreeCreate)
	if len(approvedHooks) > 0 {
		formatLine("hooks.approved", strings.Join(approvedHooks, ", "), sourcePersonal)
	} else {
		formatLine("hooks.approved", valueNotSet, sourceDefault)
	}

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Sources: personal = .git/config, team = .stackit.yaml, default = built-in")

	return nil
}
