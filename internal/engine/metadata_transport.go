package engine

import "context"

// Metadata transport methods. Engine owns the metadata-ref namespace
// (refs/stackit/metadata/* and refs/stackit/stacks/*) and is the single point
// where adapter code should reach to push, fetch, or test remote support — no
// adapter should be calling git.Runner directly for these.

// TestRemoteMetadataCompatibility probes the configured remote to verify that
// it accepts the metadata-ref namespace. Returns nil on success.
func (e *engineImpl) TestRemoteMetadataCompatibility(ctx context.Context) error {
	return e.git.TestRemoteRefCompatibility(ctx)
}

// PushMetadataForBranches pushes metadata refs for the given branch names to
// origin. A no-op when the list is empty.
func (e *engineImpl) PushMetadataForBranches(ctx context.Context, branchNames []string) error {
	return e.git.PushMetadataRefs(ctx, branchNames)
}

// ConfigureStackMetadataSync adds the stack-metadata refspec to origin so
// subsequent `git fetch origin` invocations pick up stack-ref changes.
func (e *engineImpl) ConfigureStackMetadataSync(_ context.Context) error {
	return e.git.EnsureStackMetaRefspecConfigured()
}

// FetchStackMetadata fetches stack-metadata refs from origin into the
// remote-stacks namespace.
func (e *engineImpl) FetchStackMetadata(ctx context.Context) error {
	return e.git.FetchStackMetaRefs(ctx)
}

// ListStackMetadata returns a map of local stack IDs to their ref SHAs. Used
// by stack-metadata GC during sync.
func (e *engineImpl) ListStackMetadata() (map[string]string, error) {
	return e.git.ListStackMetas()
}

// DeleteStackMetadata removes a single local stack-metadata ref. Used as the
// per-ref fallback in the GC path when the batched ref-update fails.
func (e *engineImpl) DeleteStackMetadata(ctx context.Context, stackID string) error {
	return e.git.DeleteStackMeta(ctx, stackID)
}

// DeleteRemoteStackMetadata pushes ref-deletions for the given stack IDs to
// origin. Best-effort: callers typically treat failure as non-fatal because the
// remote refs may already be absent.
func (e *engineImpl) DeleteRemoteStackMetadata(ctx context.Context, stackIDs []string) error {
	return e.git.DeleteRemoteStackMetaRefs(ctx, stackIDs)
}
