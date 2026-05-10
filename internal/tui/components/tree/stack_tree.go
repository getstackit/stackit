package tree

import "github.com/getstackit/stackit/internal/engine"

// StackTree represents the structure of a branch stack for rendering.
// It encapsulates parent/child relationships and metadata needed to visualize stacks.
// This type is designed to be embedded in events and other contexts where stack
// visualization is needed.
//
// StackTree implements the Data interface, allowing it to be used directly
// with NewRenderer for tree visualization.
type StackTree struct {
	Branches       []string            // branches in the stack, in order
	CurrentBranchV string              // the currently checked out branch
	TrunkBranch    string              // the trunk/main branch name
	ParentMap      map[string]string   // branch -> parent mapping
	ChildrenMap    map[string][]string // branch -> children mapping
	FixedMap       map[string]bool     // branch -> whether it's fixed (optional)
}

// NewStackTree creates a StackTree from a list of branches.
// It builds the parent/child relationship maps from the branch metadata.
func NewStackTree(branches []engine.Branch, currentBranch, trunkBranch string) *StackTree {
	parentMap := make(map[string]string)
	childrenMap := make(map[string][]string)
	branchNames := make([]string, len(branches))

	for i, branch := range branches {
		branchName := branch.GetName()
		branchNames[i] = branchName
		parentName := branch.GetParentOrTrunk()
		parentMap[branchName] = parentName

		// Build children map (inverse of parent map)
		if parentName != "" {
			childrenMap[parentName] = append(childrenMap[parentName], branchName)
		}
	}

	return &StackTree{
		Branches:       branchNames,
		CurrentBranchV: currentBranch,
		TrunkBranch:    trunkBranch,
		ParentMap:      parentMap,
		ChildrenMap:    childrenMap,
	}
}

// Data interface implementation

// CurrentBranch returns the currently checked out branch.
func (t *StackTree) CurrentBranch() string {
	return t.CurrentBranchV
}

// Trunk returns the trunk/main branch name.
func (t *StackTree) Trunk() string {
	return t.TrunkBranch
}

// Children returns the child branches of the given branch.
func (t *StackTree) Children(branchName string) []string {
	return t.ChildrenMap[branchName]
}

// Parent returns the parent branch of the given branch.
func (t *StackTree) Parent(branchName string) string {
	return t.ParentMap[branchName]
}

// IsTrunk returns whether the given branch is the trunk branch.
func (t *StackTree) IsTrunk(branchName string) bool {
	return branchName == t.TrunkBranch
}

// IsFixed returns whether the branch is up-to-date with its parent.
// If FixedMap is nil, all branches are considered fixed.
func (t *StackTree) IsFixed(branchName string) bool {
	if t.FixedMap == nil {
		return true // Default: all branches are fixed
	}
	return t.FixedMap[branchName]
}

// ToRenderer converts the tree data into a StackTreeRenderer with default settings.
// All branches are considered "fixed" (no restack indicators).
func (t *StackTree) ToRenderer() *StackTreeRenderer {
	return NewRenderer(t)
}

// ToRendererWithFixed converts the tree data into a StackTreeRenderer with custom
// fixed branch logic. The isBranchFixed function determines which branches should
// be marked as not needing restack.
func (t *StackTree) ToRendererWithFixed(isBranchFixed func(string) bool) *StackTreeRenderer {
	// Build the FixedMap from the function
	fixedMap := make(map[string]bool)
	for _, branch := range t.Branches {
		fixedMap[branch] = isBranchFixed(branch)
	}
	// Also check trunk and any other branches in parent/children maps
	for branch := range t.ParentMap {
		if _, exists := fixedMap[branch]; !exists {
			fixedMap[branch] = isBranchFixed(branch)
		}
	}
	for branch := range t.ChildrenMap {
		if _, exists := fixedMap[branch]; !exists {
			fixedMap[branch] = isBranchFixed(branch)
		}
	}
	fixedMap[t.TrunkBranch] = isBranchFixed(t.TrunkBranch)

	// Create a copy with the fixed map
	treeCopy := &StackTree{
		Branches:       t.Branches,
		CurrentBranchV: t.CurrentBranchV,
		TrunkBranch:    t.TrunkBranch,
		ParentMap:      t.ParentMap,
		ChildrenMap:    t.ChildrenMap,
		FixedMap:       fixedMap,
	}

	return NewRenderer(treeCopy)
}
