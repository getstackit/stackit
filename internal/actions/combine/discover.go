package combine

import (
	"stackit.dev/stackit/internal/engine"
)

// DiscoverStacks returns all independent stacks rooted at trunk.
// Each stack is represented by its root branch (direct child of trunk)
// and includes all branches in the stack in topological order.
func DiscoverStacks(eng engine.BranchReader) ([]StackInfo, error) {
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
	trunk := eng.Trunk()

	// Get the trunk node to find its direct children (stack roots)
	trunkNode := graph.Nodes[trunk.GetName()]
	if trunkNode == nil {
		return nil, nil // No trunk found
	}

	stacks := make([]StackInfo, 0, len(trunkNode.Children))
	for _, rootName := range trunkNode.Children {
		rootNode := graph.Nodes[rootName]
		if rootNode == nil {
			continue
		}

		// Collect all branches in this stack using DFS
		branches := collectStackBranches(graph, rootName)

		// Count PRs for this stack
		prCount := countPRs(eng, branches)

		// Get scope from the root branch
		scope := ""
		if s := eng.GetScope(rootNode.Branch); !s.IsEmpty() {
			scope = s.String()
		}

		stacks = append(stacks, StackInfo{
			RootBranch:  rootName,
			AllBranches: branches,
			PRCount:     prCount,
			Scope:       scope,
		})
	}

	return stacks, nil
}

// collectStackBranches collects all branches in a stack starting from the root.
// Returns branches in depth-first order (root first, then descendants).
func collectStackBranches(graph *engine.StackGraph, rootName string) []string {
	var branches []string
	var collect func(name string)
	collect = func(name string) {
		branches = append(branches, name)
		node := graph.Nodes[name]
		if node != nil {
			for _, child := range node.Children {
				collect(child)
			}
		}
	}
	collect(rootName)
	return branches
}

// countPRs counts how many branches in the list have associated PRs.
func countPRs(eng engine.BranchReader, branches []string) int {
	count := 0
	for _, name := range branches {
		branch := eng.GetBranch(name)
		prInfo, err := branch.GetPrInfo()
		if err == nil && prInfo != nil && prInfo.Number() != nil {
			count++
		}
	}
	return count
}

// FilterStacks filters stacks based on selected root branch names.
// If selectedRoots is empty, returns all stacks.
func FilterStacks(stacks []StackInfo, selectedRoots []string) []StackInfo {
	if len(selectedRoots) == 0 {
		return stacks
	}

	selectedSet := make(map[string]bool)
	for _, root := range selectedRoots {
		selectedSet[root] = true
	}

	var filtered []StackInfo
	for _, stack := range stacks {
		if selectedSet[stack.RootBranch] {
			filtered = append(filtered, stack)
		}
	}
	return filtered
}
