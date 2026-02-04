package sync

import (
	"stackit.dev/stackit/internal/app"
)

// StackMetadataGCResult contains the results of stack metadata garbage collection.
type StackMetadataGCResult struct {
	DeletedStackIDs []string // Stack IDs whose refs were deleted
	Errors          []string // Any errors encountered (non-fatal)
}

// gcOrphanedStackMetadata removes stack metadata refs that have no associated branches.
// This is called during sync after worktree cleanup to clean up refs for stacks
// whose branches have all been deleted or merged.
// This function is best-effort and will not fail sync on errors.
func gcOrphanedStackMetadata(ctx *app.Context) *StackMetadataGCResult {
	result := &StackMetadataGCResult{
		DeletedStackIDs: []string{},
		Errors:          []string{},
	}

	// 1. Get all local stack metadata refs (returns map[stackID]sha)
	allStackRefs, err := ctx.Git().ListStackMetas()
	if err != nil {
		ctx.Logger.Debug("failed to list stack metadata refs", "error", err)
		return result
	}

	if len(allStackRefs) == 0 {
		return result
	}

	// 2. Get active stack IDs from tracked branches
	activeIDs := make(map[string]bool)
	for _, branch := range ctx.Engine.AllBranches() {
		stackID := ctx.Engine.GetStackID(branch)
		if stackID != "" {
			activeIDs[stackID] = true
		}
	}

	// 3. Find orphaned refs (in allStackRefs but not activeIDs)
	var orphaned []string
	for stackID := range allStackRefs {
		if !activeIDs[stackID] {
			orphaned = append(orphaned, stackID)
		}
	}

	if len(orphaned) == 0 {
		return result
	}

	ctx.Logger.Info("found orphaned stack metadata refs", "count", len(orphaned))

	// 4. Delete local refs
	for _, stackID := range orphaned {
		if err := ctx.Git().DeleteStackMeta(stackID); err != nil {
			result.Errors = append(result.Errors, "failed to delete local stack ref "+stackID+": "+err.Error())
			ctx.Logger.Debug("failed to delete local stack ref", "stackID", stackID, "error", err)
		} else {
			result.DeletedStackIDs = append(result.DeletedStackIDs, stackID)
		}
	}

	// 5. Delete remote refs (best-effort, non-fatal)
	if len(result.DeletedStackIDs) > 0 {
		if err := ctx.Git().DeleteRemoteStackMetaRefs(ctx.Context, result.DeletedStackIDs); err != nil {
			// This is expected to fail if refs don't exist on remote, so just log it
			ctx.Logger.Debug("failed to delete remote stack refs (may not exist)", "error", err)
		}
	}

	return result
}
