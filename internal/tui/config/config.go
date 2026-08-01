// Package config provides TUI components for configuration management.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
)

// TUIAction provides an interactive TUI for editing configuration
func TUIAction(repoRoot string) error {
	if err := tui.CheckInteractiveAllowed(); err != nil {
		return err
	}
	out := output.NewDefaultOutput()

	for {
		cfg, err := config.LoadConfig(repoRoot)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Get current values
		branchPattern := cfg.BranchNamePattern()
		submitFooter := cfg.SubmitFooter()
		submitGitHubStack := cfg.SubmitGitHubStack()
		mergeMethod := cfg.MergeMethod()
		if mergeMethod == "" {
			mergeMethod = "(not set)"
		}
		worktreeBasePath := cfg.WorktreeBasePath()
		if worktreeBasePath == "" {
			worktreeBasePath = "(not set)"
		}
		worktreeAutoClean := cfg.WorktreeAutoClean()
		splitHunkSelector := cfg.SplitHunkSelector()

		// Build options with current values displayed
		options := []tui.SelectOption{
			{
				Label: fmt.Sprintf("branch.pattern: %s", branchPattern),
				Value: "branch.pattern",
			},
			{
				Label: fmt.Sprintf("submit.footer: %v", submitFooter),
				Value: "submit.footer",
			},
			{
				Label: fmt.Sprintf("submit.githubStack: %v", submitGitHubStack),
				Value: "submit.githubStack",
			},
			{
				Label: fmt.Sprintf("merge.method: %s", mergeMethod),
				Value: "merge.method",
			},
			{
				Label: fmt.Sprintf("worktree.basePath: %s", worktreeBasePath),
				Value: "worktree.basePath",
			},
			{
				Label: fmt.Sprintf("worktree.autoClean: %v", worktreeAutoClean),
				Value: "worktree.autoClean",
			},
			{
				Label: fmt.Sprintf("split.hunkSelector: %s", splitHunkSelector),
				Value: "split.hunkSelector",
			},
			{
				Label: "Exit",
				Value: "exit",
			},
		}

		// Show selection menu
		selected, err := tui.PromptSelect("Select a configuration option to edit:", options, 0)
		if err != nil {
			return err
		}

		if selected == "exit" {
			break
		}

		// Handle each option
		switch selected {
		case "branch.pattern":
			newPattern, err := tui.PromptTextInput(fmt.Sprintf("Enter branch name pattern (current: %s):", branchPattern), branchPattern)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			if newPattern != "" && newPattern != branchPattern {
				if err := cfg.SetBranchNamePattern(newPattern); err != nil {
					out.Info("Failed to set branch.pattern: %v", err)
					continue
				}
				if err := cfg.Save(); err != nil {
					out.Info("Failed to save config: %v", err)
					continue
				}
				out.Info("Set branch.pattern to: %s", newPattern)
			}

		case "submit.footer":
			newValue, err := tui.PromptConfirm(fmt.Sprintf("Include PR footer in descriptions? (current: %v):", submitFooter), submitFooter)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			if newValue != submitFooter {
				if err := cfg.SetSubmitFooter(newValue); err != nil {
					out.Info("Failed to save config: %v", err)
					continue
				}
				out.Info("Set submit.footer to: %v", newValue)
			}

		case "submit.githubStack":
			newValue, err := tui.PromptConfirm(fmt.Sprintf("Sync native GitHub Stack metadata for eligible linear submits? (current: %v):", submitGitHubStack), submitGitHubStack)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			if newValue != submitGitHubStack {
				if err := cfg.SetSubmitGitHubStack(newValue); err != nil {
					out.Info("Failed to save config: %v", err)
					continue
				}
				out.Info("Set submit.githubStack to: %v", newValue)
			}

		case "merge.method":
			methodOptions := []tui.SelectOption{
				{Label: "squash (Squash and merge)", Value: "squash"},
				{Label: "merge (Create a merge commit)", Value: "merge"},
				{Label: "rebase (Rebase and merge)", Value: "rebase"},
			}
			newValue, err := tui.PromptSelect("Select merge method:", methodOptions, 0)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			currentMethod := cfg.MergeMethod()
			if newMethod := github.MergeMethod(newValue); newMethod != currentMethod {
				if err := cfg.SetMergeMethod(newMethod); err != nil {
					out.Info("Failed to set merge.method: %v", err)
					continue
				}
				out.Info("Set merge.method to: %s", newValue)
			}

		case "worktree.basePath":
			currentPath := cfg.WorktreeBasePath()
			newPath, err := tui.PromptTextInput(fmt.Sprintf("Enter worktree base path (current: %s):", worktreeBasePath), currentPath)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			if newPath != currentPath {
				if err := cfg.SetWorktreeBasePath(newPath); err != nil {
					out.Info("Failed to save config: %v", err)
					continue
				}
				out.Info("Set worktree.basePath to: %s", newPath)
			}

		case "worktree.autoClean":
			newValue, err := tui.PromptConfirm(fmt.Sprintf("Auto-clean worktrees during sync? (current: %v):", worktreeAutoClean), worktreeAutoClean)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			if newValue != worktreeAutoClean {
				if err := cfg.SetWorktreeAutoClean(newValue); err != nil {
					out.Info("Failed to save config: %v", err)
					continue
				}
				out.Info("Set worktree.autoClean to: %v", newValue)
			}

		case "split.hunkSelector":
			selectorOptions := []tui.SelectOption{
				{Label: "tui (Custom BubbleTea hunk selector)", Value: "tui"},
				{Label: "git (Use git add -p)", Value: "git"},
			}
			newValue, err := tui.PromptSelect("Select hunk selector for split --by-hunk:", selectorOptions, 0)
			if err != nil {
				if errors.Is(err, tui.ErrInteractiveDisabled) || strings.Contains(err.Error(), "canceled") {
					continue
				}
				return err
			}
			currentSelector := cfg.SplitHunkSelector()
			if newValue != currentSelector {
				if err := cfg.SetSplitHunkSelector(newValue); err != nil {
					out.Info("Failed to set split.hunkSelector: %v", err)
					continue
				}
				if err := cfg.Save(); err != nil {
					out.Info("Failed to save config: %v", err)
					continue
				}
				out.Info("Set split.hunkSelector to: %s", newValue)
			}
		}
	}

	return nil
}
