// Package submit provides functionality for submitting stacked branches as pull requests.
package submit

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/utils"
)

// PR submission action constants
const (
	actionCreate = "create"
	actionUpdate = "update"
)

// Options contains options for the submit command
type Options struct {
	Branch               string
	StackRange           engine.StackRange
	Force                bool
	DryRun               bool
	Confirm              bool
	UpdateOnly           bool
	Always               bool
	Restack              bool
	Draft                bool
	Publish              bool
	Edit                 bool
	EditTitle            bool
	EditDescription      bool
	NoEdit               bool
	NoEditTitle          bool
	NoEditDescription    bool
	Reviewers            string
	TeamReviewers        string
	MergeWhenReady       bool
	RerequestReview      bool
	View                 bool
	Web                  bool
	Comment              string
	TargetTrunk          string
	IgnoreOutOfSyncTrunk bool
	SubmitFooter         bool // Whether to include PR footer (from config)
	NoLabels             bool // Skip applying default labels from config
	NoAssignees          bool // Skip applying default assignees from config

	// Config-driven options (these are merged with flags)
	ConfigDraft     bool     // Default draft mode from config
	ConfigWeb       string   // When to open browser from config (always/created/never)
	ConfigLabels    []string // Default labels from config
	ConfigReviewers []string // Default reviewers from config
	ConfigAssignees []string // Default assignees from config
}

// Info contains information about a branch to submit
type Info struct {
	BranchName string
	Head       string
	Base       string
	HeadSHA    string
	BaseSHA    string
	Action     string // "create" or "update"
	PRNumber   *int
	Metadata   *PRMetadata
}

// Action performs the submit operation with an event handler for progress feedback.
func Action(ctx *app.Context, opts Options, handler Handler) error {
	// Validate flags
	if opts.Draft && opts.Publish {
		return fmt.Errorf("can't use both --publish and --draft flags in one command")
	}

	nav := ctx.Navigator()
	eng := ctx.Engine

	// Determine target branch (explicit --branch flag or current branch)
	var targetBranch engine.Branch
	if opts.Branch != "" {
		targetBranch = nav.GetBranch(opts.Branch)
	} else if cb := nav.CurrentBranch(); cb != nil {
		targetBranch = *cb
	}

	// Check if target branch is untracked
	if targetBranch.GetName() != "" && !targetBranch.IsTracked() && !targetBranch.IsTrunk() {
		branchName := targetBranch.GetName()
		if !handler.IsInteractive() {
			// Non-interactive: inform and exit gracefully
			ctx.Output.Info("Branch %s is not tracked by stackit.", branchName)
			ctx.Output.Tip("Run 'stackit track' to track this branch, then try again.")
			handler.OnEvent(CompletionEvent{Success: true, Message: "Branch not tracked"})
			return nil
		}

		// Interactive: prompt to track
		message := fmt.Sprintf("Branch %s is not tracked. Track it with %s as parent?",
			branchName, nav.Trunk().GetName())
		shouldTrack, err := handler.Confirm(message, true)
		if err != nil {
			return err
		}
		if !shouldTrack {
			ctx.Output.Info("Skipping. Use 'stackit track --parent <branch>' for a different parent.")
			handler.OnEvent(CompletionEvent{Success: true, Message: "Tracking declined"})
			return nil
		}

		// Track the branch
		if err := eng.TrackBranch(ctx.Context, branchName, nav.Trunk().GetName()); err != nil {
			return fmt.Errorf("failed to track branch: %w", err)
		}
		ctx.Output.Info("Tracked %s with parent %s.", branchName, nav.Trunk().GetName())
	}

	// Get branches to submit
	branches, err := getBranchesToSubmit(ctx, opts)
	if err != nil {
		return err
	}
	if len(branches) == 0 {
		handler.OnEvent(CompletionEvent{Success: true, Message: "No branches to submit"})
		return nil
	}

	ctx.Logger.Info("submit started branchCount=%v dryRun=%v", len(branches), opts.DryRun)

	// Get current branch for display purposes (used to highlight in tree view)
	currentBranch := nav.CurrentBranch()
	currentBranchName := ""
	if currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}

	// Build tree structure for display
	branchObjs := make(engine.Branches, len(branches))
	for i, branchName := range branches {
		branchObjs[i] = nav.GetBranch(branchName)
	}

	// Resolve up-to-date status for every branch in one batched parent-revision
	// read rather than a per-branch IsBranchUpToDate() (each of which shells a
	// separate `git rev-parse` for the parent).
	statuses := eng.ReadBranchStatuses(branchObjs)

	fixedMap := make(map[string]bool)
	scopeMap := make(map[string]string)
	worktreeMap := make(map[string]string)

	for i, branchName := range branches {
		branch := branchObjs[i]
		fixedMap[branchName] = statuses.IsUpToDate(branch)
		scopeMap[branchName] = branch.GetScope().String()

		// Check if this branch belongs to a stack with a managed worktree.
		stackRoot := ctx.Worktree().GetStackRootForBranch(branch)
		if stackRoot != "" {
			if wtInfo, err := ctx.Worktree().GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
				worktreeMap[branchName] = wtInfo.Path
			}
		}
	}

	stackSnapshot := buildStackSnapshot(nav, branchObjs, currentBranchName, nav.Trunk().GetName(), fixedMap, scopeMap, worktreeMap)

	// Display the stack
	handler.OnEvent(StackDisplayEvent{
		Stack: stackSnapshot,
	})

	// Restack if requested
	if opts.Restack {
		handler.OnEvent(RestackEvent{Started: true})
		if err := actions.RestackBranches(ctx, branchObjs); err != nil {
			return fmt.Errorf("failed to restack branches: %w", err)
		}
		handler.OnEvent(RestackEvent{Completed: true})
	}

	// Validate and prepare branches
	handler.OnEvent(PreparingEvent{})

	// Read remote branch status (one `git ls-remote`) concurrently with
	// validation's PR-info sync (one GraphQL query) for real submits. Dry runs use
	// the engine's lazy batched status path below so create-only stacks stay
	// offline. The snapshot reflects post-restack local SHAs and is reused by
	// planning and the push below, so a real submit reads the remote ref list
	// exactly once. The channel is buffered so the goroutine never blocks even if
	// validation returns early.
	var remoteStatusCh chan engine.BranchRemoteStatuses
	if !opts.DryRun {
		remoteStatusCh = make(chan engine.BranchRemoteStatuses, 1)
		remoteCtx, cancelRemote := ctx.RemoteOperationContext()
		go func() {
			defer cancelRemote()
			remoteStatusCh <- eng.ReadBranchRemoteStatuses(remoteCtx, branchObjs)
		}()
	}

	if err := ValidateBranchesToSubmit(ctx, branches); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	var remoteStatuses engine.BranchRemoteStatuses
	if remoteStatusCh != nil {
		remoteStatuses = <-remoteStatusCh
	}

	// Prepare branches for submit (show planning phase with current indicator)
	submissionInfos, err := prepareBranchesForSubmit(ctx, branchObjs, opts, currentBranchName, remoteStatuses, handler)
	if err != nil {
		return fmt.Errorf("failed to prepare branches: %w", err)
	}

	// Check if we should abort
	if opts.DryRun {
		handler.OnEvent(CompletionEvent{Success: true, Message: "Dry run complete"})
		return nil
	}

	if len(submissionInfos) == 0 {
		handler.OnEvent(CompletionEvent{Success: true, Message: "All PRs up to date"})
		return nil
	}

	// Handle interactive confirmation
	if opts.Confirm {
		confirmed, err := handler.Confirm("Proceed with submit?", true)
		if err != nil {
			return fmt.Errorf("confirmation canceled: %w", err)
		}
		if !confirmed {
			handler.OnEvent(CompletionEvent{Success: false, Message: "Submit canceled"})
			return nil
		}
	}

	// Build branch info for submission start event. Track whether every action
	// is a create — new stacks submit sequentially so PRs get sequential numbers.
	branchInfos := make([]BranchInfo, len(submissionInfos))
	allCreates := true
	for i, info := range submissionInfos {
		branchInfos[i] = BranchInfo{
			Name:     info.BranchName,
			Action:   info.Action,
			PRNumber: info.PRNumber,
		}
		if info.Action != actionCreate {
			allCreates = false
		}
	}

	// Start submission phase with a worker pool to avoid spawning too many goroutines
	handler.OnEvent(SubmissionStartEvent{Branches: branchInfos, IsSequential: allCreates})

	githubClient, err := getGitHubClient(ctx)
	if err != nil {
		return err
	}
	repoOwner, repoName := githubClient.GetOwnerRepo()

	remote := nav.GetRemote()

	// Push every branch that needs it in a single git invocation instead of one
	// `git push` per branch (N network round trips). The force-with-lease guards
	// reuse remoteStatuses prefetched above, so no further `git ls-remote` runs.
	// The push runs before the create/update loop so every ref exists on the
	// remote before its PR is created; the per-branch result is consumed by
	// submitBranch below.
	pushResults := pushSubmittedBranches(ctx, opts, submissionInfos, remote, remoteStatuses)

	var submitErr error
	var errMu sync.Mutex

	if len(submissionInfos) > 0 {
		if allCreates {
			// Sequential submission for new stacks - ensures sequential PR numbers
			for _, info := range submissionInfos {
				if err := submitBranch(ctx, info, opts, handler, repoOwner, repoName, pushResults); err != nil {
					errMu.Lock()
					if submitErr == nil {
						submitErr = err
					}
					errMu.Unlock()
				}
			}
		} else {
			// Parallel submission for updates (faster when PRs already exist)
			utils.Run(submissionInfos, func(info Info) {
				if err := submitBranch(ctx, info, opts, handler, repoOwner, repoName, pushResults); err != nil {
					errMu.Lock()
					if submitErr == nil {
						submitErr = err
					}
					errMu.Unlock()
				}
			})
		}
	}

	if submitErr != nil {
		handler.OnEvent(CompletionEvent{Success: false, Message: "Submit failed"})
		return submitErr
	}

	// Update PR body footers with per-branch progress. Fetch every PR's current
	// content in one GraphQL query up front (instead of a GET per branch), then
	// apply each footer/title update in parallel.
	if opts.SubmitFooter {
		prContent := actions.FetchPRContentForBranches(ctx, branches, repoOwner, repoName)
		utils.Run(branches, func(name string) {
			handler.OnEvent(BranchProgressEvent{
				BranchName: name,
				Status:     StatusSyncing,
			})
			actions.UpdateBranchPRMetadataWithContent(ctx, name, repoOwner, repoName, prContent)
			handler.OnEvent(BranchProgressEvent{
				BranchName: name,
				Status:     StatusDone,
			})
		})
	}

	// Push metadata refs for successfully submitted branches
	if err := pushMetadataRefs(ctx, branchObjs); err != nil {
		handler.OnEvent(CompletionEvent{Success: false, Message: "Submit failed"})
		return fmt.Errorf("failed to push metadata to remote: %w. Your PRs were created/updated successfully, but metadata sync failed. Run 'st sync' and try submitting again", err)
	}

	ctx.Logger.Info("submit completed branchCount=%v", len(branches))

	handler.OnEvent(CompletionEvent{Success: true, Message: "Submit complete"})
	return nil
}

// buildStackSnapshot captures the stack relationships and metadata that submit
// adapters need for rendering.
func buildStackSnapshot(
	nav engine.StackNavigator,
	branches []engine.Branch,
	currentBranchName string,
	trunkBranchName string,
	fixedMap map[string]bool,
	scopeMap map[string]string,
	worktreeMap map[string]string,
) StackSnapshot {
	parentMap := make(map[string]string, len(branches))
	branchNames := make([]string, len(branches))
	for i, branch := range branches {
		branchName := branch.GetName()
		branchNames[i] = branchName
		parentMap[branchName] = resolveSubmitParentName(nav, branch)
	}

	return StackSnapshot{
		Branches:      branchNames,
		CurrentBranch: currentBranchName,
		TrunkBranch:   trunkBranchName,
		ParentMap:     parentMap,
		FixedMap:      fixedMap,
		ScopeMap:      scopeMap,
		WorktreeMap:   worktreeMap,
	}
}

// submitBranch creates or updates the PR for a single branch. The branch has
// already been pushed as part of the batched push in Action; pushResults carries
// that branch's push outcome.
func submitBranch(ctx *app.Context, info Info, opts Options, handler Handler, repoOwner, repoName string, pushResults map[string]error) error {
	handler.OnEvent(BranchProgressEvent{
		BranchName: info.BranchName,
		Status:     StatusSubmitting,
	})

	if err := pushResults[info.BranchName]; err != nil {
		if errors.Is(err, git.ErrStaleRemoteInfo) {
			err = fmt.Errorf("force-with-lease push of %s failed due to external changes to the remote branch. If you are collaborating on this stack, try 'stackit sync' to pull in changes. Alternatively, use the --force option to bypass the stale info warning", info.BranchName)
		}
		handler.OnEvent(BranchProgressEvent{
			BranchName: info.BranchName,
			Status:     StatusError,
			Error:      err,
		})
		return err
	}

	var prURL string
	var err error
	if info.Action == actionCreate {
		prURL, err = createPullRequestQuiet(ctx, info, repoOwner, repoName)
	} else {
		prURL, err = updatePullRequestQuiet(ctx, info, opts, repoOwner, repoName)
	}

	if err != nil {
		handler.OnEvent(BranchProgressEvent{
			BranchName: info.BranchName,
			Status:     StatusError,
			Error:      err,
		})
		return err
	}

	handler.OnEvent(BranchProgressEvent{
		BranchName: info.BranchName,
		Status:     StatusDone,
		URL:        prURL,
	})

	// Open in browser if requested (via flag or config)
	shouldOpenBrowser := false
	if prURL != "" {
		// Explicit flags take precedence
		if opts.View || opts.Web {
			shouldOpenBrowser = true
		} else {
			// Check config setting
			switch opts.ConfigWeb {
			case "always":
				shouldOpenBrowser = true
			case "created":
				shouldOpenBrowser = info.Action == actionCreate
			}
		}
	}

	if shouldOpenBrowser {
		if err := utils.OpenBrowser(prURL); err != nil {
			ctx.Output.Debug("Failed to open browser: %v", err)
		}
	}

	return nil
}

// getGitHubClient returns the GitHub client from context
func getGitHubClient(ctx *app.Context) (github.Client, error) {
	if client := ctx.GitHub(); client != nil {
		return client, nil
	}
	if err := ctx.GitHubError(); err != nil {
		return nil, fmt.Errorf("GitHub client initialization failed: %w", err)
	}
	return nil, fmt.Errorf("no GitHub client available - check your GITHUB_TOKEN")
}

// pushSubmittedBranches pushes every branch that needs it in a single git
// invocation and returns a per-branch result map (nil entry = success). A branch
// already in sync with the remote is skipped (unless --force) to avoid a
// spurious "cannot lock ref: reference already exists" rejection, and is absent
// from the map — callers treat a missing entry as success. remoteStatuses is the
// batched snapshot read once in Action, so building the push specs hits no
// network per branch.
func pushSubmittedBranches(ctx *app.Context, opts Options, infos []Info, remote string, remoteStatuses engine.BranchRemoteStatuses) map[string]error {
	nav := ctx.Navigator()
	forceWithLease := !opts.Force

	specs := make([]git.PushSpec, 0, len(infos))
	for _, info := range infos {
		branch := nav.GetBranch(info.BranchName)
		status := remoteStatuses.ForBranch(branch)
		if forceWithLease && status.Matches() {
			continue
		}
		specs = append(specs, git.PushSpec{
			BranchName:        info.BranchName,
			ExpectedRemoteSHA: status.RemoteSha,
		})
	}

	if len(specs) == 0 {
		return map[string]error{}
	}

	return ctx.PR().PushBranches(ctx.Context, remote, specs, git.PushOptions{
		Force:    opts.Force,
		NoVerify: !ctx.Verify,
	})
}

// createPullRequestQuiet creates a new pull request without logging
func createPullRequestQuiet(ctx *app.Context, submissionInfo Info, repoOwner, repoName string) (string, error) {
	pr := ctx.PR()
	nav := ctx.Navigator()

	// If body is empty, try to generate one from commits
	bodyToCreate := submissionInfo.Metadata.Body
	if bodyToCreate == "" {
		branch := nav.GetBranch(submissionInfo.BranchName)
		generatedBody, genErr := GetPRBody(branch, false, "")
		if genErr == nil && generatedBody != "" {
			bodyToCreate = generatedBody
		}
	}
	createOpts := github.CreatePROptions{
		Title:         submissionInfo.Metadata.Title,
		Body:          bodyToCreate,
		Head:          submissionInfo.Head,
		Base:          submissionInfo.Base,
		Draft:         submissionInfo.Metadata.IsDraft,
		Reviewers:     submissionInfo.Metadata.Reviewers,
		TeamReviewers: submissionInfo.Metadata.TeamReviewers,
		Labels:        submissionInfo.Metadata.Labels,
		Assignees:     submissionInfo.Metadata.Assignees,
	}
	prResult, err := ctx.GitHub().CreatePullRequest(ctx.Context, repoOwner, repoName, createOpts)
	if err != nil {
		return "", fmt.Errorf("failed to create PR for %s: %w", submissionInfo.BranchName, err)
	}

	// Log any warnings from PR creation (e.g., failed to add labels/assignees)
	for _, warning := range prResult.Warnings {
		ctx.Output.Warn("%s: %s", submissionInfo.BranchName, warning)
	}

	// Update PR info
	prNumber := prResult.Number
	prURL := prResult.HTMLURL
	branch := nav.GetBranch(submissionInfo.BranchName)
	// Use bodyToCreate (the body that was actually sent) instead of submissionInfo.Metadata.Body
	// This ensures local state matches what's on GitHub
	if err := pr.UpsertPrInfo(ctx.Context, branch, engine.NewPrInfo(
		&prNumber,
		submissionInfo.Metadata.Title,
		bodyToCreate,
		"OPEN",
		submissionInfo.Base,
		prURL,
		submissionInfo.Metadata.IsDraft,
	).WithLockReason(branch.GetLockReason()).WithBaseSHA(submissionInfo.BaseSHA)); err != nil {
		ctx.Output.Debug("Failed to store local PR info for %s: %v", submissionInfo.BranchName, err)
	}

	return prURL, nil
}

// updatePullRequestQuiet updates an existing pull request without logging
func updatePullRequestQuiet(ctx *app.Context, submissionInfo Info, opts Options, repoOwner, repoName string) (string, error) {
	pr := ctx.PR()
	nav := ctx.Navigator()

	// Check if base changed
	branch := nav.GetBranch(submissionInfo.BranchName)
	prInfo, _ := branch.GetPrInfo()
	baseChanged := prInfo != nil && prInfo.Base() != submissionInfo.Base

	// Detect stale base.sha: base branch name is the same but the parent was rewritten
	// (rebased/force-pushed) since the last submit. GitHub retains the old parent tip SHA
	// in base.sha, causing the child PR to show the parent's tip commit in its diff. A
	// fast-forward of the parent does NOT make base.sha stale, so only treat it as stale
	// when the stored BaseSHA is no longer an ancestor of the current parent tip. Fix by
	// temporarily changing the PR base to trunk before setting it back to the actual
	// parent — this forces GitHub to recompute base.sha to the current parent tip.
	parentRewritten := false
	if !baseChanged && prInfo != nil && prInfo.BaseSHA() != "" && prInfo.BaseSHA() != submissionInfo.BaseSHA {
		isAncestor, err := ctx.Engine.IsAncestor(ctx.Context, prInfo.BaseSHA(), submissionInfo.BaseSHA)
		parentRewritten = err == nil && !isAncestor
	}
	baseSHAStale := baseSHAIsStale(prInfo, submissionInfo, ctx.Engine.Trunk().GetName(), parentRewritten)

	updateOpts := github.UpdatePROptions{
		Title:           &submissionInfo.Metadata.Title,
		Reviewers:       submissionInfo.Metadata.Reviewers,
		TeamReviewers:   submissionInfo.Metadata.TeamReviewers,
		Labels:          submissionInfo.Metadata.Labels,
		Assignees:       submissionInfo.Metadata.Assignees,
		MergeWhenReady:  &opts.MergeWhenReady,
		RerequestReview: opts.RerequestReview,
	}

	// Only update body if it's not empty. GitHub will preserve the existing body if omitted.
	if submissionInfo.Metadata.Body != "" {
		updateOpts.Body = &submissionInfo.Metadata.Body
	}

	// Only update draft status if it's explicitly set via flags
	if opts.Draft || opts.Publish {
		updateOpts.Draft = &submissionInfo.Metadata.IsDraft
	}

	// Before updating the base, check if there are commits between the new base and head
	// GitHub will reject the update if there are no commits between them
	baseToStore := submissionInfo.Base
	if baseChanged {
		baseUpdated := false
		// Only update base if there are commits between base and head
		if submissionInfo.BaseSHA != submissionInfo.HeadSHA {
			// Check if there are actually commits between base and head
			branch := nav.GetBranch(submissionInfo.BranchName)
			commits, err := branch.GetAllCommits(engine.CommitFormatSHA)
			if err == nil && len(commits) > 0 {
				// There are commits, safe to update base
				updateOpts.Base = &submissionInfo.Base
				baseUpdated = true
			}
			// If no commits or error, skip base update to avoid GitHub 422 error
		}
		// If base SHA equals head SHA, skip base update (no commits between them)

		if !baseUpdated && prInfo != nil {
			// If we skipped the update, keep the existing base in our local cache
			// so it reflects what is actually on GitHub.
			baseToStore = prInfo.Base()
		}
	}

	// When the parent was rewritten (stale base.sha), temporarily retarget the PR
	// to trunk and then back to the actual parent. GitHub recomputes base.sha on each
	// base change, so the second change sets it to the current parent tip.
	retargetedToTrunk := false
	if baseSHAStale {
		trunkName := ctx.Engine.Trunk().GetName()
		trunkOpts := github.UpdatePROptions{Base: &trunkName}
		if _, err := ctx.GitHub().UpdatePullRequest(ctx.Context, repoOwner, repoName, *submissionInfo.PRNumber, trunkOpts); err != nil {
			ctx.Output.Debug("Failed to refresh stale base.sha for %s: %v", submissionInfo.BranchName, err)
		} else {
			// The main update below will set it back to the actual parent, causing GitHub
			// to recompute base.sha to the current parent tip.
			updateOpts.Base = &submissionInfo.Base
			retargetedToTrunk = true
		}
	}

	updateWarnings, err := ctx.GitHub().UpdatePullRequest(ctx.Context, repoOwner, repoName, *submissionInfo.PRNumber, updateOpts)
	if err != nil {
		if retargetedToTrunk {
			// The PR is currently targeting trunk from the base.sha refresh above; restore
			// the real parent so a failed update doesn't strand it showing the full stack diff.
			rollbackOpts := github.UpdatePROptions{Base: &submissionInfo.Base}
			if _, rollbackErr := ctx.GitHub().UpdatePullRequest(ctx.Context, repoOwner, repoName, *submissionInfo.PRNumber, rollbackOpts); rollbackErr != nil {
				ctx.Output.Debug("Failed to restore PR base for %s after update error: %v", submissionInfo.BranchName, rollbackErr)
			}
		}
		return "", fmt.Errorf("failed to update PR for %s: %w", submissionInfo.BranchName, err)
	}

	// Log any warnings from PR update (e.g., failed to add labels/assignees)
	for _, warning := range updateWarnings {
		ctx.Output.Warn("%s: %s", submissionInfo.BranchName, warning)
	}

	// If the base.sha refresh was needed but failed, keep the previously stored BaseSHA
	// so the next submit detects the rewrite again and retries the refresh. Storing the
	// current tip would make the stale diff permanent.
	baseSHAToStore := submissionInfo.BaseSHA
	if baseSHAStale && !retargetedToTrunk {
		baseSHAToStore = prInfo.BaseSHA()
	}

	// Get PR URL
	prInfo, _ = branch.GetPrInfo()
	var prURL string
	if prInfo != nil && prInfo.URL() != "" {
		prURL = prInfo.URL()
	} else {
		// Get from GitHub
		prResult, err := ctx.GitHub().GetPullRequestByBranch(ctx.Context, repoOwner, repoName, submissionInfo.BranchName)
		if err == nil && prResult != nil {
			prURL = prResult.HTMLURL
		}
	}

	if err := pr.UpsertPrInfo(ctx.Context, branch, engine.NewPrInfo(
		submissionInfo.PRNumber,
		submissionInfo.Metadata.Title,
		submissionInfo.Metadata.Body,
		"OPEN",
		baseToStore,
		prURL,
		submissionInfo.Metadata.IsDraft,
	).WithLockReason(branch.GetLockReason()).WithBaseSHA(baseSHAToStore)); err != nil {
		ctx.Output.Debug("Failed to store local PR info for %s: %v", submissionInfo.BranchName, err)
	}

	return prURL, nil
}

// baseSHAIsStale reports whether GitHub's stored base.sha for an existing PR is stale
// and needs the trunk-and-back retarget to be refreshed. It is stale only when the base
// branch name is unchanged but the parent was rewritten (rebased/force-pushed) since the
// last submit — a fast-forward of the parent keeps GitHub's base.sha valid.
// parentRewritten is whether the stored BaseSHA is no longer an ancestor of the current
// parent tip.
func baseSHAIsStale(prInfo *engine.PrInfo, info Info, trunkName string, parentRewritten bool) bool {
	if prInfo == nil || prInfo.BaseSHA() == "" {
		return false
	}
	// A base branch name change is handled by the regular base update, which already
	// makes GitHub recompute base.sha.
	if prInfo.Base() != info.Base {
		return false
	}
	if info.Base == trunkName {
		return false
	}
	// An empty child (no commits between parent and head) can't be retargeted back to
	// its parent — GitHub rejects base updates with no commits between base and head,
	// which would strand the PR targeting trunk.
	if info.BaseSHA == info.HeadSHA {
		return false
	}
	return prInfo.BaseSHA() != info.BaseSHA && parentRewritten
}

// pushMetadataRefs pushes metadata refs for submitted branches to remote
func pushMetadataRefs(ctx *app.Context, branches engine.Branches) error {
	if len(branches) == 0 {
		return nil
	}

	rm := ctx.RemoteMetadata()

	branchNames := branches.Names()

	if err := rm.BatchSetLastModifiedBy(branchNames); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	if err := rm.PrepareRemoteMetadataPush(ctx.Context); err != nil {
		return fmt.Errorf("remote does not support metadata refs (GitHub compatibility check failed): %w", err)
	}

	// Push metadata refs
	if err := rm.PushMetadataForBranches(ctx.Context, branchNames); err != nil {
		// Check if this looks like a race condition (concurrent push)
		if isRaceConditionError(err) {
			return fmt.Errorf("metadata push rejected due to concurrent changes by another user. Run 'st sync' to pull the latest metadata, then retry: %w", err)
		}
		return fmt.Errorf("failed to push metadata refs: %w", err)
	}

	// Push stack metadata refs for any stacks that have branches being submitted
	stackIDs := ctx.Engine.GetStackIDsForBranches(branches)
	if len(stackIDs) > 0 {
		if err := ctx.Engine.PushStackMetadata(ctx.Context, stackIDs); err != nil {
			ctx.Output.Debug("Failed to push stack metadata refs: %v", err)
			// Non-fatal: stack metadata push failure shouldn't fail the submit
		}
	}

	return nil
}

// isRaceConditionError checks if an error indicates a race condition during push
func isRaceConditionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Git push rejection messages that indicate concurrent changes
	return strings.Contains(errStr, "rejected") &&
		(strings.Contains(errStr, "non-fast-forward") ||
			strings.Contains(errStr, "fetch first") ||
			strings.Contains(errStr, "needs force") ||
			strings.Contains(errStr, "updates were rejected"))
}
