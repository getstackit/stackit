package sync

import (
	"fmt"
	"time"

	"github.com/getstackit/stackit/internal/actions"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/errors"
	"github.com/getstackit/stackit/internal/output"
)

// syncRemoteMetadata fetches and processes remote metadata.
//
// Deprecated: Use FetchRemoteMetadata in parallel + processRemoteMetadata
func syncRemoteMetadata(ctx *app.Context, opts *Options, handler Handler) error {
	eng := ctx.RemoteMetadata()
	out := ctx.Output

	// Fetch remote metadata refs
	remoteCtx, cancelRemote := ctx.RemoteOperationContext()
	defer cancelRemote()
	fetchStart := time.Now()
	if err := eng.FetchRemoteMetadata(remoteCtx); err != nil {
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
	if err := eng.ConfigureStackMetadataSync(ctx.Context); err != nil {
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
				out.Info("  %s: local branch gone, would delete metadata", output.BranchName(info.BranchName))
			case info.HasLocalChanges:
				out.Info("  %s: has local changes, would prompt", output.BranchName(info.BranchName))
			default:
				out.Info("  %s: no local changes, would delete sync state", output.BranchName(info.BranchName))
			}
		}
		return nil
	}

	// Collect every action so it applies in one batched transaction; branches
	// with local changes still prompt per-branch, but their decisions are
	// batched into the same delete/clear/push calls as the auto-delete ones.
	var deleteRefs, clearLocalHash, pushBranches []string
	for _, info := range orphaned {
		if info.Action == engine.OrphanedActionDelete {
			// No local changes - silently remove sync state or delete ref if branch is gone
			if !info.ExistsLocally {
				deleteRefs = append(deleteRefs, info.BranchName)
			} else {
				clearLocalHash = append(clearLocalHash, info.BranchName)
			}
			continue
		}

		// Has local changes - prompt user via handler
		pushLocal, err := resolveOrphanedMetadata(info, handler)
		if err != nil {
			return err
		}
		if pushLocal {
			pushBranches = append(pushBranches, info.BranchName)
		} else {
			clearLocalHash = append(clearLocalHash, info.BranchName)
		}
	}

	if err := eng.CleanOrphanedMetadata(ctx.Context, deleteRefs, clearLocalHash); err != nil {
		out.Debug("Failed to clean orphaned metadata: %v", err)
	}

	if len(pushBranches) > 0 {
		if err := actions.PushMetadataAndSyncPRs(ctx, pushBranches); err != nil {
			out.Debug("Failed to push metadata: %v", err)
		} else {
			for _, name := range pushBranches {
				out.Info("Pushed metadata for %s", output.BranchName(name))
			}
		}
	}

	return nil
}

// resolveOrphanedMetadata prompts the user for how to resolve orphaned
// metadata on a branch with local changes. It returns whether they chose to
// push local metadata to the remote (vs. accepting deletion).
func resolveOrphanedMetadata(info engine.OrphanedMetadataInfo, handler Handler) (bool, error) {
	pushLocal, err := handler.PromptOrphanedMetadata(info)
	if err != nil {
		// Handle user cancellation (Ctrl+C)
		if errors.Is(err, errors.ErrCanceled) {
			return false, err
		}
		return false, fmt.Errorf("prompt failed: %w", err)
	}

	return pushLocal, nil
}

// printMetadataDiffs displays metadata differences in dry-run mode
func printMetadataDiffs(diffs []*engine.MetadataDiff, splog interface{ Info(string, ...any) }) {
	splog.Info("\n=== Metadata changes (dry run) ===")
	for _, diff := range diffs {
		splog.Info("\nBranch: %s", output.BranchName(diff.Branch))
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
