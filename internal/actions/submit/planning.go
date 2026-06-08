package submit

import (
	"fmt"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
)

// prepareBranchesForSubmit prepares submission info for each branch, emitting
// events via handler. When remoteStatuses is provided, planning reuses that
// snapshot; otherwise it asks the engine for lazily batched status so create-only
// dry runs do not read remote state.
func prepareBranchesForSubmit(ctx *app.Context, branches engine.Branches, opts Options, currentBranch string, remoteStatuses engine.BranchRemoteStatuses, handler Handler) ([]Info, error) {
	submissionInfos := make([]Info, 0, len(branches))
	nav := ctx.Navigator()

	var (
		statuses map[string]engine.PRSubmissionStatus
		err      error
	)
	if remoteStatuses == nil {
		statuses, err = ctx.Engine.BatchGetPRSubmissionStatus(branches)
	} else {
		statuses, err = ctx.Engine.BatchGetPRSubmissionStatusWithRemote(branches, remoteStatuses)
	}
	if err != nil {
		return nil, err
	}

	for _, branch := range branches {
		branchName := branch.GetName()
		status := statuses[branchName]

		action := status.Action
		prNumber := status.PRNumber
		prInfo := status.PRInfo

		// If PR is closed or merged, treat as a new PR creation
		// This allows recovery when a PR was closed (e.g., due to deleted base branch)
		if prInfo != nil && (prInfo.State() == "CLOSED" || prInfo.State() == "MERGED") {
			action = actionCreate
			prNumber = nil
		}

		isCurrent := branchName == currentBranch

		// Check if we should skip
		if opts.UpdateOnly && action == actionCreate {
			handler.OnEvent(BranchPlanEvent{
				BranchName: branchName,
				Action:     action,
				IsCurrent:  isCurrent,
				Skipped:    true,
				SkipReason: "skipped, no existing PR",
			})
			continue
		}

		needsUpdate := status.NeedsUpdate
		if action == actionUpdate {
			// Check if draft status needs to change
			draftStatusNeedsChange := false
			if prInfo != nil {
				if opts.Draft && !prInfo.IsDraft() {
					draftStatusNeedsChange = true
				} else if opts.Publish && prInfo.IsDraft() {
					draftStatusNeedsChange = true
				}
			}

			needsUpdate = needsUpdate || opts.Edit || opts.Always || draftStatusNeedsChange

			if !needsUpdate && !opts.Draft && !opts.Publish {
				handler.OnEvent(BranchPlanEvent{
					BranchName: branchName,
					Action:     action,
					IsCurrent:  isCurrent,
					Skipped:    true,
					SkipReason: status.Reason,
				})
				continue
			}
		}

		// Prepare metadata
		metadataOpts := MetadataOptions{
			Edit:              opts.Edit && !opts.NoEdit,
			EditTitle:         opts.EditTitle && !opts.NoEditTitle,
			EditDescription:   opts.EditDescription && !opts.NoEditDescription,
			NoEdit:            opts.NoEdit,
			NoEditTitle:       opts.NoEditTitle,
			NoEditDescription: opts.NoEditDescription,
			Draft:             opts.Draft,
			Publish:           opts.Publish,
			Reviewers:         opts.Reviewers,
			ReviewersPrompt:   opts.Reviewers == "" && opts.Edit,
			// Config-driven options
			ConfigDraft:     opts.ConfigDraft,
			ConfigReviewers: opts.ConfigReviewers,
			ConfigLabels:    opts.ConfigLabels,
			ConfigAssignees: opts.ConfigAssignees,
		}

		metadata, err := PreparePRMetadata(branch, metadataOpts, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare metadata for %s: %w", branchName, err)
		}

		// Get SHAs
		headSHA, _ := branch.GetRevision()
		parentBranchName := resolveSubmitParentName(nav, branch)
		parentBranch := nav.GetBranch(parentBranchName)
		baseSHA, _ := parentBranch.GetRevision()

		submissionInfo := Info{
			BranchName: branchName,
			Head:       branchName,
			Base:       parentBranchName,
			HeadSHA:    headSHA,
			BaseSHA:    baseSHA,
			Action:     action,
			PRNumber:   prNumber,
			Metadata:   metadata,
		}

		handler.OnEvent(BranchPlanEvent{
			BranchName: branchName,
			Action:     action,
			IsCurrent:  isCurrent,
			Skipped:    false,
		})

		submissionInfos = append(submissionInfos, submissionInfo)
	}

	return submissionInfos, nil
}

// getBranchesToSubmit returns the list of branches to submit based on options
func getBranchesToSubmit(ctx *app.Context, opts Options) ([]string, error) {
	nav := ctx.Navigator()

	// Get branch scope
	branchName, err := resolveBranchNameFromNav(nav, opts.Branch)
	if err != nil {
		return nil, err
	}

	stackRange := opts.StackRange
	// Default to downstack if StackRange is zero value (all fields false)
	if !stackRange.RecursiveParents && !stackRange.IncludeCurrent && !stackRange.RecursiveChildren {
		stackRange = engine.StackRangeDownstack(true)
	}
	graph := ctx.Engine.Graph(engine.SortStrategyAlphabetical)
	stackBranches := graph.Range(nav.GetBranch(branchName), stackRange)
	allBranches := stackBranches.Names()

	// Remove duplicates, trunk, and worktree anchor branches (which are not submittable)
	branches := []string{}
	branchSet := make(map[string]bool)
	for _, b := range allBranches {
		branchObj := nav.GetBranch(b)
		if !branchObj.IsTrunk() && !branchObj.IsWorktreeAnchor() && !branchSet[b] {
			branches = append(branches, b)
			branchSet[b] = true
		}
	}

	return branches, nil
}

// resolveBranchNameFromNav resolves a branch name, defaulting to current branch if empty.
func resolveBranchNameFromNav(nav engine.StackNavigator, branchName string) (string, error) {
	if branchName != "" {
		return branchName, nil
	}
	currentBranch := nav.CurrentBranch()
	if currentBranch == nil {
		return "", errors.ErrNotOnBranchNoBranchSpecified
	}
	return currentBranch.GetName(), nil
}
