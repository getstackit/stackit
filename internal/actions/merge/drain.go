package merge

import (
	"errors"
	"fmt"

	"github.com/getstackit/stackit/internal/actions/submit"
	"github.com/getstackit/stackit/internal/actions/sync"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
)

// DrainOptions configures Drain.
type DrainOptions struct {
	// Plan is the bottom-up plan the caller showed and confirmed. Its
	// BranchesToMerge are locked for the duration of the drain.
	Plan *Plan
	// TargetBranch and Scope select the stack to re-plan on each iteration.
	TargetBranch string
	Scope        string
	Force        bool
	// Limit caps the number of PRs to merge (0 = all).
	Limit int
	// MergeMethod is the resolved merge method used for every PR.
	MergeMethod github.MergeMethod
}

// DrainResult reports what Drain did.
type DrainResult struct {
	Merged int
}

// Drain merges the stack's PRs bottom-up, one at a time, waiting for each to
// land before moving on. After every merge it checks out trunk, syncs with a
// restack scoped to the remaining branches, and publishes those branches so
// the next PR sees its new base.
//
// A restack conflict in a drain branch is a hard error; drain never enters the
// conflict workflow (see PostMergeSync). Branches are locked with
// LockReasonDraining for the duration and unlocked on return.
func Drain(ctx *app.Context, opts DrainOptions, handler ProgressHandler) (DrainResult, error) {
	eng := ctx.Engine
	result := DrainResult{}
	if opts.Plan == nil || len(opts.Plan.BranchesToMerge) == 0 {
		return result, nil
	}

	displayTotal := len(opts.Plan.BranchesToMerge)
	if opts.Limit > 0 {
		displayTotal = opts.Limit
	}

	// Lock all drain branches to prevent external modification
	branchesToLock := make([]engine.Branch, len(opts.Plan.BranchesToMerge))
	for i, b := range opts.Plan.BranchesToMerge {
		branchesToLock[i] = eng.GetBranch(b.BranchName)
	}
	if _, err := eng.SetLocked(ctx.Context, branchesToLock, engine.LockReasonDraining); err != nil {
		return result, fmt.Errorf("failed to lock drain branches: %w", err)
	}
	defer unlockDrainBranches(ctx, opts.Plan.BranchesToMerge)

	for opts.Limit == 0 || result.Merged < opts.Limit {
		// Re-read state each iteration (branches change after merges + sync)
		plan, _, err := CreateMergePlan(ctx.Context, eng, ctx.Output, ctx.GitHub(), CreatePlanOptions{
			Strategy:     StrategyBottomUp,
			Force:        opts.Force,
			TargetBranch: opts.TargetBranch,
			Scope:        opts.Scope,
		})
		if err != nil {
			// After merging some PRs, "not on a branch" or "on trunk" can happen
			// if post-merge sync moved us. This is expected when stack is fully drained.
			if result.Merged > 0 {
				handler.OnProgress(DrainStoppedEvent{Merged: result.Merged, Reason: err})
				break
			}
			return result, err
		}

		if len(plan.BranchesToMerge) == 0 {
			break
		}

		bottomPR := plan.BranchesToMerge[0]
		handler.OnProgress(DrainPRStartedEvent{
			BranchName: bottomPR.BranchName,
			PRNumber:   bottomPR.PRNumber,
			Position:   result.Merged + 1,
			Total:      displayTotal,
		})

		// Drain always waits for each PR to merge before proceeding.
		if _, err := MergePR(ctx, MergePROptions{
			BranchName:  bottomPR.BranchName,
			PRNumber:    bottomPR.PRNumber,
			MergeMethod: opts.MergeMethod,
			Wait:        true,
		}, handler); err != nil {
			return result, err
		}

		result.Merged++

		if err := syncAfterDrainMerge(ctx, bottomPR.PRNumber, plan.BranchesToMerge[1:], handler); err != nil {
			return result, err
		}
	}

	return result, nil
}

// syncAfterDrainMerge checks out trunk, restacks the remaining drain branches,
// and publishes them.
func syncAfterDrainMerge(ctx *app.Context, mergedPR git.PRNumber, remaining []BranchMergeInfo, handler ProgressHandler) error {
	handler.OnProgress(DrainSyncingEvent{PRNumber: mergedPR})

	// Everything after the PR we just merged
	restackScope := remainingBranchNames(remaining)

	syncResult, err := PostMergeSync(ctx, PostMergeSyncOptions{RestackScope: restackScope}, func() sync.Handler {
		return &sync.NullHandler{}
	})
	if err != nil {
		var checkoutErr *PostMergeCheckoutError
		if errors.As(err, &checkoutErr) {
			return fmt.Errorf("post-merge checkout trunk failed after PR #%d: %w", mergedPR, checkoutErr.Err)
		}
		return fmt.Errorf("post-merge sync failed after PR #%d: %w", mergedPR, err)
	}

	// Check for conflicts in drain branches — hard error
	if len(syncResult.Summary.ConflictBranches) > 0 {
		return fmt.Errorf("restack conflict in %s — resolve before continuing drain", syncResult.Summary.ConflictBranches[0])
	}

	// Publish what the restack just changed. The restack rewrote each
	// remaining branch onto the new trunk and reparented the next one off
	// the branch that just merged, and both of those live only locally
	// until pushed. Leaving them behind strands the drain:
	//
	//   - The metadata ref still names the merged (now deleted) parent, so a
	//     stack-order CI check reads a stale parent and reports the next PR
	//     as "not at the bottom of the stack". That leaves the PR in an
	//     unstable state and automerge refuses it, so the drain stalls on
	//     its second PR and every one after.
	//   - The remote branch still points at the pre-restack commits, so the
	//     PR shows a diff against the old base — including the changes that
	//     just merged.
	//
	// GitHub retargets the PR base itself when the base branch is deleted,
	// which makes the PR *look* correct while both of the above are still
	// wrong.
	if len(restackScope) > 0 {
		if err := pushDrainedBranches(ctx, restackScope, handler); err != nil {
			return fmt.Errorf("post-merge push failed after PR #%d: %w", mergedPR, err)
		}
	}
	return nil
}

// pushDrainedBranches publishes the branches the post-merge restack rewrote, so
// the remote matches what drain just did locally.
//
// Goes through submit rather than pushing refs directly: submit already pushes
// branch heads with the right force-with-lease, pushes metadata refs, and
// retargets each PR's base. UpdateOnly keeps it to branches that already have a
// PR — a drain must never open one — and NoEdit keeps it silent about
// titles and descriptions, which are not drain's business to change.
func pushDrainedBranches(ctx *app.Context, branchNames []string, handler ProgressHandler) error {
	return submit.Action(ctx, submit.Options{
		// branchNames[0] is the next PR to merge and needs pushing itself, so
		// the range has to include it as well as its descendants.
		Branch:     branchNames[0],
		StackRange: engine.StackRangeUpstack(engine.IncludeCurrentBranch),
		UpdateOnly: true,
		NoEdit:     true,
	}, &drainSubmitHandler{progress: handler})
}

// drainSubmitHandler forwards per-branch results of drain's nested submit as
// progress events, without prompting.
type drainSubmitHandler struct {
	progress ProgressHandler
}

func (h *drainSubmitHandler) OnEvent(e submit.Event) {
	ev, ok := e.(submit.BranchProgressEvent)
	if !ok {
		return
	}
	switch ev.Status {
	case submit.StatusDone:
		h.progress.OnProgress(DrainBranchPushedEvent{BranchName: ev.BranchName, URL: ev.URL})
	case submit.StatusError:
		h.progress.OnProgress(DrainBranchPushedEvent{BranchName: ev.BranchName, Failed: true, Err: ev.Error})
	}
}

func (h *drainSubmitHandler) Confirm(_ string, defaultYes bool) (bool, error) {
	return defaultYes, nil
}

func (h *drainSubmitHandler) IsInteractive() bool { return false }

// remainingBranchNames extracts branch names from a slice of BranchMergeInfo.
func remainingBranchNames(branches []BranchMergeInfo) []string {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.BranchName
	}
	return names
}

// unlockDrainBranches unlocks any remaining drain-locked branches that still exist.
func unlockDrainBranches(ctx *app.Context, branches []BranchMergeInfo) {
	var toUnlock []engine.Branch
	for _, b := range branches {
		branch := ctx.Engine.GetBranch(b.BranchName)
		if branch.IsTracked() && branch.GetLockReason() == engine.LockReasonDraining {
			toUnlock = append(toUnlock, branch)
		}
	}
	if len(toUnlock) > 0 {
		_, _ = ctx.Engine.SetLocked(ctx.Context, toUnlock, engine.LockReasonNone)
	}
}
