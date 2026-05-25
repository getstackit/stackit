package engine

// BranchSet provides O(1) branch name lookups with a cached set.
// Use BranchNames() on StackNavigator to get a cached instance.
type BranchSet struct {
	names map[string]bool
	list  []string
}

// newBranchSet creates a BranchSet from a slice of branch names.
func newBranchSet(branches []string) *BranchSet {
	names := make(map[string]bool, len(branches))
	for _, name := range branches {
		names[name] = true
	}
	return &BranchSet{
		names: names,
		list:  branches,
	}
}

// Contains returns true if the branch name exists in the set.
func (s *BranchSet) Contains(name string) bool {
	return s.names[name]
}

// Names returns all branch names as a slice.
func (s *BranchSet) Names() []string {
	return s.list
}

// Len returns the number of branches in the set.
func (s *BranchSet) Len() int {
	return len(s.names)
}

// FilterBranches returns branches matching the predicate.
func FilterBranches(eng StackNavigator, predicate func(Branch) bool) Branches {
	return eng.AllBranches().Filter(predicate)
}
