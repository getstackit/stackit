package merge

import (
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
)

// FormatStackLabel creates a display label for a stack
func FormatStackLabel(stack MultiStackInfo) string {
	label := fmt.Sprintf("%s (%d branches", stack.RootBranch, len(stack.AllBranches))
	if stack.PRCount > 0 {
		label += fmt.Sprintf(", %d PRs", stack.PRCount)
	}
	if stack.Scope != "" {
		label += fmt.Sprintf(", scope: %s", stack.Scope)
	}
	label += ")"
	return label
}

// DiscoverStacks returns all independent stacks rooted at trunk.
// Each stack is represented by its root branch (direct child of trunk)
// and includes all branches in the stack in topological order.
func DiscoverStacks(eng engine.BranchReader) ([]MultiStackInfo, error) {
	return DiscoverStacksWithSort(eng, engine.SortStrategyAlphabetical)
}

// DiscoverStacksWithSort is like DiscoverStacks but allows specifying the sort strategy.
// Use SortStrategySmart to match the ordering of `stackit tree`.
func DiscoverStacksWithSort(eng engine.BranchReader, strategy engine.SortStrategy) ([]MultiStackInfo, error) {
	independentStacks := engine.DiscoverIndependentStacksWithSort(eng, strategy)

	stacks := make([]MultiStackInfo, 0, len(independentStacks))
	for _, independentStack := range independentStacks {
		branches := engine.BranchesFromNames(eng, independentStack.Branches)

		// Get scope from the root branch
		scope := ""
		root := eng.GetBranch(independentStack.RootBranch)
		if s := eng.GetScope(root); !s.IsEmpty() {
			scope = s.String()
		}

		stacks = append(stacks, MultiStackInfo{
			RootBranch:  independentStack.RootBranch,
			AllBranches: independentStack.Branches,
			PRCount:     countPRs(branches),
			Scope:       scope,
		})
	}

	return stacks, nil
}

// countPRs counts how many branches in the list have associated PRs.
func countPRs(branches engine.Branches) int {
	count := 0
	for _, branch := range branches {
		prInfo, err := branch.GetPrInfo()
		if err == nil && prInfo != nil && prInfo.Number() != nil {
			count++
		}
	}
	return count
}

// FilterStacks filters stacks based on selected root branch names.
// If selectedRoots is empty, returns all stacks.
// The returned stacks maintain the order of selectedRoots (priority order).
func FilterStacks(stacks []MultiStackInfo, selectedRoots []string) []MultiStackInfo {
	if len(selectedRoots) == 0 {
		return stacks
	}

	// Build a map for quick lookup
	stackMap := make(map[string]MultiStackInfo)
	for _, stack := range stacks {
		stackMap[stack.RootBranch] = stack
	}

	// Return stacks in the order of selectedRoots (preserving priority)
	var filtered []MultiStackInfo
	for _, root := range selectedRoots {
		if stack, ok := stackMap[root]; ok {
			filtered = append(filtered, stack)
		}
	}
	return filtered
}
