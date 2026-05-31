package engine

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

// MarkBranchesForPRBodyUpdate marks multiple branches as needing a PR body update
// in a single atomic operation. It batch-reads local metadata, sets the flag,
// writes all the blobs in one `git hash-object` call via WriteLocalMetadataBlobsBatch,
// and atomically updates all refs.
func (e *engineImpl) MarkBranchesForPRBodyUpdate(ctx context.Context, branchNames []string) error {
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
		meta.NeedsPRBodyUpdate = true
		metas = append(metas, meta)
		orderedNames = append(orderedNames, name)
	}

	shas, err := e.git.WriteLocalMetadataBlobsBatch(ctx, metas)
	if err != nil {
		return fmt.Errorf("failed to create local metadata blobs: %w", err)
	}

	updates := make([]git.RefUpdate, len(orderedNames))
	for i, name := range orderedNames {
		updates[i] = git.RefUpdate{
			RefName: git.LocalMetadataRefName(name),
			NewSHA:  shas[i],
		}
	}

	// Atomic batch update all refs
	return e.git.UpdateRefsBatch(ctx, updates)
}

// ClearNeedsPRBodyUpdate clears the PR body update flag for a branch
func (e *engineImpl) ClearNeedsPRBodyUpdate(branchName string) error {
	localMeta, err := e.readLocalMetadata(branchName)
	if err != nil {
		// Best effort - if we can't read metadata, nothing to clear
		return nil //nolint:nilerr
	}
	if localMeta == nil || !localMeta.NeedsPRBodyUpdate {
		return nil // Nothing to clear
	}
	localMeta.NeedsPRBodyUpdate = false
	return e.writeLocalMetadata(branchName, localMeta)
}

// GetBranchesNeedingPRBodyUpdate returns all branches that need PR body updates
func (e *engineImpl) GetBranchesNeedingPRBodyUpdate() []string {
	allBranches := e.AllBranches()
	branchNames := make([]string, len(allBranches))
	for i, b := range allBranches {
		branchNames[i] = b.GetName()
	}

	localMetas := e.batchReadLocalMetadata(branchNames)
	var result []string
	for name, meta := range localMetas {
		if meta != nil && meta.NeedsPRBodyUpdate {
			result = append(result, name)
		}
	}
	return result
}
