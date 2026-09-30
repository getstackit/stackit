package merge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/utils"
)

const (
	// DefaultMergeTimeout is the default timeout for waiting on a merge to complete
	DefaultMergeTimeout = 30 * time.Minute
	// DefaultMergePollInterval is the default interval between merge status checks
	DefaultMergePollInterval = 10 * time.Second
)

// GitHub mergeStateStatus values that allow an immediate merge.
const (
	mergeStateClean    = "CLEAN"
	mergeStateHasHooks = "HAS_HOOKS"
)

// MergePROutcome represents the result of a MergePR attempt.
type MergePROutcome int

const (
	// MergePRMerged means the PR was merged (directly or via wait).
	MergePRMerged MergePROutcome = iota
	// MergePRAutomergeEnabled means automerge was enabled (fire-and-forget).
	MergePRAutomergeEnabled
)

// MergePROptions describes a single PR merge.
type MergePROptions struct {
	BranchName  string
	PRNumber    git.PRNumber
	MergeMethod github.MergeMethod
	Wait        bool // Block until the PR merges instead of returning after enabling automerge
}

// prMergeAPI abstracts the external GitHub operations used by the merge
// orchestration, enabling unit testing without real GitHub calls.
type prMergeAPI interface {
	getMergeableState(ctx context.Context, runner github.GitCommandRunner, prNodeID string) (*github.PRMergeableState, error)
	enableAutoMerge(ctx context.Context, runner github.GitCommandRunner, prNodeID string, method github.MergeMethod) error
	waitForPRMerge(ctx context.Context, runner github.GitCommandRunner, prNodeID string, timeout, interval time.Duration) error
	waitForMergeable(ctx context.Context, runner github.GitCommandRunner, prNodeID string, timeout, interval time.Duration) (*github.PRMergeableState, error)
	mergePR(ctx context.Context, branchName string, method github.MergeMethod) error
}

// defaultPRMergeAPI delegates to the real GitHub functions and client.
type defaultPRMergeAPI struct {
	client github.Client
}

func (d *defaultPRMergeAPI) getMergeableState(ctx context.Context, runner github.GitCommandRunner, prNodeID string) (*github.PRMergeableState, error) {
	return getMergeableStateWithRetry(ctx, runner, prNodeID)
}

func (d *defaultPRMergeAPI) enableAutoMerge(ctx context.Context, runner github.GitCommandRunner, prNodeID string, method github.MergeMethod) error {
	return github.EnableAutoMerge(ctx, runner, prNodeID, github.EnableAutoMergeOptions{MergeMethod: method})
}

func (d *defaultPRMergeAPI) waitForPRMerge(ctx context.Context, runner github.GitCommandRunner, prNodeID string, timeout, interval time.Duration) error {
	return github.WaitForPRMerge(ctx, runner, prNodeID, timeout, interval)
}

func (d *defaultPRMergeAPI) waitForMergeable(ctx context.Context, runner github.GitCommandRunner, prNodeID string, timeout, interval time.Duration) (*github.PRMergeableState, error) {
	return github.WaitForMergeable(ctx, runner, prNodeID, timeout, interval)
}

func (d *defaultPRMergeAPI) mergePR(ctx context.Context, branchName string, method github.MergeMethod) error {
	return d.client.MergePullRequest(ctx, branchName, github.MergePROptions{Method: method})
}

// newPRMergeAPI builds the merge API for a run. It is a variable so tests can
// drive Drain end to end rather than only orchestrateMerge, which already
// takes a prMergeAPI directly.
//
// The seam has to be here rather than on the github.Client: three of the five
// prMergeAPI methods reach GitHub's GraphQL API by shelling out to `gh` through
// the git runner, not through the client, so injecting a mock client leaves
// them talking to the real thing.
var newPRMergeAPI = func(ctx *app.Context) prMergeAPI {
	return &defaultPRMergeAPI{client: ctx.GitHub()}
}

// MergePR merges one PR: it resolves the PR's GraphQL node ID, then runs the
// 3-tier merge (direct merge → automerge → poll fallback) described on
// orchestrateMerge. Progress is reported through handler.
func MergePR(ctx *app.Context, opts MergePROptions, handler ProgressHandler) (MergePROutcome, error) {
	prNodeID, err := GetPRNodeID(ctx, opts.PRNumber)
	if err != nil {
		return 0, err
	}
	runner := ctx.GHRunner
	return orchestrateMerge(ctx.Context, handler, runner, newPRMergeAPI(ctx), opts, prNodeID)
}

// GetPRNodeID fetches the GraphQL node ID for a PR by number. The node ID is
// what the GraphQL merge and automerge operations address a PR by.
func GetPRNodeID(ctx *app.Context, prNumber git.PRNumber) (string, error) {
	remoteCtx, cancelRemote := ctx.RemoteOperationContext()
	prInfo, err := ctx.GitHub().GetPullRequest(remoteCtx, prNumber)
	cancelRemote()
	if err != nil {
		return "", fmt.Errorf("failed to get PR #%d info: %w", prNumber, err)
	}
	if prInfo.NodeID == "" {
		return "", fmt.Errorf("PR #%d does not have a Node ID", prNumber)
	}
	return prInfo.NodeID, nil
}

// EnableAutoMergeForPR enables GitHub auto-merge on a PR using the configured
// merge method (prompting for one if none is configured).
func EnableAutoMergeForPR(ctx *app.Context, prNumber git.PRNumber) error {
	prNodeID, err := GetPRNodeID(ctx, prNumber)
	if err != nil {
		return err
	}
	mergeMethod, err := GetMergeMethod(ctx, ctx.Config, ctx.GitHub())
	if err != nil {
		return fmt.Errorf("could not determine merge method: %w", err)
	}
	return github.EnableAutoMerge(ctx.Context, ctx.GHRunner, prNodeID, github.EnableAutoMergeOptions{MergeMethod: mergeMethod})
}

// orchestrateMerge implements a 3-tier merge strategy:
//  1. If the PR is already ready (CLEAN/HAS_HOOKS), merge directly via REST API.
//  2. Otherwise, try EnableAutoMerge. On success, optionally wait for merge.
//  3. If EnableAutoMerge fails:
//     - "clean status" error → direct merge (race: became ready between check and automerge).
//     - "not enabled on repo" + wait → poll until mergeable, then merge directly.
//     - "not enabled on repo" + no wait → error with --wait suggestion.
func orchestrateMerge(ctx context.Context, handler ProgressHandler, runner github.GitCommandRunner, api prMergeAPI, opts MergePROptions, prNodeID string) (MergePROutcome, error) {
	// Step 1: Check current merge state
	mergeableState, err := api.getMergeableState(ctx, runner, prNodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to check PR mergeable state: %w", err)
	}
	if mergeableState.State == git.PRStateMerged {
		handler.OnProgress(PRAlreadyMergedEvent{PRNumber: opts.PRNumber})
		return MergePRMerged, nil
	}
	if mergeableState.State != git.PRStateOpen {
		return 0, fmt.Errorf("PR #%d is %s (not open)", opts.PRNumber, mergeableState.State)
	}
	if !mergeableState.Mergeable {
		if strings.EqualFold(mergeableState.MergeStateText, "UNKNOWN") {
			return 0, fmt.Errorf("PR #%d mergeability is still being calculated by GitHub after 5 retries. Try again shortly", opts.PRNumber)
		}
		return 0, formatUnmergeableError(opts.PRNumber, mergeableState)
	}

	// If already CLEAN or HAS_HOOKS, merge directly
	if isReadyToMerge(mergeableState.MergeStateText) {
		handler.OnProgress(PRMergingDirectlyEvent{PRNumber: opts.PRNumber, Method: opts.MergeMethod, MergeState: mergeableState.MergeStateText})
		return mergeDirectly(ctx, handler, api, opts)
	}

	// Step 2: Not immediately ready — try automerge
	handler.OnProgress(PREnablingAutomergeEvent{PRNumber: opts.PRNumber, Method: opts.MergeMethod})
	if err := api.enableAutoMerge(ctx, runner, prNodeID, opts.MergeMethod); err != nil {
		return handleAutoMergeError(ctx, handler, runner, api, opts, prNodeID, err)
	}
	handler.OnProgress(PRAutomergeEnabledEvent{PRNumber: opts.PRNumber})

	// Step 2a: If --wait, wait for merge to complete
	if opts.Wait {
		handler.OnProgress(PRWaitingForMergeEvent{PRNumber: opts.PRNumber})
		if err := api.waitForPRMerge(ctx, runner, prNodeID, DefaultMergeTimeout, DefaultMergePollInterval); err != nil {
			return 0, fmt.Errorf("failed waiting for merge: %w", err)
		}
		handler.OnProgress(PRMergedEvent{PRNumber: opts.PRNumber})
		return MergePRMerged, nil
	}

	// Fire-and-forget
	return MergePRAutomergeEnabled, nil
}

// handleAutoMergeError implements step 3 of the merge strategy: handling EnableAutoMerge failures.
func handleAutoMergeError(ctx context.Context, handler ProgressHandler, runner github.GitCommandRunner, api prMergeAPI, opts MergePROptions, prNodeID string, autoMergeErr error) (MergePROutcome, error) {
	// "clean status" error → PR became ready between our check and the automerge call (race condition)
	if errors.Is(autoMergeErr, github.ErrPRCleanStatus) {
		handler.OnProgress(PRCleanStatusRaceEvent{PRNumber: opts.PRNumber})
		return mergeDirectly(ctx, handler, api, opts)
	}

	// "not enabled on repo" → fall back to polling + direct merge if --wait
	if errors.Is(autoMergeErr, github.ErrAutoMergeNotEnabled) {
		if !opts.Wait {
			return 0, fmt.Errorf("auto-merge is not enabled for this repository. Use --wait to poll and merge directly, or enable auto-merge in repository settings")
		}

		handler.OnProgress(PRWaitingForMergeableEvent{PRNumber: opts.PRNumber})
		state, err := api.waitForMergeable(ctx, runner, prNodeID, DefaultMergeTimeout, DefaultMergePollInterval)
		if errors.Is(err, github.ErrPRAlreadyMerged) {
			handler.OnProgress(PRMergedEvent{PRNumber: opts.PRNumber, External: true})
			return MergePRMerged, nil
		}
		if err != nil {
			return 0, fmt.Errorf("failed waiting for PR #%d to become mergeable: %w", opts.PRNumber, err)
		}

		handler.OnProgress(PRMergingDirectlyEvent{PRNumber: opts.PRNumber, Method: opts.MergeMethod, MergeState: state.MergeStateText, AfterWaiting: true})
		return mergeDirectly(ctx, handler, api, opts)
	}

	// Other automerge error — pass through
	return 0, fmt.Errorf("failed to enable automerge on PR #%d: %w", opts.PRNumber, autoMergeErr)
}

// mergeDirectly merges the PR through the REST API and reports success.
func mergeDirectly(ctx context.Context, handler ProgressHandler, api prMergeAPI, opts MergePROptions) (MergePROutcome, error) {
	if err := api.mergePR(ctx, opts.BranchName, opts.MergeMethod); err != nil {
		return 0, fmt.Errorf("failed to merge PR #%d: %w", opts.PRNumber, err)
	}
	handler.OnProgress(PRMergedEvent{PRNumber: opts.PRNumber})
	return MergePRMerged, nil
}

// isReadyToMerge returns true if the PR's mergeStateStatus indicates it can be merged immediately.
func isReadyToMerge(mergeStateText string) bool {
	switch mergeStateText {
	case mergeStateClean, mergeStateHasHooks:
		return true
	default:
		return false
	}
}

// formatUnmergeableError produces a user-friendly error for a PR that is not mergeable.
func formatUnmergeableError(prNumber git.PRNumber, state *github.PRMergeableState) error {
	if state.MergeStateText != "" {
		return fmt.Errorf("PR #%d is not mergeable (%s). Please resolve conflicts and try again", prNumber, state.MergeStateText)
	}
	return fmt.Errorf("PR #%d is not mergeable. Please resolve conflicts and try again", prNumber)
}

// getMergeableStateWithRetry polls for PR mergeable state, retrying when GitHub reports UNKNOWN.
func getMergeableStateWithRetry(ctx context.Context, runner github.GitCommandRunner, prNodeID string) (*github.PRMergeableState, error) {
	const (
		maxAttempts = 5
		retryDelay  = 2 * time.Second
	)

	var lastState *github.PRMergeableState
	for attempt := range maxAttempts {
		state, err := github.GetPRMergeableState(ctx, runner, prNodeID)
		if err != nil {
			return nil, err
		}
		lastState = state
		if !strings.EqualFold(state.MergeStateText, "UNKNOWN") {
			return state, nil
		}
		if attempt < maxAttempts-1 {
			if err := utils.SleepContext(ctx, retryDelay); err != nil {
				return nil, err
			}
		}
	}

	return lastState, nil
}
