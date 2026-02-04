// Package git provides a wrapper around git commands and go-git for repository operations.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"stackit.dev/stackit/internal/utils"
)

// DebugLogger is an interface for logging debug messages
type DebugLogger interface {
	Debug(msg string, args ...any)
}

var (
	// goGitMu synchronizes go-git operations that access packfiles to prevent
	// "concurrent map iteration and map write" panics
	goGitMu sync.Mutex
)

// DefaultCommandTimeout is the default timeout for git commands
const DefaultCommandTimeout = 5 * time.Minute

// ErrStaleRemoteInfo indicates that a push failed because the remote has changed
var ErrStaleRemoteInfo = errors.New("stale info")

func (r *runner) UpdateRefWithLog(ctx context.Context, refName, sha, message string) error {
	_, err := r.RunGitCommandWithContext(ctx, "update-ref", "-m", message, refName, sha)
	return err
}

func (r *runner) VerifyRef(ctx context.Context, refName string) error {
	_, err := r.RunGitCommandWithContext(ctx, "rev-parse", "--verify", refName)
	return err
}

// RefUpdate represents a single reference update operation.
type RefUpdate struct {
	RefName  string
	NewSHA   string
	OldSHA   string // Optional: for optimistic locking verification
	IsDelete bool   // If true, this is a deletion instead of an update
}

// UpdateRefsBatch performs atomic updates of multiple references using git update-ref --stdin.
// All updates succeed or all fail. Supports both updates and deletions via the IsDelete flag.
func (r *runner) UpdateRefsBatch(ctx context.Context, updates []RefUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	var stdin strings.Builder
	for _, update := range updates {
		switch {
		case update.IsDelete && update.OldSHA != "":
			fmt.Fprintf(&stdin, "delete %s %s\n", update.RefName, update.OldSHA)
		case update.IsDelete:
			fmt.Fprintf(&stdin, "delete %s\n", update.RefName)
		case update.OldSHA != "":
			fmt.Fprintf(&stdin, "update %s %s %s\n", update.RefName, update.NewSHA, update.OldSHA)
		default:
			fmt.Fprintf(&stdin, "update %s %s\n", update.RefName, update.NewSHA)
		}
	}

	_, err := r.runGitInternal(ctx, stdin.String(), nil, true, "update-ref", "--stdin")
	if err != nil {
		return fmt.Errorf("atomic ref update failed: %w", err)
	}
	return nil
}

// UpdateRefsBatchWithLog performs atomic updates with a reflog message.
// Supports both updates and deletions via the IsDelete flag.
func (r *runner) UpdateRefsBatchWithLog(ctx context.Context, updates []RefUpdate, reflogMessage string) error {
	if len(updates) == 0 {
		return nil
	}

	var stdin strings.Builder
	for _, update := range updates {
		switch {
		case update.IsDelete && update.OldSHA != "":
			fmt.Fprintf(&stdin, "delete %s %s\n", update.RefName, update.OldSHA)
		case update.IsDelete:
			fmt.Fprintf(&stdin, "delete %s\n", update.RefName)
		case update.OldSHA != "":
			fmt.Fprintf(&stdin, "update %s %s %s\n", update.RefName, update.NewSHA, update.OldSHA)
		default:
			fmt.Fprintf(&stdin, "update %s %s\n", update.RefName, update.NewSHA)
		}
	}

	_, err := r.runGitInternal(ctx, stdin.String(), nil, true, "update-ref", "--stdin", "-m", reflogMessage)
	if err != nil {
		return fmt.Errorf("atomic ref update failed: %w", err)
	}
	return nil
}

// DeleteRefsBatch atomically deletes multiple references.
func (r *runner) DeleteRefsBatch(ctx context.Context, refNames []string) error {
	if len(refNames) == 0 {
		return nil
	}

	var stdin strings.Builder
	for _, refName := range refNames {
		stdin.WriteString(fmt.Sprintf("delete %s\n", refName))
	}

	_, err := r.runGitInternal(ctx, stdin.String(), nil, true, "update-ref", "--stdin")
	if err != nil {
		return fmt.Errorf("atomic ref delete failed: %w", err)
	}
	return nil
}

func (r *runner) RunGitCommandWithEnv(ctx context.Context, env []string, args ...string) (string, error) {
	return r.runGitInternal(ctx, "", env, true, args...)
}

func (r *runner) RunGitCommandWithContext(ctx context.Context, args ...string) (string, error) {
	return r.runGitInternal(ctx, "", nil, true, args...)
}

func (r *runner) RunGitCommandRawWithContext(ctx context.Context, args ...string) (string, error) {
	return r.runGitInternal(ctx, "", nil, false, args...)
}

func (r *runner) runGitCommandWithInputInternal(input string, args ...string) (string, error) {
	return r.runGitInternal(context.Background(), input, nil, true, args...)
}

func (r *runner) runGitInternal(ctx context.Context, input string, env []string, trim bool, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCommandTimeout)
		defer cancel()
	}

	r.debugLog("git %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "git", args...)
	if r.repoRoot != "" {
		cmd.Dir = r.repoRoot
	}

	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}

	// Prevent git from prompting via TTY (important when TUI has terminal in raw mode)
	// GIT_TERMINAL_PROMPT=0 prevents git from trying to read credentials or confirmations
	// from the terminal, which could cause hangs when a TUI is active.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", NewCommandError("git", args, stdout.String(), stderr.String(), err)
	}

	result := stdout.String()
	if trim {
		result = strings.TrimSpace(result)
	}
	return result, nil
}

// runGitStreaming runs a git command and streams output to the terminal (if interactive)
// while also capturing it for error handling. This is useful for commands like commit
// where hook output should be visible to the user.
func (r *runner) runGitStreaming(ctx context.Context, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCommandTimeout)
		defer cancel()
	}

	r.debugLog("git %s (streaming)", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "git", args...)
	if r.repoRoot != "" {
		cmd.Dir = r.repoRoot
	}

	var stdoutBuf, stderrBuf bytes.Buffer

	if utils.IsInteractive() {
		// Stream to terminal AND capture for error handling
		cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	} else {
		// Non-interactive: just capture (same as runGitInternal)
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
	}

	if err := cmd.Run(); err != nil {
		return "", NewCommandError("git", args, stdoutBuf.String(), stderrBuf.String(), err)
	}

	return strings.TrimSpace(stdoutBuf.String()), nil
}

func (r *runner) RunGHCommandWithContext(ctx context.Context, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// If no timeout/deadline is set in the context, add the default one
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultCommandTimeout)
		defer cancel()
	}

	r.debugLog("gh %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "gh", args...)
	// Use repoRoot for gh commands to ensure they are scoped to the correct repo
	if r.repoRoot != "" {
		cmd.Dir = r.repoRoot
	} else {
		wd, _ := os.Getwd()
		cmd.Dir = wd
	}

	// Prevent gh from prompting via TTY (important when TUI has terminal in raw mode)
	// GH_PROMPT_DISABLED=1 prevents gh from trying to prompt for authentication
	// or confirmations, which could cause hangs when a TUI is active.
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", NewCommandError("gh", args, stdout.String(), stderr.String(), ctx.Err())
		}
		return "", NewCommandError("gh", args, stdout.String(), stderr.String(), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *runner) RunGitCommandInteractive(args ...string) error {
	if !utils.IsInteractive() {
		return fmt.Errorf("interactive git command '%v' not allowed in non-interactive mode", args)
	}

	r.debugLog("git %s (interactive)", strings.Join(args, " "))

	cmd := exec.Command("git", args...)
	if r.repoRoot != "" {
		cmd.Dir = r.repoRoot
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// StagingOptions defines which changes to stage
type StagingOptions struct {
	All    bool
	Update bool
	Patch  bool
}

// MergeOptions contains options for merging branches
type MergeOptions struct {
	FFOnly  bool
	NoEdit  bool
	NoFF    bool
	Message string
}

// Runner defines the interface for git operations used by the engine.
// This allows the engine to be used with both real git and mock implementations.
//
// Runner is a composite interface that embeds smaller, focused interfaces for better
// modularity and testability. Each embedded interface represents a logical grouping
// of related git operations.
type Runner interface {
	// Repository access and configuration
	RepositoryReader
	RepositoryWriter

	// Remote operations
	RemoteOperations

	// Branch operations
	BranchReader
	BranchWriter

	// Commit and revision access
	CommitReader

	// Diff and comparison
	DiffOperations

	// Staging area
	StagingOperations

	// Commit creation
	CommitWriter

	// Advanced git operations
	RebaseOperations
	MergeOperations
	CherryPickOperations
	StashOperations
	ResetOperations
	PathOperations
	PatchOperations

	// Worktree management
	WorktreeOperations
	WorktreeRegistryOperations

	// Repository status
	StatusOperations

	// Low-level operations
	RefOperations
	ObjectOperations
	MetadataOperations
	StackMetadataOperations

	// Raw command execution
	RunGitCommandWithContext(ctx context.Context, args ...string) (string, error)
	RunGitCommandRawWithContext(ctx context.Context, args ...string) (string, error)
	RunGitCommandWithEnv(ctx context.Context, env []string, args ...string) (string, error)
	RunGitCommandInteractive(args ...string) error
	RunGHCommandWithContext(ctx context.Context, args ...string) (string, error)

	// Logging
	SetLogger(logger DebugLogger)
}

// NewRunner returns a standard implementation of Runner that uses the current
// working directory as its repository root.
func NewRunner(logger DebugLogger) Runner {
	return &runner{logger: logger}
}

// NewRunnerWithPath returns a Runner that operates on a specific repo path.
// This is safe for parallel tests since it doesn't rely on global state.
func NewRunnerWithPath(repoRoot string, logger DebugLogger) Runner {
	abs, err := filepath.Abs(repoRoot)
	if err == nil {
		repoRoot = abs
	}
	return &runner{repoRoot: repoRoot, logger: logger}
}

// runner implements Runner by calling the actual git package functions
type runner struct {
	repo     *Repository
	repoRoot string
	repoMu   sync.Mutex
	loggerMu sync.RWMutex
	logger   DebugLogger

	// Cached git version info
	gitVersionOnce   sync.Once
	gitVersionMajor  int
	gitVersionMinor  int
	gitVersionParsed bool
}

// SetLogger sets the debug logger for git command logging
func (r *runner) SetLogger(logger DebugLogger) {
	r.loggerMu.Lock()
	r.logger = logger
	r.loggerMu.Unlock()
}

// debugLog logs a git command if a debug logger is set
func (r *runner) debugLog(format string, args ...any) {
	r.loggerMu.RLock()
	logger := r.logger
	r.loggerMu.RUnlock()
	if logger != nil {
		logger.Debug(format, args...)
	}
}

func (r *runner) ensureRepo() (*Repository, error) {
	r.repoMu.Lock()
	defer r.repoMu.Unlock()

	if r.repo != nil {
		return r.repo, nil
	}

	path := r.repoRoot
	if path == "" {
		wd, _ := os.Getwd()
		path = wd
	}

	repo, err := OpenRepository(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open repository at %s: %w", path, err)
	}

	// Discover and cache the actual root path
	wt, err := repo.Worktree()
	if err == nil {
		r.repoRoot = wt.Filesystem.Root()
	}

	r.repo = repo
	return repo, nil
}

func (r *runner) ReloadRepository() error {
	r.repoMu.Lock()
	r.repo = nil // Clearing the cache forces the next ensureRepo() to re-open the repo
	r.repoMu.Unlock()
	_, err := r.ensureRepo()
	return err
}

func (r *runner) InitDefaultRepo() error {
	_, err := r.ensureRepo()
	return err
}

func (r *runner) GetRemote() string {
	repo, err := r.ensureRepo()
	if err != nil {
		return DefaultRemote
	}
	return r.getRemote(repo)
}

func (r *runner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return nil, err
	}
	return r.fetchRemoteShas(ctx, repo, remote)
}

func (r *runner) GetRemoteSha(remote, branchName string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}
	return r.getRemoteSha(repo, remote, branchName)
}

func (r *runner) GetConfig(key string) (string, error) {
	return r.runGitCommandInternal("config", "--get", key)
}

func (r *runner) SetConfig(key, value string) error {
	_, err := r.runGitCommandInternal("config", key, value)
	return err
}

func (r *runner) GetConfigAll(key string) ([]string, error) {
	output, err := r.runGitCommandInternal("config", "--get-all", key)
	if err == nil {
		if output == "" {
			return []string{}, nil
		}
		return strings.Split(output, "\n"), nil
	}
	return []string{}, nil
}

func (r *runner) AddConfigValue(key, value string) error {
	_, err := r.runGitCommandInternal("config", "--add", key, value)
	return err
}

func (r *runner) IsInsideRepo() bool {
	_, err := r.runGitCommandInternal("rev-parse", "--is-inside-work-tree")
	return err == nil
}

func (r *runner) DiscoverRepoRoot() (string, error) {
	return r.runGitCommandInternal("rev-parse", "--show-toplevel")
}

func (r *runner) GetRepoRoot() string {
	return r.repoRoot
}

func (r *runner) GetGitCommonDir() (string, error) {
	return r.runGitCommandInternal("rev-parse", "--git-common-dir")
}

func (r *runner) GetUserName(ctx context.Context) (string, error) {
	return r.RunGitCommandWithContext(ctx, "config", "user.name")
}

func (r *runner) EnsureMetadataRefspecConfigured() error {
	const metadataRefspec = "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*"

	refspecs, err := r.GetConfigAll("remote.origin.fetch")
	if err != nil {
		return fmt.Errorf("failed to get fetch refspecs: %w", err)
	}

	// Check if already configured
	for _, rs := range refspecs {
		if rs == metadataRefspec {
			return nil // Already configured
		}
	}

	// Add refspec for metadata refs
	return r.AddConfigValue("remote.origin.fetch", metadataRefspec)
}

func (r *runner) EnsureStackMetaRefspecConfigured() error {
	const stackMetaRefspec = "+refs/stackit/stacks/*:refs/stackit/remote-stacks/*"

	refspecs, err := r.GetConfigAll("remote.origin.fetch")
	if err != nil {
		return fmt.Errorf("failed to get fetch refspecs: %w", err)
	}

	// Check if already configured
	for _, rs := range refspecs {
		if rs == stackMetaRefspec {
			return nil // Already configured
		}
	}

	// Add refspec for stack metadata refs
	return r.AddConfigValue("remote.origin.fetch", stackMetaRefspec)
}

func (r *runner) FindRemoteBranch(ctx context.Context, remote string) (string, error) {
	// Get all branch configs that have this remote
	// Format: "branch.<name>.remote <remote>"
	output, err := r.RunGitCommandWithContext(ctx, "config", "--get-regexp", "^branch\\..*\\.remote$")
	if err != nil {
		return "", nil //nolint:nilerr // git config returns 1 if no branches match
	}

	if output == "" {
		return "", nil
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		// Line format: "branch.<name>.remote <remote>"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			remoteValue := parts[1]
			if remoteValue == remote {
				// Extract branch name from "branch.<name>.remote"
				branchPart := parts[0]
				if strings.HasPrefix(branchPart, "branch.") && strings.HasSuffix(branchPart, ".remote") {
					// Remove "branch." prefix and ".remote" suffix
					branchName := branchPart[7 : len(branchPart)-7]
					return branchName, nil
				}
			}
		}
	}
	return "", nil
}

func (r *runner) GetRemoteRevision(branchName string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}
	return r.getRemoteRevision(repo, branchName)
}

func (r *runner) GetCurrentRevision(ctx context.Context) (string, error) {
	return r.RunGitCommandWithContext(ctx, "rev-parse", "HEAD")
}

func (r *runner) GetRevision(branchName string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}
	return r.getRevision(repo, branchName)
}

func (r *runner) BatchGetRevisions(branchNames []string) (map[string]string, []error) {
	repo, err := r.ensureRepo()
	if err != nil {
		var errors []error
		for i := 0; i < len(branchNames); i++ {
			errors = append(errors, fmt.Errorf("failed to get repository: %w", err))
		}
		return nil, errors
	}
	return r.batchGetRevisions(repo, branchNames)
}

func (r *runner) GetMergeBase(rev1, rev2 string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}
	return r.getMergeBase(repo, rev1, rev2)
}

func (r *runner) GetMergeBaseByRef(ref1, ref2 string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}
	return r.getMergeBaseByRef(repo, ref1, ref2)
}

func (r *runner) IsAncestor(ancestor, descendant string) (bool, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return false, err
	}
	return r.isAncestor(repo, ancestor, descendant)
}

func (r *runner) GetCommitDate(branchName string) (time.Time, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return time.Time{}, err
	}
	return r.getCommitDate(repo, branchName)
}

func (r *runner) GetCommitAuthor(branchName string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}
	return r.getCommitAuthor(repo, branchName)
}

func (r *runner) GetCommitRange(base, head, format string) ([]string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return nil, err
	}

	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	headHash, err := r.resolveRefHashInternal(repo, head)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve head: %w", err)
	}

	var baseHash plumbing.Hash
	if base != "" {
		baseHash, err = r.resolveRefHashInternal(repo, base)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve base: %w", err)
		}
	}

	// Try go-git first
	commits, err := iterateCommitsNoLock(repo, headHash, baseHash)
	if err != nil {
		// Fallback to git CLI for worktree compatibility
		// go-git doesn't properly handle the 'commondir' redirection in worktrees
		return r.getCommitRangeWithFallback(base, head, format)
	}

	return formatCommits(commits, format)
}

// formatCommits formats go-git commit objects according to format string
func formatCommits(commits []*object.Commit, format string) ([]string, error) {
	result := make([]string, 0, len(commits))
	for _, commit := range commits {
		var formatted string
		switch format {
		case "SHA":
			formatted = commit.Hash.String()
		case "READABLE":
			// Oneline format: short SHA + subject
			shortHash := commit.Hash.String()[:7]
			subject := strings.Split(strings.TrimSpace(commit.Message), "\n")[0]
			formatted = fmt.Sprintf("%s - %s", shortHash, subject)
		case "MESSAGE":
			formatted = strings.TrimSpace(commit.Message)
		case "SUBJECT":
			subject := strings.Split(strings.TrimSpace(commit.Message), "\n")[0]
			formatted = strings.TrimSpace(subject)
		default:
			return nil, fmt.Errorf("unknown commit format: %s", format)
		}

		if formatted != "" {
			result = append(result, formatted)
		}
	}
	return result, nil
}

// getCommitRangeWithFallback uses git rev-list + git log for formatting
func (r *runner) getCommitRangeWithFallback(base, head, format string) ([]string, error) {
	// Get SHAs using git rev-list
	shas, err := r.getCommitRangeGitFallback(base, head)
	if err != nil {
		return nil, err
	}

	if format == "SHA" {
		return shas, nil
	}

	// Format each commit using git log
	result := make([]string, 0, len(shas))
	for _, sha := range shas {
		var formatted string
		switch format {
		case "READABLE":
			// Oneline format: short SHA + subject
			formatted, err = r.GetCommitLog(sha, "%h - %s")
		case "MESSAGE":
			formatted, err = r.GetCommitLog(sha, "%B")
		case "SUBJECT":
			formatted, err = r.GetCommitLog(sha, "%s")
		default:
			return nil, fmt.Errorf("unknown commit format: %s", format)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to format commit %s: %w", sha, err)
		}
		if formatted != "" {
			result = append(result, strings.TrimSpace(formatted))
		}
	}

	return result, nil
}

func (r *runner) GetCommitRangeSHAs(base, head string) ([]string, error) {
	return r.GetCommitRange(base, head, "SHA")
}

func (r *runner) GetCommitHistorySHAs(branchName string) ([]string, error) {
	return r.GetCommitRangeSHAs("", branchName)
}

func (r *runner) GetCommitSHA(branchName string, offset int) (string, error) {
	if offset < 0 {
		return "", fmt.Errorf("offset must be non-negative")
	}

	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}

	// Synchronize go-git operations to prevent concurrent packfile access
	goGitMu.Lock()
	defer goGitMu.Unlock()

	// Resolve branch reference
	hash, err := r.resolveRefHashInternal(repo, branchName)
	if err != nil {
		return "", fmt.Errorf("failed to get branch reference: %w", err)
	}

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	// Walk back offset number of commits
	for i := 0; i < offset; i++ {
		if commit.NumParents() == 0 {
			return "", fmt.Errorf("commit has no parent at offset %d", i)
		}
		// Get first parent
		commit, err = commit.Parent(0)
		if err != nil {
			return "", fmt.Errorf("failed to get parent commit: %w", err)
		}
	}

	return commit.Hash.String(), nil
}

func (r *runner) CheckoutPaths(ctx context.Context, branch string, paths []string) error {
	args := []string{"checkout", branch, "--"}
	args = append(args, paths...)
	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to checkout paths from %s: %w", branch, err)
	}
	return nil
}

func (r *runner) RemovePaths(ctx context.Context, paths []string) error {
	args := []string{"rm"}
	args = append(args, paths...)
	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to remove paths: %w", err)
	}
	return nil
}

func (r *runner) GetReflog(ctx context.Context, count int, format string) (string, error) {
	args := []string{"reflog", fmt.Sprintf("-%d", count)}
	if format != "" {
		args = append(args, fmt.Sprintf("--format=%s", format))
	}
	return r.RunGitCommandWithContext(ctx, args...)
}

func (r *runner) GetDiffNumstat(base, head string) (string, error) {
	return r.runGitCommandInternal("diff", "--numstat", base, head)
}

func (r *runner) GetCommitLog(sha, format string) (string, error) {
	return r.runGitCommandInternal("log", "-1", "--format="+format, sha)
}

func (r *runner) GetStatusPorcelain(ctx context.Context) (string, error) {
	return r.RunGitCommandWithContext(ctx, "status", "--porcelain")
}

func (r *runner) GetCommitTemplate(ctx context.Context) (string, error) {
	status, err := r.RunGitCommandWithContext(ctx, "status")
	if err != nil {
		return "", fmt.Errorf("failed to get git status: %w", err)
	}

	lines := strings.Split(status, "\n")
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString("# Please enter the commit message for your changes. Lines starting\n")
	sb.WriteString("# with '#' will be ignored, and an empty message aborts the commit.\n")
	sb.WriteString("#\n")
	for _, line := range lines {
		sb.WriteString("# ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func (r *runner) runGitCommandInternal(args ...string) (string, error) {
	return r.RunGitCommandWithContext(context.Background(), args...)
}

func (r *runner) GetRef(name string) (string, error) {
	return r.RunGitCommandWithContext(context.Background(), "rev-parse", "--verify", name)
}

func (r *runner) UpdateRef(name, sha string) error {
	_, err := r.runGitCommandInternal("update-ref", name, sha)
	return err
}

func (r *runner) DeleteRef(name string) error {
	_, err := r.runGitCommandInternal("update-ref", "-d", name)
	return err
}

func (r *runner) CatFile(sha string) (string, error) {
	return r.runGitCommandInternal("cat-file", "-p", sha)
}

func (r *runner) CreateBlob(content string) (string, error) {
	return r.runGitCommandWithInputInternal(content, "hash-object", "-w", "--stdin")
}

func (r *runner) ReadBlob(sha string) (string, error) {
	return r.CatFile(sha)
}

func (r *runner) ListRefs(prefix string) (map[string]string, error) {
	output, _ := r.RunGitCommandWithContext(context.Background(), "show-ref")

	result := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		parts := strings.Split(line, " ")
		if len(parts) == 2 {
			sha := parts[0]
			refName := parts[1]
			if strings.HasPrefix(refName, prefix) {
				result[refName] = sha
			}
		}
	}
	return result, nil
}

func (r *runner) PushMetadataRefs(ctx context.Context, branches []string) error {
	if len(branches) == 0 {
		return nil
	}
	// git push origin +refs/stackit/metadata/branch1 +refs/stackit/metadata/branch2 ...
	// We use the '+' prefix to force the push because metadata refs point to blobs,
	// and updates to non-commit objects are always considered non-fast-forward by Git.
	args := []string{"push", "origin"}
	for _, branch := range branches {
		args = append(args, fmt.Sprintf("+refs/stackit/metadata/%s", branch))
	}
	_, err := r.RunGitCommandWithContext(ctx, args...)
	return err
}

func (r *runner) FetchMetadataRefs(ctx context.Context) error {
	// git fetch origin 'refs/stackit/metadata/*:refs/stackit/remote-metadata/*'
	_, err := r.RunGitCommandWithContext(ctx, "fetch", "origin", "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*")
	return err
}

func (r *runner) PushStackMetaRefs(ctx context.Context, stackIDs []string) error {
	if len(stackIDs) == 0 {
		return nil
	}
	// git push origin +refs/stackit/stacks/id1 +refs/stackit/stacks/id2 ...
	// We use the '+' prefix to force the push because stack metadata refs point to blobs,
	// and updates to non-commit objects are always considered non-fast-forward by Git.
	args := []string{"push", "origin"}
	for _, stackID := range stackIDs {
		args = append(args, fmt.Sprintf("+refs/stackit/stacks/%s", stackID))
	}
	_, err := r.RunGitCommandWithContext(ctx, args...)
	return err
}

func (r *runner) FetchStackMetaRefs(ctx context.Context) error {
	// git fetch origin 'refs/stackit/stacks/*:refs/stackit/remote-stacks/*'
	_, err := r.RunGitCommandWithContext(ctx, "fetch", "origin", "+refs/stackit/stacks/*:refs/stackit/remote-stacks/*")
	return err
}

func (r *runner) DeleteRemoteMetadataRef(ctx context.Context, branch string) error {
	// git push origin --delete refs/stackit/metadata/<branch>
	_, err := r.RunGitCommandWithContext(ctx, "push", "origin", "--delete", fmt.Sprintf("refs/stackit/metadata/%s", branch))
	return err
}

func (r *runner) BatchDeleteRemoteMetadataRefs(ctx context.Context, branches []string) error {
	if len(branches) == 0 {
		return nil
	}
	// git push origin --delete refs/stackit/metadata/branch1 refs/stackit/metadata/branch2 ...
	args := []string{"push", "origin", "--delete"}
	for _, branch := range branches {
		args = append(args, fmt.Sprintf("refs/stackit/metadata/%s", branch))
	}
	_, err := r.RunGitCommandWithContext(ctx, args...)
	return err
}

func (r *runner) TestRemoteRefCompatibility() error {
	testRef := "refs/stackit/metadata/stackit-compat-test"
	testContent := fmt.Sprintf(`{"test":true,"timestamp":%d}`, time.Now().Unix())

	// Create test blob
	sha, err := r.CreateBlob(testContent)
	if err != nil {
		return fmt.Errorf("failed to create test blob: %w", err)
	}

	// Try to push test ref
	if err := r.UpdateRef(testRef, sha); err != nil {
		return fmt.Errorf("failed to update local test ref: %w", err)
	}

	if _, err := r.runGitCommandInternal("push", "origin", "+"+testRef); err != nil {
		_ = r.DeleteRef(testRef) // Cleanup local
		return fmt.Errorf("remote rejected metadata ref push: %w", err)
	}

	// Cleanup: delete remote and local test ref
	_, _ = r.runGitCommandInternal("push", "origin", "--delete", testRef)
	_ = r.DeleteRef(testRef)

	return nil
}

func (r *runner) GetParentCommitSHA(commitSHA string) (string, error) {
	repo, err := r.ensureRepo()
	if err != nil {
		return "", err
	}

	hash, err := r.resolveRefHash(repo, commitSHA)
	if err != nil {
		return "", fmt.Errorf("failed to resolve commit: %w", err)
	}

	goGitMu.Lock()
	defer goGitMu.Unlock()

	commit, err := repo.CommitObject(hash)
	if err != nil {
		return "", fmt.Errorf("failed to get commit: %w", err)
	}

	if commit.NumParents() == 0 {
		return "", fmt.Errorf("commit has no parents")
	}

	return commit.ParentHashes[0].String(), nil
}

func (r *runner) CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error) {
	commitDiff, err := r.runGitCommandInternal("diff", parentSHA, commitSHA)
	if err != nil {
		return false, fmt.Errorf("failed to get commit diff: %w", err)
	}

	if strings.TrimSpace(commitDiff) == "" {
		return true, nil
	}

	commitHunks := parseDiffHunks(commitDiff, hunk.File)

	fileInDiff := false
	for _, line := range strings.Split(commitDiff, "\n") {
		if strings.Contains(line, hunk.File) {
			fileInDiff = true
			break
		}
	}
	if !fileInDiff {
		return true, nil
	}

	// If file appears in diff but no hunks parsed, might be a rename or parsing failed
	if len(commitHunks) == 0 {
		return false, nil
	}

	for _, commitHunk := range commitHunks {
		if commitHunk.File != hunk.File {
			continue
		}

		if hunkOverlaps(hunk, commitHunk) {
			return false, nil
		}
	}

	return true, nil
}

func (r *runner) ApplyPatch(ctx context.Context, patchFile string, threeWay bool) error {
	args := []string{"apply"}
	if threeWay {
		args = append(args, "--3way")
	}
	args = append(args, patchFile)
	_, err := r.RunGitCommandWithContext(ctx, args...)
	return err
}

func (r *runner) HasUncommittedChanges(ctx context.Context) bool {
	output, err := r.RunGitCommandWithContext(ctx, "status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(output) != ""
}
