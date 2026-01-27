package engine

import "slices"

// isOnActivePath checks if branchName is on the path from trunk to current branch
func isOnActivePath(nodes map[string]*StackNode, branchName, current string) bool {
	if current == "" {
		return false
	}
	// Walk from current up to trunk, checking if branchName is on the path
	cur := current
	for cur != "" {
		if cur == branchName {
			return true
		}
		node := nodes[cur]
		if node == nil {
			break
		}
		cur = node.Parent
	}
	return false
}

// StackNode represents a branch within a stack snapshot.
type StackNode struct {
	Branch   Branch
	Parent   string
	Children []string
	Depth    int
	IsTrunk  bool
}

// StackGraph is an immutable snapshot of the branch stack relationships.
// It is built once from the engine and then used for traversals/rendering without
// further engine calls.
type StackGraph struct {
	Nodes        map[string]*StackNode
	Roots        []string
	Current      string
	Trunk        string
	SortStrategy SortStrategy
}

// BuildStackGraph constructs a StackGraph using the provided engine reader and sorting strategy.
// The optional filter is applied to branches; filtered-out branches are omitted along with their subtrees.
func BuildStackGraph(eng BranchReader, strategy SortStrategy, filter func(Branch) bool) *StackGraph {
	branches := eng.AllBranches()

	trunk := eng.Trunk().GetName()
	current := ""
	if cb := eng.CurrentBranch(); cb != nil {
		current = cb.GetName()
	}

	allowed := make(map[string]Branch)
	for _, b := range branches {
		if filter != nil && !filter(b) {
			continue
		}
		allowed[b.GetName()] = b
	}

	graph := &StackGraph{
		Nodes:        make(map[string]*StackNode),
		Current:      current,
		Trunk:        trunk,
		SortStrategy: strategy,
	}

	// Seed nodes with parent references (only if parent is also allowed)
	for name, branch := range allowed {
		parentName := ""
		if parent := branch.GetParent(); parent != nil {
			if _, ok := allowed[parent.GetName()]; ok {
				parentName = parent.GetName()
			}
		}

		graph.Nodes[name] = &StackNode{
			Branch:  branch,
			Parent:  parentName,
			IsTrunk: name == trunk,
		}
	}

	// Populate children by deriving from parent relationships
	for name, node := range graph.Nodes {
		if node.Parent != "" {
			if parentNode := graph.Nodes[node.Parent]; parentNode != nil {
				parentNode.Children = append(parentNode.Children, name)
			}
		}
	}

	// Sort children based on strategy
	for _, node := range graph.Nodes {
		if len(node.Children) > 1 {
			switch strategy {
			case SortStrategySmart:
				// Smart sort: hoist the active path (current branch first) and then sort descending
				slices.SortFunc(node.Children, func(a, b string) int {
					// Current branch or its ancestors come first
					aOnPath := isOnActivePath(graph.Nodes, a, current)
					bOnPath := isOnActivePath(graph.Nodes, b, current)
					if aOnPath && !bOnPath {
						return -1
					}
					if !aOnPath && bOnPath {
						return 1
					}
					// Otherwise sort descending (newest/Z-first)
					if a > b {
						return -1
					}
					if a < b {
						return 1
					}
					return 0
				})
			case SortStrategyAlphabetical:
				slices.Sort(node.Children)
			}
		}
	}

	// Determine roots (nodes without an allowed parent)
	for name, node := range graph.Nodes {
		if node.Parent == "" {
			graph.Roots = append(graph.Roots, name)
		}
	}

	// Compute depth for each node (distance from root)
	depthCache := make(map[string]int)
	var computeDepth func(string) int
	computeDepth = func(name string) int {
		if d, ok := depthCache[name]; ok {
			return d
		}
		node := graph.Nodes[name]
		if node == nil || node.Parent == "" {
			depthCache[name] = 0
			return 0
		}
		depthCache[name] = computeDepth(node.Parent) + 1
		return depthCache[name]
	}
	for name, node := range graph.Nodes {
		node.Depth = computeDepth(name)
	}

	// Sort roots for deterministic traversal
	slices.Sort(graph.Roots)

	return graph
}

// GetNode returns the StackNode for a branch by name, or nil if not found.
func (g *StackGraph) GetNode(branchName string) *StackNode {
	return g.Nodes[branchName]
}

// Node returns the StackNode for the given branch.
func (g *StackGraph) Node(branch Branch) *StackNode {
	return g.Nodes[branch.GetName()]
}

// CurrentBranch returns the name of the currently checked-out branch.
func (g *StackGraph) CurrentBranch() string {
	return g.Current
}

// TrunkName returns the trunk/main branch name.
func (g *StackGraph) TrunkName() string {
	return g.Trunk
}

// RootBranches returns all root branch names (branches with no parent in the graph).
func (g *StackGraph) RootBranches() []string {
	return g.Roots
}

// Children returns the child branch names for the given branch.
func (g *StackGraph) Children(branch Branch) []string {
	if node := g.Nodes[branch.GetName()]; node != nil {
		return node.Children
	}
	return nil
}

// ChildBranches returns the child branches for the given branch.
func (g *StackGraph) ChildBranches(branch Branch) []Branch {
	node := g.Nodes[branch.GetName()]
	if node == nil {
		return nil
	}
	branches := make([]Branch, len(node.Children))
	for i, n := range node.Children {
		branches[i] = g.Nodes[n].Branch
	}
	return branches
}

// Parent returns the parent branch name (empty string if none).
func (g *StackGraph) Parent(branch Branch) string {
	if node := g.Nodes[branch.GetName()]; node != nil {
		return node.Parent
	}
	return ""
}

// IsDescendant returns true if potentialDescendant is a descendant of branch.
// This is useful for cycle detection when moving branches.
func (g *StackGraph) IsDescendant(branch Branch, potentialDescendant string) bool {
	descendants := g.Range(branch, StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    false,
	})
	for _, d := range descendants {
		if d.GetName() == potentialDescendant {
			return true
		}
	}
	return false
}

// GetBranchesByDepth returns a map from depth to branch names at that depth.
// This is useful for parallel operations where branches at the same depth are independent.
func (g *StackGraph) GetBranchesByDepth() map[int][]string {
	byDepth := make(map[int][]string)
	for name, node := range g.Nodes {
		byDepth[node.Depth] = append(byDepth[node.Depth], name)
	}
	return byDepth
}

// Range returns branches matching the provided StackRange, ordered the same as the legacy
// GetRelativeStack implementation: ancestors (oldest to nearest), current, then descendants.
// Descendants are traversed depth-first using the graph's pre-sorted children.
func (g *StackGraph) Range(branch Branch, rng StackRange) []Branch {
	start := branch.GetName()
	startNode := g.Nodes[start]
	if startNode == nil {
		return nil
	}

	var result []Branch

	// Ancestors (excluding trunk)
	if rng.RecursiveParents {
		current := startNode.Parent
		var ancestors []Branch
		for current != "" && current != g.Trunk {
			node := g.Nodes[current]
			if node == nil {
				break
			}
			ancestors = append(ancestors, node.Branch)
			current = node.Parent
		}
		// Reverse to go from trunk-ward to the starting branch
		for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
			ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
		}
		result = append(result, ancestors...)
	}

	// Current branch
	if rng.IncludeCurrent {
		result = append(result, startNode.Branch)
	}

	// Descendants (depth-first, ordered by pre-sorted children)
	if rng.RecursiveChildren {
		visited := map[string]bool{start: true}
		var collectDescendants func(string)
		collectDescendants = func(name string) {
			node := g.Nodes[name]
			if node == nil {
				return
			}
			for _, childName := range node.Children {
				if visited[childName] {
					continue
				}
				visited[childName] = true
				if child := g.Nodes[childName]; child != nil {
					result = append(result, child.Branch)
					collectDescendants(childName)
				}
			}
		}
		collectDescendants(start)
	}

	return result
}

// IsLeaf returns true if the branch has no children in the graph.
func (g *StackGraph) IsLeaf(branch Branch) bool {
	node := g.Nodes[branch.GetName()]
	return node == nil || len(node.Children) == 0
}

// CollectBranches returns all branches in depth-first order starting from root.
// The root is included as the first element.
func (g *StackGraph) CollectBranches(root Branch) []Branch {
	rootNode := g.Nodes[root.GetName()]
	if rootNode == nil {
		return nil
	}

	var branches []Branch
	var collect func(node *StackNode)
	collect = func(node *StackNode) {
		branches = append(branches, node.Branch)
		for _, childName := range node.Children {
			if child := g.Nodes[childName]; child != nil {
				collect(child)
			}
		}
	}
	collect(rootNode)
	return branches
}

// IsRelated returns true if either branch is an ancestor or descendant of the other.
func (g *StackGraph) IsRelated(branch1, branch2 Branch) bool {
	name1 := branch1.GetName()
	name2 := branch2.GetName()

	if name1 == name2 {
		return true
	}

	// Check if branch2 is a descendant of branch1
	if g.isAncestorOf(name1, name2) {
		return true
	}

	// Check if branch1 is a descendant of branch2
	return g.isAncestorOf(name2, name1)
}

// isAncestorOf returns true if ancestor is an ancestor of descendant.
func (g *StackGraph) isAncestorOf(ancestor, descendant string) bool {
	current := descendant
	for current != "" {
		node := g.Nodes[current]
		if node == nil {
			return false
		}
		if node.Parent == ancestor {
			return true
		}
		current = node.Parent
	}
	return false
}
