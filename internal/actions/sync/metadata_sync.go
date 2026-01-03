package sync

import (
	"errors"
	"fmt"

	"stackit.dev/stackit/internal/actions"
	"stackit.dev/stackit/internal/app"
	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/internal/tui"
	"stackit.dev/stackit/internal/tui/style"
)

// syncRemoteMetadata fetches and processes remote metadata
func syncRemoteMetadata(ctx *app.Context, opts *Options) error {
	eng := ctx.RemoteMetadata()
	splog := ctx.Splog

	// Fetch remote metadata refs
	if err := eng.FetchRemoteMetadata(ctx.Context); err != nil {
		// Non-fatal: remote may not have metadata yet
		splog.Debug("No remote metadata to fetch: %v", err)
	}

	// Configure refspec so future git fetch commands also fetch metadata
	if err := eng.ConfigureRemoteMetadataSync(ctx.Context); err != nil {
		splog.Debug("Failed to configure metadata refspec: %v", err)
	}

	// Load remote metadata into cache
	if err := eng.LoadRemoteMetadataCache(); err != nil {
		splog.Debug("Failed to load remote metadata cache: %v", err)
	}

	// Handle orphaned local metadata (dual-checkout scenario or manual branch deletion)
	if err := handleOrphanedMetadata(ctx, opts); err != nil {
		return err
	}

	// Compute diffs
	diffs, err := eng.ComputeAllMetadataDiffs()
	if err != nil {
		return fmt.Errorf("failed to compute metadata diffs: %w", err)
	}

	if len(diffs) == 0 {
		return nil // No conflicts
	}

	// Handle --dry-run flag
	if opts.DryRun {
		printMetadataDiffs(diffs, splog)
		return nil
	}

	// Prompt user for each conflicting branch
	for _, diff := range diffs {
		if err := promptAndResolveConflict(ctx, diff); err != nil {
			return err
		}
	}

	return nil
}

// handleOrphanedMetadata handles branches where remote metadata was deleted but local exists
func handleOrphanedMetadata(ctx *app.Context, opts *Options) error {
	eng := ctx.Engine
	splog := ctx.Splog

	orphaned, err := eng.FindOrphanedLocalMetadata()
	if err != nil {
		splog.Debug("Failed to find orphaned metadata: %v", err)
		return nil
	}

	if len(orphaned) == 0 {
		return nil
	}

	// Handle --dry-run flag
	if opts.DryRun {
		splog.Info("\n=== Orphaned metadata (dry run) ===")
		for _, info := range orphaned {
			switch {
			case !info.ExistsLocally:
				splog.Info("  %s: local branch gone, would delete metadata", style.ColorBranchName(info.BranchName, false))
			case info.HasLocalChanges:
				splog.Info("  %s: has local changes, would prompt", style.ColorBranchName(info.BranchName, false))
			default:
				splog.Info("  %s: no local changes, would delete sync state", style.ColorBranchName(info.BranchName, false))
			}
		}
		return nil
	}

	for _, info := range orphaned {
		if info.Action == engine.OrphanedActionDelete {
			// No local changes - silently remove sync state or delete ref if branch is gone
			if !info.ExistsLocally {
				if err := eng.DeleteMetadata(ctx.Context, info.BranchName); err != nil {
					splog.Debug("Failed to delete orphaned metadata ref for %s: %v", info.BranchName, err)
				}
			} else if err := eng.DeleteLocalMetadataHash(info.BranchName); err != nil {
				splog.Debug("Failed to delete metadata hash for %s: %v", info.BranchName, err)
			}
		} else {
			// Has local changes - prompt user
			if err := promptOrphanedMetadata(ctx, info); err != nil {
				return err
			}
		}
	}

	return nil
}

// promptOrphanedMetadata prompts the user about orphaned metadata with local changes
func promptOrphanedMetadata(ctx *app.Context, info engine.OrphanedMetadataInfo) error {
	eng := ctx.Engine
	splog := ctx.Splog

	splog.Info("\nRemote metadata for '%s' was deleted, but you have local changes:",
		style.ColorBranchName(info.BranchName, false))
	if info.LocalMeta.LockReason.IsLocked() {
		splog.Info("  lockReason: %s", info.LocalMeta.LockReason)
	}
	if info.LocalMeta.Scope != nil {
		splog.Info("  scope: %s", *info.LocalMeta.Scope)
	}

	accept, err := promptYesNo("Push your local metadata to remote?")
	if err != nil {
		// In non-interactive mode, PromptConfirm returns (false, ErrInteractiveDisabled)
		// We default to false (don't push) to avoid hanging in tests
		if !errors.Is(err, tui.ErrInteractiveDisabled) {
			return err
		}
		// accept is already false when ErrInteractiveDisabled
	}

	if accept {
		// Push local metadata to remote
		if err := eng.SetLastModifiedBy(info.BranchName); err != nil {
			splog.Debug("Failed to set last modified by: %v", err)
		}
		if err := actions.PushMetadataAndSyncPRs(ctx, []string{info.BranchName}); err != nil {
			splog.Debug("Failed to push metadata: %v", err)
		} else {
			splog.Info("Pushed metadata for %s", style.ColorBranchName(info.BranchName, false))
		}
	} else {
		// Accept deletion - remove sync state
		if err := eng.DeleteLocalMetadataHash(info.BranchName); err != nil {
			splog.Debug("Failed to delete metadata hash: %v", err)
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

// promptAndResolveConflict prompts the user to accept or reject remote metadata
func promptAndResolveConflict(ctx *app.Context, diff *engine.MetadataDiff) error {
	eng := ctx.RemoteMetadata()
	splog := ctx.Splog

	// Display field-level diff
	splog.Info("\nMetadata differs for branch '%s':", style.ColorBranchName(diff.Branch, false))
	for _, fd := range diff.Differences {
		splog.Info("  %s: %v (local) → %v (remote)", fd.Field, fd.LocalValue, fd.RemoteValue)
	}
	if diff.RemoteMeta.LastModifiedBy != nil {
		splog.Info("  Last modified by: %s <%s>",
			diff.RemoteMeta.LastModifiedBy.GitName,
			diff.RemoteMeta.LastModifiedBy.GitEmail)
	}

	// Prompt
	accept, err := promptYesNo("Accept remote metadata?")
	if err != nil {
		// In non-interactive mode, PromptConfirm returns (false, ErrInteractiveDisabled)
		// We default to false (reject remote) to avoid hanging in tests
		if !errors.Is(err, tui.ErrInteractiveDisabled) {
			return err
		}
		// accept is already false when ErrInteractiveDisabled
	}

	if accept {
		return eng.AcceptRemoteMetadata(diff.Branch)
	}
	eng.RejectRemoteMetadata(diff.Branch)
	return nil
}

// promptYesNo prompts the user for a yes/no answer
// Uses tui.PromptConfirm which respects non-interactive mode
func promptYesNo(prompt string) (bool, error) {
	return tui.PromptConfirm(prompt, false)
}
