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
	revBranches := make(engine.Branches, 0, len(branches)*2)
	for _, branch := range branches {
		revBranches = append(revBranches, branch, resolveSubmitParent(nav, branch))
	}
	revisions := ctx.Engine.BatchRevisions(revBranches)

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
		statuses, err = ctx.Engine.BatchGetPRSubmissionStatus(remoteCtx, branches, nil)
	} else {
		statuses, err = ctx.Engine.BatchGetPRSubmissionStatus(ctx.Context, branches, remoteStatuses)
	}
	if err != nil {
		return nil, err
	}

	// Fetch missing content only for branches that will actually be prepared.
	// Batch misses (including a failed query) retain the single-PR fallback.
	missingContent := make([]string, 0, len(branches))
	for _, branch := range branches {
		status := statuses[branch.GetName()]
		action, _ := effectiveSubmitAction(status)
		if _, skip := submissionSkipReason(status, action, opts); skip {
			continue
		}
		info, _ := branch.GetPrInfo()
		if info != nil && info.Number() != nil && (info.Title() == "" || info.Body() == "") {
			missingContent = append(missingContent, branch.GetName())
		}
	}
	var current map[git.PRNumber]github.PRContent
	if !opts.regenerate() && len(missingContent) > 0 && ctx.GitHub() != nil {
		current = actions.FetchPRContentForBranches(ctx, missingContent)
	}
	// The remote reads above are the slow part of preparation. End it here,
	// before the loop below, because PreparePRMetadata can prompt for titles,
	// bodies, and reviewers and nothing may print over a prompt.
	handler.OnEvent(PreparingEvent{Completed: true})

	for _, branch := range branches {
		branchName := branch.GetName()
		status := statuses[branchName]

		action, prNumber := effectiveSubmitAction(status)
		isCurrent := branchName == currentBranch

		if reason, skip := submissionSkipReason(status, action, opts); skip {
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
			Text:            opts.Text,
			EditTitle:       opts.editTitle(),
			EditBody:        opts.editBody(),
			Draft:           opts.Draft,
			Publish:         opts.Publish,
			Reviewers:       opts.Reviewers,
			ReviewersPrompt: opts.Reviewers == "" && opts.Edit,
			// Config-driven options
			ConfigDraft:     opts.ConfigDraft,
			ConfigReviewers: opts.ConfigReviewers,
			ConfigLabels:    opts.ConfigLabels,
			ConfigAssignees: opts.ConfigAssignees,
		}

		metadata, err := PreparePRMetadata(branch, metadataOpts, ctx, current)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare metadata for %s: %w", branchName, err)
		}
		prUpdates[branchName] = pendingPrInfo(branch, metadata)

		// Get SHAs from the batched revisions resolved above.
		parentBranchName := resolveSubmitParent(nav, branch).GetName()
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

		var regenerated *PRContentPreview
		// Show replacement text wherever the user reviews the plan before
		// anything is written: a dry run, or the --confirm prompt.
		if opts.regenerate() && (opts.DryRun || opts.Confirm) {
			regenerated = &PRContentPreview{Title: metadata.Title, Body: metadata.Body}
		}
		handler.OnEvent(BranchPlanEvent{
			Regenerated: regenerated,
			BranchName:  branchName,
			Action:      action,
			PRNumber:    prNumber,
			IsCurrent:   isCurrent,
			Empty:       empty[branchName],
			Skipped:     false,
		})

		submissionInfos = append(submissionInfos, submissionInfo)
	}

	// Persist all prepared PR info in one batched write so a later submit
	// failure can recover the titles/bodies. Non-fatal, like the per-branch
	// write it replaces.
	if !opts.DryRun && len(prUpdates) > 0 {
		if err := ctx.Engine.BatchUpsertPrInfo(ctx.Context, prUpdates); err != nil {
			ctx.Output.Debug("Failed to save PR metadata: %v", err)
		}
	}

	return submissionInfos, nil
}

// effectiveSubmitAction treats a closed or merged PR as a new PR creation. This
// allows recovery when a PR was closed (e.g., due to deleted base branch).
func effectiveSubmitAction(status engine.PRSubmissionStatus) (engine.SubmitAction, *git.PRNumber) {
	if info := status.PRInfo; info != nil && (info.State() == git.PRStateClosed || info.State() == git.PRStateMerged) {
		return engine.SubmitActionCreate, nil
	}
	return status.Action, status.PRNumber
}

// submissionSkipReason is shared by metadata prefetch and plan emission so
// skipped branches never cause extra network reads.
func submissionSkipReason(status engine.PRSubmissionStatus, action engine.SubmitAction, opts Options) (string, bool) {
	if opts.UpdateOnly && action == engine.SubmitActionCreate {
		return "no existing PR", true
	}
	if action == engine.SubmitActionUpdate && !status.NeedsUpdate && !opts.Edit && !opts.Always && !opts.regenerate() && !opts.Draft && !opts.Publish {
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
		stackRange = engine.StackRangeDownstack(engine.IncludeCurrentBranch)
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
