package actions

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	LogStyleShort  = "SHORT"
)

// LogOptions contains options for the log command
type LogOptions struct {
	Style         string // LogStyleNormal, LogStyleFull, or LogStyleShort
	Steps         *int
	BranchName    string
	ShowUntracked bool
	Interactive   bool
	ShowSHAs      bool // Show commit SHAs next to branch names
	JSON          bool // Output in JSON format
}

// LogJSONResult represents the JSON output for the log command
type LogJSONResult struct {
	Branches        []LogBranchInfo `json:"branches"`
	Summary         LogSummary      `json:"summary"`
	GitHubAvailable bool            `json:"github_available"`
}

// LogBranchInfo represents a single branch in JSON output
type LogBranchInfo struct {
	Name         string     `json:"name"`
	Parent       string     `json:"parent,omitempty"`
	IsCurrent    bool       `json:"is_current"`
	IsTrunk      bool       `json:"is_trunk"`
	IsLocked     bool       `json:"is_locked,omitempty"`
	IsFrozen     bool       `json:"is_frozen,omitempty"`
	NeedsRestack bool       `json:"needs_restack,omitempty"`
	Commits      int        `json:"commits"`
	Additions    int        `json:"additions,omitempty"`
	Deletions    int        `json:"deletions,omitempty"`
	PR           *LogPRInfo `json:"pr,omitempty"`
	Scope        string     `json:"scope,omitempty"`
	Children     []string   `json:"children,omitempty"`
}

// LogPRInfo represents PR information in JSON output
type LogPRInfo struct {
	Number       int    `json:"number"`
	URL          string `json:"url,omitempty"`
	Title        string `json:"title,omitempty"`
	State        string `json:"state"`
	IsDraft      bool   `json:"is_draft,omitempty"`
	ReviewStatus string `json:"review_status,omitempty"`
	CIStatus     string `json:"ci_status,omitempty"`
}

// LogSummary represents summary statistics in JSON output
type LogSummary struct {
	TotalBranches int `json:"total_branches"`
	ApprovedCount int `json:"approved_count"`
	InReviewCount int `json:"in_review_count"`
}

// LogAction displays the branch tree
func LogAction(ctx *app.Context, opts LogOptions) error {
	// JSON output mode
	if opts.JSON {
		return logActionJSON(ctx, opts)
	}

	// If interactive mode is requested or auto-detected
	if opts.Interactive || (utils.IsInteractive() && opts.Steps == nil) {
		// Run interactive TUI
		m := tui.NewLogModel(ctx.Context, ctx.Engine, ctx.GitHub(), tui.LogOptions{
			Style:         opts.Style,
			ShowUntracked: opts.ShowUntracked,
			Logger:        ctx.Logger,
		})
		m.SetAltScreen(true)
		p := tea.NewProgram(m)
		_, err := p.Run()
		return err
	}

	// Populate remote SHAs if needed (only for FULL mode)
	if opts.Style == LogStyleFull {
		if err := ctx.Engine.PopulateRemoteShas(); err != nil {
			ctx.Output.Debug("Failed to populate remote SHAs: %v", err)
		}
	}

	// Detect worktrees (builds both empty and stack-root maps in one call)
	wtData := tui.GetWorktreeData(ctx.Engine)

	// Create tree renderer - use empty worktrees-aware version if we have any
	var renderer *tree.StackTreeRenderer
	if len(wtData.EmptyWorktrees) > 0 {
		emptyWorktreeNames := make(map[string]bool)
		for name := range wtData.EmptyWorktrees {
			emptyWorktreeNames[name] = true
		}
		renderer = tui.NewStackTreeRendererWithEmptyWorktrees(ctx.Engine, emptyWorktreeNames)
	} else {
		renderer = tui.NewStackTreeRenderer(ctx.Engine)
	}

	// Pre-load metadata and revisions for all branches to eliminate per-branch
	// cache misses during parallel annotation building.
	ctx.Engine.PreloadBranchData()

	// Render the stack
	// First, collect annotations for all branches in the stack using a worker pool
	annotations := make(map[string]tree.BranchAnnotation)
	allBranches := ctx.Engine.AllBranches()

	// Prefetch CI status in batch if in FULL style
	var ciStatuses map[string]*github.CheckStatus
	if opts.Style == LogStyleFull && ctx.GitHub() != nil {
		branchNames := make([]string, 0, len(allBranches))
		for _, b := range allBranches {
			if !b.IsTrunk() {
				branchNames = append(branchNames, b.GetName())
			}
		}
		if len(branchNames) > 0 {
			ciStatuses, _ = ctx.GitHub().BatchGetPRChecksStatus(ctx.Context, branchNames)
		}
	}

	enrichment := &tui.AnnotationEnrichment{
		CIStatuses:          ciStatuses,
		EmptyWorktrees:      wtData.EmptyWorktrees,
		WorktreeByStackRoot: wtData.WorktreeByStackRoot,
	}

	type result struct {
		branchName string
		annotation tree.BranchAnnotation
	}
	results := make(chan result, len(allBranches))

	if len(allBranches) > 0 {
		utils.Run(allBranches, func(branchObj engine.Branch) {
			annotation := tui.BuildFullAnnotation(ctx.Engine, branchObj, enrichment)
			results <- result{branchObj.GetName(), annotation}
		})
	}
	close(results)

	for res := range results {
		annotations[res.branchName] = res.annotation
	}

	renderer.SetAnnotations(annotations)

	stackLines := renderer.RenderStack(opts.BranchName, tree.RenderOptions{
		Mode:        tree.RenderModeFull, // We want the full tree characters with stats
		Steps:       opts.Steps,
		ShowSHAs:    opts.ShowSHAs,
		HideSummary: opts.Style == LogStyleShort,
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
		switch ann.ReviewStatus {
		case "Approved":
			approvedCount++
		case "In Review":
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

// logActionJSON generates JSON output for the log command
func logActionJSON(ctx *app.Context, opts LogOptions) error {
	eng := ctx.Engine
	currentBranch := eng.CurrentBranch()
	currentBranchName := ""
	if currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}

	// Build stack graph
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)

	// Get all branches in stack order
	var branchesToInclude []engine.Branch
	if opts.BranchName != "" && opts.BranchName != eng.Trunk().GetName() {
		targetBranch := eng.GetBranch(opts.BranchName)
		stackRange := engine.StackRange{
			RecursiveParents:  true,
			IncludeCurrent:    true,
			RecursiveChildren: true,
		}
		branchesToInclude = graph.Range(targetBranch, stackRange)
	} else {
		// Get all tracked branches
		branchesToInclude = eng.AllBranches()
	}

	// Pre-load metadata and revisions for all branches to eliminate per-branch
	// cache misses during annotation building.
	eng.PreloadBranchData()

	// Prefetch CI status for JSON output (always fetched to provide complete data)
	ghClient := ctx.GitHub()
	var ciStatuses map[string]*github.CheckStatus
	if ghClient != nil {
		branchNames := make([]string, 0, len(branchesToInclude))
		for _, b := range branchesToInclude {
			if !b.IsTrunk() {
				branchNames = append(branchNames, b.GetName())
			}
		}
		if len(branchNames) > 0 {
			ciStatuses, _ = ghClient.BatchGetPRChecksStatus(ctx.Context, branchNames)
		}
	}

	// Build result
	result := LogJSONResult{
		Branches:        []LogBranchInfo{},
		Summary:         LogSummary{},
		GitHubAvailable: ghClient != nil,
	}

	// Collect branch info in parallel using worker pool (each branch requires
	// git subprocesses for commits and diff stats)
	type branchResult struct {
		info LogBranchInfo
	}
	branchResults := make(chan branchResult, len(branchesToInclude))

	// Filter out worktree anchors before parallel processing
	var processable []engine.Branch
	for _, b := range branchesToInclude {
		if !b.IsWorktreeAnchor() {
			processable = append(processable, b)
		}
	}

	if len(processable) > 0 {
		utils.Run(processable, func(branch engine.Branch) {
			branchName := branch.GetName()

			info := LogBranchInfo{
				Name:         branchName,
				IsCurrent:    branchName == currentBranchName,
				IsTrunk:      branch.IsTrunk(),
				IsLocked:     branch.IsLocked(),
				IsFrozen:     branch.IsFrozen(),
				NeedsRestack: !branch.IsBranchUpToDate() && !branch.IsTrunk(),
			}

			// Parent
			if parent := branch.GetParent(); parent != nil {
				info.Parent = parent.GetName()
			}

			// Scope
			if scope := branch.GetScope(); !scope.IsNone() {
				info.Scope = scope.String()
			}

			// Children
			children := graph.ChildBranches(branch)
			for _, child := range children {
				if !child.IsWorktreeAnchor() {
					info.Children = append(info.Children, child.GetName())
				}
			}

			// Commits and diff stats
			if !branch.IsTrunk() {
				commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
				if err == nil {
					info.Commits = len(commits)
				}

				added, deleted, err := branch.GetDiffStats()
				if err == nil {
					info.Additions = added
					info.Deletions = deleted
				}
			}

			// PR info
			if !branch.IsTrunk() {
				prInfo, _ := branch.GetPrInfo()
				if prInfo != nil && prInfo.Number() != nil {
					info.PR = &LogPRInfo{
						Number:  *prInfo.Number(),
						URL:     prInfo.URL(),
						Title:   prInfo.Title(),
						State:   prInfo.State(),
						IsDraft: prInfo.IsDraft(),
					}

					// CI status
					if ciStatuses != nil {
						if status, ok := ciStatuses[branchName]; ok && status != nil {
							switch {
							case status.Pending:
								info.PR.CIStatus = "pending"
							case status.Passing:
								info.PR.CIStatus = "passing"
							default:
								info.PR.CIStatus = "failing"
							}

							// Review status
							switch status.ReviewDecision {
							case github.ReviewDecisionApproved:
								info.PR.ReviewStatus = "approved"
							case github.ReviewDecisionChangesRequested:
								info.PR.ReviewStatus = "changes_requested"
							case github.ReviewDecisionReviewRequired:
								info.PR.ReviewStatus = "review_required"
							}
						}
					}
				}
			}

			branchResults <- branchResult{info: info}
		})
	}
	close(branchResults)

	for res := range branchResults {
		info := res.info
		result.Branches = append(result.Branches, info)

		// Update summary (exclude trunk)
		if !info.IsTrunk {
			result.Summary.TotalBranches++
			if info.PR != nil && info.PR.ReviewStatus == "approved" {
				result.Summary.ApprovedCount++
			} else if info.PR != nil && info.PR.ReviewStatus == "review_required" {
				result.Summary.InReviewCount++
			}
		}
	}

	// Keep output stable across runs even when collection happens in parallel.
	slices.SortFunc(result.Branches, func(a, b LogBranchInfo) int {
		return cmp.Compare(a.Name, b.Name)
	})

	// Output JSON
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	ctx.Output.Info("%s", string(data))

	return nil
}
