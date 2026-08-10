package shippable

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/worktree"
)

// Combiner analyzes whether multiple stacks can be merged together.
type Combiner struct {
	eng       engine.Engine
	cfg       config.Configurer
	output    output.Output
	validator *merge.LocalCIValidator
}

// NewCombiner creates a new stack combiner.
func NewCombiner(eng engine.Engine, cfg config.Configurer, out output.Output) *Combiner {
	return &Combiner{
		eng:       eng,
		cfg:       cfg,
		output:    out,
		validator: merge.NewLocalCIValidator(cfg, out),
	}
}

// CheckCombinationOptions configures how combination checking is performed.
type CheckCombinationOptions struct {
	// RunLocalCI determines whether to run local CI validation after merge.
	RunLocalCI bool
}

// CheckCombination checks if the given stacks can be merged together.
// It creates a temporary worktree, attempts to merge all stacks, and optionally
// runs local CI to verify the combined code compiles/passes tests.
func (c *Combiner) CheckCombination(ctx context.Context, stacks []Stack, opts CheckCombinationOptions) (*CombinationResult, error) {
	if len(stacks) == 0 {
		return &CombinationResult{
			Combinable:    true,
			WorkingStacks: nil,
		}, nil
	}

	// Convert shippable.Stack to merge.MultiStackInfo for the worktree executor
	multiStacks := make([]merge.MultiStackInfo, len(stacks))
	for i, s := range stacks {
		multiStacks[i] = s.Stack
	}

	// Create worktree executor and attempt merge
	executor := merge.NewMultiStackWorktreeExecutor(c.eng, c.output)
	result, err := executor.ExecuteInWorktree(ctx, multiStacks)
	if err != nil {
		return nil, fmt.Errorf("failed to execute merge in worktree: %w", err)
	}
	defer result.Cleanup()

	// Build the combination result
	combResult := &CombinationResult{
		WorkingStacks:     make([]Stack, 0, len(result.MergedStacks)),
		ConflictingStacks: make([]ExcludedStack, 0, len(result.ConflictStacks)),
	}

	// Map merged stacks back to shippable.Stack
	mergedRoots := make(map[string]bool)
	for _, ms := range result.MergedStacks {
		mergedRoots[ms.RootBranch] = true
	}

	for _, s := range stacks {
		if mergedRoots[s.RootBranch()] {
			combResult.WorkingStacks = append(combResult.WorkingStacks, s)
		} else {
			combResult.ConflictingStacks = append(combResult.ConflictingStacks, ExcludedStack{
				Stack:  s,
				Reason: ReasonMergeConflict,
			})
		}
	}

	combResult.Combinable = len(combResult.ConflictingStacks) == 0

	// Optionally run local CI
	if opts.RunLocalCI && c.validator.IsConfigured() && len(combResult.WorkingStacks) > 0 {
		err := c.validator.Validate(ctx, result.WorktreePath)
		passed := err == nil
		combResult.LocalCIPassed = &passed
		if err != nil {
			combResult.LocalCIError = err
			combResult.Combinable = false
		}
	}

	return combResult, nil
}

// FindLargestCompatibleOptions configures how the search is performed.
type FindLargestCompatibleOptions struct {
	// RunLocalCI determines whether to run local CI for each candidate set.
	RunLocalCI bool
}

// FindLargestCompatible finds the largest subset of stacks that can be merged together.
// It uses a greedy approach: try adding stacks one by one, keeping those that merge cleanly.
// If RunLocalCI is true, it also validates that the combined code passes local CI.
func (c *Combiner) FindLargestCompatible(ctx context.Context, stacks []Stack, opts FindLargestCompatibleOptions) (*CombinationResult, error) {
	if len(stacks) == 0 {
		return &CombinationResult{
			Combinable:    true,
			WorkingStacks: nil,
		}, nil
	}

	// Convert shippable.Stack to merge.MultiStackInfo
	multiStacks := make([]merge.MultiStackInfo, len(stacks))
	stackMap := make(map[string]Stack) // Map root branch to Stack
	for i, s := range stacks {
		multiStacks[i] = s.Stack
		stackMap[s.RootBranch()] = s
	}

	// Create worktree session
	wtExecutor := worktree.NewExecutor(c.eng, c.output)
	session, err := wtExecutor.CreateSession(ctx, worktree.CreateSessionOptions{
		NamePattern: "stackit-combine-*",
		PullTrunk:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree session: %w", err)
	}
	defer session.Close()

	// First, try all stacks together
	allResult, err := c.tryMergeStacks(ctx, session, multiStacks)
	if err != nil {
		return nil, err
	}

	// If all merge cleanly, check CI if requested
	if len(allResult.ConflictStacks) == 0 {
		result := &CombinationResult{
			Combinable:    true,
			WorkingStacks: stacks,
		}

		if opts.RunLocalCI && c.validator.IsConfigured() {
			ciErr := c.validator.Validate(ctx, session.Path.String())
			passed := ciErr == nil
			result.LocalCIPassed = &passed
			if ciErr != nil {
				result.LocalCIError = ciErr
				// CI failed - need to find subset that passes
				c.output.Warn("All stacks merge but CI fails, searching for passing subset...")
			} else {
				return result, nil
			}
		} else {
			return result, nil
		}
	}

	// Greedy search: try adding stacks one by one
	c.output.Info("Finding largest compatible subset...")

	var working []merge.MultiStackInfo
	var excluded []ExcludedStack

	for _, stack := range multiStacks {
		// Reset to trunk
		if err := session.ResetToTrunk(ctx); err != nil {
			return nil, fmt.Errorf("failed to reset worktree: %w", err)
		}

		// Try merging all working stacks plus this candidate
		testSet := make([]merge.MultiStackInfo, len(working)+1)
		copy(testSet, working)
		testSet[len(working)] = stack

		mergeResult, err := c.tryMergeStacks(ctx, session, testSet)
		if err != nil {
			return nil, err
		}

		// Check if this stack conflicts
		if len(mergeResult.ConflictStacks) > 0 {
			excluded = append(excluded, ExcludedStack{
				Stack:  stackMap[stack.RootBranch],
				Reason: ReasonMergeConflict,
			})
			c.output.Debug("  - %s conflicts", stack.RootBranch)
			continue
		}

		// Optionally run CI
		if opts.RunLocalCI && c.validator.IsConfigured() {
			if ciErr := c.validator.Validate(ctx, session.Path.String()); ciErr != nil {
				excluded = append(excluded, ExcludedStack{
					Stack:  stackMap[stack.RootBranch],
					Reason: ReasonLocalCIFailed,
				})
				c.output.Debug("  - %s fails CI", stack.RootBranch)
				continue
			}
		}

		// This stack works
		working = testSet
		c.output.Debug("  + %s compatible", stack.RootBranch)
	}

	// Build final result
	workingStacks := make([]Stack, len(working))
	for i, ms := range working {
		workingStacks[i] = stackMap[ms.RootBranch]
	}

	result := &CombinationResult{
		Combinable:        len(excluded) == 0,
		WorkingStacks:     workingStacks,
		ConflictingStacks: excluded,
	}

	// Final CI check on the combined result
	if opts.RunLocalCI && c.validator.IsConfigured() && len(working) > 0 {
		passed := true
		result.LocalCIPassed = &passed
	}

	return result, nil
}

// tryMergeStacks attempts to merge the given stacks in the worktree session.
// It resets to trunk first, then tries to merge all stacks.
func (c *Combiner) tryMergeStacks(
	ctx context.Context,
	session *worktree.Session,
	stacks []merge.MultiStackInfo,
) (*merge.MultiStackWorktreeResult, error) {
	// Reset to trunk first
	if err := session.ResetToTrunk(ctx); err != nil {
		return nil, fmt.Errorf("failed to reset worktree: %w", err)
	}

	// Create a new result to track this attempt
	result := &merge.MultiStackWorktreeResult{
		MergedStacks:   make([]merge.MultiStackInfo, 0),
		ConflictStacks: make([]merge.MultiStackExcluded, 0),
		WorktreePath:   session.Path.String(),
		WorktreeEngine: session.Engine,
	}

	// Try to merge each stack
	for _, stack := range stacks {
		baseline, err := session.GetCurrentRevision(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to capture worktree state: %w", err)
		}

		// Try octopus merge
		msg := fmt.Sprintf("Merge stack %s (%d branches)", stack.RootBranch, len(stack.AllBranches))
		err = session.Engine.MergeMultiple(ctx, stack.AllBranches, engine.MergeOptions{
			NoFF:    true,
			NoEdit:  true,
			Message: msg,
		})

		if err != nil {
			// Abort merge if in progress
			if session.Engine.IsMergeInProgress(ctx) {
				_ = session.Engine.MergeAbort(ctx)
			}

			// Reset to baseline
			if resetErr := session.ResetToRef(ctx, baseline); resetErr != nil {
				c.output.Debug("Failed to reset after conflict: %v", resetErr)
			}

			result.ConflictStacks = append(result.ConflictStacks, merge.MultiStackExcluded{
				Stack:  stack,
				Reason: "conflict",
			})
		} else {
			result.MergedStacks = append(result.MergedStacks, stack)
		}
	}

	return result, nil
}

// UpdateCompatibility updates the compatibility information for each stack
// based on a combination result.
func UpdateCompatibility(stacks []Stack, result *CombinationResult) {
	workingRoots := make(map[string]bool)
	for _, s := range result.WorkingStacks {
		workingRoots[s.RootBranch()] = true
	}

	conflictingRoots := make(map[string]bool)
	for _, es := range result.ConflictingStacks {
		conflictingRoots[es.Stack.RootBranch()] = true
	}

	for i := range stacks {
		root := stacks[i].RootBranch()

		// Clear existing compatibility info
		stacks[i].CompatibleWith = nil
		stacks[i].ConflictsWith = nil

		// Add compatible stacks (other working stacks)
		for _, ws := range result.WorkingStacks {
			if ws.RootBranch() != root {
				stacks[i].CompatibleWith = append(stacks[i].CompatibleWith, ws.RootBranch())
			}
		}

		// Add conflicting stacks
		for _, es := range result.ConflictingStacks {
			if es.Stack.RootBranch() != root {
				stacks[i].ConflictsWith = append(stacks[i].ConflictsWith, es.Stack.RootBranch())
			}
		}
	}
}
