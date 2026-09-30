package actions

import (
	"cmp"
	"slices"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
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
	Number       git.PRNumber `json:"number"`
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

			// Worktree anchors are an implementation detail. JSON consumers only
			// receive visible branches, so walk past anchors to avoid emitting a
			// parent that does not exist in the result.
			if parent := visibleTreeParent(branch); parent != nil {
				info.Parent = parent.GetName()
			}

			// Scope
			if scope := branch.GetScope(); !scope.IsNone() {
				info.Scope = scope.String()
			}

			// Children
			for _, child := range visibleTreeChildren(graph, branch) {
				info.Children = append(info.Children, child.GetName())
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

func visibleTreeParent(branch engine.Branch) *engine.Branch {
	parent := branch.GetParent()
	for parent != nil && parent.IsWorktreeAnchor() {
		parent = parent.GetParent()
	}
	return parent
}

func visibleTreeChildren(graph *engine.StackGraph, branch engine.Branch) engine.Branches {
	children := engine.NewBranchesBuilder(0)
	var collect func(engine.Branch)
	collect = func(parent engine.Branch) {
		for _, child := range graph.ChildBranches(parent) {
			if child.IsWorktreeAnchor() {
				collect(child)
				continue
			}
			children.Add(child)
		}
	}
	collect(branch)
	return children.Build()
}
