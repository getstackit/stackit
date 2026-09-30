package stackview

import (
	"fmt"

	"github.com/getstackit/stackit/internal/engine"
)

// StackInfo describes one independent stack rooted at a direct child of trunk.
type StackInfo struct {
	RootBranch  string   // Stack root branch name (direct child of trunk)
	AllBranches []string // All branches in the stack (root to tip, in order)
	PRCount     int      // Number of PRs in this stack
	Scope       string   // Stack scope if any
}

// Stacks is an ordered collection of independent stacks.
type Stacks []StackInfo

// Label returns the display label for a stack.
func (s StackInfo) Label() string {
	label := fmt.Sprintf("%s (%d branches", s.RootBranch, len(s.AllBranches))
	if s.PRCount > 0 {
		label += fmt.Sprintf(", %d PRs", s.PRCount)
	}
	if s.Scope != "" {
		label += fmt.Sprintf(", scope: %s", s.Scope)
	}
	return label + ")"
}

// FilterByRoots returns stacks selected by root name, preserving selection order.
// If selectedRoots is empty, returns all stacks.
func (stacks Stacks) FilterByRoots(selectedRoots []string) Stacks {
	if len(selectedRoots) == 0 {
		return stacks
	}
	byRoot := make(map[string]StackInfo, len(stacks))
	for _, stack := range stacks {
		byRoot[stack.RootBranch] = stack
	}
	filtered := make(Stacks, 0, len(selectedRoots))
	for _, root := range selectedRoots {
		if stack, ok := byRoot[root]; ok {
			filtered = append(filtered, stack)
		}
	}
	return filtered
}

// AllBranchNames returns every branch of every stack, in stack order.
func (stacks Stacks) AllBranchNames() []string {
	var names []string
	for _, stack := range stacks {
		names = append(names, stack.AllBranches...)
	}
	return names
}

// DiscoverStacks returns all independent stacks rooted at trunk, with
// siblings ordered alphabetically. Each stack is represented by its root
// branch (direct child of trunk) and includes all branches in the stack in
// topological order.
func DiscoverStacks(eng engine.StackView) Stacks {
	return DiscoverStacksWithSort(eng, engine.SortStrategyAlphabetical)
}

// DiscoverStacksWithSort is like DiscoverStacks but allows specifying the sort
// strategy. Use SortStrategySmart to match the ordering of `stackit tree`.
func DiscoverStacksWithSort(eng engine.StackView, strategy engine.SortStrategy) Stacks {
	independentStacks := engine.DiscoverIndependentStacksWithSort(eng, strategy)

	stacks := make(Stacks, 0, len(independentStacks))
	for _, independentStack := range independentStacks {
		branches := engine.BranchesFromNames(eng, independentStack.Branches)

		scope := ""
		root := eng.GetBranch(independentStack.RootBranch)
		if s := eng.GetScope(root); !s.IsEmpty() {
			scope = s.String()
		}

		stacks = append(stacks, StackInfo{
			RootBranch:  independentStack.RootBranch,
			AllBranches: independentStack.Branches,
			PRCount:     countPRs(branches),
			Scope:       scope,
		})
	}

	return stacks
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
