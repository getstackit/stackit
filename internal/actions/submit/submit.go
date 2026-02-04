// Package submit provides functionality for submitting stacked branches as pull requests.
package submit

import (
	"errors"
	"fmt"
	"sync"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/internal/github"
	"stackit.dev/stackit/internal/tui/components/tree"
	"stackit.dev/stackit/internal/utils"
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
	pr := ctx.PR()
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

	ctx.Logger.Info("submit started", "branchCount", len(branches), "dryRun", opts.DryRun)

	// Get current branch for display purposes (used to highlight in tree view)
	currentBranch := nav.CurrentBranch()
	currentBranchName := ""
	if currentBranch != nil {
		currentBranchName = currentBranch.GetName()
	}

	// Populate remote SHAs early for accurate display
	if err := pr.PopulateRemoteShas(); err != nil {
		ctx.Output.Debug("Failed to populate remote SHAs: %v", err)
	}

	// Build tree structure for display
	branchObjs := make([]engine.Branch, len(branches))
	fixedMap := make(map[string]bool)
	scopeMap := make(map[string]string)
	worktreeMap := make(map[string]string)

	for i, branchName := range branches {
		branch := nav.GetBranch(branchName)
		branchObjs[i] = branch
		fixedMap[branchName] = branch.IsBranchUpToDate()
		scopeMap[branchName] = branch.GetScope().String()

		// Check if this branch is a stack root with a managed worktree
		stackRoot := ctx.Worktree().GetStackRootForBranch(branch)
		if stackRoot == branchName {
			if wtInfo, err := ctx.Worktree().GetWorktreeForStack(stackRoot); err == nil && wtInfo != nil {
				worktreeMap[branchName] = wtInfo.Path
			}
		}
	}

	stackTree := tree.NewStackTree(branchObjs, currentBranchName, nav.Trunk().GetName())

	// Display the stack
	handler.OnEvent(StackDisplayEvent{
		Stack:       stackTree,
		FixedMap:    fixedMap,
		ScopeMap:    scopeMap,
		WorktreeMap: worktreeMap,
	})

	// Restack if requested
	if opts.Restack {
		handler.OnEvent(RestackEvent{Started: true})
		// Convert []string to []engine.Branch for RestackBranches
		branchObjects := make([]engine.Branch, len(branches))
		for i, branchName := range branches {
			branchObjects[i] = nav.GetBranch(branchName)
		}
		if err := actions.RestackBranches(ctx, branchObjects); err != nil {
			return fmt.Errorf("failed to restack branches: %w", err)
		}
		handler.OnEvent(RestackEvent{Completed: true})
	}

	// Validate and prepare branches
	handler.OnEvent(PreparingEvent{})

	if err := ValidateBranchesToSubmit(ctx, branches); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Prepare branches for submit (show planning phase with current indicator)
	submissionInfos, err := prepareBranchesForSubmit(ctx, branchObjs, opts, currentBranchName, handler)
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

	// Build branch info for submission start event
	branchInfos := make([]BranchInfo, len(submissionInfos))
	for i, info := range submissionInfos {
		branchInfos[i] = BranchInfo{
			Name:     info.BranchName,
			Action:   info.Action,
			PRNumber: info.PRNumber,
		}
	}

	// Check if this is a new stack (all creates) - use sequential submission
	// to ensure PRs get sequential numbers in GitHub
	allCreates := true
	for _, info := range submissionInfos {
		if info.Action != actionCreate {
			allCreates = false
			break
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
	var submitErr error
	var errMu sync.Mutex

	if len(submissionInfos) > 0 {
		if allCreates {
			// Sequential submission for new stacks - ensures sequential PR numbers
			for _, info := range submissionInfos {
				if err := submitBranch(ctx, info, opts, handler, repoOwner, repoName, remote); err != nil {
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
				if err := submitBranch(ctx, info, opts, handler, repoOwner, repoName, remote); err != nil {
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

	// Update PR body footers with per-branch progress
	if opts.SubmitFooter {
		utils.Run(branches, func(name string) {
			handler.OnEvent(BranchProgressEvent{
				BranchName: name,
				Status:     StatusSyncing,
			})
			actions.UpdateBranchPRMetadata(ctx, name, repoOwner, repoName)
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

	ctx.Logger.Info("submit completed", "branchCount", len(branches))

	handler.OnEvent(CompletionEvent{Success: true, Message: "Submit complete"})
	return nil
}

// submitBranch submits a single branch
func submitBranch(ctx *app.Context, info Info, opts Options, handler Handler, repoOwner, repoName, remote string) error {
	handler.OnEvent(BranchProgressEvent{
		BranchName: info.BranchName,
		Status:     StatusSubmitting,
	})

	if err := pushBranchIfNeeded(ctx, info, opts, remote); err != nil {
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
	if ctx.GitHubClient != nil {
		return ctx.GitHubClient, nil
	}
	return nil, fmt.Errorf("no GitHub client available - check your GITHUB_TOKEN")
}

// pushBranchIfNeeded pushes a branch to remote if needed
func pushBranchIfNeeded(ctx *app.Context, submissionInfo Info, opts Options, remote string) error {
	// Skip if dry run
	if opts.DryRun {
		return nil
	}

	forceWithLease := !opts.Force
	branch := ctx.Navigator().GetBranch(submissionInfo.BranchName)
	if err := ctx.PR().PushBranch(ctx.Context, branch, remote, git.PushOptions{
		Force:          opts.Force,
		ForceWithLease: forceWithLease,
		NoVerify:       !ctx.Verify,
	}); err != nil {
		if errors.Is(err, git.ErrStaleRemoteInfo) {
			return fmt.Errorf("force-with-lease push of %s failed due to external changes to the remote branch. If you are collaborating on this stack, try 'stackit sync' to pull in changes. Alternatively, use the --force option to bypass the stale info warning", submissionInfo.BranchName)
		}
		return fmt.Errorf("failed to push branch %s: %w", submissionInfo.BranchName, err)
	}
	return nil
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
	prResult, err := ctx.GitHubClient.CreatePullRequest(ctx.Context, repoOwner, repoName, createOpts)
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
	_ = pr.UpsertPrInfo(ctx.Context, branch, engine.NewPrInfo(
		&prNumber,
		submissionInfo.Metadata.Title,
		bodyToCreate,
		"OPEN",
		submissionInfo.Base,
		prURL,
		submissionInfo.Metadata.IsDraft,
	).WithLockReason(branch.GetLockReason()))

	return prURL, nil
}

// updatePullRequestQuiet updates an existing pull request without logging
func updatePullRequestQuiet(ctx *app.Context, submissionInfo Info, opts Options, repoOwner, repoName string) (string, error) {
	pr := ctx.PR()
	nav := ctx.Navigator()

	// Check if base changed
	branch := nav.GetBranch(submissionInfo.BranchName)
	prInfo, _ := branch.GetPrInfo()
	baseChanged := false
	if prInfo != nil && prInfo.Base() != submissionInfo.Base {
		baseChanged = true
	}

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

	updateWarnings, err := ctx.GitHubClient.UpdatePullRequest(ctx.Context, repoOwner, repoName, *submissionInfo.PRNumber, updateOpts)
	if err != nil {
		return "", fmt.Errorf("failed to update PR for %s: %w", submissionInfo.BranchName, err)
	}

	// Log any warnings from PR update (e.g., failed to add labels/assignees)
	for _, warning := range updateWarnings {
		ctx.Output.Warn("%s: %s", submissionInfo.BranchName, warning)
	}

	// Get PR URL
	prInfo, _ = branch.GetPrInfo()
	var prURL string
	if prInfo != nil && prInfo.URL() != "" {
		prURL = prInfo.URL()
	} else {
		// Get from GitHub
		prResult, err := ctx.GitHubClient.GetPullRequestByBranch(ctx.Context, repoOwner, repoName, submissionInfo.BranchName)
		if err == nil && prResult != nil {
			prURL = prResult.HTMLURL
		}
	}

	_ = pr.UpsertPrInfo(ctx.Context, branch, engine.NewPrInfo(
		submissionInfo.PRNumber,
		submissionInfo.Metadata.Title,
		submissionInfo.Metadata.Body,
		"OPEN",
		baseToStore,
		prURL,
		submissionInfo.Metadata.IsDraft,
	).WithLockReason(branch.GetLockReason()))

	return prURL, nil
}

// pushMetadataRefs pushes metadata refs for submitted branches to remote
func pushMetadataRefs(ctx *app.Context, branches []engine.Branch) error {
	if len(branches) == 0 {
		return nil
	}

	rm := ctx.RemoteMetadata()

	// Update LastModifiedBy for each branch
	for _, branch := range branches {
		if err := rm.SetLastModifiedBy(branch.GetName()); err != nil {
			return fmt.Errorf("failed to update metadata for %s: %w", branch.GetName(), err)
		}
	}

	// Check if remote sync is enabled; if not, run compatibility test first
	if !rm.IsRemoteSyncEnabled() {
		if err := ctx.Git().TestRemoteRefCompatibility(); err != nil {
			return fmt.Errorf("remote does not support metadata refs (GitHub compatibility check failed): %w", err)
		}
		rm.SetRemoteSyncEnabled(true)
		// Configure refspec so future git fetch commands also fetch metadata
		if err := ctx.Git().EnsureMetadataRefspecConfigured(); err != nil {
			ctx.Output.Debug("Failed to configure metadata refspec: %v", err)
		}
	}

	// Extract branch names for PushMetadataRefs
	branchNames := make([]string, len(branches))
	for i, branch := range branches {
		branchNames[i] = branch.GetName()
	}

	// Push metadata refs
	if err := ctx.Git().PushMetadataRefs(ctx.Context, branchNames); err != nil {
		// Check if this looks like a race condition (concurrent push)
		if isRaceConditionError(err) {
			return fmt.Errorf("metadata push rejected due to concurrent changes by another user. Run 'st sync' to pull the latest metadata, then retry: %w", err)
		}
		return fmt.Errorf("failed to push metadata refs: %w", err)
	}

	// Push stack metadata refs for any stacks that have branches being submitted
	stackIDs := ctx.Engine.GetStackIDsForBranches(branches)
	if len(stackIDs) > 0 {
		if err := ctx.Git().PushStackMetaRefs(ctx.Context, stackIDs); err != nil {
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
	return contains(errStr, "rejected") &&
		(contains(errStr, "non-fast-forward") ||
			contains(errStr, "fetch first") ||
			contains(errStr, "needs force") ||
			contains(errStr, "updates were rejected"))
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || (len(s) >= len(substr) && searchSubstring(s, substr)))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
