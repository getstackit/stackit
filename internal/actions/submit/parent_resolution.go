package submit

import "github.com/getstackit/stackit/internal/engine"

// resolveSubmitParent returns the nearest non-worktree-anchor ancestor.
// If no tracked non-anchor parent exists, trunk is returned.
func resolveSubmitParent(nav engine.StackNavigator, branch engine.Branch) engine.Branch {
	parent := branch.GetParent()
	visited := make(map[string]bool)

	for parent != nil {
		parentName := parent.GetName()
		if visited[parentName] {
			break
		}
		visited[parentName] = true

		if !parent.IsWorktreeAnchor() {
			return *parent
		}
		parent = parent.GetParent()
	}

	return nav.Trunk()
}
