// Package integrations provides commands for managing various integrations
package integrations

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	stackiterrors "github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/git"
)

// NewAgentsCmd creates the agent command
func NewAgentsCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent integration files for Claude Code and Codex",
		Long: `Manage agent integration files that help AI assistants use stackit effectively.

This command generates configuration files that enable AI agents (like Claude Code and Codex)
to understand how to use stackit commands for managing stacked branches.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newAgentInstallCmd(version))

	return cmd
}

// newAgentInstallCmd creates the agent install command
func newAgentInstallCmd(version string) *cobra.Command {
	var force bool
	var formats []string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install agent integration files",
		Long: `Install agent integration files for AI assistants.

By default, this command installs files in your home directory and prompts
you to select one or more skill folder formats.

This will create one or both:
  - ~/.claude/skills/stackit/ (Claude Code skill format)
  - ~/.codex/skills/stackit/ (Codex skill format)

These files contain instructions for AI agents on how to use stackit commands
to manage stacked branches, create commits, submit PRs, and more.

When run in a git repository, you will be prompted to add a stacking workflow
block to your project's CLAUDE.md or AGENTS.md file.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			return runAgentInstall(runner, force, formats, version, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing files")
	cmd.Flags().StringSliceVar(&formats, "format", nil, "Skill format(s) to install (claude,codex). Repeat flag or use comma-separated values")

	return cmd
}

func runAgentInstall(runner git.Runner, force bool, formats []string, version string, out io.Writer) error {
	repoRoot, _ := runner.DiscoverRepoRoot()

	baseDir, err := resolveInstallBaseDir()
	if err != nil {
		return err
	}

	targets, err := selectInstallTargets(baseDir, formats)
	if err != nil {
		if errors.Is(err, stackiterrors.ErrCanceled) {
			return nil
		}
		return err
	}

	if err := confirmOverwriteIfNeeded(baseDir, targets, force, version, out); err != nil {
		if errors.Is(err, stackiterrors.ErrCanceled) {
			return nil
		}
		return err
	}

	for _, target := range targets {
		groups := buildAgentFileGroups(target)
		for _, g := range groups {
			if err := installFileGroup(baseDir, g, version); err != nil {
				return err
			}
		}

		switch target.format {
		case agentSkillFormatClaude:
			cleanupOldCommandFiles(baseDir, commandTemplateFiles)
			if err := installCommandSkills(baseDir, target.skillsBaseDir, commandTemplateFiles, renderClaudeSkillContent, target.format); err != nil {
				return err
			}
		case agentSkillFormatCodex:
			if err := installCommandSkills(baseDir, target.skillsBaseDir, commandTemplateFiles, renderCodexSkillContent, target.format); err != nil {
				return err
			}
		}
	}

	// Install workflow block to CLAUDE.md or AGENTS.md if in a git repo
	var workflowBlockInstalled bool
	var workflowBlockPath string
	if repoRoot != "" {
		workflowBlockInstalled, workflowBlockPath, err = promptAndInstallWorkflowBlock(repoRoot, force)
		if err != nil {
			return err
		}
	}

	printSuccessMessage(out, targets, workflowBlockInstalled, workflowBlockPath, len(commandTemplateFiles))
	return nil
}

func printSuccessMessage(out io.Writer, targets []agentInstallTarget, workflowBlockInstalled bool, workflowBlockPath string, commandCount int) {
	_, _ = fmt.Fprintln(out, "✓ Installed agent files")

	var hasClaude, hasCodex bool
	for _, target := range targets {
		switch target.format {
		case agentSkillFormatClaude:
			hasClaude = true
		case agentSkillFormatCodex:
			hasCodex = true
		}
		_, _ = fmt.Fprintf(out, "✓ Created %s\n", target.displayPath)
		_, _ = fmt.Fprintf(out, "✓ Created ~/%s/stack-*/ (%d skills)\n", target.skillsBaseDir, commandCount)
	}

	if workflowBlockInstalled {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Stacking workflow documentation:")
		_, _ = fmt.Fprintf(out, "✓ Added stacking workflow block to %s\n", workflowBlockPath)
	}

	if hasClaude {
		printStackSkillList(out, "Available Claude Code commands:", "/")
	}
	if hasCodex {
		printStackSkillList(out, "Available Codex skills:", "")
	}
}

func printStackSkillList(out io.Writer, title, prefix string) {
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, title)
	for _, skill := range stackSkillSummaries {
		_, _ = fmt.Fprintf(out, "  %s%-14s - %s\n", prefix, skill.name, skill.description)
	}
}

var stackSkillSummaries = []struct {
	name        string
	description string
}{
	{"stack-absorb", "Intelligently absorb changes into commits"},
	{"stack-create", "Create branch with auto-naming"},
	{"stack-describe", "Generate PR descriptions from git history"},
	{"stack-extract", "Extract commits/files to independent branch"},
	{"stack-fix", "Diagnose and fix stack issues"},
	{"stack-fold", "Fold granular branches into parents"},
	{"stack-modify", "Amend current branch commit"},
	{"stack-plan", "Plan and create stack from uncommitted changes"},
	{"stack-resolve", "Resolve rebase conflicts with AI assistance"},
	{"stack-restack", "Rebase branches with scoped or multi-stack targeting"},
	{"stack-review", "Apply PR review comments and mark resolved"},
	{"stack-split", "Split changes between current and new child branch"},
	{"stack-status", "View stack state and health"},
	{"stack-submit", "Submit PRs with generated descriptions"},
	{"stack-sync", "Sync with trunk and cleanup"},
	{"stack-tidy", "Clean up fixup/WIP commits across the stack"},
	{"stack-verify", "Verify stack health by running checks"},
}
