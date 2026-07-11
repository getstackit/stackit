package absorb

import (
	"context"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/output"
)

// restoreParams captures everything restoreStashedState needs to put the
// working tree and index back after an absorb attempt.
type restoreParams struct {
	// stashedStaged is true when a staged absorb stash exists (created on the
	// primary path, or created despite the error on the fallback path).
	stashedStaged bool
	// stashedUnstaged is true when the primary path stashed unstaged/untracked
	// changes as a separate stash.
	stashedUnstaged bool
	// stagedFallback is true when `git stash push --staged` errored and the
	// fallback (capture unstaged diff + reset --hard) ran instead.
	stagedFallback bool
	// absorbSucceeded reports whether the rewrite completed.
	absorbSucceeded bool
	// unstagedPatch is the fallback-captured index->worktree diff, reapplied
	// with `git apply` (never a merging pop, so no conflict markers).
	unstagedPatch   string
	unabsorbedHunks []Unabsorbable
	originalHunks   []git.Hunk
}

// restoreStashedState restores the working tree and index after an absorb
// attempt. It is the sole owner of the stash/hunk restore flow so the behavior
// is testable in isolation from the (long) Action orchestration.
func restoreStashedState(ctx context.Context, eng engine.Engine, out output.Output, p restoreParams) {
	resolveStashRef := func(marker string) string {
		stashList, err := eng.StashList(ctx)
		if err != nil {
			return ""
		}
		return findStashRef(stashList, marker)
	}

	// Restore unstaged changes stashed on the primary path first, so any
	// unabsorbable-hunk restore below layers on top of them.
	if p.stashedUnstaged {
		restoreUnstagedStash(ctx, eng, out, resolveStashRef)
	}

	if p.absorbSucceeded {
		restoreOK := restoreUnabsorbedHunks(ctx, eng, out, p.unabsorbedHunks, resolveStashRef)

		// Reapply the fallback-captured unstaged diff last: by now the working
		// tree again holds the absorbed + restored staged content, so the
		// patch context matches and applies cleanly.
		if p.unstagedPatch != "" {
			if err := eng.ApplyPatchToWorktree(ctx, p.unstagedPatch); err != nil {
				out.Warn("Failed to restore unstaged changes: %v", err)
				restoreOK = false
			}
		}

		if p.stashedStaged {
			dropStagedStashIfRestored(ctx, eng, out, restoreOK, resolveStashRef)
		}
		return
	}

	// Failure path: put the original staged changes back.
	switch {
	case p.stashedStaged:
		if ref := resolveStashRef(absorbStashStagedMarker); ref != "" {
			if err := eng.StashPopRef(ctx, ref); err != nil {
				out.Warn("Failed to restore staged changes from stash: %v", err)
			}
		} else {
			warnStashNotFound(out, absorbStashStagedMarker)
		}
	case p.stagedFallback:
		restoreHunks := filterRestorableHunksFromHunks(p.originalHunks)
		if len(restoreHunks) > 0 {
			if err := eng.StageHunks(ctx, restoreHunks); err != nil {
				out.Warn("Failed to restore staged hunks after absorb failure: %v", err)
			}
		}
	}

	if p.unstagedPatch != "" {
		if err := eng.ApplyPatchToWorktree(ctx, p.unstagedPatch); err != nil {
			out.Warn("Failed to restore unstaged changes after absorb failure: %v", err)
		}
	}
}

// restoreUnstagedStash pops the primary-path unstaged stash by marker. When the
// marker cannot be resolved it warns rather than blindly popping stash@{0},
// which could be an unrelated user stash.
func restoreUnstagedStash(ctx context.Context, eng engine.Engine, out output.Output, resolveStashRef func(string) string) {
	if ref := resolveStashRef(absorbStashUnstagedMarker); ref != "" {
		if err := eng.StashPopRef(ctx, ref); err != nil {
			out.Warn("Failed to restore unstaged changes from stash: %v", err)
		}
		return
	}
	warnStashNotFound(out, absorbStashUnstagedMarker)
}

// restoreUnabsorbedHunks puts unabsorbable hunks back into the working tree and
// index after a successful absorb. It returns true only when every hunk was
// fully restored, so the caller knows whether the staged safety stash can be
// dropped.
func restoreUnabsorbedHunks(
	ctx context.Context,
	eng engine.Engine,
	out output.Output,
	unabsorbed []Unabsorbable,
	resolveStashRef func(string) string,
) bool {
	ok := true

	// Binary files cannot be applied as text patches; restore whole files from
	// the staged stash (which updates both the working tree and the index).
	binaryPaths := collectBinaryRestorePaths(unabsorbed)
	if len(binaryPaths) > 0 {
		switch ref := resolveStashRef(absorbStashStagedMarker); ref {
		case "":
			out.Warn("Failed to restore binary unabsorbable hunks: absorb stash not found.")
			ok = false
		default:
			if err := eng.CheckoutPaths(ctx, ref, binaryPaths); err != nil {
				out.Warn("Failed to restore binary unabsorbable hunks from stash: %v", err)
				ok = false
			}
		}
	}

	// Non-binary hunks: stage them into the index (StageHunks also writes new
	// files to disk), then materialize plain modification hunks in the working
	// tree so the user's edit survives on disk, not just in the index.
	restoreHunks := filterRestorableHunks(unabsorbed)
	if len(restoreHunks) > 0 {
		if err := eng.StageHunks(ctx, restoreHunks); err != nil {
			out.Warn("Failed to restore unabsorbable hunks to the index: %v", err)
			ok = false
		}
		if !applyModHunksToWorktree(ctx, eng, out, restoreHunks) {
			ok = false
		}
	}

	return ok
}

// applyModHunksToWorktree writes plain modification hunks into the working tree,
// file by file so an unappliable file (e.g. overlapping unstaged edits) does not
// block the rest. New-file hunks are already on disk via StageHunks, deleted
// files leave nothing to write, and binary hunks are restored elsewhere.
func applyModHunksToWorktree(ctx context.Context, eng engine.Engine, out output.Output, hunks []git.Hunk) bool {
	byFile := make(map[string][]git.Hunk)
	order := make([]string, 0, len(hunks))
	for _, h := range hunks {
		if h.Binary || h.IsNewFile || h.IsDeletedFile {
			continue
		}
		if _, seen := byFile[h.File]; !seen {
			order = append(order, h.File)
		}
		byFile[h.File] = append(byFile[h.File], h)
	}

	ok := true
	for _, file := range order {
		patch := git.Hunks(byFile[file]).Patch()
		if patch == "" {
			continue
		}
		if err := eng.ApplyPatchToWorktree(ctx, patch); err != nil {
			out.Warn("Restored '%s' to the index but could not update the working copy (it has other edits); recover it from the kept absorb stash if needed.", file)
			ok = false
		}
	}
	return ok
}

// dropStagedStashIfRestored drops the staged safety stash only after every
// unabsorbable-hunk restore has verifiably succeeded. If any restore fell short,
// the stash is kept as the recovery net and the user is told.
func dropStagedStashIfRestored(ctx context.Context, eng engine.Engine, out output.Output, restoreOK bool, resolveStashRef func(string) string) {
	if !restoreOK {
		out.Warn("Keeping the absorb staged stash for manual recovery because some changes could not be fully restored. Run 'git stash list' to find it.")
		return
	}

	ref := resolveStashRef(absorbStashStagedMarker)
	if ref == "" {
		return
	}
	if err := eng.StashDrop(ctx, ref); err != nil {
		out.Warn("Failed to drop staged absorb stash: %v", err)
	}
}

func warnStashNotFound(out output.Output, marker string) {
	out.Warn("Could not find the absorb stash (%s); leaving existing stashes untouched. Run 'git stash list' to recover manually.", marker)
}

func filterRestorableHunks(unabsorbed []Unabsorbable) []git.Hunk {
	hunks := make([]git.Hunk, 0, len(unabsorbed))
	for _, entry := range unabsorbed {
		hunks = append(hunks, entry.Hunk)
	}
	return filterRestorableHunksFromHunks(hunks)
}

func filterRestorableHunksFromHunks(hunks []git.Hunk) []git.Hunk {
	restorable := make([]git.Hunk, 0, len(hunks))
	for _, hunk := range hunks {
		if hunk.Binary {
			continue
		}
		restorable = append(restorable, hunk)
	}
	return restorable
}

func collectBinaryRestorePaths(unabsorbed []Unabsorbable) []string {
	seen := make(map[string]struct{})
	paths := make([]string, 0, len(unabsorbed))
	for _, entry := range unabsorbed {
		if !entry.Hunk.Binary || entry.Hunk.IsDeletedFile {
			continue
		}
		if _, ok := seen[entry.Hunk.File]; ok {
			continue
		}
		seen[entry.Hunk.File] = struct{}{}
		paths = append(paths, entry.Hunk.File)
	}
	return paths
}
