package engine

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"stackit.dev/stackit/internal/git"
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
func dryRunRebase(ctx context.Context, g git.Runner, branchName, upstream, oldUpstream string) (git.RebaseResult, string, []string, error) {
	// Perform rebase in detached HEAD mode using branchName~0.
	// The ~0 suffix resolves to the same commit as branchName but tells git to check out
	// the commit directly (detached HEAD) rather than the branch ref. This means the rebase
	// results stay on the detached HEAD and the actual branch ref remains unchanged,
	// keeping the main repository unmodified during validation.
	_, err := g.RunGitCommandWithContext(ctx, "rebase", "--onto", upstream, oldUpstream, branchName+"~0")
	if err != nil {
		if g.IsRebaseInProgress(ctx) {
			// Get conflicting files before aborting
			conflictFiles, _ := g.GetUnmergedFiles(ctx)
			return git.RebaseConflict, "", conflictFiles, nil
		}
		// Abort rebase if it failed for other reasons
		_, _ = g.RunGitCommandWithContext(ctx, "rebase", "--abort")
		return git.RebaseConflict, "", nil, err
	}

	// Get the resulting SHA from the detached HEAD
	newSHA, err := g.GetCurrentRevision(ctx)
	if err != nil {
		return git.RebaseConflict, "", nil, fmt.Errorf("failed to get revision after rebase: %w", err)
	}

	// DO NOT update the branch ref - this is the key difference from normal Rebase
	// The branch ref stays unchanged, keeping the main repo unmodified

	return git.RebaseDone, newSHA, nil, nil
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
	graph := BuildStackGraph(e, SortStrategySmart, nil)

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
	spec          RebaseSpec
	success       bool
	newSHA        string
	errorMessage  string
	errorType     ValidationErrorType
	oldSHA        string
	conflictFiles []string
}

// ValidateRebasesParallel validates rebases in parallel where possible.
// Branches at the same depth in the stack (independent siblings) are validated concurrently.
// This can provide significant speedup for wide stacks with many branches at the same level.
//
// The function respects a maximum concurrency limit to avoid creating too many worktrees.
// Results are tracked thread-safely across parallel validations.
func (e *engineImpl) ValidateRebasesParallel(ctx context.Context, specs []RebaseSpec) (*RebaseValidation, error) {
	if len(specs) == 0 {
		return &RebaseValidation{Success: true, NewSHAs: map[string]string{}}, nil
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
		Success: true,
		NewSHAs: make(map[string]string),
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
func (e *engineImpl) processValidationLevel(
	ctx context.Context,
	level validationLevel,
	maxConcurrency int,
	result *RebaseValidation,
	rebasedByName *sync.Map,
	rebasedBySHA *sync.Map,
) bool {
	// Create cancelable context for this level to enable early exit on first failure
	levelCtx, cancelLevel := context.WithCancel(ctx)
	defer cancelLevel() // Ensure context is always canceled to prevent leaks

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

			// Check context before acquiring semaphore to allow quick cancellation
			select {
			case <-levelCtx.Done():
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
			valResult := e.validateSingleSpec(levelCtx, spec, rebasedByName, rebasedBySHA)
			results <- valResult
		}(spec)
	}

	// Collect results as they come in, cancel on first failure
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results and cancel context on first failure for early exit
	var firstFailure *validationResult
	for valResult := range results {
		if !valResult.success {
			if firstFailure == nil {
				// First failure detected - save it and cancel remaining validations
				firstFailure = &valResult
				cancelLevel()
			}
			// Continue draining results to avoid blocking goroutines
			continue
		}

		// Store successful result
		result.NewSHAs[valResult.spec.Branch] = valResult.newSHA
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
	// Create temporary worktree for this validation
	// Use WorktreePruneSkip since ValidateRebasesParallel already prunes once before parallel execution
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

	// Create a git runner for the worktree
	wtGit := git.NewRunnerWithPath(worktreePath, nil)

	// Resolve NewParent to handle rebased parents
	newParent := spec.NewParent

	// Check if NewParent refers to a branch we already rebased
	// Try branch name first, then SHA lookup
	if val, ok := rebasedByName.Load(spec.NewParent); ok {
		if str, ok := val.(string); ok {
			newParent = str
		}
	} else if val, ok := rebasedBySHA.Load(spec.NewParent); ok {
		if str, ok := val.(string); ok {
			newParent = str
		}
	}

	// Get the branch's current SHA before rebasing (to track old SHA -> new SHA mapping)
	oldBranchSHA, err := wtGit.GetRevision(spec.Branch)
	if err != nil {
		// Branch may not exist - this is not fatal, just means we can't track SHA mapping
		// Log for debugging but continue validation
		oldBranchSHA = ""
	}

	// Perform dry-run rebase
	rebaseResult, newSHA, conflictFiles, err := dryRunRebase(ctx, wtGit, spec.Branch, newParent, spec.OldUpstream)
	if err != nil || rebaseResult == git.RebaseConflict {
		// Build helpful error message with branch name
		errorMsg := fmt.Sprintf("conflict rebasing %s onto %s", spec.Branch, spec.NewParent)
		errorType := ValidationErrorConflict
		if err != nil {
			errorMsg = fmt.Sprintf("rebase failed for %s: %v", spec.Branch, err)
			errorType = ValidationErrorSystem
		}

		// Abort the in-progress rebase if any
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
		spec:    spec,
		success: true,
		newSHA:  newSHA,
		oldSHA:  oldBranchSHA,
	}
}
