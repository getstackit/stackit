package engine

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

// MarkBranchesForPRBodyUpdate marks multiple branches as needing a PR body update
// in a single atomic operation.
func (e *engineImpl) MarkBranchesForPRBodyUpdate(ctx context.Context, branchNames []string) error {
	return e.setLocalFlag(ctx, branchNames, func(meta *git.LocalMeta) bool {
		if meta.NeedsPRBodyUpdate {
			return false
		}
		meta.NeedsPRBodyUpdate = true
		return true
	})
}

// ClearNeedsPRBodyUpdate clears the PR body update flag for a branch
func (e *engineImpl) ClearNeedsPRBodyUpdate(ctx context.Context, branchName string) error {
	localMeta, err := e.readLocalMetadata(branchName)
	if err != nil {
		// Best effort - if we can't read metadata, nothing to clear
		return nil //nolint:nilerr
	}
	if localMeta == nil || !localMeta.NeedsPRBodyUpdate {
		return nil // Nothing to clear
	}
	localMeta.NeedsPRBodyUpdate = false
	return e.writeLocalMetadata(ctx, branchName, localMeta)
}

// BranchesNeedingPRBodyUpdate returns all branches that need PR body updates
func (e *engineImpl) BranchesNeedingPRBodyUpdate() []string {
	allBranches := e.AllBranches()
	branchNames := allBranches.Names()

	localMetas := e.batchReadLocalMetadata(branchNames)
	var result []string
	for name, meta := range localMetas {
		if meta != nil && meta.NeedsPRBodyUpdate {
			result = append(result, name)
		}
	}
	return result
}

// MarkBranchesSubmitPending records that a submit is about to push these
// branches and write their pull requests. Submit clears the flag per branch as
// each PR write lands, so anything still marked afterwards is an unfinished
// submit that the next run must retry.
func (e *engineImpl) MarkBranchesSubmitPending(ctx context.Context, branchNames []string) error {
	return e.setLocalFlag(ctx, branchNames, func(meta *git.LocalMeta) bool {
		if meta.SubmitPending {
			return false
		}
		meta.SubmitPending = true
		return true
	})
}

// ClearBranchesSubmitPending clears the submit-pending flag for branches whose
// pull request write completed.
func (e *engineImpl) ClearBranchesSubmitPending(ctx context.Context, branchNames []string) error {
	return e.setLocalFlag(ctx, branchNames, func(meta *git.LocalMeta) bool {
		if !meta.SubmitPending {
			return false
		}
		meta.SubmitPending = false
		return true
	})
}

// setLocalFlag batch-reads local metadata for every named branch, applies
// mutate, and writes the changed blobs in one `git hash-object` call followed
// by one atomic ref update. mutate reports whether it changed anything, so
// branches already in the target state cost no ref write.
func (e *engineImpl) setLocalFlag(ctx context.Context, branchNames []string, mutate func(*git.LocalMeta) bool) error {
	if len(branchNames) == 0 {
		return nil
	}

	// Batch read all local metadata in parallel
	allMeta := e.batchReadLocalMetadata(branchNames)

	metas := make([]*git.LocalMeta, 0, len(branchNames))
	orderedNames := make([]string, 0, len(branchNames))
	for _, name := range branchNames {
		meta := allMeta[name]
		if meta == nil {
			meta = &git.LocalMeta{}
		}
		if !mutate(meta) {
			continue
		}
		metas = append(metas, meta)
		orderedNames = append(orderedNames, name)
	}
	if len(orderedNames) == 0 {
		return nil
	}

	shas, err := e.metadata.WriteLocalMetadataBlobs(ctx, metas)
	if err != nil {
		return fmt.Errorf("failed to create local metadata blobs: %w", err)
	}

	updates := make([]git.RefUpdate, len(orderedNames))
	committed := make([]git.CommittedMetadata, len(orderedNames))
	for i, name := range orderedNames {
		updates[i] = git.RefUpdate{
			RefName: git.LocalMetadataRefName(name),
			NewSHA:  shas[i],
		}
		committed[i] = git.CommittedMetadata{Branch: name, SHA: shas[i]}
	}

	// Atomic batch update all refs
	if err := e.git.UpdateRefs(ctx, updates, ""); err != nil {
		return err
	}
	// The flags were written outside the store; record them so a following
	// local-metadata write compares against this write rather than the
	// pre-flag version.
	e.metadata.RecordCommitted(git.MetadataTierLocal, committed...)
	return nil
}
