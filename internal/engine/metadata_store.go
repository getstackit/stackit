package engine

import (
	"context"

	"github.com/getstackit/stackit/internal/git"
)

// readMetadata loads branch metadata from the configured metadata store.
func (e *engineImpl) readMetadata(branch string) (*git.Meta, error) {
	return e.git.ReadMetadata(branch)
}

// writeMetadata persists branch metadata to the configured metadata store.
func (e *engineImpl) writeMetadata(branch string, meta *git.Meta) error {
	return e.git.WriteMetadata(branch, meta)
}

// batchReadMetadata loads metadata for many branches in one call.
func (e *engineImpl) batchReadMetadata(branches []string) (map[string]*git.Meta, map[string]error) {
	return e.git.BatchReadMetadata(branches)
}

// readLocalMetadata loads local-only metadata for a branch.
func (e *engineImpl) readLocalMetadata(branch string) (*git.LocalMeta, error) {
	return e.git.ReadLocalMetadata(branch)
}

// writeLocalMetadata persists local-only metadata for a branch.
func (e *engineImpl) writeLocalMetadata(branch string, meta *git.LocalMeta) error {
	return e.git.WriteLocalMetadata(branch, meta)
}

// batchReadLocalMetadata loads local metadata for many branches in one call.
func (e *engineImpl) batchReadLocalMetadata(branches []string) map[string]*git.LocalMeta {
	return e.git.BatchReadLocalMetadata(branches)
}

// withMetadataTx runs function logic inside a metadata transaction and commits once.
// If fn returns an error, the transaction is rolled back.
func (e *engineImpl) withMetadataTx(ctx context.Context, message string, fn func(tx *MetadataTx) error) error {
	tx := e.BeginTx(message)
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit(ctx)
}
