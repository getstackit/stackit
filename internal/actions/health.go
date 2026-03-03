package actions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/output"
	"stackit.dev/stackit/internal/tui/style"
)

// HealthOptions contains options for the health command
type HealthOptions struct {
	JSON  bool
	Quiet bool
}

// HealthReport contains the overall health status of the stack
type HealthReport struct {
	Branches        []BranchHealth   `json:"branches"`
	Recommendations []Recommendation `json:"recommendations"`
	GitHubAvailable bool             `json:"github_available"`
}

// BranchHealth represents the health status of a single branch
type BranchHealth struct {
	Name          string `json:"name"`
	Parent        string `json:"parent"`
	NeedsRestack  bool   `json:"needs_restack"`
	CommitsBehind int    `json:"commits_behind"` // Number of trunk commits the branch doesn't have
	CI            string `json:"ci"`             // passing, failing, pending, unknown
	CIError       string `json:"ci_error,omitempty"`
	PRStatus      string `json:"pr_status"` // draft, open, approved, merged, closed, none
	PRNumber      *int   `json:"pr_number,omitempty"`
	PRURL         string `json:"pr_url,omitempty"`
	IsLocked      bool   `json:"is_locked"`
	IsFrozen      bool   `json:"is_frozen"`
}

// Recommendation represents a suggested action to improve stack health
type Recommendation struct {
	Action   string `json:"action"`
	Reason   string `json:"reason"`
	Branch   string `json:"branch,omitempty"`
	Command  string `json:"command,omitempty"`
	Priority int    `json:"priority"` // 1 = high, 2 = medium, 3 = low
}

// CI status constants
const (
	CIStatusPassing = "passing"
	CIStatusFailing = "failing"
	CIStatusPending = "pending"
	CIStatusUnknown = "unknown"
)

// PR status constants
const (
	PRStatusDraft    = "draft"
	PRStatusOpen     = "open"
	PRStatusApproved = "approved"
	PRStatusMerged   = "merged"
	PRStatusClosed   = "closed"
	PRStatusNone     = "none"
)

// StaleCommitThreshold is the number of commits behind trunk before a branch
// is considered "stale" and a sync recommendation is generated.
// This threshold balances being helpful (alerting users to old branches) without
// being noisy (in fast-moving repos, small drift is normal). 20 commits typically
// represents several days to a week of activity on an active trunk.
const StaleCommitThreshold = 20

// HealthAction analyzes the health of all tracked branches
func HealthAction(ctx *app.Context, opts HealthOptions) error {
	eng := ctx.Engine
	out := ctx.Output

	report := generateHealthReport(ctx, eng)

	if opts.JSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		out.Info("%s", string(data))
		return nil
	}

	// If quiet mode and no issues, output nothing
	if opts.Quiet && len(report.Recommendations) == 0 {
		return nil
	}

	// Human-readable output
	renderHealthReport(out, report)
	return nil
}

func generateHealthReport(ctx *app.Context, eng engine.Engine) *HealthReport {
	report := &HealthReport{
		Branches:        []BranchHealth{},
		Recommendations: []Recommendation{},
		GitHubAvailable: false,
	}

	// Get all tracked branches
	allBranches := eng.AllBranches()
	var trackedBranches []engine.Branch
	for _, branch := range allBranches {
		if branch.IsTracked() && !branch.IsTrunk() {
			trackedBranches = append(trackedBranches, branch)
		}
	}

	if len(trackedBranches) == 0 {
		return report
	}

	// Batch fetch PR/CI status from GitHub
	branchNames := make([]string, len(trackedBranches))
	for i, branch := range trackedBranches {
		branchNames[i] = branch.GetName()
	}

	var checkStatuses map[string]*github.CheckStatus
	if ctx.GitHubClient != nil {
		owner, repo := ctx.GitHubClient.GetOwnerRepo()
		if owner != "" && repo != "" {
			report.GitHubAvailable = true
			var err error
			checkStatuses, err = ctx.GitHubClient.BatchGetPRChecksStatus(ctx.Context, branchNames)
			if err != nil {
				ctx.Output.Debug("Failed to fetch PR check statuses: %v", err)
			}
		}
	}

	// Analyze each branch
	for _, branch := range trackedBranches {
		health := analyzeBranchHealth(eng, branch, checkStatuses)
		report.Branches = append(report.Branches, health)
	}

	// Sort branches by parent relationship (stack order)
	sortBranchesByStackOrder(report.Branches)

	// Generate recommendations
	report.Recommendations = generateRecommendations(report.Branches)

	return report
}

func analyzeBranchHealth(eng engine.Engine, branch engine.Branch, checkStatuses map[string]*github.CheckStatus) BranchHealth {
	branchName := branch.GetName()
	health := BranchHealth{
		Name:         branchName,
		NeedsRestack: !branch.IsBranchUpToDate(),
		IsLocked:     branch.IsLocked(),
		IsFrozen:     branch.IsFrozen(),
		CI:           CIStatusUnknown,
		PRStatus:     PRStatusNone,
	}

	// Get parent
	parent := branch.GetParent()
	if parent != nil {
		health.Parent = parent.GetName()
	}

	// Calculate commits behind trunk
	health.CommitsBehind = calculateCommitsBehindTrunk(eng, branch)

	// Get PR info
	prInfo, _ := branch.GetPrInfo()
	if prInfo != nil && prInfo.Number() != nil {
		prNum := *prInfo.Number()
		health.PRNumber = &prNum
		health.PRURL = prInfo.URL()

		// Determine PR status
		switch prInfo.State() {
		case "MERGED":
			health.PRStatus = PRStatusMerged
		case "CLOSED":
			health.PRStatus = PRStatusClosed
		default:
			if prInfo.IsDraft() {
				health.PRStatus = PRStatusDraft
			} else {
				health.PRStatus = PRStatusOpen
			}
		}

		// Check CI status from batch results
		if checkStatuses != nil {
			if status, ok := checkStatuses[branchName]; ok && status != nil {
				health.CI = determineCIStatus(status)
				if !status.Passing {
					health.CIError = getFirstFailingCheck(status)
				}
				// Override PR status if approved
				if status.IsApproved() && health.PRStatus == PRStatusOpen {
					health.PRStatus = PRStatusApproved
				}
			}
		}
	}

	return health
}

// calculateCommitsBehindTrunk returns the number of commits on trunk that the branch doesn't have.
// This measures how "stale" a branch is relative to trunk.
func calculateCommitsBehindTrunk(eng engine.Engine, branch engine.Branch) int {
	trunk := eng.Trunk()
	if trunk.GetName() == "" {
		return 0
	}
	branchName := branch.GetName()
	trunkName := trunk.GetName()

	// Find merge base between branch and trunk
	mergeBase, err := eng.GetMergeBase(branchName, trunkName)
	if err != nil {
		return 0
	}

	// Count commits from merge base to trunk (commits the branch doesn't have)
	commits, err := eng.Git().GetCommitRangeSHAs(mergeBase, trunkName)
	if err != nil {
		return 0
	}

	return len(commits)
}

func determineCIStatus(status *github.CheckStatus) string {
	if status == nil {
		return CIStatusUnknown
	}
	if status.Pending {
		return CIStatusPending
	}
	if status.Passing {
		return CIStatusPassing
	}
	return CIStatusFailing
}

func getFirstFailingCheck(status *github.CheckStatus) string {
	for _, check := range status.Checks {
		if check.Conclusion == "FAILURE" || check.Conclusion == "TIMED_OUT" || check.Conclusion == "CANCELED" {
			return check.Name
		}
	}
	return ""
}

func sortBranchesByStackOrder(branches []BranchHealth) {
	// Build parent -> children map for ordering
	childrenOf := make(map[string][]string)
	for _, b := range branches {
		if b.Parent != "" {
			childrenOf[b.Parent] = append(childrenOf[b.Parent], b.Name)
		}
	}

	// Find roots (branches whose parent is trunk or not in our list)
	branchSet := make(map[string]bool)
	for _, b := range branches {
		branchSet[b.Name] = true
	}

	var roots []string
	for _, b := range branches {
		if b.Parent == "" || !branchSet[b.Parent] {
			roots = append(roots, b.Name)
		}
	}
	sort.Strings(roots)

	// DFS to get ordered list with cycle detection
	var ordered []string
	visited := make(map[string]bool)
	var dfs func(name string)
	dfs = func(name string) {
		if visited[name] {
			return // Prevent infinite loop on cycles
		}
		visited[name] = true
		ordered = append(ordered, name)
		children := childrenOf[name]
		sort.Strings(children)
		for _, child := range children {
			dfs(child)
		}
	}
	for _, root := range roots {
		dfs(root)
	}

	// Create name -> health map
	healthMap := make(map[string]BranchHealth)
	for _, b := range branches {
		healthMap[b.Name] = b
	}

	// Reorder branches slice in place, handling case where ordered may have
	// fewer entries than branches (due to cycles or orphans in the graph)
	reordered := make([]BranchHealth, 0, len(branches))
	for _, name := range ordered {
		if h, ok := healthMap[name]; ok {
			reordered = append(reordered, h)
			delete(healthMap, name)
		}
	}
	// Append any branches not reached by DFS (shouldn't happen, but be safe)
	for _, h := range healthMap {
		reordered = append(reordered, h)
	}
	copy(branches, reordered)
}

func generateRecommendations(branches []BranchHealth) []Recommendation {
	var recs []Recommendation

	// Check for branches that need restack
	needsRestackCount := 0
	for _, b := range branches {
		if b.NeedsRestack {
			needsRestackCount++
		}
	}
	if needsRestackCount > 0 {
		recs = append(recs, Recommendation{
			Action:   "restack",
			Reason:   fmt.Sprintf("%d branch(es) need restacking", needsRestackCount),
			Command:  "stackit restack",
			Priority: 2,
		})
	}

	// Check for stale branches (more than StaleCommitThreshold commits behind trunk)
	var staleBranches []string
	maxBehind := 0
	for _, b := range branches {
		if b.CommitsBehind > StaleCommitThreshold {
			staleBranches = append(staleBranches, b.Name)
			if b.CommitsBehind > maxBehind {
				maxBehind = b.CommitsBehind
			}
		}
	}
	if len(staleBranches) > 0 {
		reason := fmt.Sprintf("%d branch(es) are significantly behind trunk (up to %d commits)", len(staleBranches), maxBehind)
		recs = append(recs, Recommendation{
			Action:   "sync",
			Reason:   reason,
			Command:  "stackit sync",
			Priority: 2,
		})
	}

	// Check for failing CI
	for _, b := range branches {
		if b.CI == CIStatusFailing {
			reason := fmt.Sprintf("%s has failing CI", b.Name)
			if b.CIError != "" {
				reason = fmt.Sprintf("%s: %s", reason, b.CIError)
			}
			recs = append(recs, Recommendation{
				Action:   "fix_ci",
				Reason:   reason,
				Branch:   b.Name,
				Priority: 1,
			})
		}
	}

	// Check for branches ready to merge
	for _, b := range branches {
		if b.PRStatus == PRStatusApproved && b.CI == CIStatusPassing {
			recs = append(recs, Recommendation{
				Action:   "merge",
				Reason:   fmt.Sprintf("%s is approved and CI passing", b.Name),
				Branch:   b.Name,
				Command:  fmt.Sprintf("stackit merge %s", b.Name),
				Priority: 3,
			})
		}
	}

	// Check for branches without PRs
	noPRCount := 0
	for _, b := range branches {
		if b.PRStatus == PRStatusNone && !b.IsLocked && !b.IsFrozen {
			noPRCount++
		}
	}
	if noPRCount > 0 {
		recs = append(recs, Recommendation{
			Action:   "submit",
			Reason:   fmt.Sprintf("%d branch(es) have no PR", noPRCount),
			Command:  "stackit submit",
			Priority: 3,
		})
	}

	// Sort by priority
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Priority < recs[j].Priority
	})

	return recs
}

func renderHealthReport(out output.Output, report *HealthReport) {
	out.Info("%s", style.ColorCyan("Stack Health Report"))
	out.Info("%s", style.ColorDim("───────────────────"))

	if len(report.Branches) == 0 {
		out.Info("No tracked branches found.")
		return
	}

	// Show branch health
	for _, b := range report.Branches {
		icon := style.IconCIPassing() // green dot
		issues := []string{}

		if b.NeedsRestack {
			icon = style.IconCIPending() // yellow dot
			issues = append(issues, "needs restack")
		}
		switch b.CI {
		case CIStatusFailing:
			icon = style.IconCIFailing() // red dot
			ciMsg := "CI failing"
			if b.CIError != "" {
				ciMsg = fmt.Sprintf("CI failing: %s", b.CIError)
			}
			issues = append(issues, ciMsg)
		case CIStatusPending:
			issues = append(issues, "CI pending")
		}
		if b.CommitsBehind > StaleCommitThreshold {
			issues = append(issues, fmt.Sprintf("%d commits behind", b.CommitsBehind))
		}
		if b.PRStatus == PRStatusApproved && b.CI == CIStatusPassing {
			icon = style.IconReviewApproved() // green checkmark
			issues = append(issues, "ready to merge")
		}

		statusStr := ""
		if len(issues) > 0 {
			statusStr = fmt.Sprintf(" (%s)", strings.Join(issues, ", "))
		}

		prInfo := ""
		if b.PRNumber != nil {
			prInfo = fmt.Sprintf(" #%d", *b.PRNumber)
			switch b.PRStatus {
			case PRStatusDraft:
				prInfo += " (draft)"
			case PRStatusApproved:
				prInfo += " (approved)"
			}
		}

		out.Info("%s %s%s%s", icon, style.ColorBranchName(b.Name, false), prInfo, statusStr)
	}

	// Show recommendations
	if len(report.Recommendations) > 0 {
		out.Newline()
		out.Info("%s", style.ColorCyan("Recommendations:"))
		for _, rec := range report.Recommendations {
			var icon string
			switch rec.Priority {
			case 1:
				icon = style.IconCIFailing() // red dot for high priority
			case 2:
				icon = style.IconCIPending() // yellow dot for medium
			default:
				icon = style.IconInfo() // blue dot for low priority
			}
			out.Info("%s %s", icon, rec.Reason)
			if rec.Command != "" {
				out.Info("   %s %s", style.ColorDim("→"), style.ColorCyan(rec.Command))
			}
		}
	} else {
		out.Newline()
		out.Info("%s Stack is healthy!", style.IconCIPassing())
	}
}
