package actions

import (
	"context"
)

// MetadataPushEngine defines the engine capabilities needed for pushing metadata.
type MetadataPushEngine interface {
	BatchSetLastModifiedBy(ctx context.Context, branchNames []string) error
	PrepareRemoteMetadataPush(ctx context.Context) error
	PushMetadataForBranches(ctx context.Context, branchNames []string) error
}

// DebugLogger receives diagnostics for best-effort steps that do not fail the
// operation. output.Output satisfies it.
type DebugLogger interface {
	Debug(format string, args ...any)
}

// PushMetadataOnly pushes metadata refs without updating GitHub PRs.
// Use this when you want to push metadata changes but defer PR updates to sync.
func PushMetadataOnly(ctx context.Context, out DebugLogger, eng MetadataPushEngine, branchNames []string) error {
	// Update LastModifiedBy for all branches in one metadata commit
	if err := eng.BatchSetLastModifiedBy(ctx, branchNames); err != nil {
		out.Debug("Failed to update metadata: %v", err)
	}

	if err := eng.PrepareRemoteMetadataPush(ctx); err != nil {
		out.Debug("Remote metadata sync not supported: %v", err)
		return nil // Non-fatal
	}

	// Push metadata refs
	if err := eng.PushMetadataForBranches(ctx, branchNames); err != nil {
		out.Debug("Failed to push metadata refs: %v", err)
		return err
	}

	return nil
}
