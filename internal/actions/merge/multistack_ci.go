package merge

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/getstackit/stackit/internal/config"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// LocalCIValidator runs local CI validation on merged code
type LocalCIValidator struct {
	Command string
	Timeout time.Duration
	output  output.Output
}

// NewLocalCIValidator creates a new local CI validator from config.
// Uses the unified ci.command config, with fallback to combine.ciCommand for backwards compatibility.
func NewLocalCIValidator(cfg config.Configurer, out output.Output) *LocalCIValidator {
	return &LocalCIValidator{
		Command: cfg.CICommand(),
		Timeout: time.Duration(cfg.CITimeout()) * time.Second,
		output:  out,
	}
}

// IsConfigured returns true if a CI command is configured
func (v *LocalCIValidator) IsConfigured() bool {
	return v.Command != ""
}

// Validate runs the CI command in the specified directory
func (v *LocalCIValidator) Validate(ctx context.Context, workdir string) error {
	if !v.IsConfigured() {
		return fmt.Errorf("CI command not configured. Set it with: stackit config set ci.command \"your-command\"")
	}

	v.output.Info("Running local CI validation: %s", v.Command)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", v.Command)
	cmd.Dir = workdir

	cmdOutput, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("CI validation timed out after %v", v.Timeout)
	}
	if err != nil {
		v.output.Debug("CI output:\n%s", string(cmdOutput))
		return fmt.Errorf("CI validation failed: %w", err)
	}

	v.output.Success("Local CI validation passed")
	return nil
}

// LocalCISearchResult contains the result of binary search for working stacks
type LocalCISearchResult struct {
	WorkingStacks MultiStacks          // Stacks that pass CI together
	FailedStacks  []MultiStackExcluded // Stacks that failed CI
}

// FindLargestWorkingSet finds the maximum subset of stacks that pass CI.
// It uses a greedy approach: try adding stacks one by one, keeping those that pass.
func FindLargestWorkingSet(
	ctx context.Context,
	validator *LocalCIValidator,
	executor *MultiStackWorktreeExecutor,
	worktreeEng engine.Engine,
	worktreePath string,
	stacks []MultiStackInfo,
) (*LocalCISearchResult, error) {
	trunk := executor.eng.Trunk()

	// First, try all stacks together
	if err := validator.Validate(ctx, worktreePath); err == nil {
		// All stacks pass together
		return &LocalCISearchResult{
			WorkingStacks: stacks,
			FailedStacks:  nil,
		}, nil
	}

	validator.output.Warn("CI failed with all stacks, searching for largest working subset...")

	// Greedy approach: try adding stacks one by one
	var working []MultiStackInfo
	var failed []MultiStackExcluded

	for _, stack := range stacks {
		// Reset to trunk
		if err := worktreeEng.ResetHard(ctx, trunk.GetName()); err != nil {
			return nil, fmt.Errorf("failed to reset worktree: %w", err)
		}

		// Try merging all working stacks plus this candidate
		testSet := make([]MultiStackInfo, len(working)+1)
		copy(testSet, working)
		testSet[len(working)] = stack

		allMerged := true

		for _, s := range testSet {
			if err := executor.tryMergeStack(ctx, worktreeEng, s); err != nil {
				// This stack conflicts with the working set
				allMerged = false
				failed = append(failed, MultiStackExcluded{
					Stack:  stack,
					Reason: excludeReasonConflict,
				})
				break
			}
		}

		if !allMerged {
			continue
		}

		// Run CI on the test set
		if err := validator.Validate(ctx, worktreePath); err == nil {
			// This stack works with the others
			working = testSet
			validator.output.Info("  + %s passes CI", stack.RootBranch)
		} else {
			// This stack breaks CI
			failed = append(failed, MultiStackExcluded{
				Stack:  stack,
				Reason: excludeReasonCIFailure,
			})
			validator.output.Warn("  - %s fails CI", stack.RootBranch)
		}
	}

	// Final state: reset and merge only working stacks
	if len(working) > 0 {
		if err := worktreeEng.ResetHard(ctx, trunk.GetName()); err != nil {
			return nil, fmt.Errorf("failed to reset worktree: %w", err)
		}
		for _, s := range working {
			if err := executor.tryMergeStack(ctx, worktreeEng, s); err != nil {
				return nil, fmt.Errorf("unexpected conflict during final merge: %w", err)
			}
		}
	}

	return &LocalCISearchResult{
		WorkingStacks: working,
		FailedStacks:  failed,
	}, nil
}
