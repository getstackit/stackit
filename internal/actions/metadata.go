package actions

import (
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
)

// PushMetadataOnly pushes metadata refs without updating GitHub PRs.
// Use this when you want to push metadata changes but defer PR updates to sync.
func PushMetadataOnly(ctx *app.Context, eng engine.Engine, branchNames []string) error {
	out := ctx.Output

	// Update LastModifiedBy for all branches (parallel with config caching)
	if err := eng.BatchSetLastModifiedBy(branchNames); err != nil {
		out.Debug("Failed to update metadata: %v", err)
	}

	// Check if remote sync is enabled; if not, run compatibility test first
	if !eng.IsRemoteSyncEnabled() {
		if err := eng.Git().TestRemoteRefCompatibility(); err != nil {
			out.Debug("Remote metadata sync not supported: %v", err)
			return nil // Non-fatal
		}
		eng.SetRemoteSyncEnabled(true)
		// Configure refspec so future git fetch commands also fetch metadata
		if err := eng.Git().EnsureMetadataRefspecConfigured(); err != nil {
			out.Debug("Failed to configure metadata refspec: %v", err)
		}
	}

	// Push metadata refs
	if err := eng.Git().PushMetadataRefs(ctx.Context, branchNames); err != nil {
		out.Debug("Failed to push metadata refs: %v", err)
		return err
	}

	return nil
}
