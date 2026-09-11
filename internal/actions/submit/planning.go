package submit

import (
	"fmt"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// prepareBranchesForSubmit prepares submission info for each branch, emitting
// events via handler. When remoteStatuses is provided, planning reuses that
// snapshot; otherwise it asks the engine for lazily batched status so create-only
// dry runs do not read remote state. empty flags branches with no commits so
// plan output can surface them.
func prepareBranchesForSubmit(ctx *app.Context, branches engine.Branches, opts Options, currentBranch string, remoteStatuses engine.BranchRemoteStatuses, empty map[string]bool, handler Handler) ([]Info, error) {
	submissionInfos := make([]Info, 0, len(branches))
	nav := ctx.Navigator()

	// Resolve every branch and parent revision in one batched call so the head
	// and base SHAs below are map lookups, not a git rev-parse per branch.
	revNames := make([]string, 0, len(branches)*2)
	for _, branch := range branches {
		revNames = append(revNames, branch.GetName(), resolveSubmitParentName(nav, branch))
	}
	revisions, _ := ctx.Engine.GetRevisions(revNames)

	// PR-info writes are collected here and persisted in one batched ref write
	// after planning, instead of a git ref write per branch.
	prUpdates := make(map[string]*engine.PrInfo, len(branches))

	var (
		statuses map[string]engine.PRSubmissionStatus
		err      error
	)
	if remoteStatuses == nil {
		remoteCtx, cancelRemote := ctx.RemoteOperationContext()
		defer cancelRemote()
		statuses, err = ctx.Engine.BatchGetPRSubmissionStatus(remoteCtx, branches)
	} else {
		statuses, err = ctx.Engine.BatchGetPRSubmissionStatusWithRemote(branches, remoteStatuses)
	}
	if err != nil {
		return nil, err
	}

	// Fetch missing content only for branches that will actually be prepared.
	// Batch misses (including a failed query) retain the single-PR fallback.
	missingContent := make([]string, 0, len(branches))
	for _, branch := range branches {
		status := statuses[branch.GetName()]
		if _, skip := submissionSkipReason(status, opts); skip {
			continue
		}
		info, _ := branch.GetPrInfo()
		if info != nil && info.Number() != nil && (info.Title() == "" || info.Body() == "") {
			missingContent = append(missingContent, branch.GetName())
		}
	}
	var current map[int]github.PRContent
	if len(missingContent) > 0 && ctx.GitHub() != nil {
		current = actions.FetchPRContentForBranches(ctx, missingContent)
	}

	for _, branch := range branches {
		branchName := branch.GetName()
		status := statuses[branchName]

		action := status.Action
		prNumber := status.PRNumber
		prInfo := status.PRInfo

		// If PR is closed or merged, treat as a new PR creation
		// This allows recovery when a PR was closed (e.g., due to deleted base branch)
		if prInfo != nil && (prInfo.State() == git.PRStateClosed || prInfo.State() == git.PRStateMerged) {
			action = engine.SubmitActionCreate
			prNumber = nil
		}

		isCurrent := branchName == currentBranch

		if reason, skip := submissionSkipReason(status, opts); skip {
			handler.OnEvent(BranchPlanEvent{
				BranchName: branchName,
				Action:     action,
				PRNumber:   prNumber,
				IsCurrent:  isCurrent,
				Skipped:    true,
				SkipReason: reason,
			})
			continue
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

		metadata, err := preparePRMetadataWithContent(branch, metadataOpts, ctx, current)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare metadata for %s: %w", branchName, err)
		}
		prUpdates[branchName] = pendingPrInfo(branch, metadata)

		// Get SHAs from the batched revisions resolved above.
		parentBranchName := resolveSubmitParentName(nav, branch)
		headSHA := revisions[branchName]
		baseSHA := revisions[parentBranchName]

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
			PRNumber:   prNumber,
			IsCurrent:  isCurrent,
			Empty:      empty[branchName],
			Skipped:    false,
		})

		submissionInfos = append(submissionInfos, submissionInfo)
	}

	// Persist all prepared PR info in one batched write so a later submit
	// failure can recover the titles/bodies. Non-fatal, like the per-branch
	// write it replaces.
	if len(prUpdates) > 0 {
		if err := ctx.Engine.BatchUpsertPrInfo(ctx.Context, prUpdates); err != nil {
			ctx.Output.Debug("Failed to save PR metadata: %v", err)
		}
	}

	return submissionInfos, nil
}

// submissionSkipReason is shared by metadata prefetch and plan emission so
// skipped branches never cause extra network reads.
func submissionSkipReason(status engine.PRSubmissionStatus, opts Options) (string, bool) {
	action := status.Action
	if info := status.PRInfo; info != nil && (info.State() == git.PRStateClosed || info.State() == git.PRStateMerged) {
		action = engine.SubmitActionCreate
	}
	if opts.UpdateOnly && action == engine.SubmitActionCreate {
		return "no existing PR", true
	}
	if action == engine.SubmitActionUpdate && !status.NeedsUpdate && !opts.Edit && !opts.Always && !opts.Draft && !opts.Publish {
		return status.Reason, true
	}
	return "", false
}

// confirmPrompt describes what --confirm is about to do in concrete terms.
func confirmPrompt(infos []Info) string {
	creates, updates := 0, 0
	for _, info := range infos {
		if info.Action == engine.SubmitActionCreate {
			creates++
		} else {
			updates++
		}
	}
	switch {
	case creates > 0 && updates > 0:
		return fmt.Sprintf("Create %d and update %d PRs?", creates, updates)
	case creates > 0:
		return fmt.Sprintf("Create %d %s?", creates, pluralPR(creates))
	default:
		return fmt.Sprintf("Update %d %s?", updates, pluralPR(updates))
	}
}

func pluralPR(count int) string {
	if count == 1 {
		return "PR"
	}
	return "PRs"
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
	return actions.ResolveBranchName(nav, branchName)
}
