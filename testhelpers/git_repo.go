package testhelpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const textFileName = "test.txt"

// GitRepo represents a Git repository for testing purposes.
// This is the Go equivalent of the TypeScript GitRepo class.
type GitRepo struct {
	Dir            string
	UserConfigPath string
}

// NewGitRepo initializes a new Git repository in the specified directory using 'git init'.
func NewGitRepo(dir string) (*GitRepo, error) {
	return newGitRepoInternal(dir, &gitRepoOptions{})
}

// NewGitRepoFromTemplate clones a repository from a local template using 'git clone --local'.
func NewGitRepoFromTemplate(dir string, templatePath string) (*GitRepo, error) {
	return newGitRepoInternal(dir, &gitRepoOptions{templatePath: templatePath})
}

// NewGitRepoFromURL clones a repository from a remote URL.
func NewGitRepoFromURL(dir string, repoURL string) (*GitRepo, error) {
	return newGitRepoInternal(dir, &gitRepoOptions{repoURL: repoURL})
}

// gitRepoOptions holds options for creating a GitRepo.
type gitRepoOptions struct {
	existingRepo bool
	repoURL      string
	templatePath string
}

// newGitRepoInternal is the internal implementation for creating a GitRepo.
func newGitRepoInternal(dir string, options *gitRepoOptions) (*GitRepo, error) {
	repo := &GitRepo{
		Dir:            dir,
		UserConfigPath: filepath.Join(dir, ".git", ".stackit_user_config"),
	}

	if options.existingRepo {
		return repo, nil
	}

	switch {
	case options.repoURL != "":
		// Clone repository
		cmd := exec.Command("git", "clone", options.repoURL, dir)
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to clone repo: %w", err)
		}
	case options.templatePath != "":
		// Clone from template using --local for speed.
		// Use --no-hardlinks to avoid potential issues with concurrent access or cross-filesystem clones.
		cmd := exec.Command("git", "clone", "--local", "--no-hardlinks", options.templatePath, dir)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("failed to clone from template: %w\nOutput: %s", err, string(output))
		}

		// Remove the 'origin' remote that git clone automatically creates
		// as it points to the template directory and will interfere with tests
		// that want to set up their own remotes.
		_ = repo.runGitCommand("remote", "remove", "origin")
	default:
		// Initialize new repository with optimized config
		// Use git -c flags to avoid reading global config and set local configs
		cmd := exec.Command("git", "-c", "init.defaultBranch=main", "-c", "core.autocrlf=false", "-c", "core.fileMode=false", "init", dir, "-b", "main")
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("failed to init repo: %w", err)
		}
	}

	// Configure Git user (required for commits)
	if err := repo.runGitCommand("config", "user.name", "Test User"); err != nil {
		return nil, err
	}
	if err := repo.runGitCommand("config", "user.email", "test@example.com"); err != nil {
		return nil, err
	}

	return repo, nil
}

// runGitCommand executes a git command in the repository directory.
// Uses GIT_CONFIG_GLOBAL=/dev/null to avoid reading global config for faster operations.
func (r *GitRepo) runGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	// Set environment to avoid reading global git config for faster operations in tests
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if os.Getenv("DEBUG") == "" {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	return cmd.Run()
}

// RunGitCommand executes a git command and returns an error if it fails.
func (r *GitRepo) RunGitCommand(args ...string) error {
	return r.runGitCommand(args...)
}

// runGitCommandAndGetOutput executes a git command and returns its output.
// Uses GIT_CONFIG_GLOBAL=/dev/null to avoid reading global config for faster operations.
func (r *GitRepo) runGitCommandAndGetOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	// Set environment to avoid reading global git config for faster operations in tests
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git command failed: %w", err)
	}
	// Trim all trailing whitespace including newlines
	result := strings.TrimSpace(string(output))
	return result, nil
}

// RunGitCommandAndGetOutput executes a git command and returns its output.
func (r *GitRepo) RunGitCommandAndGetOutput(args ...string) (string, error) {
	return r.runGitCommandAndGetOutput(args...)
}

// RunCliCommand executes a Stackit CLI command in the repository directory.
func (r *GitRepo) RunCliCommand(command []string) error {
	cliPath := GetSharedBinaryPath()
	if cliPath == "" {
		return fmt.Errorf("failed to get shared binary path")
	}

	cmd := exec.Command(cliPath, command...)
	cmd.Dir = r.Dir

	env := os.Environ()
	env = append(env, "STACKIT_USER_CONFIG_PATH="+r.UserConfigPath)
	cmd.Env = env

	if os.Getenv("DEBUG") == "" {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("CLI command failed: %w", err)
	}

	return nil
}

// RunCliCommandAndGetOutput executes a Stackit CLI command and returns its output.
func (r *GitRepo) RunCliCommandAndGetOutput(command []string) (string, error) {
	cliPath := GetSharedBinaryPath()
	if cliPath == "" {
		return "", fmt.Errorf("failed to get shared binary path")
	}

	cmd := exec.Command(cliPath, command...)
	cmd.Dir = r.Dir

	env := os.Environ()
	env = append(env, "STACKIT_USER_CONFIG_PATH="+r.UserConfigPath)
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// CreateChange creates a file change in the repository.
func (r *GitRepo) CreateChange(textValue string, prefix string, unstaged bool) error {
	fileName := textFileName
	if prefix != "" {
		fileName = prefix + "_" + fileName
	}
	filePath := filepath.Join(r.Dir, fileName)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(textValue), 0600); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if !unstaged {
		return r.runGitCommand("add", filePath)
	}

	return nil
}

// CreateChangeAndCommit creates a file change and commits it.
func (r *GitRepo) CreateChangeAndCommit(textValue string, prefix string) error {
	if err := r.CreateChange(textValue, prefix, false); err != nil {
		return err
	}
	if err := r.runGitCommand("add", "."); err != nil {
		return err
	}
	return r.runGitCommand("commit", "-m", textValue)
}

// CreateChangeAndAmend creates a file change and amends the last commit.
func (r *GitRepo) CreateChangeAndAmend(textValue string, prefix string) error {
	if err := r.CreateChange(textValue, prefix, false); err != nil {
		return err
	}
	if err := r.runGitCommand("add", "."); err != nil {
		return err
	}
	return r.runGitCommand("commit", "--amend", "--no-edit")
}

// DeleteBranch deletes a branch.
func (r *GitRepo) DeleteBranch(name string) error {
	return r.runGitCommand("branch", "-D", name)
}

// CreatePrecommitHook creates a pre-commit hook.
func (r *GitRepo) CreatePrecommitHook(contents string) error {
	hookDir := filepath.Join(r.Dir, ".git", "hooks")
	if err := os.MkdirAll(hookDir, 0700); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hookPath := filepath.Join(hookDir, "pre-commit")
	if err := os.WriteFile(hookPath, []byte(contents), 0600); err != nil {
		return fmt.Errorf("failed to write hook: %w", err)
	}
	// nolint:gosec // Hook must be executable
	if err := os.Chmod(hookPath, 0700); err != nil {
		return fmt.Errorf("failed to make hook executable: %w", err)
	}

	return nil
}

// CreateBranch creates a new branch without checking it out.
func (r *GitRepo) CreateBranch(name string) error {
	return r.runGitCommand("branch", name)
}

// CreateAndCheckoutBranch creates and checks out a new branch.
func (r *GitRepo) CreateAndCheckoutBranch(name string) error {
	return r.runGitCommand("checkout", "-b", name)
}

// CheckoutBranch checks out a branch.
func (r *GitRepo) CheckoutBranch(name string) error {
	return r.runGitCommand("checkout", name)
}

// RebaseInProgress checks if a rebase is in progress.
func (r *GitRepo) RebaseInProgress() bool {
	rebasePath := filepath.Join(r.Dir, ".git", "rebase-merge")
	_, err := os.Stat(rebasePath)
	return err == nil
}

// ResolveMergeConflicts resolves merge conflicts by accepting theirs.
func (r *GitRepo) ResolveMergeConflicts() error {
	return r.runGitCommand("checkout", "--theirs", ".")
}

// MarkMergeConflictsAsResolved marks merge conflicts as resolved.
func (r *GitRepo) MarkMergeConflictsAsResolved() error {
	return r.runGitCommand("add", ".")
}

// CurrentBranchName returns the name of the current branch.
func (r *GitRepo) CurrentBranchName() (string, error) {
	output, err := r.runGitCommandAndGetOutput("branch", "--show-current")
	if err != nil {
		return "", err
	}
	// The output from runGitCommandAndGetOutput is already trimmed, but ensure it's clean
	return strings.TrimSpace(output), nil
}

// GetRef returns the SHA of a ref.
func (r *GitRepo) GetRef(refName string) (string, error) {
	return r.runGitCommandAndGetOutput("show-ref", "-s", refName)
}

// ListCurrentBranchCommitMessages returns the commit messages on the current branch.
func (r *GitRepo) ListCurrentBranchCommitMessages() ([]string, error) {
	output, err := r.runGitCommandAndGetOutput("log", "--oneline", "--format=%B")
	if err != nil {
		return nil, err
	}

	lines := []string{}
	for _, line := range splitLines(output) {
		if len(line) > 0 {
			lines = append(lines, line)
		}
	}

	return lines, nil
}

// MergeBranch merges a branch into another.
func (r *GitRepo) MergeBranch(branch, mergeIn string) error {
	if err := r.CheckoutBranch(branch); err != nil {
		return err
	}
	return r.runGitCommand("merge", mergeIn)
}

// TrackBranch tracks a branch using the CLI.
func (r *GitRepo) TrackBranch(branch string, parentBranch string) error {
	args := []string{"branch", "track"}
	if parentBranch != "" {
		args = append(args, "--parent", parentBranch)
	}
	args = append(args, branch)
	return r.RunCliCommand(args)
}

// splitLines splits a string by newlines and returns non-empty lines.
func splitLines(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// CreateBareRemote creates a bare git repository to act as a remote.
// Returns the path to the bare repository.
func (r *GitRepo) CreateBareRemote(name string) (string, error) {
	// Create bare repo as a sibling directory with a unique name based on the repo dir
	// This ensures each test gets its own unique remote
	bareDir := r.Dir + "-" + name + ".git"

	cmd := exec.Command("git", "init", "--bare", bareDir)
	// Set environment to avoid reading global git config for faster operations in tests
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create bare repo: %w", err)
	}

	// Add as remote
	if err := r.runGitCommand("remote", "add", name, bareDir); err != nil {
		return "", fmt.Errorf("failed to add remote: %w", err)
	}

	return bareDir, nil
}

// PushBranch pushes a branch to a remote.
func (r *GitRepo) PushBranch(remote, branch string) error {
	cmd := exec.Command("git", "push", "-u", remote, branch)
	cmd.Dir = r.Dir
	// Set environment to avoid reading global git config for faster operations in tests
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("push failed: %w, output: %s", err, string(output))
	}
	return nil
}

// ForcePushBranch force pushes a branch to a remote.
func (r *GitRepo) ForcePushBranch(remote, branch string) error {
	return r.runGitCommand("push", "-f", remote, branch)
}

// GetRevision returns the SHA of a revision (branch, tag, or commit reference).
func (r *GitRepo) GetRevision(rev string) (string, error) {
	return r.runGitCommandAndGetOutput("rev-parse", rev)
}

// GetCommitCount returns the number of commits between two refs.
func (r *GitRepo) GetCommitCount(from, to string) (int, error) {
	output, err := r.runGitCommandAndGetOutput("rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, err
	}
	var count int
	_, err = fmt.Sscanf(output, "%d", &count)
	if err != nil {
		return 0, fmt.Errorf("failed to parse commit count: %w", err)
	}
	return count, nil
}

// GetBranchSHA returns the SHA of a branch.
func (r *GitRepo) GetBranchSHA(branch string) (string, error) {
	return r.GetRevision(branch)
}

// GetCurrentSHA returns the SHA of HEAD.
func (r *GitRepo) GetCurrentSHA() (string, error) {
	return r.GetRevision("HEAD")
}

// CheckoutDetached checks out a revision in detached HEAD state.
func (r *GitRepo) CheckoutDetached(rev string) error {
	return r.runGitCommand("checkout", "--detach", rev)
}

// HasUnstagedChanges checks if there are unstaged changes to tracked files.
func (r *GitRepo) HasUnstagedChanges() (bool, error) {
	output, err := r.runGitCommandAndGetOutput("diff", "--name-only")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// HasUntrackedFiles checks if there are untracked files.
func (r *GitRepo) HasUntrackedFiles() (bool, error) {
	output, err := r.runGitCommandAndGetOutput("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(output) != "", nil
}

// GetLocalBranches returns a list of all local branches.
func (r *GitRepo) GetLocalBranches() ([]string, error) {
	output, err := r.runGitCommandAndGetOutput("branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return splitLines(output), nil
}

// IsAncestor checks if the first ref is an ancestor of the second ref.
func (r *GitRepo) IsAncestor(ancestor, descendant string) (bool, error) {
	err := r.runGitCommand("merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	return false, nil
}
