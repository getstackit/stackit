// Package integrations provides commands for managing various integrations
package integrations

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/git"
)

// NewAgentsCmd creates the agent command
func NewAgentsCmd(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage agent integration files for Cursor and Claude Code",
		Long: `Manage agent integration files that help AI assistants use stackit effectively.

This command generates configuration files that enable AI agents (like Cursor and Claude Code)
to understand how to use stackit commands for managing stacked branches.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newAgentInstallCmd(version))

	return cmd
}

// newAgentInstallCmd creates the agent install command
func newAgentInstallCmd(version string) *cobra.Command {
	var local bool
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install agent integration files",
		Long: `Install agent integration files for AI assistants.

By default, files are installed globally in ~/.claude/ and work across all repositories.
Use --local to install files in the current repository instead.

This will create:
  - .cursor/rules/stackit.md (for Cursor)
  - .claude/skills/stackit/ (Claude Code skill)
  - .claude/commands/ (Claude Code slash commands)

These files contain instructions for AI agents on how to use stackit commands
to manage stacked branches, create commits, submit PRs, and more.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner()
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd)
			}
			return runAgentInstall(runner, local, force, version, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&local, "local", false, "Install files in current repository instead of globally")
	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing files")

	return cmd
}

func runAgentInstall(runner git.Runner, local, force bool, version string, out io.Writer) error {
	var baseDir string
	var err error

	if local {
		// Local installation - install in current repo
		repoRoot, err := runner.DiscoverRepoRoot()
		if err != nil {
			return fmt.Errorf("not a git repository: %w", err)
		}
		baseDir = repoRoot
	} else {
		// Global installation - install in home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = homeDir
	}

	// Check for existing installation and version
	if !force {
		if err := checkExistingInstallation(baseDir, version, out); err != nil {
			return err
		}
	}

	// Create .claude/skills/stackit directory
	skillDir := filepath.Join(baseDir, ".claude", "skills", "stackit")
	if err := os.MkdirAll(skillDir, 0750); err != nil {
		return fmt.Errorf("failed to create .claude/skills/stackit directory: %w", err)
	}

	// Write skill files
	skillFiles := map[string]string{
		"SKILL.md":     "agents/templates/skill/SKILL.md",
		"reference.md": "agents/templates/skill/reference.md",
	}

	for filename, templatePath := range skillFiles {
		content, err := agentTemplates.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}

		// Replace version placeholder with actual version
		contentStr := string(content)
		contentStr = strings.ReplaceAll(contentStr, "{{VERSION}}", version)

		filePath := filepath.Join(skillDir, filename)
		if err := os.WriteFile(filePath, []byte(contentStr), 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Create subdirectories for commands, workflows, and scripts
	commandRefDir := filepath.Join(skillDir, "commands")
	if err := os.MkdirAll(commandRefDir, 0750); err != nil {
		return fmt.Errorf("failed to create commands directory: %w", err)
	}

	workflowsDir := filepath.Join(skillDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0750); err != nil {
		return fmt.Errorf("failed to create workflows directory: %w", err)
	}

	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		return fmt.Errorf("failed to create scripts directory: %w", err)
	}

	// Write command reference files
	commandRefFiles := []string{
		"navigation.md",
		"branch.md",
		"stack.md",
		"recovery.md",
	}

	for _, filename := range commandRefFiles {
		templatePath := "agents/templates/skill/commands/" + filename
		content, err := agentTemplates.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}

		filePath := filepath.Join(commandRefDir, filename)
		if err := os.WriteFile(filePath, content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Write workflow files
	workflowFiles := []string{
		"absorb-conflict.md",
		"conflict-resolution.md",
		"fix-absorb.md",
		"stack-fold.md",
	}

	for _, filename := range workflowFiles {
		templatePath := "agents/templates/skill/workflows/" + filename
		content, err := agentTemplates.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}

		filePath := filepath.Join(workflowsDir, filename)
		if err := os.WriteFile(filePath, content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Write script files
	scriptFiles := []string{
		"analyze_stack.sh",
		"validate_pr.sh",
	}

	for _, filename := range scriptFiles {
		templatePath := "agents/templates/skill/scripts/" + filename
		content, err := agentTemplates.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}

		filePath := filepath.Join(scriptsDir, filename)
		if err := os.WriteFile(filePath, content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}

		// Make scripts executable (0700 = owner can read, write, execute)
		// #nosec G302 - Scripts need to be executable
		if err := os.Chmod(filePath, 0700); err != nil {
			return fmt.Errorf("failed to make %s executable: %w", filename, err)
		}
	}

	// Create .claude/commands directory
	commandsDir := filepath.Join(baseDir, ".claude", "commands")
	if err := os.MkdirAll(commandsDir, 0750); err != nil {
		return fmt.Errorf("failed to create .claude/commands directory: %w", err)
	}

	// Write slash command files
	commands := []string{
		"stack-absorb.md",
		"stack-create.md",
		"stack-fix.md",
		"stack-fold.md",
		"stack-restack.md",
		"stack-status.md",
		"stack-submit.md",
		"stack-sync.md",
	}

	for _, filename := range commands {
		templatePath := "agents/templates/commands/" + filename
		content, err := agentTemplates.ReadFile(templatePath)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", templatePath, err)
		}

		cmdPath := filepath.Join(commandsDir, filename)
		if err := os.WriteFile(cmdPath, content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	// Create .cursor/rules directory if it doesn't exist
	cursorRulesDir := filepath.Join(baseDir, ".cursor", "rules")
	if err := os.MkdirAll(cursorRulesDir, 0750); err != nil {
		return fmt.Errorf("failed to create .cursor/rules directory: %w", err)
	}

	// Write Cursor rules file
	cursorContent, err := agentTemplates.ReadFile("agents/templates/cursor/stackit.md")
	if err != nil {
		return fmt.Errorf("failed to read Cursor template: %w", err)
	}

	cursorRulesPath := filepath.Join(cursorRulesDir, "stackit.md")
	if err := os.WriteFile(cursorRulesPath, cursorContent, 0600); err != nil {
		return fmt.Errorf("failed to write Cursor rules file: %w", err)
	}

	// Print success message
	installType := "globally"
	if local {
		installType = "locally"
	}

	_, _ = fmt.Fprintf(out, "✓ Installed agent files %s (version %s)\n\n", installType, version)
	_, _ = fmt.Fprintln(out, "Claude Code integration:")
	_, _ = fmt.Fprintf(out, "✓ Created %s/.claude/skills/stackit/SKILL.md\n", getDisplayPath(baseDir, local))
	_, _ = fmt.Fprintf(out, "✓ Created %s/.claude/skills/stackit/reference.md\n", getDisplayPath(baseDir, local))
	_, _ = fmt.Fprintf(out, "✓ Created %s/.claude/skills/stackit/commands/ (4 reference files)\n", getDisplayPath(baseDir, local))
	_, _ = fmt.Fprintf(out, "✓ Created %s/.claude/skills/stackit/workflows/ (4 workflow guides)\n", getDisplayPath(baseDir, local))
	_, _ = fmt.Fprintf(out, "✓ Created %s/.claude/skills/stackit/scripts/ (2 utility scripts)\n", getDisplayPath(baseDir, local))
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Slash commands:")
	_, _ = fmt.Fprintf(out, "✓ Created %s/.claude/commands/stack-*.md (8 commands)\n", getDisplayPath(baseDir, local))
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Cursor integration:")
	_, _ = fmt.Fprintf(out, "✓ Created %s/.cursor/rules/stackit.md\n", getDisplayPath(baseDir, local))

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Available Claude Code commands:")
	_, _ = fmt.Fprintln(out, "  /stack-absorb  - Intelligently absorb changes into commits")
	_, _ = fmt.Fprintln(out, "  /stack-create  - Create branch with auto-naming")
	_, _ = fmt.Fprintln(out, "  /stack-fix     - Diagnose and fix stack issues")
	_, _ = fmt.Fprintln(out, "  /stack-fold    - Fold granular branches into parents")
	_, _ = fmt.Fprintln(out, "  /stack-restack - Rebase all branches in stack")
	_, _ = fmt.Fprintln(out, "  /stack-status  - View stack state and health")
	_, _ = fmt.Fprintln(out, "  /stack-submit  - Submit PRs with generated descriptions")
	_, _ = fmt.Fprintln(out, "  /stack-sync    - Sync with trunk and cleanup")

	if !local {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "Tip: Use 'stackit agent install --local' to install files in a specific repository")
	}

	return nil
}

func checkExistingInstallation(baseDir, version string, out io.Writer) error {
	// Check if SKILL.md exists and has version info
	skillPath := filepath.Join(baseDir, ".claude", "skills", "stackit", "SKILL.md")
	if content, err := os.ReadFile(skillPath); err == nil {
		// File exists, check version
		existingVersion := extractVersion(string(content))
		if existingVersion != "" && existingVersion != version {
			_, _ = fmt.Fprintf(out, "Found existing installation (version %s)\n", existingVersion)
			_, _ = fmt.Fprintf(out, "New version available: %s\n", version)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Run with --force to update")
			return fmt.Errorf("existing installation found")
		}
	}

	return nil
}

func extractVersion(content string) string {
	// Look for version in frontmatter
	lines := strings.Split(content, "\n")
	inFrontmatter := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			}
			break
		}

		if inFrontmatter && strings.HasPrefix(line, "version:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}

	return ""
}

func getDisplayPath(_ string, local bool) string {
	if local {
		return "."
	}
	return "~"
}
