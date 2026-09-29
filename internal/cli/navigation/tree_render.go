package navigation

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/actions/stackview"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/utils"
)

// RunTree displays the branch tree: JSON, the interactive TUI, or the static
// rendered tree, depending on opts. The stack data comes from actions and
// stackview; this adapter only renders it.
func RunTree(ctx *app.Context, opts actions.TreeOptions) error {
	if opts.JSON {
		return printTreeJSON(ctx, opts)
	}

	if opts.Interactive || (utils.IsInteractive() && opts.Steps == nil) {
		m := tui.NewTreeModel(ctx.Context, ctx.Engine, ctx.GitHub(), tui.TreeOptions{
			Style:         string(opts.Style),
			ShowUntracked: opts.ShowUntracked,
			Logger:        ctx.Logger,
		})
		m.SetAltScreen(true)
		p := tea.NewProgram(m)
		_, err := p.Run()
		return err
	}

	return printTree(ctx, opts)
}

func printTreeJSON(ctx *app.Context, opts actions.TreeOptions) error {
	result := actions.BuildTreeJSON(ctx, opts)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	ctx.Output.Info("%s", string(data))
	return nil
}

func printTree(ctx *app.Context, opts actions.TreeOptions) error {
	eng := ctx.Engine

	// One managed-worktree listing backs both the renderer (which empty
	// anchors stay visible) and the per-branch worktree annotations.
	worktrees := stackview.BuildWorktreeIndex(eng)

	var renderer *tree.StackTreeRenderer
	if emptyAnchors := worktrees.EmptyAnchorNames(); emptyAnchors != nil {
		renderer = tui.NewStackTreeRendererWithEmptyWorktrees(eng, emptyAnchors)
	} else {
		renderer = tui.NewStackTreeRenderer(eng)
	}

	renderOpts := tree.RenderOptions{
		Mode:        tree.RenderModeFull, // We want the full tree characters with stats
		Steps:       opts.Steps,
		ShowSHAs:    opts.ShowSHAs,
		HideSummary: opts.Style == actions.TreeStyleShort,
	}
	visibleBranches := visibleTreeBranches(renderer, opts.BranchName, renderOpts, eng.AllBranches())

	annotations := buildTreeAnnotations(ctx, opts, visibleBranches, worktrees)
	renderer.SetAnnotations(annotations)

	stackLines := renderer.RenderStack(opts.BranchName, renderOpts)
	stackLines = append(stackLines, treeSummaryLines(eng, annotations)...)

	if opts.ShowUntracked {
		untracked := engine.FilterBranches(eng, func(b engine.Branch) bool {
			return !b.IsTrunk() && !b.IsTracked()
		}).Names()
		if len(untracked) > 0 {
			stackLines = append(stackLines, "", "Untracked branches:")
			stackLines = append(stackLines, untracked...)
		}
	}

	ctx.Output.Print(strings.Join(stackLines, "\n"))
	ctx.Output.Newline()
	return nil
}

// buildTreeAnnotations resolves annotations for just the branches that will
// be rendered, so bounded views stay cheap.
func buildTreeAnnotations(ctx *app.Context, opts actions.TreeOptions, visibleBranches engine.Branches, worktrees *stackview.WorktreeIndex) map[string]tree.BranchAnnotation {
	eng := ctx.Engine

	// The git-computed stats (short SHA, commit count, diff stats) come from
	// one batched read; the short style needs none.
	var stats map[string]engine.BranchStat
	if opts.Style != actions.TreeStyleShort {
		stats = eng.BatchBranchStats(visibleBranches)
	}

	// The short style skips BatchBranchStats above, so with --shas it needs its
	// own batched revision lookup instead of resolving each SHA individually.
	var revisions engine.RevisionMap
	if opts.Style == actions.TreeStyleShort && opts.ShowSHAs {
		revisions = eng.BatchRevisions(visibleBranches)
	}

	// Forge status is fetched separately (FULL style only) and joined below.
	var ciStatuses github.ChecksByBranch
	if opts.Style == actions.TreeStyleFull && ctx.GitHub() != nil {
		branchNames := visibleBranches.Select(engine.BranchFilter{ExcludeTrunk: true, RequirePR: true}).Names()
		if len(branchNames) > 0 {
			ciStatuses, _ = ctx.GitHub().BatchGetPRChecksStatus(ctx.Context, branchNames)
		}
	}

	enrichment := &stackview.Enrichment{
		CIStatuses: ciStatuses,
		Worktrees:  worktrees,
	}

	// Build into a positional slice so concurrent workers don't race on a
	// shared map, then assemble the map serially.
	type indexedBranch struct {
		index  int
		branch engine.Branch
	}
	items := make([]indexedBranch, len(visibleBranches))
	for i, b := range visibleBranches {
		items[i] = indexedBranch{index: i, branch: b}
	}
	built := make([]tree.BranchAnnotation, len(visibleBranches))
	utils.Run(items, func(item indexedBranch) {
		name := item.branch.GetName()
		if opts.Style != actions.TreeStyleShort {
			built[item.index] = tui.TreeAnnotation(stackview.FullAnnotation(eng, item.branch, stats[name], enrichment, stackview.AnnotationOptions{
				SkipCommitMessages: true,
			}))
			return
		}
		// The short style has no stats; its SHA comes from the batched revisions.
		annotation := tui.TreeAnnotation(stackview.MinimalAnnotation(eng, item.branch, worktrees))
		if opts.ShowSHAs && revisions[name] != "" {
			annotation.LocalSHA = utils.ShortRevision(revisions[name], 0)
		}
		built[item.index] = annotation
	})

	annotations := make(map[string]tree.BranchAnnotation, len(visibleBranches))
	for i, b := range visibleBranches {
		annotations[b.GetName()] = built[i]
	}
	return annotations
}

// treeSummaryLines renders the "N branches · N approved · N in review" footer.
func treeSummaryLines(eng engine.BranchReader, annotations map[string]tree.BranchAnnotation) []string {
	branchCount := 0
	approvedCount := 0
	inReviewCount := 0
	for name, ann := range annotations {
		branch := eng.GetBranch(name)
		if branch.IsTrunk() || branch.IsWorktreeAnchor() {
			continue
		}
		branchCount++
		switch ann.ReviewStatus {
		case tree.ReviewStatusApproved:
			approvedCount++
		case "In Review":
			inReviewCount++
		}
	}

	if branchCount == 0 {
		return nil
	}
	summaryParts := []string{fmt.Sprintf("%d branches", branchCount)}
	if approvedCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d approved", approvedCount))
	}
	if inReviewCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d in review", inReviewCount))
	}
	return []string{"", output.Dim(strings.Join(summaryParts, " · "))}
}

// visibleTreeBranches returns the branches the renderer will actually draw,
// in render order, so annotation work is scoped to them.
func visibleTreeBranches(renderer *tree.StackTreeRenderer, branchName string, opts tree.RenderOptions, branches engine.Branches) engine.Branches {
	branchByName := make(map[string]engine.Branch, len(branches))
	for _, b := range branches {
		branchByName[b.GetName()] = b
	}

	rendered := renderer.RenderStackDetailed(branchName, opts)
	visible := engine.NewBranchesBuilder(len(rendered))
	seen := make(map[string]struct{}, len(rendered))
	for _, renderedBranch := range rendered {
		if _, ok := seen[renderedBranch.Name]; ok {
			continue
		}
		seen[renderedBranch.Name] = struct{}{}
		if branch, ok := branchByName[renderedBranch.Name]; ok {
			visible.Add(branch)
		}
	}
	return visible.Build()
}
