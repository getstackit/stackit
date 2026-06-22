package integrations

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/getstackit/stackit/internal/actions/integrations"
	"github.com/getstackit/stackit/internal/git"
)

// cmdInstall is the subcommand name shared across all integrations.
const cmdInstall = "install"

// InstallGitHub installs GitHub Actions workflow for stackit CI checks.
// This is a convenience wrapper for use during init.
// When called from init, skipIfExists is true to avoid errors if workflows already exist.
func InstallGitHub(runner git.Runner, force bool, out io.Writer) error {
	// When called from init, we use skipIfExists to be friendlier
	// since the user can't pass --force through the init prompts
	return runGithubInstallWithOptions(runner, githubInstallOptions{
		Force:        force,
		SkipIfExists: !force, // If not forcing, skip existing files gracefully
	}, out)
}

// InstallPrecommit installs the pre-commit hook.
// This is a convenience wrapper for use during init.
func InstallPrecommit(runner git.Runner, out io.Writer) error {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return err
	}
	return integrations.PrecommitInstallActionWithOutput(repoRoot, out)
}

// InstallPrepush installs the pre-push hook.
// This is a convenience wrapper for use during init.
func InstallPrepush(runner git.Runner, out io.Writer) error {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return err
	}
	return integrations.PrepushInstallActionWithOutput(repoRoot, out)
}

// InstallAgents installs AI agent integration files.
// This is a convenience wrapper for use during init.
// It auto-detects formats from existing directories to avoid a nested prompt.
func InstallAgents(runner git.Runner, force bool, version string, out io.Writer) error {
	formats := autoDetectFormats()
	return runAgentInstall(runner, force, formats, version, out)
}

// autoDetectFormats returns format flags based on which agent directories exist,
// defaulting to both first-class formats if none are found.
func autoDetectFormats() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return []string{string(agentSkillFormatClaude), string(agentSkillFormatCodex)}
	}

	var formats []string
	if dirExists(filepath.Join(homeDir, ".claude")) {
		formats = append(formats, string(agentSkillFormatClaude))
	}
	if dirExists(filepath.Join(homeDir, ".codex")) {
		formats = append(formats, string(agentSkillFormatCodex))
	}
	if len(formats) == 0 {
		formats = []string{string(agentSkillFormatClaude), string(agentSkillFormatCodex)}
	}
	return formats
}

// stackitWorkflowMarker is a string that identifies a stackit-generated GitHub workflow.
// We check for this to distinguish stackit workflows from manually created ones.
const stackitWorkflowMarker = "refs/stackit/metadata"

// IsGitHubInstalled checks if GitHub Actions workflow is already installed.
// Returns true only if the workflow exists AND contains stackit-specific content,
// to distinguish from manually created workflows with the same name.
func IsGitHubInstalled(runner git.Runner) bool {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return false
	}

	for _, workflow := range githubWorkflows {
		workflowPath := filepath.Join(repoRoot, ".github", "workflows", workflow)
		content, err := os.ReadFile(workflowPath)
		if err != nil {
			return false
		}
		// Check that it's actually a stackit-generated workflow
		if !strings.Contains(string(content), stackitWorkflowMarker) {
			return false
		}
	}
	return true
}

// IsPrecommitInstalled checks if the pre-commit hook is already installed.
func IsPrecommitInstalled(runner git.Runner) bool {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return false
	}

	hookPath := filepath.Join(repoRoot, ".git", "hooks", "pre-commit")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), "stackit precommit verify")
}

// IsPrepushInstalled checks if the pre-push hook is already installed.
func IsPrepushInstalled(runner git.Runner) bool {
	repoRoot, err := runner.DiscoverRepoRoot()
	if err != nil {
		return false
	}

	hookPath := filepath.Join(repoRoot, ".git", "hooks", "pre-push")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), "stackit prepush verify")
}

// IsAgentsInstalled checks if agent integration files are already installed.
// Checks both global (~/.claude/, ~/.codex/) and local (.claude/, .codex/) installations.
func IsAgentsInstalled(runner git.Runner) bool {
	// Check global installation
	homeDir, err := os.UserHomeDir()
	if err == nil && isAnySkillInstalled(homeDir) {
		return true
	}

	// Check local installation
	repoRoot, err := runner.DiscoverRepoRoot()
	if err == nil && isAnySkillInstalled(repoRoot) {
		return true
	}

	return false
}
