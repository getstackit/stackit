package tui

import (
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/tui/components/tree"
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
	return newStackTreeRendererInternal(eng, strategy, filter, nil)
}

// NewStackTreeRendererWithEmptyWorktrees creates a tree renderer that shows empty worktree anchors
func NewStackTreeRendererWithEmptyWorktrees(eng engine.BranchReader, emptyWorktrees map[string]bool) *tree.StackTreeRenderer {
	return newStackTreeRendererInternal(eng, engine.SortStrategySmart, nil, emptyWorktrees)
}

// NewStackTreeRendererWithOptions creates a tree renderer with all options
func NewStackTreeRendererWithOptions(eng engine.BranchReader, strategy engine.SortStrategy, filter func(string) bool, emptyWorktrees map[string]bool) *tree.StackTreeRenderer {
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
func newStackTreeRendererInternal(eng engine.BranchReader, strategy engine.SortStrategy, filter func(string) bool, emptyWorktrees map[string]bool) *tree.StackTreeRenderer {
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

// WorktreeData holds pre-computed worktree information from a single ListManagedWorktrees call.
type WorktreeData struct {
	EmptyWorktrees      map[string]*engine.WorktreeInfo // Anchor branches with no children
	WorktreeByStackRoot map[string]*engine.WorktreeInfo // Stack root -> worktree info
}

// GetWorktreeData builds both empty worktree and stack-root worktree maps from a single
// ListManagedWorktrees call, avoiding redundant lookups.
func GetWorktreeData(eng engine.Engine) *WorktreeData {
	data := &WorktreeData{
		EmptyWorktrees:      make(map[string]*engine.WorktreeInfo),
		WorktreeByStackRoot: make(map[string]*engine.WorktreeInfo),
	}

	worktrees, err := eng.ListManagedWorktrees()
	if err != nil {
		return data
	}

	for i := range worktrees {
		wt := &worktrees[i]
		anchor := eng.GetBranch(wt.AnchorBranch)

		// Build stack-root map for all worktrees (used by addWorktreeInfo)
		data.WorktreeByStackRoot[wt.AnchorBranch] = wt

		if !anchor.IsTracked() || !anchor.IsWorktreeAnchor() {
			continue
		}

		// Check if anchor has any children
		hasChildren := false
		for _, depth := range eng.BranchesDepthFirst(anchor) {
			if depth > 0 {
				hasChildren = true
				break
			}
		}

		if !hasChildren {
			data.EmptyWorktrees[wt.AnchorBranch] = wt
		}
	}

	return data
}

// GetEmptyWorktrees returns a map of worktree anchor branch names to their WorktreeInfo
// for worktrees that have no child branches (empty worktrees).
func GetEmptyWorktrees(eng engine.Engine) map[string]*engine.WorktreeInfo {
	return GetWorktreeData(eng).EmptyWorktrees
}

// GetMinimalAnnotationWithWorktreeAndEmpty returns minimal annotations plus worktree info,
// with support for marking empty worktrees.
// This is used for fast initial rendering before full data is loaded.
// Only includes cached/instant fields - no git or network calls.
func GetMinimalAnnotationWithWorktreeAndEmpty(eng engine.Engine, branch engine.Branch, wtData *WorktreeData) tree.BranchAnnotation {
	ann := tree.BranchAnnotation{
		IsLocked:      branch.IsLocked(),
		IsFrozen:      branch.IsFrozen(),
		Scope:         eng.GetScope(branch).String(),
		ExplicitScope: branch.GetExplicitScope().String(),
	}

	addWorktreeInfo(eng, branch, &ann, wtData)

	return ann
}

// addWorktreeInfo populates worktree-related fields on a BranchAnnotation.
// If the branch is an empty worktree anchor, it sets IsEmptyWorktree and WorktreePath.
// Otherwise, if the branch belongs to a stack that has a managed worktree,
// it sets WorktreePath so stack ownership is visible on every branch.
// When wtData is provided, uses O(1) map lookups instead of calling GetWorktreeForStack.
func addWorktreeInfo(eng engine.Engine, branch engine.Branch, ann *tree.BranchAnnotation, wtData *WorktreeData) {
	if wtData != nil {
		if wtInfo, ok := wtData.EmptyWorktrees[branch.GetName()]; ok {
			ann.IsEmptyWorktree = true
			ann.WorktreePath = wtInfo.Path.String()
			return
		}
	}

	stackRoot := eng.GetStackRootForBranch(branch)
	// Use pre-built map if available, otherwise fall back to engine call.
	if wtData != nil {
		if wtInfo, ok := wtData.WorktreeByStackRoot[stackRoot]; ok {
			ann.WorktreePath = wtInfo.Path.String()
		}
	} else if wtInfo, err := eng.GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
		ann.WorktreePath = wtInfo.Path.String()
	}
}

// AnnotationEnrichment holds pre-fetched data for enriching branch annotations.
// This allows CI statuses and worktree info to be computed once and shared
// across all branches, avoiding redundant lookups.
type AnnotationEnrichment struct {
	CIStatuses          github.ChecksByBranch
	EmptyWorktrees      map[string]*engine.WorktreeInfo
	WorktreeByStackRoot map[string]*engine.WorktreeInfo
}

// AnnotationOptions controls which expensive git-derived fields are skipped.
// The zero value populates every field — opt out of work, never into it, so
// callers that forget the struct still get a complete annotation.
type AnnotationOptions struct {
	SkipCommitMessages bool
	SkipDiffStats      bool
}

// BuildFullAnnotation returns a fully populated BranchAnnotation including
// git operations (SHA, commits, diff stats), CI status, review status, and worktree info.
// Pass nil for enrichment to get just the base annotation without CI/worktree enrichment.
// Pass AnnotationOptions{} for the full annotation, or set Skip* flags to skip work.
func BuildFullAnnotation(eng engine.Engine, branch engine.Branch, stat engine.BranchStat, enrichment *AnnotationEnrichment, opts AnnotationOptions) tree.BranchAnnotation {
	ann := GetBranchAnnotation(eng, branch, stat, opts)

	if enrichment == nil {
		return ann
	}

	// Apply CI status and review status
	if !branch.IsTrunk() {
		if status := enrichment.CIStatuses.Get(branch.GetName()); status != nil {
			ann.CheckStatus = tree.CheckStatusPassing
			if status.Pending {
				ann.CheckStatus = tree.CheckStatusPending
			} else if !status.Passing {
				ann.CheckStatus = tree.CheckStatusFailing
			}

			switch status.ReviewDecision {
			case github.ReviewDecisionApproved:
				ann.ReviewStatus = tree.ReviewStatusApproved
			case github.ReviewDecisionChangesRequested:
				ann.ReviewStatus = tree.ReviewStatusChangesRequested
			case github.ReviewDecisionReviewRequired:
				ann.ReviewStatus = "Awaiting Review"
			}
		}
	}

	// Apply worktree info
	addWorktreeInfo(eng, branch, &ann, &WorktreeData{
		EmptyWorktrees:      enrichment.EmptyWorktrees,
		WorktreeByStackRoot: enrichment.WorktreeByStackRoot,
	})

	return ann
}

// GetBranchAnnotation returns a tree.BranchAnnotation for a branch. The
// git-computed fields (short SHA, commit count, diff stats) come from stat, a
// batched value the caller resolves with BatchBranchStats — code annotating a
// set of branches must build that batch rather than reading per branch. Pass
// AnnotationOptions{} for the full annotation, or set Skip* flags to skip work.
func GetBranchAnnotation(eng engine.BranchReader, branch engine.Branch, stat engine.BranchStat, opts AnnotationOptions) tree.BranchAnnotation {
	ann := tree.BranchAnnotation{
		IsLocked:      branch.IsLocked(),
		IsFrozen:      branch.IsFrozen(),
		Scope:         eng.GetScope(branch).String(),
		ExplicitScope: branch.GetExplicitScope().String(),
		LocalSHA:      stat.ShortSHA,
	}

	if !branch.IsTrunk() {
		// PR info (local metadata)
		if prInfo, _ := branch.GetPrInfo(); prInfo != nil {
			ann.PRNumber = prInfo.Number()
			ann.PRState = prInfo.State()
			ann.IsDraft = prInfo.IsDraft()
			ann.PRURL = prInfo.URL()
		}

		// Commit messages for the detailed view; otherwise the batched count.
		if !opts.SkipCommitMessages {
			if commits, err := branch.GetAllCommits(engine.CommitFormatReadable); err == nil {
				ann.CommitMessages = commits
				ann.CommitCount = len(commits)
			}
		} else {
			ann.CommitCount = stat.CommitCount
		}

		// Merged downstack history
		if mergedHistory := branch.GetMergedDownstack(); len(mergedHistory) > 0 {
			ann.MergedDownstack = make([]tree.MergedParentDisplay, len(mergedHistory))
			for i, mp := range mergedHistory {
				ann.MergedDownstack[i] = tree.MergedParentDisplay{
					BranchName: mp.BranchName,
					PRNumber:   mp.PRNumber,
				}
				if mp.PRState != nil {
					ann.MergedDownstack[i].PRState = *mp.PRState
				}
			}
		}

		// Local stats
		if !opts.SkipDiffStats {
			ann.LinesAdded = stat.LinesAdded
			ann.LinesDeleted = stat.LinesDeleted
		}
	}

	return ann
}
