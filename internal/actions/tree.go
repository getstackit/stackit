package actions

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/output"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/internal/tui/components/tree"
	"github.com/getstackit/stackit/internal/utils"
)

// TreeStyle defines the output style for the tree command
type TreeStyle string

const (
	TreeStyleNormal TreeStyle = "NORMAL"
	TreeStyleFull   TreeStyle = "FULL"
	TreeStyleShort  TreeStyle = "SHORT"
)

// TreeOptions contains options for the tree command
type TreeOptions struct {
	Style         TreeStyle
	Steps         *int
	BranchName    string
	ShowUntracked bool
	Interactive   bool
	ShowSHAs      bool // Show commit SHAs next to branch names
	JSON          bool // Output in JSON format
}

// TreeJSONResult represents the JSON output for the tree command
type TreeJSONResult struct {
	Branches        []TreeBranchInfo `json:"branches"`
	Summary         TreeSummary      `json:"summary"`
	GitHubAvailable bool             `json:"github_available"`
}

// TreeBranchInfo represents a single branch in JSON output
type TreeBranchInfo struct {
	Name      string `json:"name"`
	Parent    string `json:"parent,omitempty"`
	IsCurrent bool   `json:"is_current"`
	IsTrunk   bool   `json:"is_trunk"`
	// Status booleans are always emitted (no omitempty) so consumers can rely on
	// `tree --json` as a complete, self-describing status source: an explicit
	// false is unambiguous, whereas an omitted field is indistinguishable from
	// "this field isn't reported". This is what lets agents read PR/CI status and
	// needs_restack/locked/frozen from a single command instead of also querying
	// `info --stack --json`.
	IsLocked     bool        `json:"is_locked"`
	IsFrozen     bool        `json:"is_frozen"`
	NeedsRestack bool        `json:"needs_restack"`
	Commits      int         `json:"commits"`
	Additions    int         `json:"additions,omitempty"`
	Deletions    int         `json:"deletions,omitempty"`
	PR           *TreePRInfo `json:"pr,omitempty"`
	Scope        string      `json:"scope,omitempty"`
	Children     []string    `json:"children,omitempty"`
}

// ReviewStatus is the PR review state reported in tree JSON output.
type ReviewStatus string

// Review status values for TreePRInfo.
const (
	ReviewApproved         ReviewStatus = "approved"
	ReviewChangesRequested ReviewStatus = "changes_requested"
	ReviewRequired         ReviewStatus = "review_required"
)

// TreePRInfo represents PR information in JSON output
type TreePRInfo struct {
	Number       int          `json:"number"`
	URL          string       `json:"url,omitempty"`
	Title        string       `json:"title,omitempty"`
	State        git.PRState  `json:"state"`
	IsDraft      bool         `json:"is_draft,omitempty"`
	ReviewStatus ReviewStatus `json:"review_status,omitempty"`
	CIStatus     string       `json:"ci_status,omitempty"`
}

// TreeSummary represents summary statistics in JSON output
type TreeSummary struct {
	TotalBranches int `json:"total_branches"`
	ApprovedCount int `json:"approved_count"`
	InReviewCount int `json:"in_review_count"`
}

// TreeAction displays the branch tree
func TreeAction(ctx *app.Context, opts TreeOptions) error {
	// JSON output mode
	if opts.JSON {
		return treeActionJSON(ctx, opts)
	}

	// If interactive mode is requested or auto-detected
	if opts.Interactive || (utils.IsInteractive() && opts.Steps == nil) {
		// Run interactive TUI
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

	allBranches := ctx.Engine.AllBranches()
	renderOpts := tree.RenderOptions{
		Mode:        tree.RenderModeFull, // We want the full tree characters with stats
		Steps:       opts.Steps,
		ShowSHAs:    opts.ShowSHAs,
		HideSummary: opts.Style == TreeStyleShort,
	}
	visibleBranches := visibleTreeBranches(renderer, opts.BranchName, renderOpts, allBranches)

	// Resolve the git-computed annotation stats (short SHA, commit count, diff
	// stats) for just the visible branches as one batched value. Scoped to the
	// visible set, so bounded views stay cheap; the short style needs none.
	var stats map[string]engine.BranchStat
	if opts.Style != TreeStyleShort {
		stats = ctx.Engine.BatchBranchStats(visibleBranches)
	}

	// Collect annotations only for branches that will be rendered.
	annotations := make(map[string]tree.BranchAnnotation, len(visibleBranches))

	// Prefetch CI status in batch if in FULL style
	var ciStatuses github.ChecksByBranch
	if opts.Style == TreeStyleFull && ctx.GitHub() != nil {
		branchNames := visibleBranches.Select(engine.BranchFilter{ExcludeTrunk: true, RequirePR: true}).Names()
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
	results := make(chan result, len(visibleBranches))

	if len(visibleBranches) > 0 {
		utils.Run(visibleBranches.All(), func(branchObj engine.Branch) {
			annotation := buildTreeAnnotation(ctx.Engine, branchObj, stats[branchObj.GetName()], opts, wtData, enrichment)
			results <- result{branchObj.GetName(), annotation}
		})
	}
	close(results)

	for res := range results {
		annotations[res.branchName] = res.annotation
	}

	renderer.SetAnnotations(annotations)

	stackLines := renderer.RenderStack(opts.BranchName, renderOpts)

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
		stackLines = append(stackLines, output.Dim(strings.Join(summaryParts, " · ")))
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

func buildTreeAnnotation(
	eng engine.Engine,
	branch engine.Branch,
	stat engine.BranchStat,
	opts TreeOptions,
	wtData *tui.WorktreeData,
	enrichment *tui.AnnotationEnrichment,
) tree.BranchAnnotation {
	if opts.Style != TreeStyleShort {
		return tui.BuildFullAnnotation(eng, branch, stat, enrichment, tui.AnnotationOptions{
			SkipCommitMessages: true,
		})
	}

	annotation := tui.GetMinimalAnnotationWithWorktreeAndEmpty(eng, branch, wtData)
	if opts.ShowSHAs {
		// The short style does not batch stats, so resolve the SHA directly.
		if stat.ShortSHA != "" {
			annotation.LocalSHA = stat.ShortSHA
		} else if sha, err := branch.GetRevision(); err == nil && len(sha) >= 7 {
			annotation.LocalSHA = sha[:7]
		}
	}
	return annotation
}

func getUntrackedBranchNames(ctx *app.Context) []string {
	untracked := engine.FilterBranches(ctx.Engine, func(b engine.Branch) bool {
		return !b.IsTrunk() && !b.IsTracked()
	})
	return untracked.Names()
}

// treeActionJSON generates JSON output for the tree command.
func treeActionJSON(ctx *app.Context, opts TreeOptions) error {
	result := BuildTreeJSON(ctx, opts)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	ctx.Output.Info("%s", string(data))
	return nil
}

// BuildTreeJSON builds the structured tree result (branch tree, PR/CI status, and
// per-branch health) without printing it, so other commands — e.g. `status` —
// can embed the same stack snapshot. The CI status prefetch always runs to
// provide complete data.
func BuildTreeJSON(ctx *app.Context, opts TreeOptions) TreeJSONResult {
	eng := ctx.Engine
	currentBranch := eng.CurrentBranch()
	currentBranchName := ""
	if currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}

	// Build stack graph
	graph := eng.Graph(engine.SortStrategyAlphabetical)

	// Get all branches in stack order
	var branchesToInclude engine.Branches
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

	// Prefetch CI status for JSON output (always fetched to provide complete data)
	ghClient := ctx.GitHub()
	var ciStatuses github.ChecksByBranch
	if ghClient != nil {
		branchNames := branchesToInclude.Select(engine.BranchFilter{ExcludeTrunk: true, RequirePR: true}).Names()
		if len(branchNames) > 0 {
			ciStatuses, _ = ghClient.BatchGetPRChecksStatus(ctx.Context, branchNames)
		}
	}

	// Build result
	result := TreeJSONResult{
		Branches:        []TreeBranchInfo{},
		Summary:         TreeSummary{},
		GitHubAvailable: ghClient != nil,
	}

	// Collect branch info in parallel using worker pool (each branch requires
	// git subprocesses for commits and diff stats)
	type branchResult struct {
		info TreeBranchInfo
	}
	branchResults := make(chan branchResult, len(branchesToInclude))

	// Filter out worktree anchors before parallel processing
	processable := engine.NewBranchesBuilder(len(branchesToInclude))
	for _, b := range branchesToInclude {
		if !b.IsWorktreeAnchor() {
			processable.Add(b)
		}
	}
	processableBranches := processable.Build()

	// Resolve commit count, diff stats, and up-to-date status for all
	// processable branches as batched values, read in the loop below, instead
	// of a per-branch IsBranchUpToDate() inside each worker (each of which
	// would shell a separate `git rev-parse` for the parent).
	stats := eng.BatchBranchStats(processableBranches)
	statuses := eng.ReadBranchStatuses(processableBranches)

	if len(processableBranches) > 0 {
		utils.Run(processableBranches.All(), func(branch engine.Branch) {
			branchName := branch.GetName()

			info := TreeBranchInfo{
				Name:         branchName,
				IsCurrent:    branchName == currentBranchName,
				IsTrunk:      branch.IsTrunk(),
				IsLocked:     branch.IsLocked(),
				IsFrozen:     branch.IsFrozen(),
				NeedsRestack: !statuses.IsUpToDate(branch) && !branch.IsTrunk(),
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

			// Commits and diff stats from the batched stats resolved above.
			if !branch.IsTrunk() {
				stat := stats[branchName]
				info.Commits = stat.CommitCount
				info.Additions = stat.LinesAdded
				info.Deletions = stat.LinesDeleted
			}

			// PR info
			if !branch.IsTrunk() {
				prInfo, _ := branch.GetPrInfo()
				if prInfo != nil && prInfo.Number() != nil {
					info.PR = &TreePRInfo{
						Number:  *prInfo.Number(),
						URL:     prInfo.URL(),
						Title:   prInfo.Title(),
						State:   prInfo.State(),
						IsDraft: prInfo.IsDraft(),
					}

					// CI status
					if status := ciStatuses.Get(branchName); status != nil {
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
							info.PR.ReviewStatus = ReviewApproved
						case github.ReviewDecisionChangesRequested:
							info.PR.ReviewStatus = ReviewChangesRequested
						case github.ReviewDecisionReviewRequired:
							info.PR.ReviewStatus = ReviewRequired
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
			if info.PR != nil && info.PR.ReviewStatus == ReviewApproved {
				result.Summary.ApprovedCount++
			} else if info.PR != nil && info.PR.ReviewStatus == ReviewRequired {
				result.Summary.InReviewCount++
			}
		}
	}

	// Keep output stable across runs even when collection happens in parallel.
	slices.SortFunc(result.Branches, func(a, b TreeBranchInfo) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return result
}
