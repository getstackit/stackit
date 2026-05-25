package engine

// Branches is an ordered set of branches.
type Branches []Branch

// NewBranches creates an ordered branch set from branches.
func NewBranches(branches []Branch) Branches {
	list := make([]Branch, 0, len(branches))
	seen := make(map[string]bool, len(branches))
	for _, branch := range branches {
		name := branch.GetName()
		if seen[name] {
			continue
		}
		list = append(list, branch)
		seen[name] = true
	}
	return Branches(list)
}

// BranchesOf creates an ordered branch set from variadic branches.
func BranchesOf(branches ...Branch) Branches {
	return NewBranches(branches)
}

// Append returns a new branch set with branches appended in order.
func (b Branches) Append(branches ...Branch) Branches {
	all := b.All()
	all = append(all, branches...)
	return NewBranches(all)
}

// Concat returns a new branch set with other appended in order.
func (b Branches) Concat(other Branches) Branches {
	all := b.All()
	all = append(all, other...)
	return NewBranches(all)
}

// Filter returns a new branch set containing branches that match predicate.
func (b Branches) Filter(predicate func(Branch) bool) Branches {
	filtered := make([]Branch, 0, len(b))
	for _, branch := range b {
		if predicate(branch) {
			filtered = append(filtered, branch)
		}
	}
	return NewBranches(filtered)
}

// WithoutTrunk returns a new branch set without trunk branches.
func (b Branches) WithoutTrunk() Branches {
	return b.Filter(func(branch Branch) bool {
		return !branch.IsTrunk()
	})
}

// Last returns a branch set containing up to the last n branches.
func (b Branches) Last(n int) Branches {
	if n <= 0 {
		return Branches{}
	}
	if n >= len(b) {
		return NewBranches(b)
	}
	return NewBranches(b[len(b)-n:])
}

// Reverse returns a branch set with the branch order reversed.
func (b Branches) Reverse() Branches {
	reversed := make([]Branch, 0, len(b))
	for i := len(b) - 1; i >= 0; i-- {
		reversed = append(reversed, b[i])
	}
	return NewBranches(reversed)
}

// All returns the branches in insertion order.
func (b Branches) All() []Branch {
	branches := make([]Branch, len(b))
	copy(branches, b)
	return branches
}

// Names returns branch names in insertion order.
func (b Branches) Names() []string {
	names := make([]string, 0, len(b))
	for _, branch := range b {
		names = append(names, branch.GetName())
	}
	return names
}

// Contains returns true if the branch set contains name.
func (b Branches) Contains(name string) bool {
	for _, branch := range b {
		if branch.GetName() == name {
			return true
		}
	}
	return false
}

// Len returns the number of branches in the set.
func (b Branches) Len() int {
	return len(b)
}

// BranchStatuses contains status facts for an ordered branch set.
type BranchStatuses struct {
	upToDate map[string]bool
}

func newBranchStatuses(upToDate map[string]bool) BranchStatuses {
	statuses := make(map[string]bool, len(upToDate))
	for name, upToDate := range upToDate {
		statuses[name] = upToDate
	}
	return BranchStatuses{upToDate: statuses}
}

// IsUpToDate returns true when branch is up to date with its parent.
func (s BranchStatuses) IsUpToDate(branch Branch) bool {
	return s.IsUpToDateByName(branch.GetName())
}

// IsUpToDateByName returns true when branchName is up to date with its parent.
func (s BranchStatuses) IsUpToDateByName(branchName string) bool {
	return s.upToDate[branchName]
}
