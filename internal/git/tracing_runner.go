package git

import (
	"context"
	"log/slog"
	"time"
)

// TraceLogger is an optional interface for structured trace logging.
// If the logger implements this, traces use structured logging with minimal allocations.
type TraceLogger interface {
	Trace(op string, durationMicros int64, success bool, err error, attrs ...slog.Attr)
}

// tracingRunner wraps a Runner and logs operation traces for performance analysis.
// All operations are delegated to the inner runner after recording timing information.
//
// All methods implement the Runner interface - see interfaces.go for documentation.
type tracingRunner struct {
	inner       Runner
	logger      DebugLogger
	traceLogger TraceLogger // cached type assertion result, may be nil
}

// NewTracingRunner creates a tracing wrapper around the given runner.
// If logger is nil, tracing is disabled but operations still delegate to inner.
func NewTracingRunner(inner Runner, logger DebugLogger) Runner {
	tr := &tracingRunner{inner: inner, logger: logger}
	// Cache the type assertion for TraceLogger
	tr.traceLogger, _ = logger.(TraceLogger)
	return tr
}

// trace logs an operation trace using structured logging if available.
func (t *tracingRunner) trace(op string, duration time.Duration, success bool, err error, attrs ...slog.Attr) {
	if t.traceLogger != nil {
		t.traceLogger.Trace(op, duration.Microseconds(), success, err, attrs...)
	}
	// If no trace logger, silently skip - debug logging happens in the inner runner
}

// RepositoryReader methods

func (t *tracingRunner) GetRemote() string {
	start := time.Now()
	result := t.inner.GetRemote()
	t.trace("GetRemote", time.Since(start), true, nil)
	return result
}

func (t *tracingRunner) GetConfig(key string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetConfig(key)
	t.trace("GetConfig", time.Since(start), err == nil, err, slog.String("key", key))
	return result, err
}

func (t *tracingRunner) GetConfigAll(key string) ([]string, error) {
	start := time.Now()
	result, err := t.inner.GetConfigAll(key)
	t.trace("GetConfigAll", time.Since(start), err == nil, err, slog.String("key", key))
	return result, err
}

func (t *tracingRunner) GetRepoRoot() string {
	// Don't trace - this is just a cached value return
	return t.inner.GetRepoRoot()
}

func (t *tracingRunner) DiscoverRepoRoot() (string, error) {
	start := time.Now()
	result, err := t.inner.DiscoverRepoRoot()
	t.trace("DiscoverRepoRoot", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetGitCommonDir() (string, error) {
	start := time.Now()
	result, err := t.inner.GetGitCommonDir()
	t.trace("GetGitCommonDir", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) IsInsideRepo() bool {
	start := time.Now()
	result := t.inner.IsInsideRepo()
	t.trace("IsInsideRepo", time.Since(start), true, nil)
	return result
}

func (t *tracingRunner) GetUserName(ctx context.Context) (string, error) {
	start := time.Now()
	result, err := t.inner.GetUserName(ctx)
	t.trace("GetUserName", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetRepoInfo(ctx context.Context) (string, string, error) {
	start := time.Now()
	owner, repo, err := t.inner.GetRepoInfo(ctx)
	t.trace("GetRepoInfo", time.Since(start), err == nil, err)
	return owner, repo, err
}

// RepositoryWriter methods

func (t *tracingRunner) InitDefaultRepo() error {
	start := time.Now()
	err := t.inner.InitDefaultRepo()
	t.trace("InitDefaultRepo", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) SetConfig(key, value string) error {
	start := time.Now()
	err := t.inner.SetConfig(key, value)
	t.trace("SetConfig", time.Since(start), err == nil, err, slog.String("key", key))
	return err
}

func (t *tracingRunner) AddConfigValue(key, value string) error {
	start := time.Now()
	err := t.inner.AddConfigValue(key, value)
	t.trace("AddConfigValue", time.Since(start), err == nil, err, slog.String("key", key))
	return err
}

func (t *tracingRunner) EnsureMetadataRefspecConfigured() error {
	start := time.Now()
	err := t.inner.EnsureMetadataRefspecConfigured()
	t.trace("EnsureMetadataRefspecConfigured", time.Since(start), err == nil, err)
	return err
}

// RemoteOperations methods

func (t *tracingRunner) FetchRemoteShas(ctx context.Context, remote string) (map[string]string, error) {
	start := time.Now()
	result, err := t.inner.FetchRemoteShas(ctx, remote)
	t.trace("FetchRemoteShas", time.Since(start), err == nil, err, slog.String("remote", remote))
	return result, err
}

func (t *tracingRunner) GetRemoteSha(remote, branchName string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetRemoteSha(remote, branchName)
	t.trace("GetRemoteSha", time.Since(start), err == nil, err, slog.String("remote", remote), slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) GetRemoteRevision(branchName string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetRemoteRevision(branchName)
	t.trace("GetRemoteRevision", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) FindRemoteBranch(ctx context.Context, remote string) (string, error) {
	start := time.Now()
	result, err := t.inner.FindRemoteBranch(ctx, remote)
	t.trace("FindRemoteBranch", time.Since(start), err == nil, err, slog.String("remote", remote))
	return result, err
}

func (t *tracingRunner) PushBranch(ctx context.Context, branchName, remote string, opts PushOptions) error {
	start := time.Now()
	err := t.inner.PushBranch(ctx, branchName, remote, opts)
	t.trace("PushBranch", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("remote", remote))
	return err
}

func (t *tracingRunner) PullBranch(ctx context.Context, remote, branchName string) (PullResult, error) {
	start := time.Now()
	result, err := t.inner.PullBranch(ctx, remote, branchName)
	t.trace("PullBranch", time.Since(start), err == nil, err, slog.String("remote", remote), slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) Fetch(ctx context.Context, remote, branch string) error {
	start := time.Now()
	err := t.inner.Fetch(ctx, remote, branch)
	t.trace("Fetch", time.Since(start), err == nil, err, slog.String("remote", remote), slog.String("branch", branch))
	return err
}

func (t *tracingRunner) PushMetadataRefs(ctx context.Context, branches []string) error {
	start := time.Now()
	err := t.inner.PushMetadataRefs(ctx, branches)
	t.trace("PushMetadataRefs", time.Since(start), err == nil, err, slog.Int("count", len(branches)))
	return err
}

func (t *tracingRunner) FetchMetadataRefs(ctx context.Context) error {
	start := time.Now()
	err := t.inner.FetchMetadataRefs(ctx)
	t.trace("FetchMetadataRefs", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) DeleteRemoteMetadataRef(ctx context.Context, branch string) error {
	start := time.Now()
	err := t.inner.DeleteRemoteMetadataRef(ctx, branch)
	t.trace("DeleteRemoteMetadataRef", time.Since(start), err == nil, err, slog.String("branch", branch))
	return err
}

func (t *tracingRunner) BatchDeleteRemoteMetadataRefs(ctx context.Context, branches []string) error {
	start := time.Now()
	err := t.inner.BatchDeleteRemoteMetadataRefs(ctx, branches)
	t.trace("BatchDeleteRemoteMetadataRefs", time.Since(start), err == nil, err, slog.Int("count", len(branches)))
	return err
}

func (t *tracingRunner) TestRemoteRefCompatibility() error {
	start := time.Now()
	err := t.inner.TestRemoteRefCompatibility()
	t.trace("TestRemoteRefCompatibility", time.Since(start), err == nil, err)
	return err
}

// BranchReader methods

func (t *tracingRunner) GetCurrentBranch() (string, error) {
	start := time.Now()
	result, err := t.inner.GetCurrentBranch()
	t.trace("GetCurrentBranch", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetAllBranchNames() ([]string, error) {
	start := time.Now()
	result, err := t.inner.GetAllBranchNames()
	t.trace("GetAllBranchNames", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetCurrentBranchOrSHA(ctx context.Context) (string, error) {
	start := time.Now()
	result, err := t.inner.GetCurrentBranchOrSHA(ctx)
	t.trace("GetCurrentBranchOrSHA", time.Since(start), err == nil, err)
	return result, err
}

// BranchWriter methods

func (t *tracingRunner) CheckoutBranch(ctx context.Context, branchName string) error {
	start := time.Now()
	err := t.inner.CheckoutBranch(ctx, branchName)
	t.trace("CheckoutBranch", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) CheckoutBranchForce(ctx context.Context, branchName string) error {
	start := time.Now()
	err := t.inner.CheckoutBranchForce(ctx, branchName)
	t.trace("CheckoutBranchForce", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) CheckoutDetached(ctx context.Context, revision string) error {
	start := time.Now()
	err := t.inner.CheckoutDetached(ctx, revision)
	t.trace("CheckoutDetached", time.Since(start), err == nil, err, slog.String("revision", revision))
	return err
}

func (t *tracingRunner) CreateAndCheckoutBranch(ctx context.Context, branchName string) error {
	start := time.Now()
	err := t.inner.CreateAndCheckoutBranch(ctx, branchName)
	t.trace("CreateAndCheckoutBranch", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) CreateBranch(ctx context.Context, branchName, startPoint string) error {
	start := time.Now()
	err := t.inner.CreateBranch(ctx, branchName, startPoint)
	t.trace("CreateBranch", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("startPoint", startPoint))
	return err
}

func (t *tracingRunner) CreateBranchForce(ctx context.Context, branchName, revision string) error {
	start := time.Now()
	err := t.inner.CreateBranchForce(ctx, branchName, revision)
	t.trace("CreateBranchForce", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("revision", revision))
	return err
}

func (t *tracingRunner) DeleteBranch(ctx context.Context, branchName string) error {
	start := time.Now()
	err := t.inner.DeleteBranch(ctx, branchName)
	t.trace("DeleteBranch", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) RenameBranch(ctx context.Context, oldName, newName string) error {
	start := time.Now()
	err := t.inner.RenameBranch(ctx, oldName, newName)
	t.trace("RenameBranch", time.Since(start), err == nil, err, slog.String("oldName", oldName), slog.String("newName", newName))
	return err
}

func (t *tracingRunner) UpdateBranchRef(ctx context.Context, branchName, revision string) error {
	start := time.Now()
	err := t.inner.UpdateBranchRef(ctx, branchName, revision)
	t.trace("UpdateBranchRef", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("revision", revision))
	return err
}

// CommitReader methods

func (t *tracingRunner) GetRevision(branchName string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetRevision(branchName)
	t.trace("GetRevision", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) GetCurrentRevision(ctx context.Context) (string, error) {
	start := time.Now()
	result, err := t.inner.GetCurrentRevision(ctx)
	t.trace("GetCurrentRevision", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) BatchGetRevisions(branchNames []string) (map[string]string, []error) {
	start := time.Now()
	result, errs := t.inner.BatchGetRevisions(branchNames)
	// Count errors
	errCount := 0
	for _, e := range errs {
		if e != nil {
			errCount++
		}
	}
	t.trace("BatchGetRevisions", time.Since(start), errCount == 0, nil, slog.Int("count", len(branchNames)), slog.Int("errors", errCount))
	return result, errs
}

func (t *tracingRunner) GetCommitDate(branchName string) (time.Time, error) {
	start := time.Now()
	result, err := t.inner.GetCommitDate(branchName)
	t.trace("GetCommitDate", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) GetCommitAuthor(branchName string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetCommitAuthor(branchName)
	t.trace("GetCommitAuthor", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) GetCommitRange(base, head, format string) ([]string, error) {
	start := time.Now()
	result, err := t.inner.GetCommitRange(base, head, format)
	t.trace("GetCommitRange", time.Since(start), err == nil, err, slog.String("base", base), slog.String("head", head))
	return result, err
}

func (t *tracingRunner) GetCommitRangeSHAs(base, head string) ([]string, error) {
	// Don't trace - delegates to GetCommitRange which is traced
	return t.inner.GetCommitRangeSHAs(base, head)
}

func (t *tracingRunner) GetCommitHistorySHAs(branchName string) ([]string, error) {
	// Don't trace - delegates to GetCommitRangeSHAs which delegates to GetCommitRange
	return t.inner.GetCommitHistorySHAs(branchName)
}

func (t *tracingRunner) GetCommitSHA(branchName string, offset int) (string, error) {
	start := time.Now()
	result, err := t.inner.GetCommitSHA(branchName, offset)
	t.trace("GetCommitSHA", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.Int("offset", offset))
	return result, err
}

func (t *tracingRunner) GetCommitLog(sha, format string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetCommitLog(sha, format)
	t.trace("GetCommitLog", time.Since(start), err == nil, err, slog.String("sha", sha))
	return result, err
}

func (t *tracingRunner) GetCommitTemplate(ctx context.Context) (string, error) {
	start := time.Now()
	result, err := t.inner.GetCommitTemplate(ctx)
	t.trace("GetCommitTemplate", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetParentCommitSHA(commitSHA string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetParentCommitSHA(commitSHA)
	t.trace("GetParentCommitSHA", time.Since(start), err == nil, err, slog.String("sha", commitSHA))
	return result, err
}

// DiffOperations methods

func (t *tracingRunner) GetMergeBase(rev1, rev2 string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetMergeBase(rev1, rev2)
	t.trace("GetMergeBase", time.Since(start), err == nil, err, slog.String("rev1", rev1), slog.String("rev2", rev2))
	return result, err
}

func (t *tracingRunner) GetMergeBaseByRef(ref1, ref2 string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetMergeBaseByRef(ref1, ref2)
	t.trace("GetMergeBaseByRef", time.Since(start), err == nil, err, slog.String("ref1", ref1), slog.String("ref2", ref2))
	return result, err
}

func (t *tracingRunner) IsAncestor(ancestor, descendant string) (bool, error) {
	start := time.Now()
	result, err := t.inner.IsAncestor(ancestor, descendant)
	t.trace("IsAncestor", time.Since(start), err == nil, err, slog.String("ancestor", ancestor), slog.String("descendant", descendant))
	return result, err
}

func (t *tracingRunner) IsMerged(ctx context.Context, branchName, target string) (bool, error) {
	start := time.Now()
	result, err := t.inner.IsMerged(ctx, branchName, target)
	t.trace("IsMerged", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("target", target))
	return result, err
}

func (t *tracingRunner) GetMergedBranches(ctx context.Context, target string) (map[string]bool, error) {
	start := time.Now()
	result, err := t.inner.GetMergedBranches(ctx, target)
	t.trace("GetMergedBranches", time.Since(start), err == nil, err, slog.String("target", target))
	return result, err
}

func (t *tracingRunner) IsDiffEmpty(ctx context.Context, branchName, base string) (bool, error) {
	start := time.Now()
	result, err := t.inner.IsDiffEmpty(ctx, branchName, base)
	t.trace("IsDiffEmpty", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("base", base))
	return result, err
}

func (t *tracingRunner) GetChangedFiles(ctx context.Context, base, head string) ([]string, error) {
	start := time.Now()
	result, err := t.inner.GetChangedFiles(ctx, base, head)
	t.trace("GetChangedFiles", time.Since(start), err == nil, err, slog.String("base", base), slog.String("head", head))
	return result, err
}

func (t *tracingRunner) ShowDiff(ctx context.Context, left, right string, stat bool) (string, error) {
	start := time.Now()
	result, err := t.inner.ShowDiff(ctx, left, right, stat)
	t.trace("ShowDiff", time.Since(start), err == nil, err, slog.String("left", left), slog.String("right", right))
	return result, err
}

func (t *tracingRunner) ShowCommits(ctx context.Context, base, head string, patch, stat bool) (string, error) {
	start := time.Now()
	result, err := t.inner.ShowCommits(ctx, base, head, patch, stat)
	t.trace("ShowCommits", time.Since(start), err == nil, err, slog.String("base", base), slog.String("head", head))
	return result, err
}

func (t *tracingRunner) GetDiffNumstat(base, head string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetDiffNumstat(base, head)
	t.trace("GetDiffNumstat", time.Since(start), err == nil, err, slog.String("base", base), slog.String("head", head))
	return result, err
}

func (t *tracingRunner) GetStagedDiff(ctx context.Context, files ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetStagedDiff(ctx, files...)
	t.trace("GetStagedDiff", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetUnstagedDiff(ctx context.Context, files ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetUnstagedDiff(ctx, files...)
	t.trace("GetUnstagedDiff", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetDiffBetween(ctx context.Context, base, head string, files ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetDiffBetween(ctx, base, head, files...)
	t.trace("GetDiffBetween", time.Since(start), err == nil, err)
	return result, err
}

// StagingOperations methods

func (t *tracingRunner) StageAll(ctx context.Context) error {
	start := time.Now()
	err := t.inner.StageAll(ctx)
	t.trace("StageAll", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) StagePatch(ctx context.Context) error {
	start := time.Now()
	err := t.inner.StagePatch(ctx)
	t.trace("StagePatch", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) StageTracked(ctx context.Context) error {
	start := time.Now()
	err := t.inner.StageTracked(ctx)
	t.trace("StageTracked", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) AddAll(ctx context.Context) error {
	// Don't trace - delegates to StageAll which is traced
	return t.inner.AddAll(ctx)
}

func (t *tracingRunner) StageChanges(ctx context.Context, opts StagingOptions) error {
	start := time.Now()
	err := t.inner.StageChanges(ctx, opts)
	t.trace("StageChanges", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) HasStagedChanges(ctx context.Context) (bool, error) {
	start := time.Now()
	result, err := t.inner.HasStagedChanges(ctx)
	t.trace("HasStagedChanges", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) HasUnstagedChanges(ctx context.Context) (bool, error) {
	start := time.Now()
	result, err := t.inner.HasUnstagedChanges(ctx)
	t.trace("HasUnstagedChanges", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) HasUntrackedFiles(ctx context.Context) (bool, error) {
	start := time.Now()
	result, err := t.inner.HasUntrackedFiles(ctx)
	t.trace("HasUntrackedFiles", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetUntrackedFiles(ctx context.Context) ([]string, error) {
	start := time.Now()
	result, err := t.inner.GetUntrackedFiles(ctx)
	t.trace("GetUntrackedFiles", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) ParseStagedHunks(ctx context.Context) ([]Hunk, error) {
	start := time.Now()
	result, err := t.inner.ParseStagedHunks(ctx)
	t.trace("ParseStagedHunks", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) StageHunks(ctx context.Context, hunks []Hunk) error {
	start := time.Now()
	err := t.inner.StageHunks(ctx, hunks)
	t.trace("StageHunks", time.Since(start), err == nil, err, slog.Int("count", len(hunks)))
	return err
}

func (t *tracingRunner) UnstageAll(ctx context.Context) error {
	start := time.Now()
	err := t.inner.UnstageAll(ctx)
	t.trace("UnstageAll", time.Since(start), err == nil, err)
	return err
}

// CommitWriter methods

func (t *tracingRunner) Commit(message string, verbose int, noVerify bool) error {
	// Don't trace - delegates to CommitWithOptions which is traced
	return t.inner.Commit(message, verbose, noVerify)
}

func (t *tracingRunner) CommitWithOptions(opts CommitOptions) error {
	start := time.Now()
	err := t.inner.CommitWithOptions(opts)
	t.trace("CommitWithOptions", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) CommitAmendNoEdit(ctx context.Context) error {
	start := time.Now()
	err := t.inner.CommitAmendNoEdit(ctx)
	t.trace("CommitAmendNoEdit", time.Since(start), err == nil, err)
	return err
}

// RebaseOperations methods

func (t *tracingRunner) Rebase(ctx context.Context, branchName, upstream, oldUpstream string) (RebaseResult, error) {
	start := time.Now()
	result, err := t.inner.Rebase(ctx, branchName, upstream, oldUpstream)
	t.trace("Rebase", time.Since(start), err == nil, err, slog.String("branch", branchName), slog.String("upstream", upstream))
	return result, err
}

func (t *tracingRunner) RebaseContinue(ctx context.Context) (RebaseResult, error) {
	start := time.Now()
	result, err := t.inner.RebaseContinue(ctx)
	t.trace("RebaseContinue", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) RebaseContinueNoEdit(ctx context.Context) (RebaseResult, error) {
	start := time.Now()
	result, err := t.inner.RebaseContinueNoEdit(ctx)
	t.trace("RebaseContinueNoEdit", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) RebaseAbort(ctx context.Context) error {
	start := time.Now()
	err := t.inner.RebaseAbort(ctx)
	t.trace("RebaseAbort", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) InteractiveRebase(ctx context.Context, onto string) error {
	start := time.Now()
	err := t.inner.InteractiveRebase(ctx, onto)
	t.trace("InteractiveRebase", time.Since(start), err == nil, err, slog.String("onto", onto))
	return err
}

func (t *tracingRunner) IsRebaseInProgress(ctx context.Context) bool {
	start := time.Now()
	result := t.inner.IsRebaseInProgress(ctx)
	t.trace("IsRebaseInProgress", time.Since(start), true, nil)
	return result
}

func (t *tracingRunner) GetRebaseHead() (string, error) {
	start := time.Now()
	result, err := t.inner.GetRebaseHead()
	t.trace("GetRebaseHead", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) CheckRebaseInProgress(ctx context.Context) error {
	start := time.Now()
	err := t.inner.CheckRebaseInProgress(ctx)
	t.trace("CheckRebaseInProgress", time.Since(start), err == nil, err)
	return err
}

// MergeOperations methods

func (t *tracingRunner) Merge(ctx context.Context, branchName string, opts MergeOptions) error {
	start := time.Now()
	err := t.inner.Merge(ctx, branchName, opts)
	t.trace("Merge", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) MergeMultiple(ctx context.Context, branches []string, opts MergeOptions) error {
	start := time.Now()
	err := t.inner.MergeMultiple(ctx, branches, opts)
	t.trace("MergeMultiple", time.Since(start), err == nil, err, slog.Int("count", len(branches)))
	return err
}

func (t *tracingRunner) IsMergeInProgress(ctx context.Context) bool {
	start := time.Now()
	result := t.inner.IsMergeInProgress(ctx)
	t.trace("IsMergeInProgress", time.Since(start), true, nil)
	return result
}

func (t *tracingRunner) MergeAbort(ctx context.Context) error {
	start := time.Now()
	err := t.inner.MergeAbort(ctx)
	t.trace("MergeAbort", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) GetUnmergedFiles(ctx context.Context) ([]string, error) {
	start := time.Now()
	result, err := t.inner.GetUnmergedFiles(ctx)
	t.trace("GetUnmergedFiles", time.Since(start), err == nil, err)
	return result, err
}

// CherryPickOperations methods

func (t *tracingRunner) CherryPick(ctx context.Context, commitSHA, onto string) (string, error) {
	start := time.Now()
	result, err := t.inner.CherryPick(ctx, commitSHA, onto)
	t.trace("CherryPick", time.Since(start), err == nil, err, slog.String("sha", commitSHA), slog.String("onto", onto))
	return result, err
}

func (t *tracingRunner) CherryPickSimple(ctx context.Context, commitSHA string) error {
	start := time.Now()
	err := t.inner.CherryPickSimple(ctx, commitSHA)
	t.trace("CherryPickSimple", time.Since(start), err == nil, err, slog.String("sha", commitSHA))
	return err
}

func (t *tracingRunner) CherryPickAbort(ctx context.Context) error {
	start := time.Now()
	err := t.inner.CherryPickAbort(ctx)
	t.trace("CherryPickAbort", time.Since(start), err == nil, err)
	return err
}

// StashOperations methods

func (t *tracingRunner) StashPush(ctx context.Context, message string) (string, error) {
	start := time.Now()
	result, err := t.inner.StashPush(ctx, message)
	t.trace("StashPush", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) StashPushStaged(ctx context.Context, message string) (string, error) {
	start := time.Now()
	result, err := t.inner.StashPushStaged(ctx, message)
	t.trace("StashPushStaged", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) StashPop(ctx context.Context) error {
	start := time.Now()
	err := t.inner.StashPop(ctx)
	t.trace("StashPop", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) ListStash(ctx context.Context) (string, error) {
	start := time.Now()
	result, err := t.inner.ListStash(ctx)
	t.trace("ListStash", time.Since(start), err == nil, err)
	return result, err
}

// ResetOperations methods

func (t *tracingRunner) HardReset(ctx context.Context, revision string) error {
	start := time.Now()
	err := t.inner.HardReset(ctx, revision)
	t.trace("HardReset", time.Since(start), err == nil, err, slog.String("revision", revision))
	return err
}

func (t *tracingRunner) ResetMerge(ctx context.Context, revision string) error {
	start := time.Now()
	err := t.inner.ResetMerge(ctx, revision)
	t.trace("ResetMerge", time.Since(start), err == nil, err, slog.String("revision", revision))
	return err
}

func (t *tracingRunner) SoftReset(ctx context.Context, revision string) error {
	start := time.Now()
	err := t.inner.SoftReset(ctx, revision)
	t.trace("SoftReset", time.Since(start), err == nil, err, slog.String("revision", revision))
	return err
}

func (t *tracingRunner) MixedReset(ctx context.Context, revision string) error {
	start := time.Now()
	err := t.inner.MixedReset(ctx, revision)
	t.trace("MixedReset", time.Since(start), err == nil, err, slog.String("revision", revision))
	return err
}

// PathOperations methods

func (t *tracingRunner) CheckoutPaths(ctx context.Context, branch string, paths []string) error {
	start := time.Now()
	err := t.inner.CheckoutPaths(ctx, branch, paths)
	t.trace("CheckoutPaths", time.Since(start), err == nil, err, slog.String("branch", branch), slog.Int("count", len(paths)))
	return err
}

func (t *tracingRunner) RemovePaths(ctx context.Context, paths []string) error {
	start := time.Now()
	err := t.inner.RemovePaths(ctx, paths)
	t.trace("RemovePaths", time.Since(start), err == nil, err, slog.Int("count", len(paths)))
	return err
}

// PatchOperations methods

func (t *tracingRunner) ApplyPatch(ctx context.Context, patchFile string, threeWay bool) error {
	start := time.Now()
	err := t.inner.ApplyPatch(ctx, patchFile, threeWay)
	t.trace("ApplyPatch", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) CheckCommutation(hunk Hunk, commitSHA, parentSHA string) (bool, error) {
	start := time.Now()
	result, err := t.inner.CheckCommutation(hunk, commitSHA, parentSHA)
	t.trace("CheckCommutation", time.Since(start), err == nil, err, slog.String("sha", commitSHA))
	return result, err
}

// WorktreeOperations methods

func (t *tracingRunner) AddWorktree(ctx context.Context, path string, branch string, detach bool) error {
	// Don't trace - delegates to AddWorktreeWithOptions which is traced
	return t.inner.AddWorktree(ctx, path, branch, detach)
}

func (t *tracingRunner) AddWorktreeWithOptions(ctx context.Context, path string, branch string, detach bool, noCheckout bool) error {
	start := time.Now()
	err := t.inner.AddWorktreeWithOptions(ctx, path, branch, detach, noCheckout)
	t.trace("AddWorktreeWithOptions", time.Since(start), err == nil, err, slog.String("branch", branch))
	return err
}

func (t *tracingRunner) RemoveWorktree(ctx context.Context, path string) error {
	start := time.Now()
	err := t.inner.RemoveWorktree(ctx, path)
	t.trace("RemoveWorktree", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) ListWorktrees(ctx context.Context) ([]string, error) {
	start := time.Now()
	result, err := t.inner.ListWorktrees(ctx)
	t.trace("ListWorktrees", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) PruneWorktrees(ctx context.Context) error {
	start := time.Now()
	err := t.inner.PruneWorktrees(ctx)
	t.trace("PruneWorktrees", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) GetWorktreePathForBranch(ctx context.Context, branchName string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetWorktreePathForBranch(ctx, branchName)
	t.trace("GetWorktreePathForBranch", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) GetWorktreeCurrentBranch(ctx context.Context, worktreePath string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetWorktreeCurrentBranch(ctx, worktreePath)
	t.trace("GetWorktreeCurrentBranch", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) ResetWorktreeWorkingDir(ctx context.Context, worktreePath string) error {
	start := time.Now()
	err := t.inner.ResetWorktreeWorkingDir(ctx, worktreePath)
	t.trace("ResetWorktreeWorkingDir", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) WorktreeHasUncommittedChanges(ctx context.Context, worktreePath string) (bool, error) {
	start := time.Now()
	result, err := t.inner.WorktreeHasUncommittedChanges(ctx, worktreePath)
	t.trace("WorktreeHasUncommittedChanges", time.Since(start), err == nil, err)
	return result, err
}

// WorktreeRegistryOperations methods

func (t *tracingRunner) ReadWorktreeMeta(stackRoot string) (*WorktreeMeta, error) {
	start := time.Now()
	result, err := t.inner.ReadWorktreeMeta(stackRoot)
	t.trace("ReadWorktreeMeta", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) WriteWorktreeMeta(stackRoot string, meta *WorktreeMeta) error {
	start := time.Now()
	err := t.inner.WriteWorktreeMeta(stackRoot, meta)
	t.trace("WriteWorktreeMeta", time.Since(start), err == nil, err)
	return err
}

func (t *tracingRunner) DeleteWorktreeMeta(stackRoot string) error {
	// Don't trace - delegates to DeleteRef which is traced
	return t.inner.DeleteWorktreeMeta(stackRoot)
}

func (t *tracingRunner) ListWorktreeMetas() (map[string]*WorktreeMeta, error) {
	start := time.Now()
	result, err := t.inner.ListWorktreeMetas()
	t.trace("ListWorktreeMetas", time.Since(start), err == nil, err)
	return result, err
}

// StatusOperations methods

func (t *tracingRunner) GetStatusPorcelain(ctx context.Context) (string, error) {
	start := time.Now()
	result, err := t.inner.GetStatusPorcelain(ctx)
	t.trace("GetStatusPorcelain", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetReflog(ctx context.Context, count int, format string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetReflog(ctx, count, format)
	t.trace("GetReflog", time.Since(start), err == nil, err, slog.Int("count", count))
	return result, err
}

func (t *tracingRunner) HasUncommittedChanges(ctx context.Context) bool {
	start := time.Now()
	result := t.inner.HasUncommittedChanges(ctx)
	t.trace("HasUncommittedChanges", time.Since(start), true, nil)
	return result
}

// RefOperations methods

func (t *tracingRunner) GetRef(name string) (string, error) {
	start := time.Now()
	result, err := t.inner.GetRef(name)
	t.trace("GetRef", time.Since(start), err == nil, err, slog.String("ref", name))
	return result, err
}

func (t *tracingRunner) UpdateRef(name, sha string) error {
	start := time.Now()
	err := t.inner.UpdateRef(name, sha)
	t.trace("UpdateRef", time.Since(start), err == nil, err, slog.String("ref", name))
	return err
}

func (t *tracingRunner) UpdateRefWithLog(ctx context.Context, refName, sha, message string) error {
	start := time.Now()
	err := t.inner.UpdateRefWithLog(ctx, refName, sha, message)
	t.trace("UpdateRefWithLog", time.Since(start), err == nil, err, slog.String("ref", refName))
	return err
}

func (t *tracingRunner) UpdateRefsBatch(ctx context.Context, updates []RefUpdate) error {
	start := time.Now()
	err := t.inner.UpdateRefsBatch(ctx, updates)
	t.trace("UpdateRefsBatch", time.Since(start), err == nil, err, slog.Int("count", len(updates)))
	return err
}

func (t *tracingRunner) UpdateRefsBatchWithLog(ctx context.Context, updates []RefUpdate, reflogMessage string) error {
	start := time.Now()
	err := t.inner.UpdateRefsBatchWithLog(ctx, updates, reflogMessage)
	t.trace("UpdateRefsBatchWithLog", time.Since(start), err == nil, err, slog.Int("count", len(updates)))
	return err
}

func (t *tracingRunner) DeleteRefsBatch(ctx context.Context, refNames []string) error {
	start := time.Now()
	err := t.inner.DeleteRefsBatch(ctx, refNames)
	t.trace("DeleteRefsBatch", time.Since(start), err == nil, err, slog.Int("count", len(refNames)))
	return err
}

func (t *tracingRunner) VerifyRef(ctx context.Context, refName string) error {
	start := time.Now()
	err := t.inner.VerifyRef(ctx, refName)
	t.trace("VerifyRef", time.Since(start), err == nil, err, slog.String("ref", refName))
	return err
}

func (t *tracingRunner) DeleteRef(name string) error {
	start := time.Now()
	err := t.inner.DeleteRef(name)
	t.trace("DeleteRef", time.Since(start), err == nil, err, slog.String("ref", name))
	return err
}

func (t *tracingRunner) ListRefs(prefix string) (map[string]string, error) {
	start := time.Now()
	result, err := t.inner.ListRefs(prefix)
	t.trace("ListRefs", time.Since(start), err == nil, err, slog.String("prefix", prefix))
	return result, err
}

// ObjectOperations methods

func (t *tracingRunner) CreateBlob(content string) (string, error) {
	start := time.Now()
	result, err := t.inner.CreateBlob(content)
	t.trace("CreateBlob", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) ReadBlob(sha string) (string, error) {
	// Don't trace - delegates to CatFile which is traced
	return t.inner.ReadBlob(sha)
}

func (t *tracingRunner) CatFile(sha string) (string, error) {
	start := time.Now()
	result, err := t.inner.CatFile(sha)
	t.trace("CatFile", time.Since(start), err == nil, err, slog.String("sha", sha))
	return result, err
}

// MetadataOperations methods

func (t *tracingRunner) ReadMetadata(branchName string) (*Meta, error) {
	start := time.Now()
	result, err := t.inner.ReadMetadata(branchName)
	t.trace("ReadMetadata", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) BatchReadMetadata(branchNames []string) (map[string]*Meta, map[string]error) {
	start := time.Now()
	result, errs := t.inner.BatchReadMetadata(branchNames)
	errCount := len(errs)
	t.trace("BatchReadMetadata", time.Since(start), errCount == 0, nil, slog.Int("count", len(branchNames)), slog.Int("errors", errCount))
	return result, errs
}

func (t *tracingRunner) WriteMetadata(branchName string, meta *Meta) error {
	start := time.Now()
	err := t.inner.WriteMetadata(branchName, meta)
	t.trace("WriteMetadata", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) DeleteMetadata(branchName string) error {
	// Don't trace - delegates to DeleteRef which is traced
	return t.inner.DeleteMetadata(branchName)
}

func (t *tracingRunner) RenameMetadata(oldName, newName string) error {
	start := time.Now()
	err := t.inner.RenameMetadata(oldName, newName)
	t.trace("RenameMetadata", time.Since(start), err == nil, err, slog.String("oldName", oldName), slog.String("newName", newName))
	return err
}

func (t *tracingRunner) ListMetadata() (map[string]string, error) {
	start := time.Now()
	result, err := t.inner.ListMetadata()
	t.trace("ListMetadata", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) ReadLocalMetadata(branchName string) (*LocalMeta, error) {
	start := time.Now()
	result, err := t.inner.ReadLocalMetadata(branchName)
	t.trace("ReadLocalMetadata", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return result, err
}

func (t *tracingRunner) BatchReadLocalMetadata(branchNames []string) map[string]*LocalMeta {
	start := time.Now()
	result := t.inner.BatchReadLocalMetadata(branchNames)
	t.trace("BatchReadLocalMetadata", time.Since(start), true, nil, slog.Int("count", len(branchNames)))
	return result
}

func (t *tracingRunner) WriteLocalMetadata(branchName string, meta *LocalMeta) error {
	start := time.Now()
	err := t.inner.WriteLocalMetadata(branchName, meta)
	t.trace("WriteLocalMetadata", time.Since(start), err == nil, err, slog.String("branch", branchName))
	return err
}

func (t *tracingRunner) WriteMetadataBlob(meta *Meta) (string, error) {
	start := time.Now()
	result, err := t.inner.WriteMetadataBlob(meta)
	t.trace("WriteMetadataBlob", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) WriteLocalMetadataBlob(meta *LocalMeta) (string, error) {
	start := time.Now()
	result, err := t.inner.WriteLocalMetadataBlob(meta)
	t.trace("WriteLocalMetadataBlob", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetMetadataRefSHA(branchName string) string {
	// Don't trace - this is just a ref lookup, very fast
	return t.inner.GetMetadataRefSHA(branchName)
}

func (t *tracingRunner) GetLocalMetadataRefSHA(branchName string) string {
	// Don't trace - this is just a ref lookup, very fast
	return t.inner.GetLocalMetadataRefSHA(branchName)
}

// Raw command execution methods

// cmdName returns the command name with optional first argument for tracing.
func cmdName(base string, args []string) string {
	if len(args) > 0 {
		return base + " " + args[0]
	}
	return base
}

func (t *tracingRunner) RunGitCommandWithContext(ctx context.Context, args ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.RunGitCommandWithContext(ctx, args...)
	t.trace("RunGitCommandWithContext", time.Since(start), err == nil, err, slog.String("cmd", cmdName("git", args)))
	return result, err
}

func (t *tracingRunner) RunGitCommandRawWithContext(ctx context.Context, args ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.RunGitCommandRawWithContext(ctx, args...)
	t.trace("RunGitCommandRawWithContext", time.Since(start), err == nil, err, slog.String("cmd", cmdName("git", args)))
	return result, err
}

func (t *tracingRunner) RunGitCommandWithEnv(ctx context.Context, env []string, args ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.RunGitCommandWithEnv(ctx, env, args...)
	t.trace("RunGitCommandWithEnv", time.Since(start), err == nil, err, slog.String("cmd", cmdName("git", args)))
	return result, err
}

func (t *tracingRunner) RunGitCommandInteractive(args ...string) error {
	start := time.Now()
	err := t.inner.RunGitCommandInteractive(args...)
	t.trace("RunGitCommandInteractive", time.Since(start), err == nil, err, slog.String("cmd", cmdName("git", args)))
	return err
}

func (t *tracingRunner) RunGHCommandWithContext(ctx context.Context, args ...string) (string, error) {
	start := time.Now()
	result, err := t.inner.RunGHCommandWithContext(ctx, args...)
	t.trace("RunGHCommandWithContext", time.Since(start), err == nil, err, slog.String("cmd", cmdName("gh", args)))
	return result, err
}

// Logging method - delegate without tracing

func (t *tracingRunner) SetLogger(logger DebugLogger) {
	t.inner.SetLogger(logger)
}

// StackMetadataOperations methods

func (t *tracingRunner) ReadStackMeta(stackID string) (*StackMeta, error) {
	start := time.Now()
	result, err := t.inner.ReadStackMeta(stackID)
	t.trace("ReadStackMeta", time.Since(start), err == nil, err, slog.String("stackID", stackID))
	return result, err
}

func (t *tracingRunner) WriteStackMeta(stackID string, meta *StackMeta) error {
	start := time.Now()
	err := t.inner.WriteStackMeta(stackID, meta)
	t.trace("WriteStackMeta", time.Since(start), err == nil, err, slog.String("stackID", stackID))
	return err
}

func (t *tracingRunner) DeleteStackMeta(stackID string) error {
	// Don't trace - delegates to DeleteRef which is traced
	return t.inner.DeleteStackMeta(stackID)
}

func (t *tracingRunner) ListStackMetas() (map[string]string, error) {
	start := time.Now()
	result, err := t.inner.ListStackMetas()
	t.trace("ListStackMetas", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) WriteStackMetaBlob(meta *StackMeta) (string, error) {
	start := time.Now()
	result, err := t.inner.WriteStackMetaBlob(meta)
	t.trace("WriteStackMetaBlob", time.Since(start), err == nil, err)
	return result, err
}

func (t *tracingRunner) GetStackMetaRefSHA(stackID string) string {
	// Don't trace - this is just a ref lookup, very fast
	return t.inner.GetStackMetaRefSHA(stackID)
}
