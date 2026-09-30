package merge

import (
	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/engine"
)

// DiscoverStacks returns all independent stacks rooted at trunk. Discovery
// lives in stackview so read-only consumers (API, shippable) need not depend
// on the merge package; this wrapper keeps merge's call sites unchanged.
func DiscoverStacks(eng engine.BranchReader) (MultiStacks, error) {
	return stackview.DiscoverStacks(eng), nil
}

// FilterStacks filters stacks based on selected root branch names.
// If selectedRoots is empty, returns all stacks.
// The returned stacks maintain the order of selectedRoots (priority order).
func FilterStacks(stacks []MultiStackInfo, selectedRoots []string) []MultiStackInfo {
	return MultiStacks(stacks).FilterByRoots(selectedRoots)
}
