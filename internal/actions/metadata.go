package actions

import (
	"context"

	"github.com/getstackit/stackit/internal/app"
)

// MetadataPushEngine defines the engine capabilities needed for pushing metadata.
type MetadataPushEngine interface {
	BatchSetLastModifiedBy(branchNames []string) error
	PrepareRemoteMetadataPush(ctx context.Context) error
	PushMetadataForBranches(ctx context.Context, branchNames []string) error
}

// PushMetadataOnly pushes metadata refs without updating GitHub PRs.
// Use this when you want to push metadata changes but defer PR updates to sync.
func PushMetadataOnly(ctx *app.Context, eng MetadataPushEngine, branchNames []string) error {
	out := ctx.Output

	// Update LastModifiedBy for all branches (parallel with config caching)
	if err := eng.BatchSetLastModifiedBy(branchNames); err != nil {
		out.Debug("Failed to update metadata: %v", err)
	}

	if err := eng.PrepareRemoteMetadataPush(ctx.Context); err != nil {
		out.Debug("Remote metadata sync not supported: %v", err)
		return nil // Non-fatal
	}

	// Push metadata refs
	if err := eng.PushMetadataForBranches(ctx.Context, branchNames); err != nil {
		out.Debug("Failed to push metadata refs: %v", err)
		return err
	}

	return nil
}
