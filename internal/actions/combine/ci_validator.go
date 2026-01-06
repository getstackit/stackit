package combine

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"stackit.dev/stackit/internal/config"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/output"
)

// CIValidator runs local CI validation on combined code
type CIValidator struct {
	Command string
	Timeout time.Duration
	output  output.Output
}

// NewCIValidator creates a new CI validator from config
func NewCIValidator(cfg *config.Config, out output.Output) *CIValidator {
	return &CIValidator{
		Command: cfg.CombineCICommand(),
		Timeout: time.Duration(cfg.CombineCITimeout()) * time.Second,
		output:  out,
	}
}

// IsConfigured returns true if a CI command is configured
func (v *CIValidator) IsConfigured() bool {
	return v.Command != ""
}

// Validate runs the CI command in the specified directory
func (v *CIValidator) Validate(ctx context.Context, workdir string) error {
	if !v.IsConfigured() {
		return fmt.Errorf("CI command not configured. Set it with: stackit config set combine.ciCommand \"your-command\"")
	}

	v.output.Info("Running CI validation: %s", v.Command)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, v.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", v.Command)
	cmd.Dir = workdir

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("CI validation timed out after %v", v.Timeout)
	}
	if err != nil {
		v.output.Debug("CI output:\n%s", string(output))
		return fmt.Errorf("CI validation failed: %w", err)
	}

	v.output.Success("CI validation passed")
	return nil
}

// BinarySearchResult contains the result of binary search for working stacks
type BinarySearchResult struct {
	WorkingStacks []StackInfo     // Stacks that pass CI together
	FailedStacks  []ExcludedStack // Stacks that failed CI
}

// FindLargestWorkingSet finds the maximum subset of stacks that pass CI.
// It uses a greedy approach: try adding stacks one by one, keeping those that pass.
func FindLargestWorkingSet(
	ctx context.Context,
	validator *CIValidator,
	executor *WorktreeExecutor,
	worktreeEng engine.Engine,
	worktreePath string,
	stacks []StackInfo,
) (*BinarySearchResult, error) {
	trunk := executor.eng.Trunk()

	// First, try all stacks together
	if err := validator.Validate(ctx, worktreePath); err == nil {
		// All stacks pass together
		return &BinarySearchResult{
			WorkingStacks: stacks,
			FailedStacks:  nil,
		}, nil
	}

	validator.output.Warn("CI failed with all stacks, searching for largest working subset...")

	// Greedy approach: try adding stacks one by one
	var working []StackInfo
	var failed []ExcludedStack

	for _, stack := range stacks {
		// Reset to trunk
		if err := worktreeEng.ResetHard(ctx, trunk.GetName()); err != nil {
			return nil, fmt.Errorf("failed to reset worktree: %w", err)
		}

		// Try merging all working stacks plus this candidate
		testSet := make([]StackInfo, len(working)+1)
		copy(testSet, working)
		testSet[len(working)] = stack

		allMerged := true

		for _, s := range testSet {
			if err := executor.tryMergeStack(ctx, worktreeEng, s); err != nil {
				// This stack conflicts with the working set
				allMerged = false
				failed = append(failed, ExcludedStack{
					Stack:  stack,
					Reason: "conflict",
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
			failed = append(failed, ExcludedStack{
				Stack:  stack,
				Reason: "ci_failure",
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

	return &BinarySearchResult{
		WorkingStacks: working,
		FailedStacks:  failed,
	}, nil
}
