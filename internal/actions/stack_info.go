package actions

import (
	"context"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// StackInfoBranch is structured info for one non-trunk branch in a stack.
type StackInfoBranch struct {
	Name           string
	Parent         string
	IsLocked       bool
	IsFrozen       bool
	Scope          string
	PRNumber       *int
	PRURL          string
	CommitMessages []string
	DiffStats      BranchDiffStats
}

// StackInfoResult contains structured data for `stackit info --stack`. It carries
// both the per-branch annotations (Branches) and the stack shape needed to draw
// the tree (Order/Parents/UpToDate), all as plain data — the CLI adapter turns it
// into a tree widget or JSON.
type StackInfoResult struct {
	StackTitle       string
	StackDescription string
	Current          string
	Trunk            string
	Branches         []StackInfoBranch // non-trunk branches, in stack order
	Order            []string          // every branch name (incl trunk), in stack order
	Parents          map[string]string // branch -> parent (incl trunk)
	UpToDate         map[string]bool   // branch -> up to date with parent (incl trunk)
}

// QueryStackInfo gathers structured information about the current stack.
func QueryStackInfo(ctx context.Context, eng engine.Engine) (StackInfoResult, error) {
	current := eng.CurrentBranch()
	if current == nil {
		return StackInfoResult{}, errors.ErrNotOnBranch
	}

	graph := eng.Graph(engine.SortStrategyAlphabetical)
	stackBranches := graph.Range(*current, engine.StackRange{
		RecursiveParents:  true,
		IncludeCurrent:    true,
		RecursiveChildren: true,
	})

	// Read each per-branch concern the projection needs as its own batched value
	// map — commits, diff stats, changed-file counts, PR status, and restack
	// status — instead of warming an engine-global cache.
	commits := eng.BatchCommits(stackBranches, engine.CommitFormatReadable)
	diffs := eng.BatchDiffStats(stackBranches)
	fileCounts := eng.BatchChangedFileCounts(ctx, stackBranches)
	prStatuses, _ := eng.BatchGetPRSubmissionStatus(ctx, stackBranches)
	statuses := eng.ReadBranchStatuses(stackBranches)

	trunk := eng.Trunk().GetName()
	result := StackInfoResult{
		Current:  current.GetName(),
		Trunk:    trunk,
		Order:    make([]string, 0, len(stackBranches)),
		Parents:  make(map[string]string, len(stackBranches)),
		UpToDate: make(map[string]bool, len(stackBranches)),
		Branches: make([]StackInfoBranch, 0, len(stackBranches)),
	}

	for _, branch := range stackBranches {
		name := branch.GetName()
		result.Order = append(result.Order, name)
		result.Parents[name] = branch.GetParentOrTrunk()
		result.UpToDate[name] = statuses.IsUpToDate(branch)

		if branch.IsTrunk() {
			continue
		}

		diff := diffs[name]
		info := StackInfoBranch{
			Name:           name,
			Parent:         branch.GetParentOrTrunk(),
			IsLocked:       branch.IsLocked(),
			IsFrozen:       branch.IsFrozen(),
			Scope:          branch.GetScope().String(),
			CommitMessages: commits[name],
			DiffStats: BranchDiffStats{
				Additions:    diff.Added,
				Deletions:    diff.Deleted,
				FilesChanged: fileCounts[name],
			},
		}
		if info.CommitMessages == nil {
			info.CommitMessages = []string{}
		}

		if prStatus := prStatuses[name]; prStatus.PRNumber != nil {
			info.PRNumber = prStatus.PRNumber
			if prStatus.PRInfo != nil {
				info.PRURL = prStatus.PRInfo.URL()
			}
		}

		result.Branches = append(result.Branches, info)
	}

	// Trunk never needs a restack; mirror the tree renderer's own invariant.
	result.UpToDate[trunk] = true

	if stackDesc := eng.GetStackDescription(*current); stackDesc != nil && !stackDesc.IsEmpty() {
		result.StackTitle = stackDesc.Title
		result.StackDescription = stackDesc.Description
	}

	return result, nil
}
