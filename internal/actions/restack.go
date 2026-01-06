package actions

import (
	"fmt"

	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/handlers"
)

// RestackOptions contains options for the restack command
type RestackOptions struct {
	BranchName string
	Scope      engine.StackRange
}

// RestackAction performs the restack operation
func RestackAction(ctx *app.Context, opts RestackOptions, handler handlers.RestackHandler) error {
	eng := ctx.Engine
	out := ctx.Output

	// Get branches to restack based on scope
	branch := eng.GetBranch(opts.BranchName)
	graph := engine.BuildStackGraph(eng, engine.SortStrategyAlphabetical, nil)
	branches := graph.Range(branch, opts.Scope)

	if len(branches) == 0 {
		out.Info("No branches to restack.")
		return nil
	}

	ctx.Logger.Info("restack started", "branchCount", len(branches))

	// Take snapshot before modifying the repository
	snapshotOpts := NewSnapshot("restack",
		WithArg(opts.BranchName),
	)
	if err := eng.TakeSnapshot(snapshotOpts); err != nil {
		// Log but don't fail - snapshot is best effort
		out.Debug("Failed to take snapshot: %v", err)
	}

	// If no handler provided, use NullRestackHandler (silent)
	if handler == nil {
		handler = &handlers.NullRestackHandler{}
	}

	// For standalone restack, we need to sort branches topologically for correct restack order
	sortedBranches := eng.SortBranchesTopologically(branches)

	// Use RestackHandler for consistent output
	handler.OnRestackStart(len(sortedBranches))

	var restacked, skipped int
	var conflicts []string

	if err := RestackBranchesWithHandler(ctx, sortedBranches, func(branchName string, result engine.RestackResult, newRev string, _ bool, lockReason engine.LockReason, frozen bool, isCurrent bool, reparented bool, oldParent, newParent string) {
		res := handlers.RestackDone
		switch result {
		case engine.RestackDone:
			restacked++
			res = handlers.RestackDone
		case engine.RestackUnneeded:
			res = handlers.RestackUnneeded
		case engine.RestackConflict:
			skipped++
			conflicts = append(conflicts, branchName)
			res = handlers.RestackConflict
		}

		// Determine parent name for consistent output
		parentName := ""
		br := eng.GetBranch(branchName)
		if br.GetName() != "" {
			if p := br.GetParent(); p != nil {
				parentName = p.GetName()
			} else {
				parentName = eng.Trunk().GetName()
			}
		}

		// PR number is not always available without extra fetching, but we can try
		var prNumber *int
		if br.GetName() != "" {
			if pr, err := eng.GetPrInfo(br); err == nil && pr != nil {
				num := pr.Number()
				prNumber = num
			}
		}

		handler.OnRestackBranch(branchName, res, newRev, prNumber, lockReason, frozen, isCurrent, parentName, reparented, oldParent, newParent)
	}); err != nil {
		handler.OnRestackComplete(restacked, skipped, conflicts)
		return fmt.Errorf("restack failed: %w", err)
	}

	ctx.Logger.Info("restack completed", "restacked", restacked, "skipped", skipped, "conflicts", len(conflicts))

	handler.OnRestackComplete(restacked, skipped, conflicts)
	return nil
}
