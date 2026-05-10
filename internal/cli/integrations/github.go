// Package integrations provides commands for managing various integrations
package integrations

import (
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/getstackit/stackit/internal/git"
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
		Short: "Install GitHub Action workflows for stackit",
		Long: `Install GitHub Action workflow for stackit CI checks.

This will create .github/workflows/stackit.yml which includes:
  - Lock check: Prevents merging locked PRs
  - Stack order check: Prevents merging PRs that are not at the bottom of the stack`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := cmd.Flags().GetString("cwd")
			runner := git.NewRunner(nil)
			if cwd != "" {
				runner = git.NewRunnerWithPath(cwd, nil)
			}
			return runGithubInstall(runner, force, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Force overwrite existing files")

	return cmd
}

var githubWorkflows = []string{
	"stackit.yml",
}

// githubInstallOptions configures the behavior of GitHub workflow installation.
type githubInstallOptions struct {
	// Force overwrites existing files if true
	Force bool
	// SkipIfExists silently skips if files already exist (used during init)
	SkipIfExists bool
}

func runGithubInstall(runner git.Runner, force bool, out io.Writer) error {
	return runGithubInstallWithOptions(runner, githubInstallOptions{Force: force}, out)
}

func runGithubInstallWithOptions(runner git.Runner, opts githubInstallOptions, out io.Writer) error {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	workflowDir := filepath.Join(repoRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0750); err != nil {
		return fmt.Errorf("failed to create .github/workflows directory: %w", err)
	}

	for _, workflow := range githubWorkflows {
		workflowPath := filepath.Join(workflowDir, workflow)

		// Check if file already exists
		if _, err := os.Stat(workflowPath); err == nil {
			if !opts.Force {
				if opts.SkipIfExists {
					_, _ = fmt.Fprintf(out, "GitHub Action workflow already exists: %s (skipped)\n", filepath.Join(".github", "workflows", workflow))
					continue
				}
				return fmt.Errorf("file already exists: %s. Use --force to overwrite", workflowPath)
			}
			// Force is true, continue to overwrite
		}

		content, err := githubTemplates.ReadFile("github/templates/" + workflow)
		if err != nil {
			return fmt.Errorf("failed to read template: %w", err)
		}

		if err := os.WriteFile(workflowPath, content, 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", workflowPath, err)
		}

		_, _ = fmt.Fprintf(out, "✓ Installed GitHub Action workflow: %s\n", filepath.Join(".github", "workflows", workflow))
	}

	return nil
}
