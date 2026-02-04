package sync

import (
	"fmt"
	"time"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/errors"
	"stackit.dev/stackit/internal/tui/style"
)

// syncRemoteMetadata fetches and processes remote metadata.
// Deprecated: Use FetchRemoteMetadata in parallel + processRemoteMetadata
func syncRemoteMetadata(ctx *app.Context, opts *Options, handler Handler) error {
	eng := ctx.RemoteMetadata()
	out := ctx.Output

	// Fetch remote metadata refs
	fetchStart := time.Now()
	if err := eng.FetchRemoteMetadata(ctx.Context); err != nil {
		// Non-fatal: remote may not have metadata yet
		out.Debug("No remote metadata to fetch: %v", err)
	}
	ctx.Logger.Info("fetch remote metadata completed durationMs=%d", time.Since(fetchStart).Milliseconds())

	return processRemoteMetadata(ctx, opts, handler)
}

// processRemoteMetadata processes remote metadata after fetch completes
// This is designed to run after the network fetch operation completes in parallel
func processRemoteMetadata(ctx *app.Context, opts *Options, handler Handler) error {
	eng := ctx.RemoteMetadata()
	out := ctx.Output

	// Configure refspec so future git fetch commands also fetch metadata
	configStart := time.Now()
	if err := eng.ConfigureRemoteMetadataSync(ctx.Context); err != nil {
		out.Debug("Failed to configure metadata refspec: %v", err)
	}
	// Also configure stack metadata refspec
	if err := ctx.Git().EnsureStackMetaRefspecConfigured(); err != nil {
		out.Debug("Failed to configure stack metadata refspec: %v", err)
	}
	ctx.Logger.Info("configure remote metadata sync completed durationMs=%d", time.Since(configStart).Milliseconds())

	// Load remote metadata into cache
	loadCacheStart := time.Now()
	if err := eng.LoadRemoteMetadataCache(); err != nil {
		out.Debug("Failed to load remote metadata cache: %v", err)
	}
	ctx.Logger.Info("load remote metadata cache completed durationMs=%d", time.Since(loadCacheStart).Milliseconds())

	// Handle orphaned local metadata (dual-checkout scenario or manual branch deletion)
	orphanedStart := time.Now()
	if err := handleOrphanedMetadata(ctx, opts, handler); err != nil {
		return err
	}
	ctx.Logger.Info("handle orphaned metadata completed durationMs=%d", time.Since(orphanedStart).Milliseconds())

	// Compute diffs
	diffsStart := time.Now()
	diffs, err := eng.ComputeAllMetadataDiffs()
	ctx.Logger.Info("compute all metadata diffs completed durationMs=%d diffCount=%d", time.Since(diffsStart).Milliseconds(), len(diffs))
	if err != nil {
		return fmt.Errorf("failed to compute metadata diffs: %w", err)
	}

	if len(diffs) == 0 {
		return nil // No conflicts
	}

	// Handle --dry-run flag
	if opts.DryRun {
		printMetadataDiffs(diffs, out)
		return nil
	}

	// Resolve each conflicting branch via handler
	for _, diff := range diffs {
		if err := resolveMetadataConflict(ctx, diff, handler); err != nil {
			return err
		}
	}

	return nil
}

// handleOrphanedMetadata handles branches where remote metadata was deleted but local exists
func handleOrphanedMetadata(ctx *app.Context, opts *Options, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output

	orphaned, err := eng.FindOrphanedLocalMetadata()
	if err != nil {
		out.Debug("Failed to find orphaned metadata: %v", err)
		return nil
	}

	if len(orphaned) == 0 {
		return nil
	}

	// Handle --dry-run flag
	if opts.DryRun {
		out.Info("\n=== Orphaned metadata (dry run) ===")
		for _, info := range orphaned {
			switch {
			case !info.ExistsLocally:
				out.Info("  %s: local branch gone, would delete metadata", style.ColorBranchName(info.BranchName, false))
			case info.HasLocalChanges:
				out.Info("  %s: has local changes, would prompt", style.ColorBranchName(info.BranchName, false))
			default:
				out.Info("  %s: no local changes, would delete sync state", style.ColorBranchName(info.BranchName, false))
			}
		}
		return nil
	}

	for _, info := range orphaned {
		if info.Action == engine.OrphanedActionDelete {
			// No local changes - silently remove sync state or delete ref if branch is gone
			if !info.ExistsLocally {
				if err := eng.DeleteMetadata(ctx.Context, info.BranchName); err != nil {
					out.Debug("Failed to delete orphaned metadata ref for %s: %v", info.BranchName, err)
				}
			} else if err := eng.DeleteLocalMetadataHash(info.BranchName); err != nil {
				out.Debug("Failed to delete metadata hash for %s: %v", info.BranchName, err)
			}
		} else {
			// Has local changes - prompt user via handler
			if err := resolveOrphanedMetadata(ctx, info, handler); err != nil {
				return err
			}
		}
	}

	return nil
}

// resolveOrphanedMetadata resolves orphaned metadata by prompting via handler
func resolveOrphanedMetadata(ctx *app.Context, info engine.OrphanedMetadataInfo, handler Handler) error {
	eng := ctx.Engine
	out := ctx.Output

	pushLocal, err := handler.PromptOrphanedMetadata(info)
	if err != nil {
		// Handle user cancellation (Ctrl+C)
		if errors.Is(err, errors.ErrCanceled) {
			return err
		}
		return fmt.Errorf("prompt failed: %w", err)
	}

	if pushLocal {
		// Push local metadata to remote
		if err := eng.SetLastModifiedBy(info.BranchName); err != nil {
			out.Debug("Failed to set last modified by: %v", err)
		}
		if err := actions.PushMetadataAndSyncPRs(ctx, []string{info.BranchName}); err != nil {
			out.Debug("Failed to push metadata: %v", err)
		} else {
			out.Info("Pushed metadata for %s", style.ColorBranchName(info.BranchName, false))
		}
	} else {
		// Accept deletion - remove sync state
		if err := eng.DeleteLocalMetadataHash(info.BranchName); err != nil {
			out.Debug("Failed to delete metadata hash: %v", err)
		}
	}

	return nil
}

// printMetadataDiffs displays metadata differences in dry-run mode
func printMetadataDiffs(diffs []*engine.MetadataDiff, splog interface{ Info(string, ...interface{}) }) {
	splog.Info("\n=== Metadata changes (dry run) ===")
	for _, diff := range diffs {
		splog.Info("\nBranch: %s", style.ColorBranchName(diff.Branch, false))
		for _, fd := range diff.Differences {
			splog.Info("  %s: %v → %v", fd.Field, fd.LocalValue, fd.RemoteValue)
		}
	}
	splog.Info("\nRun without --dry-run to apply changes.")
}

// resolveMetadataConflict resolves a metadata conflict by prompting via handler
func resolveMetadataConflict(ctx *app.Context, diff *engine.MetadataDiff, handler Handler) error {
	eng := ctx.RemoteMetadata()

	acceptRemote, err := handler.PromptMetadataConflict(diff)
	if err != nil {
		// Handle user cancellation (Ctrl+C)
		if errors.Is(err, errors.ErrCanceled) {
			return err
		}
		return fmt.Errorf("prompt failed: %w", err)
	}

	if acceptRemote {
		return eng.AcceptRemoteMetadata(diff.Branch)
	}
	eng.RejectRemoteMetadata(diff.Branch)
	return nil
}
