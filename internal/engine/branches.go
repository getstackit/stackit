package engine

// Branches is an ordered set of branches.
type Branches []Branch

// BranchesBuilder incrementally builds an ordered branch set without repeatedly
// copying intermediate slices.
type BranchesBuilder struct {
	branches []Branch
	seen     map[string]struct{}
}

// NewBranchesBuilder creates a builder sized for up to capHint branches.
func NewBranchesBuilder(capHint int) *BranchesBuilder {
	if capHint < 0 {
		capHint = 0
	}
	return &BranchesBuilder{
		branches: make([]Branch, 0, capHint),
		seen:     make(map[string]struct{}, capHint),
	}
}

// Add adds branch if it is not already present.
func (b *BranchesBuilder) Add(branch Branch) {
	if b == nil {
		return
	}
	name := branch.GetName()
	if _, exists := b.seen[name]; exists {
		return
	}
	b.branches = append(b.branches, branch)
	b.seen[name] = struct{}{}
}

// AddAll adds each branch in order, deduplicating by branch name.
func (b *BranchesBuilder) AddAll(branches Branches) {
	for _, branch := range branches {
		b.Add(branch)
	}
}

// Build returns the built ordered branch set.
func (b *BranchesBuilder) Build() Branches {
	if b == nil || len(b.branches) == 0 {
		return Branches{}
	}
	branches := make([]Branch, len(b.branches))
	copy(branches, b.branches)
	return Branches(branches)
}

// NewBranches creates an ordered branch set from branches.
func NewBranches(branches []Branch) Branches {
	builder := NewBranchesBuilder(len(branches))
	for _, branch := range branches {
		builder.Add(branch)
	}
	return builder.Build()
}

// BranchesOf creates an ordered branch set from variadic branches.
func BranchesOf(branches ...Branch) Branches {
	return NewBranches(branches)
}

// BranchesFromNames resolves branch names into an ordered branch set.
func BranchesFromNames(nav StackNavigator, branchNames []string) Branches {
	builder := NewBranchesBuilder(len(branchNames))
	for _, branchName := range branchNames {
		builder.Add(nav.GetBranch(branchName))
	}
	return builder.Build()
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

// BranchFilter declaratively expresses the set of constraints to apply when
// selecting branches. The zero value matches every branch — opt into
// constraints, never out of them. Use with Branches.Select for single-pass
// multi-criteria filtering instead of chaining WithoutTrunk().WithPR() etc.
type BranchFilter struct {
	ExcludeTrunk bool // when true, drop trunk branches
	RequirePR    bool // when true, keep only branches with a submitted PR
}

// Matches reports whether branch satisfies every constraint in f.
func (f BranchFilter) Matches(branch Branch) bool {
	if f.ExcludeTrunk && branch.IsTrunk() {
		return false
	}
	if f.RequirePR && !branch.HasPR() {
		return false
	}
	return true
}

// Select returns the branches that match every constraint in f. Prefer this
// over WithoutTrunk().WithPR() chains: one pass over b instead of N.
func (b Branches) Select(f BranchFilter) Branches {
	return b.Filter(f.Matches)
}

// WithoutTrunk returns a new branch set without trunk branches.
func (b Branches) WithoutTrunk() Branches {
	return b.Select(BranchFilter{ExcludeTrunk: true})
}

// WithPR returns a new branch set containing only branches that have a
// submitted PR recorded.
func (b Branches) WithPR() Branches {
	return b.Select(BranchFilter{RequirePR: true})
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
