package actions

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/tui/style"
	"stackit.dev/stackit/internal/utils"
)

// LogStyle defines the output style for the log command
const (
	LogStyleNormal   = "NORMAL"
	LogStyleFull     = "FULL"
	LogStyleShort    = "SHORT"
	LogStyleDetailed = "DETAILED"
)

// LogOptions contains options for the log command
type LogOptions struct {
	Style         string // LogStyleNormal, LogStyleFull, or LogStyleShort
	Steps         *int
	BranchName    string
	ShowUntracked bool
	Interactive   bool
	ShowSHAs      bool // Show commit SHAs next to branch names
}

// LogAction displays the branch tree
func LogAction(ctx *app.Context, opts LogOptions) error {
	// If interactive mode is requested or auto-detected
	if opts.Interactive || (utils.IsInteractive() && opts.Steps == nil) {
		// Run interactive TUI
		m := tui.NewLogModel(ctx.Context, ctx.Engine, ctx.GitHubClient, tui.LogOptions{
			Style:         opts.Style,
			ShowUntracked: opts.ShowUntracked,
			Logger:        ctx.Logger,
		})
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err := p.Run()
		return err
	}

	// Populate remote SHAs if needed (for FULL and DETAILED modes)
	if opts.Style == LogStyleFull || opts.Style == LogStyleDetailed {
		if err := ctx.Engine.PopulateRemoteShas(); err != nil {
			ctx.Output.Debug("Failed to populate remote SHAs: %v", err)
		}
	}

	// Detect empty worktrees (worktree anchors with no children)
	emptyWorktrees := getEmptyWorktrees(ctx)

	// Create tree renderer - use empty worktrees-aware version if we have any
	var renderer *tree.StackTreeRenderer
	if len(emptyWorktrees) > 0 {
		emptyWorktreeNames := make(map[string]bool)
		for name := range emptyWorktrees {
			emptyWorktreeNames[name] = true
		}
		renderer = tui.NewStackTreeRendererWithEmptyWorktrees(ctx.Engine, emptyWorktreeNames)
	} else {
		renderer = tui.NewStackTreeRenderer(ctx.Engine)
	}

	// Render the stack
	// First, collect annotations for all branches in the stack using a worker pool
	annotations := make(map[string]tree.BranchAnnotation)
	allBranches := ctx.Engine.AllBranches()

	// Prefetch CI status in batch if in FULL or DETAILED style
	var ciStatuses map[string]*github.CheckStatus
	if (opts.Style == LogStyleFull || opts.Style == LogStyleDetailed) && ctx.GitHubClient != nil {
		branchNames := make([]string, 0, len(allBranches))
		for _, b := range allBranches {
			if !b.IsTrunk() {
				branchNames = append(branchNames, b.GetName())
			}
		}
		if len(branchNames) > 0 {
			ciStatuses, _ = ctx.GitHubClient.BatchGetPRChecksStatus(ctx.Context, branchNames)
		}
	}

	type result struct {
		branchName string
		annotation tree.BranchAnnotation
	}
	results := make(chan result, len(allBranches))

	if len(allBranches) > 0 {
		utils.Run(allBranches, func(branchObj engine.Branch) {
			annotation := getBranchAnnotation(ctx, branchObj, opts, ciStatuses, emptyWorktrees)
			results <- result{branchObj.GetName(), annotation}
		})
	}
	close(results)

	for res := range results {
		annotations[res.branchName] = res.annotation
	}

	renderer.SetAnnotations(annotations)

	// Determine render mode based on style
	renderMode := tree.RenderModeFull
	if opts.Style == LogStyleDetailed {
		renderMode = tree.RenderModeDetailed
	}

	stackLines := renderer.RenderStack(opts.BranchName, tree.RenderOptions{
		Mode:                renderMode,
		Steps:               opts.Steps,
		ShowSHAs:            opts.ShowSHAs,
		ShowCommitMessages:  opts.Style == LogStyleDetailed,
		HideSummary:         opts.Style == LogStyleShort,
		SkipSelectionPrefix: opts.Style == LogStyleDetailed,
	})

	// Add summary footer
	branchCount := 0
	approvedCount := 0
	inReviewCount := 0
	for name, ann := range annotations {
		branch := ctx.Engine.GetBranch(name)
		if branch.IsTrunk() || branch.IsWorktreeAnchor() {
			continue
		}
		branchCount++
		if ann.ReviewStatus == "Approved" {
			approvedCount++
		} else if ann.ReviewStatus == "In Review" {
			inReviewCount++
		}
	}

	if branchCount > 0 {
		summaryParts := []string{fmt.Sprintf("%d branches", branchCount)}
		if approvedCount > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%d approved", approvedCount))
		}
		if inReviewCount > 0 {
			summaryParts = append(summaryParts, fmt.Sprintf("%d in review", inReviewCount))
		}
		stackLines = append(stackLines, "")
		stackLines = append(stackLines, style.ColorDim(strings.Join(summaryParts, " · ")))
	}

	// Add untracked branches if requested
	if opts.ShowUntracked {
		untracked := getUntrackedBranchNames(ctx)
		if len(untracked) > 0 {
			stackLines = append(stackLines, "")
			stackLines = append(stackLines, "Untracked branches:")
			stackLines = append(stackLines, untracked...)
		}
	}

	// Output the result
	ctx.Output.Print(strings.Join(stackLines, "\n"))
	ctx.Output.Newline()

	return nil
}

func getUntrackedBranchNames(ctx *app.Context) []string {
	untracked := engine.FilterBranches(ctx.Engine, func(b engine.Branch) bool {
		return !b.IsTrunk() && !b.IsTracked()
	})
	names := make([]string, len(untracked))
	for i, b := range untracked {
		names[i] = b.GetName()
	}
	return names
}

// getEmptyWorktrees returns a map of worktree anchor branch names to their WorktreeInfo
// for worktrees that have no child branches (empty worktrees).
func getEmptyWorktrees(ctx *app.Context) map[string]*engine.WorktreeInfo {
	emptyWorktrees := make(map[string]*engine.WorktreeInfo)

	worktrees, err := ctx.Engine.ListManagedWorktrees()
	if err != nil {
		return emptyWorktrees
	}

	for i := range worktrees {
		wt := &worktrees[i]
		anchor := ctx.Engine.GetBranch(wt.AnchorBranch)
		if !anchor.IsTracked() || !anchor.IsWorktreeAnchor() {
			continue
		}

		// Check if anchor has any children
		hasChildren := false
		for _, depth := range ctx.Engine.BranchesDepthFirst(anchor) {
			if depth > 0 {
				hasChildren = true
				break
			}
		}

		if !hasChildren {
			emptyWorktrees[wt.AnchorBranch] = wt
		}
	}

	return emptyWorktrees
}

func getBranchAnnotation(ctx *app.Context, branchObj engine.Branch, opts LogOptions, ciStatuses map[string]*github.CheckStatus, emptyWorktrees map[string]*engine.WorktreeInfo) tree.BranchAnnotation {
	annotation := tui.GetBranchAnnotation(ctx.Engine, branchObj)

	// Working tree changes (only in DETAILED mode, only for current branch)
	if opts.Style == LogStyleDetailed && !branchObj.IsTrunk() {
		currentBranch := ctx.Engine.CurrentBranch()
		if currentBranch != nil && currentBranch.GetName() == branchObj.GetName() {
			if changes, err := ctx.Engine.GetPendingChanges(ctx.Context); err == nil && len(changes) > 0 {
				annotation.HasWorkingTreeChanges = true
				for _, change := range changes {
					if !change.Staged {
						annotation.UnstagedFiles = append(annotation.UnstagedFiles, change.Status+" "+change.Path)
					}
				}
			}
		}
	}

	// CI status (only in FULL or DETAILED mode)
	if (opts.Style == LogStyleFull || opts.Style == LogStyleDetailed) && !branchObj.IsTrunk() {
		status := ciStatuses[branchObj.GetName()]
		if status != nil {
			annotation.CheckStatus = tree.CheckStatusPassing
			if status.Pending {
				annotation.CheckStatus = tree.CheckStatusPending
			} else if !status.Passing {
				annotation.CheckStatus = tree.CheckStatusFailing
			}
		}
	}

	// Check if this is an empty worktree anchor
	if wtInfo, ok := emptyWorktrees[branchObj.GetName()]; ok {
		annotation.IsEmptyWorktree = true
		annotation.WorktreePath = wtInfo.Path
		return annotation
	}

	// Check if this branch is a stack root with a managed worktree
	stackRoot := ctx.Engine.GetStackRootForBranch(branchObj)
	if stackRoot == branchObj.GetName() {
		if wtInfo, err := ctx.Engine.GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
			annotation.WorktreePath = wtInfo.Path
		}
	}

	return annotation
}
