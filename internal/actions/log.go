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
	LogStyleNormal = "NORMAL"
	LogStyleFull   = "FULL"
)

// LogOptions contains options for the log command
type LogOptions struct {
	Style         string // LogStyleNormal or LogStyleFull
	Reverse       bool
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
			Reverse:       opts.Reverse,
			ShowUntracked: opts.ShowUntracked,
			Logger:        ctx.Logger,
		})
		p := tea.NewProgram(m, tea.WithAltScreen())
		_, err := p.Run()
		return err
	}

	// Populate remote SHAs if needed (only for FULL mode)
	if opts.Style == LogStyleFull {
		if err := ctx.Engine.PopulateRemoteShas(); err != nil {
			ctx.Output.Debug("Failed to populate remote SHAs: %v", err)
		}
	}

	// Create tree renderer
	renderer := tui.NewStackTreeRenderer(ctx.Engine)

	// Render the stack
	// First, collect annotations for all branches in the stack using a worker pool
	annotations := make(map[string]tree.BranchAnnotation)
	allBranches := ctx.Engine.AllBranches()

	// Prefetch CI status in batch if in FULL style
	var ciStatuses map[string]*github.CheckStatus
	if opts.Style == LogStyleFull && ctx.GitHubClient != nil {
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
			annotation := getBranchAnnotation(ctx, branchObj, opts, ciStatuses)
			results <- result{branchObj.GetName(), annotation}
		})
	}
	close(results)

	for res := range results {
		annotations[res.branchName] = res.annotation
	}

	renderer.SetAnnotations(annotations)

	stackLines := renderer.RenderStack(opts.BranchName, tree.RenderOptions{
		Short:    false, // We want the full tree characters with stats
		Reverse:  opts.Reverse,
		Steps:    opts.Steps,
		ShowSHAs: opts.ShowSHAs,
	})

	// Add summary footer
	branchCount := 0
	approvedCount := 0
	inReviewCount := 0
	for name, ann := range annotations {
		branch := ctx.Engine.GetBranch(name)
		if branch.IsTrunk() {
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
	var untracked []string
	for _, branch := range ctx.Engine.AllBranches() {
		branchName := branch.GetName()
		if !branch.IsTrunk() && !branch.IsTracked() {
			untracked = append(untracked, branchName)
		}
	}
	return untracked
}

func getBranchAnnotation(ctx *app.Context, branchObj engine.Branch, opts LogOptions, ciStatuses map[string]*github.CheckStatus) tree.BranchAnnotation {
	annotation := tui.GetBranchAnnotation(ctx.Engine, branchObj)

	// CI status (only in FULL mode)
	if opts.Style == LogStyleFull && !branchObj.IsTrunk() {
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

	// Check if this branch is a stack root with a managed worktree
	stackRoot := ctx.Engine.GetStackRootForBranch(branchObj)
	if stackRoot == branchObj.GetName() {
		if wtInfo, err := ctx.Engine.GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
			annotation.WorktreePath = wtInfo.Path
		}
	}

	return annotation
}
