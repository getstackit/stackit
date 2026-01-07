package tui

import (
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui/components/tree"
)

// NewStackTreeRenderer creates a tree renderer configured for the current engine state
// using the SMART sorting strategy (active path hoisting + newest first).
func NewStackTreeRenderer(eng engine.BranchReader) *tree.StackTreeRenderer {
	return NewStackTreeRendererWithStrategy(eng, engine.SortStrategySmart, nil)
}

// NewStackTreeRendererWithFilter creates a tree renderer with a filter function
func NewStackTreeRendererWithFilter(eng engine.BranchReader, filter func(string) bool) *tree.StackTreeRenderer {
	return NewStackTreeRendererWithStrategy(eng, engine.SortStrategySmart, filter)
}

// NewStackTreeRendererWithStrategy creates a tree renderer with a specific sorting strategy and optional filter
func NewStackTreeRendererWithStrategy(eng engine.BranchReader, strategy engine.SortStrategy, filter func(string) bool) *tree.StackTreeRenderer {
	var branchFilter func(engine.Branch) bool
	if filter != nil {
		branchFilter = func(b engine.Branch) bool {
			return filter(b.GetName())
		}
	}

	graph := engine.BuildStackGraph(eng, strategy, branchFilter)

	return tree.NewStackTreeRenderer(
		graph.Current,
		graph.Trunk,
		func(branchName string) []string {
			node := graph.Nodes[branchName]
			if node == nil {
				return nil
			}
			return graph.Children(node.Branch)
		},
		func(branchName string) string {
			node := graph.Nodes[branchName]
			if node == nil {
				return ""
			}
			return graph.Parent(node.Branch)
		},
		func(branchName string) bool { return branchName == graph.Trunk },
		func(branchName string) bool {
			node := graph.Nodes[branchName]
			if node == nil {
				return false
			}
			return eng.IsUpToDate(node.Branch)
		},
	)
}

// GetMinimalAnnotationWithWorktree returns minimal annotations plus worktree info.
// This is used for fast initial rendering before full data is loaded.
// Only includes cached/instant fields - no git or network calls.
func GetMinimalAnnotationWithWorktree(eng engine.Engine, branch engine.Branch) tree.BranchAnnotation {
	ann := tree.BranchAnnotation{
		IsLocked:      branch.IsLocked(),
		IsFrozen:      branch.IsFrozen(),
		Scope:         eng.GetScope(branch).String(),
		ExplicitScope: branch.GetExplicitScope().String(),
	}

	// Add worktree info if this branch is a stack root with a managed worktree
	stackRoot := eng.GetStackRootForBranch(branch)
	if stackRoot == branch.GetName() {
		if wtInfo, err := eng.GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
			ann.WorktreePath = wtInfo.Path
		}
	}

	return ann
}

// GetBranchAnnotation returns a tree.BranchAnnotation populated with standard branch metadata.
// This includes git operations (SHA, commit count, diff stats) which may be slow.
func GetBranchAnnotation(eng engine.BranchReader, branch engine.Branch) tree.BranchAnnotation {
	ann := tree.BranchAnnotation{
		IsLocked:      branch.IsLocked(),
		IsFrozen:      branch.IsFrozen(),
		Scope:         eng.GetScope(branch).String(),
		ExplicitScope: branch.GetExplicitScope().String(),
	}

	// Get short SHA for the branch
	if sha, err := branch.GetRevision(); err == nil && len(sha) >= 7 {
		ann.LocalSHA = sha[:7]
	}

	if !branch.IsTrunk() {
		// PR info (local metadata)
		if prInfo, _ := branch.GetPrInfo(); prInfo != nil {
			ann.PRNumber = prInfo.Number()
			ann.PRState = prInfo.State()
			ann.IsDraft = prInfo.IsDraft()
		}

		// Local stats
		if count, err := branch.GetCommitCount(); err == nil {
			ann.CommitCount = count
		}
		if added, deleted, err := branch.GetDiffStats(); err == nil {
			ann.LinesAdded = added
			ann.LinesDeleted = deleted
		}
	}

	return ann
}
