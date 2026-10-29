// Package git provides a wrapper around the git CLI for repository operations.
package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

func (r *runner) VerifyRef(ctx context.Context, refName string) error {
	_, err := r.ReadRevisions(ctx, refName).One()
	return err
}

// RefUpdate represents a single reference update operation.
type RefUpdate struct {
	RefName      string
	NewSHA       string
	OldSHA       string // Optional: for optimistic locking verification
	MustNotExist bool   // Require the ref to be absent when creating it.
	IsDelete     bool   // If true, this is a deletion instead of an update
}

// UpdateRefs atomically applies one or many updates, with an optional reflog message.
// Supports both updates and deletions via the IsDelete flag.
func (r *runner) UpdateRefs(ctx context.Context, updates []RefUpdate, reflogMessage string) error {
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
		case update.MustNotExist:
			fmt.Fprintf(&stdin, "create %s %s\n", update.RefName, update.NewSHA)
		case update.OldSHA != "":
			fmt.Fprintf(&stdin, "update %s %s %s\n", update.RefName, update.NewSHA, update.OldSHA)
		default:
			fmt.Fprintf(&stdin, "update %s %s\n", update.RefName, update.NewSHA)
		}
	}

	args := []string{"update-ref", "--stdin"}
	if reflogMessage != "" {
		args = append(args, "-m", reflogMessage)
	}
	_, err := r.runStdin(ctx, stdin.String(), args...)
	if err != nil {
		return fmt.Errorf("atomic ref update failed: %w", err)
	}
	for _, update := range updates {
		r.markRefChanged(update.RefName)
	}
	return nil
}

// DeleteRefs atomically deletes multiple references.
func (r *runner) DeleteRefs(ctx context.Context, refNames ...string) error {
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

	_, err := r.runStdin(ctx, stdin.String(), "update-ref", "--stdin")
	if err != nil {
		return fmt.Errorf("atomic ref delete failed: %w", err)
	}
	for _, name := range refNames {
		r.markRefChanged(name)
	}
	return nil
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

	// Environment
	GitVersion(ctx context.Context) (Version, error)

	// Raw command execution
	Run(ctx context.Context, args ...string) (string, error)
	RunRaw(ctx context.Context, args ...string) (string, error)
	RunWithEnv(ctx context.Context, env []string, args ...string) (string, error)
	RunInteractive(ctx context.Context, args ...string) error
	RunGH(ctx context.Context, args ...string) (string, error)

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

	refGenerations sync.Map // ref name -> *atomic.Uint64

	// objects is a persistent git cat-file --batch process for zero-spawn object reads.
	// Started lazily on first use; lives for the lifetime of the runner.
	objects *objectReader

	// Cached git version info. Guarded by a mutex rather than a sync.Once so a
	// failed probe can be retried — see GitVersion.
	gitVersionMu     sync.Mutex
	gitVersion       Version
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

	root, err := resolveToplevel(dir)
	if err != nil {
		return err
	}
	r.repoRoot = root
	r.repoChecked = true
	return nil
}

// toplevelCache memoizes `git rev-parse --show-toplevel` per directory.
//
// The cache is per-runner otherwise, and runners are constructed constantly —
// one per config load, per action, per worktree session — so the same handful
// of directories were re-resolved thousands of times, each costing a git
// process. A directory's toplevel cannot change while the repo is there, and
// the cheap os.Stat below catches the case where it is not.
var toplevelCache sync.Map // dir (abs) -> toplevel

func resolveToplevel(dir string) (string, error) {
	if cached, ok := toplevelCache.Load(dir); ok {
		root := cached.(string)
		// Confirm the repo is still present rather than trusting the memo
		// blindly: a removed worktree must still report "not a git
		// repository". A stat costs microseconds against a ~7ms process.
		if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
			return root, nil
		}
		toplevelCache.Delete(dir)
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository at %s: %w", dir, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("not a git repository at %s: empty toplevel", dir)
	}
	toplevelCache.Store(dir, root)
	return root, nil
}

func (r *runner) InitDefaultRepo() error {
	return r.ensureRepo()
}

func (r *runner) GetRemote() string {
	return r.getRemote()
}

func (r *runner) FetchRemoteShas(ctx context.Context, remote string) (RemoteBranchSHAs, error) {
	return r.fetchRemoteShas(ctx, remote)
}

func (r *runner) GetConfig(key string) (string, error) {
	if err := r.ensureRepo(); err != nil {
		return "", err
	}
	return r.Run(context.Background(), "config", "--get", key)
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
	out, err := r.Run(ctx, "config", "--get-regexp", `branch\..*\.remote`)
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

func (r *runner) GetMergeBase(ctx context.Context, rev1, rev2 string) (string, error) {
	return r.getMergeBaseByRef(ctx, rev1, rev2)
}

func (r *runner) IsAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	return r.isAncestor(ctx, ancestor, descendant)
}

// splitNulTerminated splits NUL-separated git output (as produced by -z) into
// its non-empty records.
func splitNulTerminated(s string) []string {
	var records []string
	for record := range strings.SplitSeq(s, "\x00") {
		if record != "" {
			records = append(records, record)
		}
	}
	return records
}

// for-each-ref field atoms shared by the ref listings.
const (
	atomRefName    = "refname"
	atomObjectName = "objectname"
)

// forEachRef lists the refs matching patterns with one `git for-each-ref` and
// returns, per ref, the values of the requested atoms (e.g. "refname",
// "objectname") in order. An empty atom value stays as an empty field.
func (r *runner) forEachRef(ctx context.Context, atoms []string, patterns ...string) ([][]string, error) {
	format := "--format=%(" + strings.Join(atoms, ")%00%(") + ")"
	out, err := r.Run(ctx, slices.Concat([]string{"for-each-ref", format}, patterns)...)
	if err != nil {
		return nil, err
	}
	var refs [][]string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Split(line, "\x00")
		if len(fields) == len(atoms) {
			refs = append(refs, fields)
		}
	}
	return refs, nil
}

func (r *runner) CheckoutPaths(ctx context.Context, branch string, paths []string) error {
	args := make([]string, 0, 3+len(paths))
	args = append(args, "checkout", branch, "--")
	args = append(args, paths...)
	_, err := r.Run(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to checkout paths from %s: %w", branch, err)
	}
	return nil
}

func (r *runner) RemovePaths(ctx context.Context, paths []string) error {
	args := make([]string, 0, 1+len(paths))
	args = append(args, "rm")
	args = append(args, paths...)
	_, err := r.Run(ctx, args...)
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
	return r.Run(ctx, args...)
}

func (r *runner) GetStatusPorcelain(ctx context.Context) (string, error) {
	return r.RunRaw(ctx, "status", "--porcelain")
}

func (r *runner) GetCommitTemplate(ctx context.Context) (string, error) {
	status, err := r.Run(ctx, "status")
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

// CreateBlobs writes N blobs to the object store in a single
// `git hash-object -w --stdin-paths` invocation. Each content is staged to a
// temp file (so git's path-based hashing can read it) and the SHAs come back
// on stdout in input order.
//
// Empty input does no work. One blob uses stdin directly; larger batches stage
// temporary files. The implementation chooses the fast path for the input size.
func (r *runner) CreateBlobs(ctx context.Context, contents ...string) ([]string, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	if len(contents) == 1 {
		sha, err := r.runStdin(ctx, contents[0], "hash-object", "-w", "--stdin")
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
	out, err := r.runStdin(ctx, stdin, "hash-object", "-w", "--stdin-paths")
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

func (r *runner) ListRefs(prefix string) (map[string]string, error) {
	var patterns []string
	if prefix != "" {
		patterns = append(patterns, prefix)
	}
	refs, err := r.forEachRef(context.Background(), []string{atomRefName, atomObjectName}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("failed to read refs: %w", err)
	}
	result := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref[1] != "" {
			result[ref[0]] = ref[1]
		}
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

func (r *runner) DeleteRemoteMetadataRefs(ctx context.Context, branches ...string) error {
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

	sha, err := One(r.CreateBlobs(ctx, testContent))
	if err != nil {
		return fmt.Errorf("failed to create test blob: %w", err)
	}

	if err := r.UpdateRefs(ctx, []RefUpdate{{RefName: testRef, NewSHA: sha}}, ""); err != nil {
		return fmt.Errorf("failed to update local test ref: %w", err)
	}

	if err := r.pushOriginRefSpecs(ctx, []string{"+" + testRef}); err != nil {
		_ = r.DeleteRefs(ctx, testRef)
		return fmt.Errorf("remote rejected metadata ref push: %w", err)
	}

	_ = r.pushOriginRefSpecs(ctx, []string{":" + testRef})
	_ = r.DeleteRefs(ctx, testRef)

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
	if _, err := r.Run(ctx, args...); err != nil {
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
	if _, err := r.Run(ctx, args...); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	return nil
}

func (r *runner) CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error) {
	commitDiff, err := r.Run(context.Background(), gitCmdDiff, parentSHA, commitSHA)
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

// PatchApplyMode selects how ApplyPatch handles hunks that do not apply
// cleanly.
type PatchApplyMode int

const (
	// PatchApplyStrict fails on any hunk that does not apply cleanly.
	PatchApplyStrict PatchApplyMode = iota
	// PatchApplyThreeWay falls back to a three-way merge (--3way).
	PatchApplyThreeWay
)

func (r *runner) ApplyPatch(ctx context.Context, patchFile string, mode PatchApplyMode) error {
	args := []string{"apply"}
	if mode == PatchApplyThreeWay {
		args = append(args, "--3way")
	}
	args = append(args, patchFile)
	_, err := r.Run(ctx, args...)
	return err
}

func (r *runner) HasUncommittedChanges(ctx context.Context) bool {
	output, err := r.Run(ctx, "status", "--porcelain")
	if err != nil {
		return false
	}
	return strings.TrimSpace(output) != ""
}

// RefGeneration changes after successful ref mutations through this runner.
func (r *runner) RefGeneration(ref string) uint64 {
	value, ok := r.refGenerations.Load(ref)
	if !ok {
		return 0
	}
	return value.(*atomic.Uint64).Load()
}

func (r *runner) markRefChanged(ref string) {
	value, _ := r.refGenerations.LoadOrStore(ref, &atomic.Uint64{})
	value.(*atomic.Uint64).Add(1)
}

// ReadObjects returns content and the ref version from the same cat-file read.
// Missing refs are omitted; empty blobs remain present with their SHA.
func (r *runner) ReadObjects(ctx context.Context, refs ...string) (map[string]BatchObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(refs) == 1 {
		content, sha, found, err := r.objects.ReadObjectWithSHA(refs[0])
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return map[string]BatchObject{refs[0]: {Content: content, SHA: sha}}, nil
	}
	return r.objects.ReadObjectsBatch(refs)
}
