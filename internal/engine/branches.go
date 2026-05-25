package engine

// Branches is an ordered set of branches.
type Branches struct {
	list   []Branch
	byName map[string]Branch
}

// NewBranches creates an ordered branch set from branches.
func NewBranches(branches []Branch) Branches {
	list := make([]Branch, 0, len(branches))
	byName := make(map[string]Branch, len(branches))
	for _, branch := range branches {
		name := branch.GetName()
		if _, ok := byName[name]; ok {
			continue
		}
		list = append(list, branch)
		byName[name] = branch
	}
	return Branches{
		list:   list,
		byName: byName,
	}
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

// Last returns a branch set containing up to the last n branches.
func (b Branches) Last(n int) Branches {
	if n <= 0 {
		return Branches{}
	}
	if n >= len(b.list) {
		return NewBranches(b.list)
	}
	return NewBranches(b.list[len(b.list)-n:])
}

// Reverse returns a branch set with the branch order reversed.
func (b Branches) Reverse() Branches {
	reversed := make([]Branch, 0, len(b.list))
	for i := len(b.list) - 1; i >= 0; i-- {
		reversed = append(reversed, b.list[i])
	}
	return NewBranches(reversed)
}

// All returns the branches in insertion order.
func (b Branches) All() []Branch {
	branches := make([]Branch, len(b.list))
	copy(branches, b.list)
	return branches
}

// Names returns branch names in insertion order.
func (b Branches) Names() []string {
	names := make([]string, 0, len(b.list))
	for _, branch := range b.list {
		names = append(names, branch.GetName())
	}
	return names
}

// Contains returns true if the branch set contains name.
func (b Branches) Contains(name string) bool {
	_, ok := b.byName[name]
	return ok
}

// Len returns the number of branches in the set.
func (b Branches) Len() int {
	return len(b.list)
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
