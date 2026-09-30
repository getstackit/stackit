package engine

// CurrentBranchMode selects whether a directional stack traversal includes
// the branch it starts from.
type CurrentBranchMode int

const (
	// ExcludeCurrentBranch returns only the branches above or below the start.
	ExcludeCurrentBranch CurrentBranchMode = iota
	// IncludeCurrentBranch also returns the starting branch.
	IncludeCurrentBranch
)

// StackRangeUpstack returns a range for children (upstack) traversal.
func StackRangeUpstack(current CurrentBranchMode) StackRange {
	return StackRange{
		RecursiveChildren: true,
		IncludeCurrent:    current == IncludeCurrentBranch,
	}
}

// StackRangeDownstack returns a range for parents (downstack) traversal.
func StackRangeDownstack(current CurrentBranchMode) StackRange {
	return StackRange{
		RecursiveParents: true,
		IncludeCurrent:   current == IncludeCurrentBranch,
	}
}

// StackRangeFull returns a range for full stack (parents + current + children).
func StackRangeFull() StackRange {
	return StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    true,
		RecursiveChildren: true,
	}
}
