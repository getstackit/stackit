package engine

import (
	"context"
	"fmt"

	"github.com/getstackit/stackit/internal/git"
)

// Metadata transport methods. Engine owns the metadata-ref namespace
// (refs/stackit/metadata/* and refs/stackit/stacks/*) and is the single point
// where adapter code should reach to push, fetch, or test remote support — no
// adapter should be calling git.Runner directly for these.

// EnsureRemoteMetadata fetches the latest branch metadata refs, configures the
// fetch refspec for future git fetches, and loads the fetched refs into the
// engine cache. Callers must pass a context with a deadline because this may
// perform network I/O.
func (e *engineImpl) EnsureRemoteMetadata(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("ensure remote metadata requires a context with a deadline")
	}
	if _, ok := ctx.Deadline(); !ok {
		return fmt.Errorf("ensure remote metadata requires a context with a deadline")
	}

	if err := e.FetchRemote(ctx, RemoteFetchRequest{IncludeMetadata: true}); err != nil {
		return err
	}
	if err := e.ConfigureRemoteMetadataSync(ctx); err != nil {
		return err
	}
	if err := e.LoadRemoteMetadataCache(); err != nil {
		return err
	}
	return nil
}

// TestRemoteMetadataCompatibility probes the configured remote to verify that
// it accepts the metadata-ref namespace. Returns nil on success.
func (e *engineImpl) TestRemoteMetadataCompatibility(ctx context.Context) error {
	return e.git.TestRemoteRefCompatibility(ctx)
}

// PrepareRemoteMetadataPush verifies that the remote accepts metadata refs and
// records that support locally. Fetch-refspec setup is best-effort because
// metadata pushes can still succeed when local fetch configuration cannot be
// updated.
func (e *engineImpl) PrepareRemoteMetadataPush(ctx context.Context) error {
	if e.IsRemoteSyncEnabled() {
		return nil
	}
	if err := e.TestRemoteMetadataCompatibility(ctx); err != nil {
		return err
	}
	e.SetRemoteSyncEnabled(true)
	_ = e.ConfigureRemoteMetadataSync(ctx)
	return nil
}

// PushMetadataForBranches pushes metadata refs for the given branch names to
// origin. A no-op when the list is empty.
func (e *engineImpl) PushMetadataForBranches(ctx context.Context, branchNames []string) error {
	return e.git.PushMetadataRefs(ctx, branchNames)
}

// DeleteRemoteMetadataForBranches pushes ref-deletions for the given branches'
// metadata refs to origin. Best-effort: callers typically treat failure as
// non-fatal because the remote refs may already be absent.
func (e *engineImpl) DeleteRemoteMetadataForBranches(ctx context.Context, branchNames []string) error {
	return e.git.BatchDeleteRemoteMetadataRefs(ctx, branchNames)
}

// PushStackMetadata pushes the stack-metadata refs for the given stack IDs to
// origin. A no-op when the list is empty.
func (e *engineImpl) PushStackMetadata(ctx context.Context, stackIDs []string) error {
	return e.git.PushStackMetaRefs(ctx, stackIDs)
}

// ConfigureStackMetadataSync adds the stack-metadata refspec to the configured
// remote so subsequent git fetches pick up stack-ref changes.
func (e *engineImpl) ConfigureStackMetadataSync(_ context.Context) error {
	return e.git.EnsureStackMetaRefspecConfigured()
}

// FetchStackMetadata fetches stack-metadata refs into the
// remote-stacks namespace.
func (e *engineImpl) FetchStackMetadata(ctx context.Context) error {
	return e.FetchRemote(ctx, RemoteFetchRequest{IncludeStackMetadata: true})
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

// DeleteStackMetadataBatch removes the local stack-metadata refs for the given
// stack IDs in a single update-ref --stdin batch. The engine owns the
// stack-ref name format, so callers pass stack IDs rather than raw ref names.
func (e *engineImpl) DeleteStackMetadataBatch(ctx context.Context, stackIDs []string) error {
	refs := make([]string, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		refs = append(refs, git.StackMetaRefName(stackID))
	}
	return e.git.DeleteRefsBatch(ctx, refs)
}

// DeleteRemoteStackMetadata pushes ref-deletions for the given stack IDs to
// origin. Best-effort: callers typically treat failure as non-fatal because the
// remote refs may already be absent.
func (e *engineImpl) DeleteRemoteStackMetadata(ctx context.Context, stackIDs []string) error {
	return e.git.DeleteRemoteStackMetaRefs(ctx, stackIDs)
}
