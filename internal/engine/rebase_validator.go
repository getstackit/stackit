package engine

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/getstackit/stackit/internal/git"
)

// ValidationErrorType distinguishes between conflict errors and system errors
type ValidationErrorType int

const (
	// ValidationErrorNone indicates no error occurred
	ValidationErrorNone ValidationErrorType = iota
	// ValidationErrorConflict indicates a merge conflict occurred
	ValidationErrorConflict
	// ValidationErrorSystem indicates a system error (not a conflict)
	ValidationErrorSystem
)

// getMaxConcurrency returns the maximum number of concurrent validations.
// Uses the configured maxConcurrency if set, otherwise defaults to min(NumCPU, 8).
func (e *engineImpl) getMaxConcurrency() int {
	if e.maxConcurrency > 0 {
		return e.maxConcurrency
	}
	// Default to number of CPUs, capped at 8 to avoid creating too many worktrees
	cpus := runtime.NumCPU()
	if cpus > 8 {
		return 8
	}
	if cpus < 1 {
		return 1
	}
	return cpus
}

// RebaseSpec describes a planned rebase operation
type RebaseSpec struct {
	Branch      string // Branch to rebase
	NewParent   string // New upstream to rebase onto
	OldUpstream string // Current base to replay commits from
}

// RebaseValidation is the result of dry-run validation
type RebaseValidation struct {
	Success          bool                // Whether all rebases would succeed
	FailedBranch     string              // Which branch caused the conflict (if any)
	ErrorType        ValidationErrorType // Type of error (conflict vs system error)
	ErrorMessage     string              // Error message describing the failure
	ConflictingFiles []string            // Files that have conflicts (if ErrorType is ValidationErrorConflict)
	NewSHAs          map[string]string   // Branch -> resulting SHA after rebase (if successful)
	RerereResolved   map[string]int      // Branch -> number of conflicts auto-resolved by rerere during validation
}

// ValidateRebases tests if a sequence of rebases will succeed by performing them
// in isolated temporary worktrees. This allows checking for conflicts before
// modifying any state in the main repository.
//
// IMPORTANT: This uses dry-run rebases that do NOT update branch refs, keeping
// the main repository completely unmodified.
//
// Returns a RebaseValidation indicating success or the first failure encountered.
// Worktrees are cleaned up automatically regardless of outcome.
//
// Uses parallel validation for improved performance on wide stacks. Branches at
// the same depth are validated concurrently, providing 2-3x speedup for stacks
// with many sibling branches.
func (e *engineImpl) ValidateRebases(ctx context.Context, specs []RebaseSpec) (*RebaseValidation, error) {
	return e.ValidateRebasesParallel(ctx, specs)
}

// dryRunRebase performs a rebase without updating branch refs.
// This allows testing if a rebase would succeed without modifying the repository.
// Returns the rebase result, the new SHA (if successful), conflicting files (if any), and any error.
func dryRunRebase(ctx context.Context, g git.Runner, branchName, upstream, oldUpstream string) (git.RebaseOutcome, string, []string, error) {
	outcome := git.RebaseOutcome{Result: git.RebaseDone}

	// Perform rebase in detached HEAD mode using branchName~0.
	// The ~0 suffix resolves to the same commit as branchName but tells git to check out
	// the commit directly (detached HEAD) rather than the branch ref. This means the rebase
	// results stay on the detached HEAD and the actual branch ref remains unchanged,
	// keeping the main repository unmodified during validation.
	_, err := g.RunGitCommandWithContext(ctx, "rebase", "--onto", upstream, oldUpstream, branchName+"~0")
	if err != nil {
		if g.IsRebaseInProgress(ctx) {
			autoOutcome, conflictFiles, autoErr := git.AutoContinueRerereRebase(ctx, g, err)
			if autoErr != nil || autoOutcome.Result == git.RebaseConflict {
				return autoOutcome, "", conflictFiles, autoErr
			}
			outcome = autoOutcome
		} else {
			// Abort rebase if it failed for other reasons
			_, _ = g.RunGitCommandWithContext(ctx, "rebase", "--abort")
			return git.RebaseOutcome{Result: git.RebaseConflict}, "", nil, err
		}
	}

	newSHA, err := g.GetCurrentRevision(ctx)
	if err != nil {
		return git.RebaseOutcome{Result: git.RebaseConflict, RerereResolvedCount: outcome.RerereResolvedCount}, "", nil, fmt.Errorf("failed to get revision after rebase: %w", err)
	}

	// DO NOT update the branch ref - this is the key difference from normal Rebase
	// The branch ref stays unchanged, keeping the main repo unmodified

	return outcome, newSHA, nil, nil
}

// validationLevel represents a group of specs that can be validated in parallel
type validationLevel struct {
	depth int
	specs []RebaseSpec
}

// groupSpecsByDepth organizes specs into levels based on their dependency depth.
// Specs at the same depth can be validated in parallel since they're independent.
func (e *engineImpl) groupSpecsByDepth(specs []RebaseSpec) []validationLevel {
	if len(specs) == 0 {
		return nil
	}

	// Build a graph to understand branch relationships
	graph := e.Graph(SortStrategySmart)

	// Group specs by their depth in the stack
	specsByDepth := make(map[int][]RebaseSpec)
	for _, spec := range specs {
		node := graph.GetNode(spec.Branch)
		if node == nil {
			// Branch not in graph, treat as depth 0
			specsByDepth[0] = append(specsByDepth[0], spec)
			continue
		}
		specsByDepth[node.Depth] = append(specsByDepth[node.Depth], spec)
	}

	// Convert to sorted levels
	var levels []validationLevel
	for depth := 0; depth <= maxDepth(specsByDepth); depth++ {
		if specs, ok := specsByDepth[depth]; ok && len(specs) > 0 {
			levels = append(levels, validationLevel{
				depth: depth,
				specs: specs,
			})
		}
	}

	return levels
}

// maxDepth finds the maximum depth in the map
func maxDepth(m map[int][]RebaseSpec) int {
	maxVal := 0
	for depth := range m {
		if depth > maxVal {
			maxVal = depth
		}
	}
	return maxVal
}

// validationResult holds the result of validating a single spec
type validationResult struct {
	spec           RebaseSpec
	success        bool
	newSHA         string
	errorMessage   string
	errorType      ValidationErrorType
	oldSHA         string
	conflictFiles  []string
	rerereResolved int
}

// ValidateRebasesParallel validates rebases in parallel where possible.
// Branches at the same depth in the stack (independent siblings) are validated concurrently.
// This can provide significant speedup for wide stacks with many branches at the same level.
//
// The function respects a maximum concurrency limit to avoid creating too many worktrees.
// Results are tracked thread-safely across parallel validations.
func (e *engineImpl) ValidateRebasesParallel(ctx context.Context, specs []RebaseSpec) (*RebaseValidation, error) {
	if len(specs) == 0 {
		return &RebaseValidation{Success: true, NewSHAs: map[string]string{}, RerereResolved: map[string]int{}}, nil
	}

	// Prune stale worktree entries ONCE before starting parallel validation.
	// This cleans up entries in .git/worktrees/ that may be left behind from
	// incomplete cleanup after failed, canceled, or crashed operations.
	// We do this before any parallel worktree creation to avoid race conditions
	// where a prune call could interfere with a worktree being created by another goroutine.
	_ = e.git.PruneWorktrees(ctx)

	// Validate specs for duplicate branches (programming error, but check anyway)
	seenBranches := make(map[string]bool)
	for _, spec := range specs {
		if seenBranches[spec.Branch] {
			return nil, fmt.Errorf("duplicate branch in specs: %s", spec.Branch)
		}
		seenBranches[spec.Branch] = true
	}

	// Group specs by dependency depth
	levels := e.groupSpecsByDepth(specs)

	result := &RebaseValidation{
		Success:        true,
		NewSHAs:        make(map[string]string),
		RerereResolved: make(map[string]int),
	}

	// Thread-safe maps for tracking rebased SHAs across parallel executions
	rebasedByName := &sync.Map{} // branch name -> new SHA
	rebasedBySHA := &sync.Map{}  // old SHA -> new SHA

	// Maximum number of concurrent worktrees
	maxConcurrency := e.getMaxConcurrency()

	// Process each level sequentially (levels must wait for prior levels)
	for _, level := range levels {
		// Process this level and check for failures
		failed := e.processValidationLevel(ctx, level, maxConcurrency, result, rebasedByName, rebasedBySHA)
		if failed {
			return result, nil
		}
	}

	return result, nil
}

// processValidationLevel processes all specs at a given depth level in parallel.
// Returns true if any validation failed, false if all succeeded.
//
// Siblings at the same depth are independent: one failing does not invalidate
// the others. We let every sibling run to completion so that a failed sibling
// does not cancel an in-flight successful one — that race would silently drop
// the survivor from NewSHAs and prevent it from being restacked. Deeper levels
// are still skipped because the caller stops iterating after the first failed
// level.
func (e *engineImpl) processValidationLevel(
	ctx context.Context,
	level validationLevel,
	maxConcurrency int,
	result *RebaseValidation,
	rebasedByName *sync.Map,
	rebasedBySHA *sync.Map,
) bool {
	// Within each level, validate specs in parallel
	semaphore := make(chan struct{}, maxConcurrency)
	results := make(chan validationResult, len(level.specs))
	var wg sync.WaitGroup

	for _, spec := range level.specs {
		wg.Add(1)
		go func(spec RebaseSpec) {
			// Panic recovery at outermost level to ensure cleanup always happens
			defer func() {
				if r := recover(); r != nil {
					results <- validationResult{
						spec:         spec,
						success:      false,
						errorMessage: fmt.Sprintf("panic during validation: %v", r),
						errorType:    ValidationErrorSystem,
					}
				}
			}()
			defer wg.Done()

			// Check parent context before acquiring semaphore so an outer cancel
			// (e.g. user Ctrl+C) short-circuits queued siblings.
			select {
			case <-ctx.Done():
				results <- validationResult{
					spec:         spec,
					success:      false,
					errorMessage: "validation canceled",
					errorType:    ValidationErrorSystem,
				}
				return
			case semaphore <- struct{}{}:
				// Acquired semaphore, ensure it's always released
				defer func() { <-semaphore }()
			}

			// Validate this single spec
			valResult := e.validateSingleSpec(ctx, spec, rebasedByName, rebasedBySHA)
			results <- valResult
		}(spec)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect every sibling's result. The first failure wins for reporting,
	// but we keep accumulating successes so they don't get dropped.
	var firstFailure *validationResult
	for valResult := range results {
		if !valResult.success {
			if firstFailure == nil {
				firstFailure = &valResult
			}
			continue
		}

		// Store successful result
		result.NewSHAs[valResult.spec.Branch] = valResult.newSHA
		if valResult.rerereResolved > 0 {
			result.RerereResolved[valResult.spec.Branch] = valResult.rerereResolved
		}
		rebasedByName.Store(valResult.spec.Branch, valResult.newSHA)
		if valResult.oldSHA != "" {
			rebasedBySHA.Store(valResult.oldSHA, valResult.newSHA)
		}
	}

	// If there was a failure, populate result and return true to signal failure
	if firstFailure != nil {
		result.Success = false
		result.FailedBranch = firstFailure.spec.Branch
		result.ErrorMessage = firstFailure.errorMessage
		result.ErrorType = firstFailure.errorType
		result.ConflictingFiles = firstFailure.conflictFiles
		return true
	}

	return false
}

// validateSingleSpec validates a single rebase spec in isolation.
func (e *engineImpl) validateSingleSpec(
	ctx context.Context,
	spec RebaseSpec,
	rebasedByName *sync.Map,
	rebasedBySHA *sync.Map,
) validationResult {
	// Resolve NewParent to handle rebased parents: try branch name first, then SHA lookup.
	resolvedParent := spec.NewParent
	if val, ok := rebasedByName.Load(spec.NewParent); ok {
		if str, ok := val.(string); ok {
			resolvedParent = str
		}
	} else if val, ok := rebasedBySHA.Load(spec.NewParent); ok {
		if str, ok := val.(string); ok {
			resolvedParent = str
		}
	}

	// Fast path: skip worktree creation for single-commit branches where the
	// parent and branch changes touch disjoint file sets (conflict impossible).
	if newSHA, ok := e.tryConflictFreeReplay(ctx, spec, resolvedParent); ok {
		oldSHA, _ := e.git.GetRevision(spec.Branch)
		return validationResult{
			spec:    spec,
			success: true,
			newSHA:  newSHA,
			oldSHA:  oldSHA,
		}
	}

	// Slow path: dry-run the rebase inside a temporary worktree.
	// Use WorktreePruneSkip since ValidateRebasesParallel already prunes once before parallel execution.
	worktreePath, cleanup, err := e.CreateTemporaryWorktreeWithOptions(ctx, "HEAD", "stackit-validate-*", WorktreeCheckoutFull, WorktreePruneSkip)
	if err != nil {
		return validationResult{
			spec:         spec,
			success:      false,
			errorMessage: fmt.Sprintf("failed to create worktree: %v", err),
			errorType:    ValidationErrorSystem,
		}
	}
	defer cleanup()

	wtGit := git.NewRunnerWithPath(worktreePath, nil)

	// Get the branch's current SHA before rebasing (to track old SHA -> new SHA mapping).
	oldBranchSHA, err := wtGit.GetRevision(spec.Branch)
	if err != nil {
		// Branch may not exist — not fatal, just means we can't track the SHA mapping.
		oldBranchSHA = ""
	}

	// Perform dry-run rebase
	rebaseResult, newSHA, conflictFiles, err := dryRunRebase(ctx, wtGit, spec.Branch, resolvedParent, spec.OldUpstream)
	if err != nil || rebaseResult.Result == git.RebaseConflict {
		errorMsg := fmt.Sprintf("conflict rebasing %s onto %s", spec.Branch, spec.NewParent)
		errorType := ValidationErrorConflict
		if err != nil {
			errorMsg = fmt.Sprintf("rebase failed for %s: %v", spec.Branch, err)
			errorType = ValidationErrorSystem
		}

		if wtGit.IsRebaseInProgress(ctx) {
			_ = wtGit.RebaseAbort(ctx)
		}

		return validationResult{
			spec:          spec,
			success:       false,
			errorMessage:  errorMsg,
			errorType:     errorType,
			conflictFiles: conflictFiles,
		}
	}

	return validationResult{
		spec:           spec,
		success:        true,
		newSHA:         newSHA,
		oldSHA:         oldBranchSHA,
		rerereResolved: rebaseResult.RerereResolvedCount,
	}
}

// tryConflictFreeReplay tries to produce the rebased SHA without a worktree for
// branches where the parent's new changes and the branch's changes touch
// completely disjoint file sets (a content conflict is therefore impossible).
//
// Multi-commit branches are supported only when the commit range is linear: each
// commit is replayed individually, oldest first, chained onto the previous
// rebased result so per-commit author identity and messages are preserved. The
// branch-level disjoint-file check is a sufficient conflict guarantee for every
// commit, because each commit's changes are a subset of the branch's overall
// changes — none of which touch a file the parent changed.
//
// Returns (newSHA, true) on success, where newSHA is the rebased branch tip;
// returns ("", false) to signal the caller to fall back to the full worktree path.
func (e *engineImpl) tryConflictFreeReplay(
	ctx context.Context,
	spec RebaseSpec,
	resolvedParent string,
) (string, bool) {
	commits, err := e.git.GetCommitRangeSHAs(ctx, spec.OldUpstream, spec.Branch)
	if err != nil || len(commits) == 0 {
		return "", false
	}
	if !e.commitRangeIsLinear(commits, spec.OldUpstream) {
		return "", false
	}

	// Get the files changed by the parent's new commits (what we're rebasing onto).
	parentFiles, err := e.git.GetChangedFiles(ctx, spec.OldUpstream, resolvedParent)
	if err != nil || len(parentFiles) == 0 {
		return "", false
	}

	// Get the files changed by our branch.
	branchFiles, err := e.git.GetChangedFiles(ctx, spec.OldUpstream, spec.Branch)
	if err != nil {
		return "", false
	}

	// If any file appears in both change sets, a conflict is possible.
	if rebaseFileOverlap(parentFiles, branchFiles) {
		return "", false
	}

	// File sets are disjoint — replay each commit onto the new base. commits is
	// newest-first, so iterate in reverse (oldest first). The oldest commit's
	// original parent is OldUpstream; every later commit's original parent is the
	// commit immediately before it. Each replayed commit becomes the new base for
	// the next, so author identity and message are preserved per commit.
	newBase := resolvedParent
	for i := len(commits) - 1; i >= 0; i-- {
		mergeBase := spec.OldUpstream
		if i+1 < len(commits) {
			mergeBase = commits[i+1]
		}
		newSHA, ok := e.replayCommitConflictFree(ctx, commits[i], mergeBase, newBase)
		if !ok {
			return "", false
		}
		newBase = newSHA
	}
	return newBase, true
}

// commitRangeIsLinear reports whether commits (newest-first) form a simple
// single-parent chain rooted at oldUpstream. The replay fast path relies on that
// shape to preserve git rebase semantics; merge commits and side-branch commits
// must fall back to the real worktree rebase.
func (e *engineImpl) commitRangeIsLinear(commits []string, oldUpstream string) bool {
	for i := len(commits) - 1; i >= 0; i-- {
		parentRaw, err := e.git.GetCommitLog(commits[i], "%P")
		if err != nil {
			return false
		}
		parents := strings.Fields(parentRaw)
		if len(parents) != 1 {
			return false
		}

		expectedParent := oldUpstream
		if i+1 < len(commits) {
			expectedParent = commits[i+1]
		}
		if parents[0] != expectedParent {
			return false
		}
	}
	return true
}

// replayCommitConflictFree replays a single commit onto newBase without a
// worktree, given the commit's original parent (the 3-way merge base). The caller
// must have established that the branch's changes are disjoint from the new base's
// changes, so the merge is guaranteed conflict-free.
//
// Returns (newSHA, true) on success; returns ("", false) to signal fallback.
func (e *engineImpl) replayCommitConflictFree(
	ctx context.Context,
	commitSHA, mergeBase, newBase string,
) (string, bool) {
	// Compute the rebased tree via merge-tree.
	// git merge-tree --write-tree --merge-base <base> <ours> <theirs>
	// OURS  = newBase  (the rebased parent so far)
	// THEIRS = commitSHA (this commit's snapshot on top of its original parent)
	treeSHARaw, err := e.git.RunGitCommandWithContext(ctx,
		"merge-tree", "--write-tree", "--merge-base", mergeBase, newBase, commitSHA)
	if err != nil {
		return "", false
	}
	// merge-tree --write-tree may emit additional lines (conflict markers) after
	// the tree SHA on a conflict; a non-zero exit covers that, but trim just in case.
	treeSHA := strings.TrimSpace(strings.SplitN(treeSHARaw, "\n", 2)[0])
	if treeSHA == "" {
		return "", false
	}

	// Preserve the original commit's author identity and message.
	authorName, err := e.git.GetCommitLog(commitSHA, "%an")
	if err != nil {
		return "", false
	}
	authorEmail, err := e.git.GetCommitLog(commitSHA, "%ae")
	if err != nil {
		return "", false
	}
	authorDate, err := e.git.GetCommitLog(commitSHA, "%aI")
	if err != nil {
		return "", false
	}
	msg, err := e.git.GetCommitLog(commitSHA, "%B")
	if err != nil {
		return "", false
	}

	env := []string{
		"GIT_AUTHOR_NAME=" + strings.TrimSpace(authorName),
		"GIT_AUTHOR_EMAIL=" + strings.TrimSpace(authorEmail),
		"GIT_AUTHOR_DATE=" + strings.TrimSpace(authorDate),
	}

	newSHARaw, err := e.git.RunGitCommandWithEnv(ctx, env,
		"commit-tree", treeSHA, "-p", newBase, "-m", strings.TrimSpace(msg))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(newSHARaw), true
}

// rebaseFileOverlap reports whether two sorted file-path slices share any element.
// Both slices must be sorted (GetChangedFiles guarantees this).
func rebaseFileOverlap(a, b []string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			return true
		}
	}
	return false
}
