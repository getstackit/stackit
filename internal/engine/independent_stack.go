package engine

// IndependentStack describes a standalone stack rooted at a direct child of trunk.
type IndependentStack struct {
	RootBranch string
	Branches   []string
}

// DiscoverIndependentStacks returns all stacks rooted at direct children of trunk.
//
// Each stack includes its root branch first, followed by descendants in the graph's
// depth-first order. Branches from separate stacks are independent of each other.
func DiscoverIndependentStacks(eng BranchReader) []IndependentStack {
	return DiscoverIndependentStacksWithSort(eng, SortStrategyAlphabetical)
}

// DiscoverIndependentStacksWithSort is like DiscoverIndependentStacks, but allows
// callers to choose how sibling branches are ordered.
func DiscoverIndependentStacksWithSort(eng BranchReader, strategy SortStrategy) []IndependentStack {
	graph := eng.Graph(strategy)
	trunkNode := graph.GetNode(eng.Trunk().GetName())
	if trunkNode == nil {
		return nil
	}

	stacks := make([]IndependentStack, 0, len(trunkNode.Children))
	for _, rootName := range trunkNode.Children {
		rootNode := graph.GetNode(rootName)
		if rootNode == nil {
			continue
		}

		branches := graph.CollectBranches(rootNode.Branch)

		stacks = append(stacks, IndependentStack{
			RootBranch: rootName,
			Branches:   branches.Names(),
		})
	}

	return stacks
}
