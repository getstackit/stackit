// Package integrations provides commands for managing various integrations
package integrations

import (
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"stackit.dev/stackit/internal/git"
)

//go:embed github/templates/*.yml
var githubTemplates embed.FS

// NewGithubCmd creates the github command
func NewGithubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "github",
		Short:        "Manage GitHub integration",
		Long:         `Manage GitHub integration for stackit, including CI checks.`,
		SilenceUsage: true,
	}

	cmd.AddCommand(newGithubInstallCmd())

	return cmd
}

// newGithubInstallCmd creates the github install command
func newGithubInstallCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install GitHub Action for stackit lock check",
		Long: `Install a GitHub Action workflow that prevents merging locked PRs.
		
This will create .github/workflows/stackit-lock-check.yml in your repository.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner()
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd)
			}
			return runGithubInstall(runner, force, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing files")

	return cmd
}

func runGithubInstall(runner git.Runner, force bool, out io.Writer) error {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0750); err != nil {
		return fmt.Errorf("failed to create .github/workflows directory: %w", err)
	}

	workflowPath := filepath.Join(workflowDir, "stackit-lock-check.yml")
	if _, err := os.Stat(workflowPath); err == nil && !force {
		return fmt.Errorf("file already exists: %s. Use --force to overwrite", workflowPath)
	}

	content, err := githubTemplates.ReadFile("github/templates/stackit-lock-check.yml")
	if err != nil {
		return fmt.Errorf("failed to read template: %w", err)
	}

	if err := os.WriteFile(workflowPath, content, 0600); err != nil {
		return fmt.Errorf("failed to write %s: %w", workflowPath, err)
	}

	_, _ = fmt.Fprintf(out, "✓ Installed GitHub Action workflow: %s\n", filepath.Join(".github", "workflows", "stackit-lock-check.yml"))

	return nil
}
