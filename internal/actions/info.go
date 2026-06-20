package actions

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/output"
)

// SingleBranchInfo represents JSON-serializable info for a single branch (used by info --json)
type SingleBranchInfo struct {
	Name             string              `json:"name"`
	IsCurrent        bool                `json:"is_current"`
	IsTrunk          bool                `json:"is_trunk"`
	IsLocked         bool                `json:"is_locked"`
	IsFrozen         bool                `json:"is_frozen"`
	NeedsRestack     bool                `json:"needs_restack"`
	Scope            string              `json:"scope"`
	CommitDate       string              `json:"commit_date,omitempty"`
	Parent           string              `json:"parent,omitempty"`
	Children         []string            `json:"children,omitempty"`
	PR               *SingleBranchPRInfo `json:"pr,omitempty"`
	CommitMessages   []string            `json:"commit_messages"`
	DiffStats        SingleBranchStats   `json:"diff_stats"`
	StackTitle       string              `json:"stack_title,omitempty"`
	StackDescription string              `json:"stack_description,omitempty"`
}

// SingleBranchPRInfo represents PR information for JSON output
type SingleBranchPRInfo struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	IsDraft bool   `json:"is_draft"`
	URL     string `json:"url"`
}

// SingleBranchStats represents diff statistics for a branch
type SingleBranchStats struct {
	FilesChanged int `json:"files_changed"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

// InfoOptions contains options for the info command
type InfoOptions struct {
	BranchName string
	Body       bool
	Diff       bool
	Patch      bool
	Stat       bool
	Stack      bool
	JSON       bool
}

// InfoAction displays information about a branch or the entire stack
func InfoAction(ctx *app.Context, opts InfoOptions) error {
	if opts.Stack {
		return StackInfoAction(ctx, StackInfoOptions{
			JSON: opts.JSON,
		})
	}

	eng := ctx.Engine
	out := ctx.Output

	branchName, err := ResolveBranchName(eng, opts.BranchName)
	if err != nil {
		return err
	}

	branch := eng.GetBranch(branchName)

	if !branch.IsTracked() && !branch.IsTrunk() {
		_, err := eng.GetRevision(branch)
		if err != nil {
			return fmt.Errorf("branch %s does not exist", branchName)
		}

		// For remote branches, fetch metadata to show the latest info
		if err := eng.FetchRemoteMetadata(ctx.Context); err != nil {
			out.Debug("Failed to fetch remote metadata: %v", err)
		} else if err := eng.ApplyRemoteMetadataForBranches(ctx.Context, []string{branchName}); err != nil {
			out.Debug("Failed to apply remote metadata for %s: %v", branchName, err)
		}
	}

	// Handle JSON output for single branch
	if opts.JSON {
		return outputBranchInfoJSON(ctx, branch)
	}

	// If stat is set without diff or patch, it implies diff
	effectiveDiff := opts.Diff || (opts.Stat && !opts.Patch)
	effectivePatch := opts.Patch && !opts.Diff

	var outputLines []string

	currentBranch := eng.CurrentBranch()
	isCurrent := branchName == currentBranch.GetName()
	isTrunk := branch.IsTrunk()

	coloredBranchName := output.BranchWithTrunk(branchName, isCurrent, isTrunk)

	if branch.IsLocked() {
		coloredBranchName += " " + output.IconLocked() + " " + output.Dim("(locked)")
	}
	if branch.IsFrozen() {
		coloredBranchName += " " + output.IconFrozen() + " " + output.Dim("(frozen)")
	}

	if !isTrunk && !branch.IsBranchUpToDate() {
		coloredBranchName += " " + output.NeedsRestack("(needs restack)")
	}

	if scope := branch.GetScope(); !scope.IsNone() {
		coloredBranchName += " " + output.Scope(scope.String())
	}

	outputLines = append(outputLines, coloredBranchName)

	// Show stack description if present
	stackDesc := eng.GetStackDescription(branch)
	if stackDesc != nil && !stackDesc.IsEmpty() {
		outputLines = append(outputLines, "")
		// Render title and description together through glamour for consistent formatting
		var markdown string
		if stackDesc.Description != "" {
			markdown = "# " + stackDesc.Title + "\n\n" + stackDesc.Description
		} else {
			markdown = "# " + stackDesc.Title
		}
		rendered := output.RenderMarkdown(markdown)
		outputLines = append(outputLines, rendered)
	}

	commitDate, err := branch.GetCommitDate()
	if err == nil {
		dateStr := commitDate.Format(time.RFC3339)
		outputLines = append(outputLines, output.Dim(dateStr))
	}

	var prInfo *engine.PrInfo
	if !isTrunk {
		prInfo, _ = branch.GetPrInfo()
		if prInfo != nil && prInfo.Number() != nil {
			prTitleLine := getPRTitleLine(prInfo)
			if prTitleLine != "" {
				outputLines = append(outputLines, "")
				outputLines = append(outputLines, prTitleLine)
			}
			if prInfo.URL() != "" {
				outputLines = append(outputLines, output.Magenta(prInfo.URL()))
			}
		}
	}

	parentBranch := branch.GetParent()
	if parentBranch != nil {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, fmt.Sprintf("%s: %s", output.Cyan("Parent"), output.BranchWithTrunk(parentBranch.GetName(), false, parentBranch.IsTrunk())))
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	children := graph.ChildBranches(branch)
	if len(children) > 0 {
		outputLines = append(outputLines, fmt.Sprintf("%s:", output.Cyan("Children")))
		for _, child := range children {
			outputLines = append(outputLines, fmt.Sprintf("▸ %s", output.BranchWithTrunk(child.GetName(), false, child.IsTrunk())))
		}
	}

	if opts.Body && prInfo != nil && prInfo.Body() != "" {
		outputLines = append(outputLines, "")
		outputLines = append(outputLines, prInfo.Body())
	}

	outputLines = append(outputLines, "")
	if effectivePatch {
		baseRevision := ""
		if isTrunk {
			baseRevision = branchName + "~"
		} else {
			commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
			if err == nil && len(commits) > 0 {
				oldestSHA := commits[0]
				baseRevision, _ = eng.GetParentCommitSHA(oldestSHA)
			}
		}
		branchRevision, err := branch.GetRevision()
		if err == nil {
			commitsOutput, err := eng.ShowCommits(ctx.Context, baseRevision, branchRevision, true, opts.Stat)
			if err == nil && commitsOutput != "" {
				outputLines = append(outputLines, commitsOutput)
			}
		}
	} else {
		commits, err := branch.GetAllCommits(engine.CommitFormatReadable)
		if err == nil {
			for _, commit := range commits {
				outputLines = append(outputLines, output.Dim(commit))
			}
		}
	}

	if effectiveDiff {
		outputLines = append(outputLines, "")
		if isTrunk {
			headRevision, err := branch.GetRevision()
			if err == nil {
				parentSHA, err := eng.GetCommitSHA(branchName, 1)
				if err == nil {
					diffOutput, err := eng.ShowDiff(ctx.Context, parentSHA, headRevision, opts.Stat)
					if err == nil && diffOutput != "" {
						outputLines = append(outputLines, diffOutput)
					}
				}
			}
		} else {
			commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
			if err == nil && len(commits) > 0 {
				oldestSHA := commits[0]
				parentSHA, _ := eng.GetParentCommitSHA(oldestSHA)
				branchRevision, err := branch.GetRevision()
				if err == nil {
					diffOutput, err := eng.ShowDiff(ctx.Context, parentSHA, branchRevision, opts.Stat)
					if err == nil && diffOutput != "" {
						outputLines = append(outputLines, diffOutput)
					}
				}
			}
		}
	}

	// Apply dimming for merged/closed PRs
	const (
		prStateMerged = "MERGED"
		prStateClosed = "CLOSED"
	)
	if prInfo != nil && (prInfo.State() == prStateMerged || prInfo.State() == prStateClosed) {
		for i := range outputLines {
			outputLines[i] = output.Dim(outputLines[i])
		}
	}

	out.Print(strings.Join(outputLines, "\n"))
	out.Newline()

	return nil
}

func getPRTitleLine(prInfo *engine.PrInfo) string {
	if prInfo == nil || prInfo.Number() == nil || prInfo.Title() == "" {
		return ""
	}

	state := prInfo.State()

	const (
		prStateMerged = "MERGED"
		prStateClosed = "CLOSED"
	)

	prNumber := output.PRNumber(*prInfo.Number())

	switch state {
	case prStateMerged:
		return fmt.Sprintf("%s (Merged) %s", prNumber, prInfo.Title())
	case prStateClosed:
		return fmt.Sprintf("%s (Abandoned) %s", prNumber, output.Dim(prInfo.Title()))
	default:
		prState := output.PRState(state, prInfo.IsDraft())
		return fmt.Sprintf("%s %s %s", prNumber, prState, prInfo.Title())
	}
}

// outputBranchInfoJSON outputs branch information as JSON
func outputBranchInfoJSON(ctx *app.Context, branch engine.Branch) error {
	eng := ctx.Engine
	branchName := branch.GetName()
	currentBranch := eng.CurrentBranch()
	isCurrent := currentBranch != nil && branchName == currentBranch.GetName()
	isTrunk := branch.IsTrunk()

	info := SingleBranchInfo{
		Name:           branchName,
		IsCurrent:      isCurrent,
		IsTrunk:        isTrunk,
		IsLocked:       branch.IsLocked(),
		IsFrozen:       branch.IsFrozen(),
		NeedsRestack:   !isTrunk && !branch.IsBranchUpToDate(),
		Scope:          branch.GetScope().String(),
		CommitMessages: []string{},
		Children:       []string{},
	}

	// Commit date
	commitDate, err := branch.GetCommitDate()
	if err == nil {
		info.CommitDate = commitDate.Format(time.RFC3339)
	}

	// Parent
	if parent := branch.GetParent(); parent != nil {
		info.Parent = parent.GetName()
	}

	// Children
	graph := eng.Graph(engine.SortStrategyAlphabetical)
	for _, child := range graph.ChildBranches(branch) {
		info.Children = append(info.Children, child.GetName())
	}

	// PR info
	if !isTrunk {
		prInfo, _ := branch.GetPrInfo()
		if prInfo != nil && prInfo.Number() != nil {
			info.PR = &SingleBranchPRInfo{
				Number:  *prInfo.Number(),
				Title:   prInfo.Title(),
				State:   prInfo.State(),
				IsDraft: prInfo.IsDraft(),
				URL:     prInfo.URL(),
			}
		}
	}

	// Commit messages
	commits, err := branch.GetAllCommits(engine.CommitFormatReadable)
	if err == nil {
		info.CommitMessages = commits
	}

	// Diff stats
	added, deleted, err := branch.GetDiffStats()
	if err == nil {
		info.DiffStats.Additions = added
		info.DiffStats.Deletions = deleted
	}

	// Files changed — measured against the branch's divergence point, the same
	// base GetDiffStats uses above, so the file count stays consistent with the
	// additions/deletions when the parent has advanced since the branch diverged.
	if !isTrunk {
		base, err := eng.GetDivergencePoint(branchName)
		if err == nil && base != "" {
			branchRev, err := branch.GetRevision()
			if err == nil {
				files, err := eng.GetChangedFiles(ctx.Context, base, branchRev)
				if err == nil {
					info.DiffStats.FilesChanged = len(files)
				}
			}
		}
	}

	// Stack description
	stackDesc := eng.GetStackDescription(branch)
	if stackDesc != nil && !stackDesc.IsEmpty() {
		info.StackTitle = stackDesc.Title
		info.StackDescription = stackDesc.Description
	}

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal branch info to JSON: %w", err)
	}
	ctx.Output.Info("%s", string(data))
	return nil
}
