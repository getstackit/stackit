package shippable

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/app"
)

// ShipOptions configures the ship operation.
type ShipOptions struct {
	// SkipLocalCI skips local CI validation.
	SkipLocalCI bool
	// Wait waits for CI and auto-merges the PR.
	Wait bool
}

// ShipResult contains the result of a ship operation.
type ShipResult struct {
	// IncludedStacks are stacks that were successfully shipped.
	IncludedStacks []Stack
	// ExcludedStacks are stacks that were excluded due to conflicts.
	ExcludedStacks []ExcludedStack
	// PRNumber is the created PR number.
	PRNumber int
	// PRURL is the created PR URL.
	PRURL string
	// BranchName is the consolidation branch name.
	BranchName string
}

// Shipper handles shipping selected stacks to trunk.
type Shipper struct {
	ctx *app.Context
}

// NewShipper creates a new shipper.
func NewShipper(ctx *app.Context) *Shipper {
	return &Shipper{ctx: ctx}
}

// Ship ships the selected stacks to trunk by creating a consolidated PR.
// It uses the existing multi-stack merge functionality.
func (s *Shipper) Ship(stacks []Stack, opts ShipOptions) (*ShipResult, error) {
	if len(stacks) == 0 {
		return nil, fmt.Errorf("no stacks to ship")
	}

	// Verify all stacks are shippable
	for _, stack := range stacks {
		if !stack.IsShippable() {
			return nil, fmt.Errorf("stack %s is not shippable: status is %s", stack.RootBranch(), stack.Status)
		}
	}

	// Extract root branch names for the merge operation
	selectedRoots := make([]string, len(stacks))
	for i, stack := range stacks {
		selectedRoots[i] = stack.RootBranch()
	}

	// Create map for converting results back to shippable.Stack
	stackMap := make(map[string]Stack)
	for _, stack := range stacks {
		stackMap[stack.RootBranch()] = stack
	}

	// Execute the multi-stack merge
	mergeResult, err := merge.ExecuteMultiStack(s.ctx, merge.MultiStackOptions{
		SelectedStacks: selectedRoots,
		SkipLocalCI:    opts.SkipLocalCI,
		Wait:           opts.Wait,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ship stacks: %w", err)
	}

	// Convert merge result to ship result
	result := &ShipResult{
		PRNumber:   mergeResult.PRNumber,
		PRURL:      mergeResult.PRURL,
		BranchName: mergeResult.BranchName,
	}

	// Convert included stacks
	for _, ms := range mergeResult.IncludedStacks {
		if stack, ok := stackMap[ms.RootBranch]; ok {
			result.IncludedStacks = append(result.IncludedStacks, stack)
		}
	}

	// Convert excluded stacks
	for _, excluded := range mergeResult.ExcludedStacks {
		if stack, ok := stackMap[excluded.Stack.RootBranch]; ok {
			var reason ExclusionReason
			switch excluded.Reason {
			case "conflict":
				reason = ReasonMergeConflict
			case "ci_failure":
				reason = ReasonLocalCIFailed
			default:
				reason = ExclusionReason(excluded.Reason)
			}
			result.ExcludedStacks = append(result.ExcludedStacks, ExcludedStack{
				Stack:  stack,
				Reason: reason,
			})
		}
	}

	return result, nil
}
