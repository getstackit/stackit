package sync

import (
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/git"
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
	allStackRefs, err := ctx.Engine.ListStackMetadata()
	if err != nil {
		ctx.Logger.Debug("failed to list stack metadata refs error=%v", err)
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

	ctx.Logger.Info("found orphaned stack metadata refs count=%v", len(orphaned))

	// 4. Delete local refs in a single update-ref --stdin batch. The previous
	// per-orphan DeleteStackMeta loop spawned 2 git subprocesses per ref
	// (update-ref -d + show-ref --verify) — same N+1 pattern fixed for branch
	// deletion. If the atomic batch fails, fall back to per-ref so partial
	// progress is still reported.
	refs := make([]string, 0, len(orphaned))
	for _, stackID := range orphaned {
		refs = append(refs, git.StackMetaRefName(stackID))
	}
	if err := ctx.Git().DeleteRefsBatch(ctx.Context, refs); err != nil {
		ctx.Logger.Debug("batch delete of local stack refs failed, falling back per-ref error=%v", err)
		for _, stackID := range orphaned {
			if perRefErr := ctx.Engine.DeleteStackMetadata(ctx.Context, stackID); perRefErr != nil {
				result.Errors = append(result.Errors, "failed to delete local stack ref "+stackID+": "+perRefErr.Error())
				ctx.Logger.Debug("failed to delete local stack ref stackID=%v error=%v", stackID, perRefErr)
			} else {
				result.DeletedStackIDs = append(result.DeletedStackIDs, stackID)
			}
		}
	} else {
		result.DeletedStackIDs = append(result.DeletedStackIDs, orphaned...)
	}

	// 5. Delete remote refs (best-effort, non-fatal)
	if len(result.DeletedStackIDs) > 0 {
		if err := ctx.Engine.DeleteRemoteStackMetadata(ctx.Context, result.DeletedStackIDs); err != nil {
			// This is expected to fail if refs don't exist on remote, so just log it
			ctx.Logger.Debug("failed to delete remote stack refs (may not exist) error=%v", err)
		}
	}

	return result
}
