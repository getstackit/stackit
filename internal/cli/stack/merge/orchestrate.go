// Package merge provides CLI commands for merging stacked PRs.
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
	"github.com/getstackit/stackit/internal/output"
)

const (
	// DefaultMergeTimeout is the default timeout for waiting on a merge to complete
	DefaultMergeTimeout = 30 * time.Minute
	// DefaultMergePollInterval is the default interval between merge status checks
	DefaultMergePollInterval = 10 * time.Second
)

// Merge-state and merge strategy string constants shared across merge subcommands.
const (
	mergeStateClean    = "CLEAN"
	mergeStateHasHooks = "HAS_HOOKS"

	mergeStrategyShip     = "ship"
	mergeStrategyBottomUp = "bottom-up"
	mergeStrategyDone     = "done"
)

// Outcome represents the result of a merge orchestration attempt.
type Outcome int

const (
	// OutcomeMerged means the PR was merged (directly or via wait).
	OutcomeMerged Outcome = iota
	// OutcomeAutomergeEnabled means automerge was enabled (fire-and-forget).
	OutcomeAutomergeEnabled
)

type orchestrateMergeOptions struct {
	branchName  string
	prNumber    int
	prNodeID    string
	mergeMethod github.MergeMethod
	wait        bool
}

// prMergeAPI abstracts the external GitHub operations used by orchestrateMerge,
// enabling unit testing without real GitHub calls.
type prMergeAPI interface {
	getMergeableState(ctx context.Context, runner git.Runner, prNodeID string) (*github.PRMergeableState, error)
	enableAutoMerge(ctx context.Context, runner git.Runner, prNodeID string, method github.MergeMethod) error
	waitForPRMerge(ctx context.Context, runner git.Runner, prNodeID string, timeout, interval time.Duration) error
	waitForMergeable(ctx context.Context, runner git.Runner, prNodeID string, timeout, interval time.Duration) (*github.PRMergeableState, error)
	mergePR(ctx context.Context, branchName string, method github.MergeMethod) error
}

// defaultPRMergeAPI delegates to the real GitHub functions and client.
type defaultPRMergeAPI struct {
	client github.Client
}

func (d *defaultPRMergeAPI) getMergeableState(ctx context.Context, runner git.Runner, prNodeID string) (*github.PRMergeableState, error) {
	return getMergeableStateWithRetry(ctx, runner, prNodeID)
}

func (d *defaultPRMergeAPI) enableAutoMerge(ctx context.Context, runner git.Runner, prNodeID string, method github.MergeMethod) error {
	return github.EnableAutoMerge(ctx, runner, prNodeID, github.EnableAutoMergeOptions{MergeMethod: method})
}

func (d *defaultPRMergeAPI) waitForPRMerge(ctx context.Context, runner git.Runner, prNodeID string, timeout, interval time.Duration) error {
	return github.WaitForPRMerge(ctx, runner, prNodeID, timeout, interval)
}

func (d *defaultPRMergeAPI) waitForMergeable(ctx context.Context, runner git.Runner, prNodeID string, timeout, interval time.Duration) (*github.PRMergeableState, error) {
	return github.WaitForMergeable(ctx, runner, prNodeID, timeout, interval)
}

func (d *defaultPRMergeAPI) mergePR(ctx context.Context, branchName string, method github.MergeMethod) error {
	return d.client.MergePullRequest(ctx, branchName, github.MergePROptions{Method: method})
}

// orchestrateMerge implements a 3-tier merge strategy:
//  1. If the PR is already ready (CLEAN/HAS_HOOKS), merge directly via REST API.
//  2. Otherwise, try EnableAutoMerge. On success, optionally wait for merge.
//  3. If EnableAutoMerge fails:
//     - "clean status" error → direct merge (race: became ready between check and automerge).
//     - "not enabled on repo" + wait → poll until mergeable, then merge directly.
//     - "not enabled on repo" + no wait → error with --wait suggestion.
func orchestrateMerge(ctx *app.Context, opts orchestrateMergeOptions) (Outcome, error) {
	api := &defaultPRMergeAPI{client: ctx.GitHub()}
	return doOrchestrateMerge(ctx.Context, ctx.Output, ctx.Engine.Git(), api, opts) //nolint:forbidigo // GitHub integration needs the git runner to run gh; not a domain bypass
}

// doOrchestrateMerge contains the core merge orchestration logic, accepting a prMergeAPI
// for testability.
func doOrchestrateMerge(ctx context.Context, out output.Output, runner git.Runner, api prMergeAPI, opts orchestrateMergeOptions) (Outcome, error) {
	// Step 1: Check current merge state
	mergeableState, err := api.getMergeableState(ctx, runner, opts.prNodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to check PR mergeable state: %w", err)
	}
	if mergeableState.State == git.PRStateMerged {
		out.Success("PR #%d is already merged", opts.prNumber)
		return OutcomeMerged, nil
	}
	if mergeableState.State != git.PRStateOpen {
		return 0, fmt.Errorf("PR #%d is %s (not open)", opts.prNumber, mergeableState.State)
	}
	if !mergeableState.Mergeable {
		if strings.EqualFold(mergeableState.MergeStateText, "UNKNOWN") {
			return 0, fmt.Errorf("PR #%d mergeability is still being calculated by GitHub after 5 retries. Try again shortly", opts.prNumber)
		}
		return 0, formatUnmergeableError(opts.prNumber, mergeableState)
	}

	// If already CLEAN or HAS_HOOKS, merge directly
	if isReadyToMerge(mergeableState.MergeStateText) {
		out.Info("PR #%d is ready to merge — merging directly (method: %s)...", opts.prNumber, opts.mergeMethod)
		if err := api.mergePR(ctx, opts.branchName, opts.mergeMethod); err != nil {
			return 0, fmt.Errorf("failed to merge PR #%d: %w", opts.prNumber, err)
		}
		out.Success("PR #%d merged successfully!", opts.prNumber)
		return OutcomeMerged, nil
	}

	// Step 2: Not immediately ready — try automerge
	out.Info("Enabling automerge on PR #%d (method: %s)...", opts.prNumber, opts.mergeMethod)
	if err := api.enableAutoMerge(ctx, runner, opts.prNodeID, opts.mergeMethod); err != nil {
		return handleAutoMergeError(ctx, out, runner, api, opts, err)
	}
	out.Success("Automerge enabled on PR #%d", opts.prNumber)

	// Step 2a: If --wait, wait for merge to complete
	if opts.wait {
		out.Info("Waiting for PR #%d to be merged...", opts.prNumber)
		if err := api.waitForPRMerge(ctx, runner, opts.prNodeID, DefaultMergeTimeout, DefaultMergePollInterval); err != nil {
			return 0, fmt.Errorf("failed waiting for merge: %w", err)
		}
		out.Success("PR #%d merged successfully!", opts.prNumber)
		return OutcomeMerged, nil
	}

	// Fire-and-forget
	return OutcomeAutomergeEnabled, nil
}

// handleAutoMergeError implements step 3 of the merge strategy: handling EnableAutoMerge failures.
func handleAutoMergeError(ctx context.Context, out output.Output, runner git.Runner, api prMergeAPI, opts orchestrateMergeOptions, autoMergeErr error) (Outcome, error) {
	// "clean status" error → PR became ready between our check and the automerge call (race condition)
	if errors.Is(autoMergeErr, github.ErrPRCleanStatus) {
		out.Debug("Automerge returned clean status — PR became ready, merging directly")
		if err := api.mergePR(ctx, opts.branchName, opts.mergeMethod); err != nil {
			return 0, fmt.Errorf("failed to merge PR #%d: %w", opts.prNumber, err)
		}
		out.Success("PR #%d merged successfully!", opts.prNumber)
		return OutcomeMerged, nil
	}

	// "not enabled on repo" → fall back to polling + direct merge if --wait
	if errors.Is(autoMergeErr, github.ErrAutoMergeNotEnabled) {
		if opts.wait {
			out.Info("Auto-merge not enabled on this repo — waiting for PR #%d to become mergeable...", opts.prNumber)
			state, err := api.waitForMergeable(ctx, runner, opts.prNodeID, DefaultMergeTimeout, DefaultMergePollInterval)
			if errors.Is(err, github.ErrPRAlreadyMerged) {
				out.Success("PR #%d was merged externally!", opts.prNumber)
				return OutcomeMerged, nil
			}
			if err != nil {
				return 0, fmt.Errorf("failed waiting for PR #%d to become mergeable: %w", opts.prNumber, err)
			}

			out.Info("PR #%d is now %s — merging directly (method: %s)...", opts.prNumber, state.MergeStateText, opts.mergeMethod)
			if err := api.mergePR(ctx, opts.branchName, opts.mergeMethod); err != nil {
				return 0, fmt.Errorf("failed to merge PR #%d: %w", opts.prNumber, err)
			}
			out.Success("PR #%d merged successfully!", opts.prNumber)
			return OutcomeMerged, nil
		}

		return 0, fmt.Errorf("auto-merge is not enabled for this repository. Use --wait to poll and merge directly, or enable auto-merge in repository settings")
	}

	// Other automerge error — pass through
	return 0, fmt.Errorf("failed to enable automerge on PR #%d: %w", opts.prNumber, autoMergeErr)
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
func formatUnmergeableError(prNumber int, state *github.PRMergeableState) error {
	if state.MergeStateText != "" {
		return fmt.Errorf("PR #%d is not mergeable (%s). Please resolve conflicts and try again", prNumber, state.MergeStateText)
	}
	return fmt.Errorf("PR #%d is not mergeable. Please resolve conflicts and try again", prNumber)
}

// getMergeableStateWithRetry polls for PR mergeable state, retrying when GitHub reports UNKNOWN.
func getMergeableStateWithRetry(ctx context.Context, runner git.Runner, prNodeID string) (*github.PRMergeableState, error) {
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
			time.Sleep(retryDelay)
		}
	}

	return lastState, nil
}
