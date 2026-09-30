package tui

import (
	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/tui/components/tree"
)

// NewStackTreeRenderer creates a tree renderer configured for the current engine state
// using the SMART sorting strategy (active path hoisting + newest first).
func NewStackTreeRenderer(eng engine.StackView) *tree.StackTreeRenderer {
	return NewStackTreeRendererWithStrategy(eng, engine.SortStrategySmart, nil)
}

// NewStackTreeRendererWithFilter creates a tree renderer with a filter function
func NewStackTreeRendererWithFilter(eng engine.StackView, filter func(string) bool) *tree.StackTreeRenderer {
	return NewStackTreeRendererWithStrategy(eng, engine.SortStrategySmart, filter)
}

// NewStackTreeRendererWithStrategy creates a tree renderer with a specific sorting strategy and optional filter
func NewStackTreeRendererWithStrategy(eng engine.StackView, strategy engine.SortStrategy, filter func(string) bool) *tree.StackTreeRenderer {
	return newStackTreeRendererInternal(eng, strategy, filter, nil)
}

// NewStackTreeRendererWithEmptyWorktrees creates a tree renderer that shows empty worktree anchors
func NewStackTreeRendererWithEmptyWorktrees(eng engine.StackView, emptyWorktrees map[string]bool) *tree.StackTreeRenderer {
	return newStackTreeRendererInternal(eng, engine.SortStrategySmart, nil, emptyWorktrees)
}

// NewStackTreeRendererWithOptions creates a tree renderer with all options
func NewStackTreeRendererWithOptions(eng engine.StackView, strategy engine.SortStrategy, filter func(string) bool, emptyWorktrees map[string]bool) *tree.StackTreeRenderer {
	return newStackTreeRendererInternal(eng, strategy, filter, emptyWorktrees)
}

// graphData implements tree.Data using an engine.StackGraph.
// This decouples the tree renderer from the engine package.
type graphData struct {
	graph            *engine.StackGraph
	statuses         engine.BranchStatuses
	visibleAnchors   map[string]bool
	flattenedChildOf map[string][]string
}

// CurrentBranch returns the currently checked out branch.
func (d *graphData) CurrentBranch() string {
	return d.graph.CurrentBranch()
}

// Trunk returns the trunk/main branch name.
func (d *graphData) Trunk() string {
	return d.graph.TrunkName()
}

// Children returns the child branches of the given branch.
func (d *graphData) Children(branchName string) []string {
	node := d.graph.GetNode(branchName)
	if node == nil {
		return nil
	}

	// Cache flattened children for this parent since tree rendering
	// repeatedly asks for children during traversal.
	if d.flattenedChildOf != nil {
		if cached, ok := d.flattenedChildOf[branchName]; ok {
			return cached
		}
	}

	rawChildren := d.graph.Children(node.Branch)
	if len(rawChildren) == 0 {
		if d.flattenedChildOf != nil {
			d.flattenedChildOf[branchName] = nil
		}
		return nil
	}

	result := make([]string, 0, len(rawChildren))
	for _, childName := range rawChildren {
		result = append(result, d.flattenHiddenAnchors(childName)...)
	}

	if d.flattenedChildOf != nil {
		d.flattenedChildOf[branchName] = result
	}
	return result
}

// Parent returns the parent branch of the given branch.
func (d *graphData) Parent(branchName string) string {
	node := d.graph.GetNode(branchName)
	if node == nil {
		return ""
	}

	parent := d.graph.Parent(node.Branch)
	for parent != "" {
		if !d.isHiddenAnchor(parent) {
			return parent
		}
		parentNode := d.graph.GetNode(parent)
		if parentNode == nil {
			return ""
		}
		parent = d.graph.Parent(parentNode.Branch)
	}

	return ""
}

// IsTrunk returns whether the given branch is the trunk branch.
func (d *graphData) IsTrunk(branchName string) bool {
	return branchName == d.graph.TrunkName()
}

// IsFixed returns whether the branch is up-to-date with its parent.
func (d *graphData) IsFixed(branchName string) bool {
	if d.graph.GetNode(branchName) == nil {
		return false
	}
	return d.statuses.IsUpToDateByName(branchName)
}

func (d *graphData) isHiddenAnchor(branchName string) bool {
	node := d.graph.GetNode(branchName)
	if node == nil || !node.Branch.IsWorktreeAnchor() {
		return false
	}
	return !d.visibleAnchors[branchName]
}

func (d *graphData) flattenHiddenAnchors(branchName string) []string {
	node := d.graph.GetNode(branchName)
	if node == nil {
		return nil
	}
	if !d.isHiddenAnchor(branchName) {
		return []string{branchName}
	}

	children := d.graph.Children(node.Branch)
	if len(children) == 0 {
		return nil
	}

	result := make([]string, 0, len(children))
	for _, childName := range children {
		result = append(result, d.flattenHiddenAnchors(childName)...)
	}
	return result
}

// newStackTreeRendererInternal is the internal implementation that handles all renderer options
func newStackTreeRendererInternal(eng engine.StackView, strategy engine.SortStrategy, filter func(string) bool, emptyWorktrees map[string]bool) *tree.StackTreeRenderer {
	branchFilter := func(b engine.Branch) bool {
		if b.IsWorktreeAnchor() {
			// Keep anchors in the graph so children remain connected even when
			// anchors are hidden from rendering.
			return true
		}
		// Apply user-provided filter if any
		if filter != nil {
			return filter(b.GetName())
		}
		return true
	}

	graph := engine.BuildStackGraph(eng, strategy, branchFilter)

	// Resolve restack status for every branch in one batched parent-revision
	// read instead of a per-branch IsUpToDate() call during tree traversal
	// (each of which would shell a separate `git rev-parse` for the parent).
	statuses := eng.ReadBranchStatuses(eng.AllBranches())

	// Use the Data interface instead of callback functions
	data := &graphData{
		graph:            graph,
		statuses:         statuses,
		visibleAnchors:   emptyWorktrees,
		flattenedChildOf: make(map[string][]string),
	}

	return tree.NewRenderer(data)
}

// TreeAnnotation converts a stackview annotation into the tree component's
// render type. Annotation data is built in stackview; tui only renders it.
func TreeAnnotation(a stackview.BranchAnnotation) tree.BranchAnnotation {
	ann := tree.BranchAnnotation{
		PRNumber:        a.PRNumber,
		CheckStatus:     treeCheckStatus(a.Check),
		ReviewStatus:    treeReviewStatus(a.Review),
		IsDraft:         a.IsDraft,
		IsLocked:        a.IsLocked,
		IsFrozen:        a.IsFrozen,
		Scope:           a.Scope,
		ExplicitScope:   a.ExplicitScope,
		CommitCount:     a.CommitCount,
		LinesAdded:      a.LinesAdded,
		LinesDeleted:    a.LinesDeleted,
		PRState:         a.PRState,
		WorktreePath:    a.WorktreePath.String(),
		IsEmptyWorktree: a.IsEmptyWorktree,
		LocalSHA:        a.LocalSHA,
		CommitMessages:  a.CommitMessages,
		PRURL:           a.PRURL,
	}
	if len(a.MergedDownstack) > 0 {
		ann.MergedDownstack = make([]tree.MergedParentDisplay, len(a.MergedDownstack))
		for i, mp := range a.MergedDownstack {
			ann.MergedDownstack[i] = tree.MergedParentDisplay{
				BranchName: mp.BranchName,
				PRNumber:   mp.PRNumber,
			}
			if mp.PRState != nil {
				ann.MergedDownstack[i].PRState = *mp.PRState
			}
		}
	}
	return ann
}

func treeCheckStatus(state stackview.CheckState) tree.CheckStatus {
	switch state {
	case stackview.CheckPassing:
		return tree.CheckStatusPassing
	case stackview.CheckFailing:
		return tree.CheckStatusFailing
	case stackview.CheckPending:
		return tree.CheckStatusPending
	default:
		return ""
	}
}

func treeReviewStatus(state stackview.ReviewState) string {
	switch state {
	case stackview.ReviewApproved:
		return tree.ReviewStatusApproved
	case stackview.ReviewChangesRequested:
		return tree.ReviewStatusChangesRequested
	case stackview.ReviewAwaiting:
		return tree.ReviewStatusAwaitingReview
	default:
		return ""
	}
}
