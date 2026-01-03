package submit

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
)

// prepareBranchesForSubmit prepares submission info for each branch, emitting events via handler
func prepareBranchesForSubmit(ctx *app.Context, branches []engine.Branch, opts Options, currentBranch string, handler Handler) ([]Info, error) {
	submissionInfos := make([]Info, 0, len(branches))
	nav := ctx.Navigator()

	for _, branch := range branches {
		branchName := branch.GetName()
		status, err := branch.GetPRSubmissionStatus()
		if err != nil {
			return nil, err
		}

		action := status.Action
		prNumber := status.PRNumber
		prInfo := status.PRInfo

		isCurrent := branchName == currentBranch

		// Check if we should skip
		if opts.UpdateOnly && action == "create" {
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
		if action == "update" {
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
		}

		metadata, err := PreparePRMetadata(branch, metadataOpts, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare metadata for %s: %w", branchName, err)
		}

		// Get SHAs
		headSHA, _ := branch.GetRevision()
		parentBranchName := branch.GetParentPrecondition()
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
	branchName := opts.Branch
	if branchName == "" {
		currentBranch := nav.CurrentBranch()
		if currentBranch == nil {
			return nil, fmt.Errorf("not on a branch and no branch specified")
		}
		branchName = currentBranch.GetName()
	}

	branch := nav.GetBranch(branchName)
	stackRange := opts.StackRange
	// Default to downstack if StackRange is zero value (all fields false)
	if !stackRange.RecursiveParents && !stackRange.IncludeCurrent && !stackRange.RecursiveChildren {
		stackRange = StackRangeDownstack()
	}
	stackBranches := nav.GetRelativeStack(branch, stackRange)
	allBranches := make([]string, len(stackBranches))
	for i, b := range stackBranches {
		allBranches[i] = b.GetName()
	}

	// Remove duplicates and trunk
	branches := []string{}
	branchSet := make(map[string]bool)
	for _, b := range allBranches {
		branchObj := nav.GetBranch(b)
		if !branchObj.IsTrunk() && !branchSet[b] {
			branches = append(branches, b)
			branchSet[b] = true
		}
	}

	return branches, nil
}
