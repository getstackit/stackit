// Package git provides a wrapper around the git CLI for repository operations.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getstackit/stackit/internal/utils"
)

// DebugLogger is an interface for logging messages from the git layer.
// Despite its name, it carries both debug and informational events; the latter
// is used for instrumentation lines (cache stats, batch-load timings) that
// should be visible at the default log level.
type DebugLogger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
}

type traceLogger interface {
	Trace(op string, durationMicros int64, success bool, err error, attrs ...slog.Attr)
}

// DefaultCommandTimeout is the default timeout for git commands
const DefaultCommandTimeout = 5 * time.Minute

// CommandWaitDelay bounds how long we wait for a killed git/gh subprocess to
// release its stdio pipes before force-closing them. Without it, a `git fetch`
// can spawn an `ssh` grandchild that inherits the captured stdout/stderr pipe;
// when the context deadline kills git, ssh keeps the pipe open and cmd.Run()
// blocks indefinitely — the command times out but never returns, which is the
// hang-until-reboot behind issue #1330. Setting WaitDelay makes Go force-close
// the pipes shortly after the kill so Run() always returns.
const CommandWaitDelay = 10 * time.Second

// ErrStaleRemoteInfo indicates that a push failed because the remote has changed
var ErrStaleRemoteInfo = errors.New("stale info")

func traceOp(base string, args []string) string {
	if len(args) == 0 {
		return base
	}
	return base + "." + args[0]
}

func traceCmd(base string, args []string) string {
	if len(args) == 0 {
		return base
	}
	return base + " " + strings.Join(args, " ")
}

func (r *runner) UpdateRefWithLog(ctx context.Context, refName, sha, message string) error {
	_, err := r.RunGitCommandWithContext(ctx, "update-ref", "-m", message, refName, sha)
	return err
}

func (r *runner) VerifyRef(ctx context.Context, refName string) error {
	_, err := r.GetRef(refName)
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
	r.metadataCache.InvalidateForRefs(updates)
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
	r.metadataCache.InvalidateForRefs(updates)
	return nil
}

// DeleteRefsBatch atomically deletes multiple references.
func (r *runner) DeleteRefsBatch(ctx context.Context, refNames []string) error {
	if len(refNames) == 0 {
		return nil
	}

	if err := r.ensureRefBranchesNotCheckedOut(ctx, refNames); err != nil {
		return err
	}

	var stdin strings.Builder
	for _, refName := range refNames {
		fmt.Fprintf(&stdin, "delete %s\n", refName)
	}

	_, err := r.runGitInternal(ctx, stdin.String(), nil, true, "update-ref", "--stdin")
	if err != nil {
		return fmt.Errorf("atomic ref delete failed: %w", err)
	}
	r.metadataCache.InvalidateForRefNames(refNames)
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

func (r *runner) runGitInternal(ctx context.Context, input string, env []string, trim bool, args ...string) (string, error) {
	start := time.Now()
	op := traceOp("git", args)
	cmdAttr := slog.String("cmd", traceCmd("git", args))

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
	cmd.WaitDelay = CommandWaitDelay
	if root := r.getRepoRoot(); root != "" {
		cmd.Dir = root
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
		cmdErr := NewCommandError("git", args, stdout.String(), stderr.String(), err)
		r.traceLog(op, time.Since(start), false, cmdErr, cmdAttr)
		return "", cmdErr
	}

	result := stdout.String()
	if trim {
		result = strings.TrimSpace(result)
	}
	r.traceLog(op, time.Since(start), true, nil, cmdAttr)
	return result, nil
}

// runGitStreaming runs a git command and streams output to the terminal (if interactive)
// while also capturing it for error handling. This is useful for commands like commit
// where hook output should be visible to the user.
func (r *runner) runGitStreaming(ctx context.Context, args ...string) (string, error) {
	start := time.Now()
	op := traceOp("git", args)
	cmdAttr := slog.String("cmd", traceCmd("git", args))

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
	cmd.WaitDelay = CommandWaitDelay
	if root := r.getRepoRoot(); root != "" {
		cmd.Dir = root
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
		cmdErr := NewCommandError("git", args, stdoutBuf.String(), stderrBuf.String(), err)
		r.traceLog(op, time.Since(start), false, cmdErr, cmdAttr)
		return "", cmdErr
	}

	r.traceLog(op, time.Since(start), true, nil, cmdAttr)
	return strings.TrimSpace(stdoutBuf.String()), nil
}

func (r *runner) RunGHCommandWithContext(ctx context.Context, args ...string) (string, error) {
	start := time.Now()
	op := traceOp("gh", args)
	cmdAttr := slog.String("cmd", traceCmd("gh", args))

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
	cmd.WaitDelay = CommandWaitDelay
	// Use repoRoot for gh commands to ensure they are scoped to the correct repo
	if root := r.getRepoRoot(); root != "" {
		cmd.Dir = root
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
			cmdErr := NewCommandError("gh", args, stdout.String(), stderr.String(), ctx.Err())
			r.traceLog(op, time.Since(start), false, cmdErr, cmdAttr)
			return "", cmdErr
		}
		cmdErr := NewCommandError("gh", args, stdout.String(), stderr.String(), err)
		r.traceLog(op, time.Since(start), false, cmdErr, cmdAttr)
		return "", cmdErr
	}
	r.traceLog(op, time.Since(start), true, nil, cmdAttr)
	return strings.TrimSpace(stdout.String()), nil
}

func (r *runner) RunGitCommandInteractive(args ...string) error {
	if !utils.IsInteractive() {
		return fmt.Errorf("interactive git command '%v' not allowed in non-interactive mode", args)
	}

	start := time.Now()
	op := traceOp("git", args)
	cmdAttr := slog.String("cmd", traceCmd("git", args))

	r.debugLog("git %s (interactive)", strings.Join(args, " "))

	cmd := exec.Command("git", args...)
	if root := r.getRepoRoot(); root != "" {
		cmd.Dir = root
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	r.traceLog(op, time.Since(start), err == nil, err, cmdAttr)
	return err
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
	r := &runner{logger: logger}
	r.objects = newObjectReader(func() (string, error) {
		if err := r.ensureRepo(); err != nil {
			return "", err
		}
		return r.getRepoRoot(), nil
	})
	return r
}

// NewRunnerWithPath returns a Runner that operates on a specific repo path.
// This is safe for parallel tests since it doesn't rely on global state.
func NewRunnerWithPath(repoRoot string, logger DebugLogger) Runner {
	abs, err := filepath.Abs(repoRoot)
	if err == nil {
		repoRoot = abs
	}
	root := repoRoot
	r := &runner{repoRoot: root, logger: logger}
	r.objects = newObjectReader(func() (string, error) {
		return root, nil
	})
	return r
}

// runner implements Runner by calling the actual git package functions
type runner struct {
	repoRoot    string
	repoRootMu  sync.Mutex
	repoChecked bool // true once ensureRepo has validated repoRoot resolves to a git dir
	loggerMu    sync.RWMutex
	logger      DebugLogger

	// Cached metadata to avoid redundant ReadMetadata calls (each spawns 2 git processes).
	// Thread-safe: sync.Map handles concurrent reads from worker pools.
	metadataCache metadataCache

	// objects is a persistent git cat-file --batch process for zero-spawn object reads.
	// Started lazily on first use; lives for the lifetime of the runner.
	objects *objectReader

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

func (r *runner) traceLog(op string, duration time.Duration, success bool, err error, attrs ...slog.Attr) {
	r.loggerMu.RLock()
	logger := r.logger
	r.loggerMu.RUnlock()
	if tracer, ok := logger.(traceLogger); ok {
		tracer.Trace(op, duration.Microseconds(), success, err, attrs...)
	}
}

// infoLog emits an informational log entry through the attached logger.
// Used for low-volume instrumentation events (e.g. metadata batch-load timings)
// that should be visible at the default log level.
func (r *runner) infoLog(format string, args ...any) {
	r.loggerMu.RLock()
	logger := r.logger
	r.loggerMu.RUnlock()
	if logger != nil {
		logger.Info(format, args...)
	}
}

// ensureRepo discovers and validates the repository root via
// `git rev-parse --show-toplevel`. The result is cached after the first
// successful validation; pre-set repoRoot values (from NewRunnerWithPath)
// are validated by running the command in that directory.
//
// Outside a git repository the error message starts with "not a git
// repository" so callers (and tests) can match on that phrase.
func (r *runner) ensureRepo() error {
	r.repoRootMu.Lock()
	defer r.repoRootMu.Unlock()

	if r.repoChecked {
		return nil
	}

	dir := r.repoRoot
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("not a git repository: failed to resolve working directory: %w", err)
		}
		dir = wd
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("not a git repository at %s: %w", dir, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return fmt.Errorf("not a git repository at %s: empty toplevel", dir)
	}
	r.repoRoot = root
	r.repoChecked = true
	return nil
}

func (r *runner) InitDefaultRepo() error {
	return r.ensureRepo()
}

func (r *runner) GetRemote() string {
	return r.getRemote()
}

func (r *runner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	return r.fetchRemoteShas(ctx, remote)
}

func (r *runner) GetRemoteSha(remote, branchName string) (string, error) {
	return r.getRemoteSha(remote, branchName)
}

func (r *runner) GetConfig(key string) (string, error) {
	if err := r.ensureRepo(); err != nil {
		return "", err
	}
	return r.runGitCommandInternal("config", "--get", key)
}

func (r *runner) SetConfig(key, value string) error {
	if err := r.ensureRepo(); err != nil {
		return err
	}
	return NewConfigStore(r.getRepoRoot()).Set(key, value)
}

func (r *runner) GetConfigAll(key string) ([]string, error) {
	if err := r.ensureRepo(); err != nil {
		return []string{}, err
	}
	values, err := NewConfigStore(r.getRepoRoot()).GetAll(key)
	if err != nil || len(values) == 0 {
		return []string{}, err
	}
	return values, nil
}

func (r *runner) AddConfigValue(key, value string) error {
	if err := r.ensureRepo(); err != nil {
		return err
	}
	return NewConfigStore(r.getRepoRoot()).Add(key, value)
}

func (r *runner) IsInsideRepo() bool {
	return r.ensureRepo() == nil
}

func (r *runner) DiscoverRepoRoot() (string, error) {
	if err := r.ensureRepo(); err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	return r.getRepoRoot(), nil
}

func (r *runner) GetRepoRoot() string {
	return r.getRepoRoot()
}

// getRepoRoot returns r.repoRoot under repoRootMu. All reads must go through
// this getter so they don't race with ensureRepo's normalization write.
func (r *runner) getRepoRoot() string {
	r.repoRootMu.Lock()
	defer r.repoRootMu.Unlock()
	return r.repoRoot
}

func (r *runner) GetGitCommonDir() (string, error) {
	root, err := r.DiscoverRepoRoot()
	if err != nil {
		return "", err
	}
	return commonGitDir(resolveGitDir(root)), nil
}

func (r *runner) GetUserName(ctx context.Context) (string, error) {
	if err := r.ensureRepo(); err != nil {
		return "", err
	}
	return NewConfigStore(r.getRepoRoot()).Get("user.name")
}

func (r *runner) EnsureMetadataRefspecConfigured() error {
	const metadataRefspec = "+refs/stackit/metadata/*:refs/stackit/remote-metadata/*"

	fetchConfigKey := fmt.Sprintf("remote.%s.fetch", r.GetRemote())
	refspecs, err := r.GetConfigAll(fetchConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get fetch refspecs: %w", err)
	}

	// Check if already configured
	if slices.Contains(refspecs, metadataRefspec) {
		return nil // Already configured
	}

	// Add refspec for metadata refs
	return r.AddConfigValue(fetchConfigKey, metadataRefspec)
}

func (r *runner) EnsureStackMetaRefspecConfigured() error {
	const stackMetaRefspec = "+refs/stackit/stacks/*:refs/stackit/remote-stacks/*"

	fetchConfigKey := fmt.Sprintf("remote.%s.fetch", r.GetRemote())
	refspecs, err := r.GetConfigAll(fetchConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get fetch refspecs: %w", err)
	}

	// Check if already configured
	if slices.Contains(refspecs, stackMetaRefspec) {
		return nil // Already configured
	}

	// Add refspec for stack metadata refs
	return r.AddConfigValue(fetchConfigKey, stackMetaRefspec)
}

func (r *runner) FindRemoteBranch(ctx context.Context, remote string) (string, error) {
	// `git config --get-regexp 'branch\..*\.remote'` lists each tracked
	// branch's remote on its own line: "branch.<name>.remote <value>".
	out, err := r.RunGitCommandWithContext(ctx, "config", "--get-regexp", `branch\..*\.remote`)
	if err != nil {
		return "", nil //nolint:nilerr // no branch config is non-fatal here
	}
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(line, " ")
		if !ok || strings.TrimSpace(value) != remote {
			continue
		}
		// key is "branch.<name>.remote" — extract <name>.
		name, ok := strings.CutPrefix(key, "branch.")
		if !ok {
			continue
		}
		name = strings.TrimSuffix(name, ".remote")
		if name != "" {
			return name, nil
		}
	}
	return "", nil
}

func (r *runner) GetRemoteRevision(branchName string) (string, error) {
	return r.getRemoteRevision(branchName)
}

func (r *runner) GetCurrentRevision(ctx context.Context) (string, error) {
	out, err := r.RunGitCommandWithContext(ctx, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to resolve HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (r *runner) GetRevision(branchName string) (string, error) {
	return r.getRevision(branchName)
}

func (r *runner) BatchGetRevisions(branchNames []string) (map[string]string, []error) {
	return r.batchGetRevisions(branchNames)
}

func (r *runner) GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error) {
	return r.getMergeBaseByRef(ctx, rev1, rev2)
}

func (r *runner) GetMergeBaseByRef(ctx context.Context, ref1, ref2 string) (string, error) {
	return r.getMergeBaseByRef(ctx, ref1, ref2)
}

func (r *runner) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	return r.isAncestor(ctx, ancestor, descendant)
}

func (r *runner) GetCommitDate(branchName string) (time.Time, error) {
	return r.getCommitDate(branchName)
}

func (r *runner) GetCommitAuthor(branchName string) (string, error) {
	return r.getCommitAuthor(branchName)
}

func (r *runner) GetCommitRange(ctx context.Context, base, head, format string) ([]string, error) {
	rangeArg := head
	if base != "" {
		rangeArg = base + ".." + head
	}

	switch format {
	case "SHA":
		return r.gitLogLines(ctx, rangeArg, "%H")
	case "READABLE":
		return r.gitLogLines(ctx, rangeArg, "%h %s")
	case "SUBJECT":
		return r.gitLogLines(ctx, rangeArg, "%s")
	case "SHA_SUBJECT":
		// NUL-separated full SHA and subject on one record per commit. The line
		// is never blank (%H is always present) so gitLogLines won't drop a
		// commit with an empty subject, and the literal NUL survives the split
		// on "\n". Callers split each entry on "\x00" to recover SHA + subject.
		return r.gitLogLines(ctx, rangeArg, "%H%x00%s")
	case "READABLE_WITH_DATE":
		// Tab-separated: short SHA, RFC3339 UTC date, subject. Use Unix
		// epoch from git and convert in Go to preserve the prior behavior of
		// normalizing to UTC regardless of the commit's recorded TZ.
		lines, err := r.gitLogLines(ctx, rangeArg, "%h\t%at\t%s")
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			sha, rest, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}
			epochStr, subject, ok := strings.Cut(rest, "\t")
			if !ok {
				continue
			}
			epoch, err := strconv.ParseInt(epochStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse commit epoch %q: %w", epochStr, err)
			}
			out = append(out, fmt.Sprintf("%s\t%s\t%s", sha, time.Unix(epoch, 0).UTC().Format(time.RFC3339), subject))
		}
		return out, nil
	case "MESSAGE":
		// Bodies can contain newlines; use NUL record separator.
		out, err := r.RunGitCommandRawWithContext(ctx, "log", "-z", "--format=%B", rangeArg)
		if err != nil {
			return nil, fmt.Errorf("failed to walk commits %s: %w", rangeArg, err)
		}
		records := splitNulTerminated(out)
		result := make([]string, 0, len(records))
		for _, rec := range records {
			rec = strings.TrimSpace(rec)
			if rec != "" {
				result = append(result, rec)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unknown commit format: %s", format)
	}
}

// gitLogLines runs `git log` over a range with a single-line format and
// returns one element per commit, dropping blank lines. Suitable for any
// format that does not include literal newlines.
func (r *runner) gitLogLines(ctx context.Context, rangeArg, format string) ([]string, error) {
	out, err := r.RunGitCommandWithContext(ctx, "log", "--format="+format, rangeArg)
	if err != nil {
		return nil, fmt.Errorf("failed to walk commits %s: %w", rangeArg, err)
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil, nil
	}
	lines := strings.Split(out, "\n")
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result, nil
}

// splitNulTerminated splits a NUL-terminated string (as produced by git -z)
// into non-empty records. The trailing NUL appended by git is stripped first.
func splitNulTerminated(s string) []string {
	s = strings.TrimRight(s, "\x00")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\x00")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func (r *runner) GetCommitRangeSHAs(ctx context.Context, rr RevRange) ([]string, error) {
	base, head := rr.Base, rr.Head
	return r.GetCommitRange(ctx, base, head, "SHA")
}

func (r *runner) GetCommitHistorySHAs(ctx context.Context, branchName string) ([]string, error) {
	return r.GetCommitRangeSHAs(ctx, RevRange{Head: branchName})
}

func (r *runner) GetCommitSHA(branchName string, offset int) (string, error) {
	if offset < 0 {
		return "", fmt.Errorf("offset must be non-negative")
	}
	ref := branchName
	if offset > 0 {
		ref = fmt.Sprintf("%s~%d", branchName, offset)
	}
	out, err := r.RunGitCommandWithContext(context.Background(), "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to walk %d commit(s) back from %s: %w", offset, branchName, err)
	}
	return strings.TrimSpace(out), nil
}

func (r *runner) CheckoutPaths(ctx context.Context, branch string, paths []string) error {
	args := make([]string, 0, 3+len(paths))
	args = append(args, "checkout", branch, "--")
	args = append(args, paths...)
	_, err := r.RunGitCommandWithContext(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to checkout paths from %s: %w", branch, err)
	}
	return nil
}

func (r *runner) RemovePaths(ctx context.Context, paths []string) error {
	args := make([]string, 0, 1+len(paths))
	args = append(args, "rm")
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

func (r *runner) GetDiffNumstat(rr RevRange) (string, error) {
	base, head := rr.Base, rr.Head
	return r.runGitCommandInternal(gitCmdDiff, "--numstat", base, head)
}

func (r *runner) GetCommitLog(sha, format string) (string, error) {
	return r.runGitCommandInternal("log", "-1", "--format="+format, sha)
}

func (r *runner) GetStatusPorcelain(ctx context.Context) (string, error) {
	return r.RunGitCommandRawWithContext(ctx, "status", "--porcelain")
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
	return r.resolveRefSHA(name)
}

func (r *runner) UpdateRef(name, sha string) error {
	if _, err := r.RunGitCommandWithContext(context.Background(), "update-ref", name, sha); err != nil {
		return fmt.Errorf("failed to update ref %s: %w", name, err)
	}
	r.metadataCache.InvalidateForRefNames([]string{name})
	return nil
}

func (r *runner) DeleteRef(ctx context.Context, name string) error {
	// `git update-ref -d` rewrites packed-refs correctly and is lenient when
	// the ref doesn't exist.
	if _, err := r.RunGitCommandWithContext(ctx, "update-ref", "-d", name); err != nil {
		return fmt.Errorf("failed to delete ref %s: %w", name, err)
	}

	if err := r.verifyRefDeleted(ctx, name); err != nil {
		return err
	}

	r.metadataCache.InvalidateForRefNames([]string{name})
	return nil
}

func (r *runner) CatFile(sha string) (string, error) {
	return r.ReadBlob(sha)
}

func (r *runner) CreateBlob(content string) (string, error) {
	out, err := r.runGitInternal(context.Background(), content, nil, true, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", fmt.Errorf("failed to create blob: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// CreateBlobsBatch writes N blobs to the object store in a single
// `git hash-object -w --stdin-paths` invocation. Each content is staged to a
// temp file (so git's path-based hashing can read it) and the SHAs come back
// on stdout in input order.
//
// For very small N the overhead of staging temp files exceeds the savings
// from collapsing subprocess calls; callers with N==1 should use CreateBlob
// directly. We still handle N==0/1 here so the method's contract holds.
func (r *runner) CreateBlobsBatch(ctx context.Context, contents []string) ([]string, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	if len(contents) == 1 {
		sha, err := r.CreateBlob(contents[0])
		if err != nil {
			return nil, err
		}
		return []string{sha}, nil
	}

	tempDir, err := os.MkdirTemp("", "stackit-blob-batch-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir for blob batch: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Stage each blob to a deterministically-named file so we can pass the
	// list to stdin in input order and rely on git's output order matching.
	paths := make([]string, len(contents))
	for i, content := range contents {
		p := filepath.Join(tempDir, fmt.Sprintf("blob-%06d", i))
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return nil, fmt.Errorf("failed to stage blob %d: %w", i, err)
		}
		paths[i] = p
	}

	stdin := strings.Join(paths, "\n") + "\n"
	out, err := r.runGitInternal(ctx, stdin, nil, true, "hash-object", "-w", "--stdin-paths")
	if err != nil {
		return nil, fmt.Errorf("failed to batch-create blobs: %w", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(contents) {
		return nil, fmt.Errorf("git hash-object returned %d shas for %d blobs", len(lines), len(contents))
	}
	shas := make([]string, len(lines))
	for i, line := range lines {
		sha := strings.TrimSpace(line)
		if sha == "" {
			return nil, fmt.Errorf("git hash-object returned empty sha at index %d", i)
		}
		shas[i] = sha
	}
	return shas, nil
}

func (r *runner) ReadBlob(sha string) (string, error) {
	content, found, err := r.objects.ReadObject(sha)
	if err != nil {
		return "", fmt.Errorf("failed to read blob %s: %w", sha, err)
	}
	if !found {
		return "", fmt.Errorf("blob %s not found", sha)
	}
	return content, nil
}

func (r *runner) ListRefs(prefix string) (map[string]string, error) {
	// `git for-each-ref` lists refs with optional prefix filtering. When the
	// prefix doesn't match any refs the command still exits 0 with empty
	// output, so we don't need exit-code handling.
	args := []string{"for-each-ref", "--format=%(refname)%00%(objectname)"}
	if prefix != "" {
		args = append(args, prefix)
	}
	out, err := r.RunGitCommandWithContext(context.Background(), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to read refs: %w", err)
	}
	result := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		name, sha, ok := strings.Cut(line, "\x00")
		if !ok || sha == "" {
			continue
		}
		result[name] = sha
	}
	return result, nil
}

func (r *runner) PushMetadataRefs(ctx context.Context, branches []string) error {
	if len(branches) == 0 {
		return nil
	}
	// git push origin +refs/stackit/metadata/branch1 +refs/stackit/metadata/branch2 ...
	// The '+' prefix forces the push: metadata refs point to blobs, and
	// updates to non-commit objects are always considered non-fast-forward
	// by Git.
	refspecs := make([]string, 0, len(branches))
	for _, branch := range branches {
		refspecs = append(refspecs, fmt.Sprintf("+refs/stackit/metadata/%s", branch))
	}
	return r.pushOriginRefSpecs(ctx, refspecs)
}

func (r *runner) FetchMetadataRefs(ctx context.Context) error {
	return r.fetchRemoteRefSpecs(ctx, r.GetRemote(), []string{
		"+refs/stackit/metadata/*:refs/stackit/remote-metadata/*",
	})
}

func (r *runner) PushStackMetaRefs(ctx context.Context, stackIDs []string) error {
	if len(stackIDs) == 0 {
		return nil
	}
	refspecs := make([]string, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		refspecs = append(refspecs, fmt.Sprintf("+refs/stackit/stacks/%s", stackID))
	}
	return r.pushOriginRefSpecs(ctx, refspecs)
}

func (r *runner) FetchStackMetaRefs(ctx context.Context) error {
	return r.fetchRemoteRefSpecs(ctx, r.GetRemote(), []string{
		"+refs/stackit/stacks/*:refs/stackit/remote-stacks/*",
	})
}

func (r *runner) DeleteRemoteStackMetaRefs(ctx context.Context, stackIDs []string) error {
	if len(stackIDs) == 0 {
		return nil
	}
	refspecs := make([]string, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		refspecs = append(refspecs, fmt.Sprintf(":refs/stackit/stacks/%s", stackID))
	}
	return r.pushOriginRefSpecs(ctx, refspecs)
}

func (r *runner) DeleteRemoteMetadataRef(ctx context.Context, branch string) error {
	return r.pushOriginRefSpecs(ctx, []string{
		fmt.Sprintf(":refs/stackit/metadata/%s", branch),
	})
}

func (r *runner) BatchDeleteRemoteMetadataRefs(ctx context.Context, branches []string) error {
	if len(branches) == 0 {
		return nil
	}
	refspecs := make([]string, 0, len(branches))
	for _, branch := range branches {
		refspecs = append(refspecs, fmt.Sprintf(":refs/stackit/metadata/%s", branch))
	}
	return r.pushOriginRefSpecs(ctx, refspecs)
}

func (r *runner) TestRemoteRefCompatibility(ctx context.Context) error {
	testRef := "refs/stackit/metadata/stackit-compat-test"
	testContent := fmt.Sprintf(`{"test":true,"timestamp":%d}`, time.Now().Unix())

	sha, err := r.CreateBlob(testContent)
	if err != nil {
		return fmt.Errorf("failed to create test blob: %w", err)
	}

	if err := r.UpdateRef(testRef, sha); err != nil {
		return fmt.Errorf("failed to update local test ref: %w", err)
	}

	if err := r.pushOriginRefSpecs(ctx, []string{"+" + testRef}); err != nil {
		_ = r.DeleteRef(ctx, testRef)
		return fmt.Errorf("remote rejected metadata ref push: %w", err)
	}

	_ = r.pushOriginRefSpecs(ctx, []string{":" + testRef})
	_ = r.DeleteRef(ctx, testRef)

	return nil
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *runner) fetchRemoteRefSpecs(ctx context.Context, remote string, refspecs []string) error {
	if len(refspecs) == 0 {
		return nil
	}
	ctx = ctxOrBackground(ctx)
	args := append([]string{"fetch", remote}, refspecs...)
	if _, err := r.RunGitCommandWithContext(ctx, args...); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	return nil
}

func (r *runner) pushOriginRefSpecs(ctx context.Context, refspecs []string) error {
	if len(refspecs) == 0 {
		return nil
	}
	ctx = ctxOrBackground(ctx)
	args := append([]string{gitCmdPush, "origin"}, refspecs...)
	if _, err := r.RunGitCommandWithContext(ctx, args...); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	return nil
}

func (r *runner) GetParentCommitSHA(commitSHA string) (string, error) {
	out, err := r.RunGitCommandWithContext(context.Background(), "rev-parse", "--verify", "--end-of-options", commitSHA+"^")
	if err != nil {
		return "", fmt.Errorf("failed to get parent of %s: %w", commitSHA, err)
	}
	return strings.TrimSpace(out), nil
}

func (r *runner) CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error) {
	commitDiff, err := r.runGitCommandInternal(gitCmdDiff, parentSHA, commitSHA)
	if err != nil {
		return false, fmt.Errorf("failed to get commit diff: %w", err)
	}

	if strings.TrimSpace(commitDiff) == "" {
		return true, nil
	}

	commitHunks := parseDiffHunks(commitDiff, hunk.File)

	fileInDiff := false
	for line := range strings.SplitSeq(commitDiff, "\n") {
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

		if hunk.Overlaps(commitHunk) {
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
