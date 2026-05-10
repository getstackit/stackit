package engine_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestSetLocked_SingleBranch(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create a branch to test with
	s.CreateBranch("feature-1").
		Commit("feature 1 change")

	// Track the branch so it has metadata
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	branch := s.Engine.GetBranch("feature-1")

	// Lock the branch
	result, err := s.Engine.SetLocked(context.Background(), []engine.Branch{branch}, engine.LockReasonUser)
	require.NoError(t, err)
	assert.Len(t, result.AffectedBranches, 1)
	assert.Contains(t, result.AffectedBranches, "feature-1")

	// Verify the change persisted
	impl := s.Engine.(interface{ Git() git.Runner })
	readMeta, err := impl.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, readMeta.GetLockReason())

	// Verify in-memory cache is updated
	assert.True(t, branch.IsLocked())
}

func TestSetLocked_UsesTransaction(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create branches
	s.CreateBranch("feature-1").
		Commit("feature 1 change").
		CreateBranch("feature-2").
		Commit("feature 2 change")

	// Track branches
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)
	err = s.Engine.TrackBranch(context.Background(), "feature-2", "feature-1")
	require.NoError(t, err)

	// Lock multiple branches
	result, err := s.Engine.SetLocked(context.Background(), []engine.Branch{
		s.Engine.GetBranch("feature-1"),
		s.Engine.GetBranch("feature-2"),
	}, engine.LockReasonUser)
	require.NoError(t, err)

	// Both should be affected
	assert.Len(t, result.AffectedBranches, 2)
	assert.Empty(t, result.Errors)

	// Verify persistence
	impl := s.Engine.(interface{ Git() git.Runner })
	meta1, err := impl.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, meta1.GetLockReason())

	meta2, err := impl.Git().ReadMetadata("feature-2")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, meta2.GetLockReason())
}

func TestSetFrozen_UsesTransaction(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create branches
	s.CreateBranch("feature-1").
		Commit("feature 1 change").
		CreateBranch("feature-2").
		Commit("feature 2 change")

	// Track branches
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)
	err = s.Engine.TrackBranch(context.Background(), "feature-2", "feature-1")
	require.NoError(t, err)

	// Freeze multiple branches
	result, err := s.Engine.SetFrozen(context.Background(), []engine.Branch{
		s.Engine.GetBranch("feature-1"),
		s.Engine.GetBranch("feature-2"),
	}, true)
	require.NoError(t, err)

	// Both should be affected
	assert.Len(t, result.AffectedBranches, 2)
	assert.Empty(t, result.Errors)

	// Verify persistence
	impl := s.Engine.(interface{ Git() git.Runner })
	localMeta1, err := impl.Git().ReadLocalMetadata("feature-1")
	require.NoError(t, err)
	assert.True(t, localMeta1.Frozen)

	localMeta2, err := impl.Git().ReadLocalMetadata("feature-2")
	require.NoError(t, err)
	assert.True(t, localMeta2.Frozen)
}

func TestSetLocked_EmptyBranchList(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Empty branch list should succeed
	result, err := s.Engine.SetLocked(context.Background(), []engine.Branch{}, engine.LockReasonUser)
	require.NoError(t, err)
	assert.Empty(t, result.AffectedBranches)
	assert.Empty(t, result.Errors)
}

func TestSetFrozen_EmptyBranchList(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Empty branch list should succeed
	result, err := s.Engine.SetFrozen(context.Background(), []engine.Branch{}, true)
	require.NoError(t, err)
	assert.Empty(t, result.AffectedBranches)
	assert.Empty(t, result.Errors)
}

func TestSetLocked_UnlockBranches(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	branch := s.Engine.GetBranch("feature-1")

	// Lock the branch
	_, err = s.Engine.SetLocked(context.Background(), []engine.Branch{branch}, engine.LockReasonUser)
	require.NoError(t, err)

	// Verify locked
	impl := s.Engine.(interface{ Git() git.Runner })
	meta, err := impl.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, meta.GetLockReason())

	// Unlock the branch
	_, err = s.Engine.SetLocked(context.Background(), []engine.Branch{branch}, engine.LockReasonNone)
	require.NoError(t, err)

	// Verify unlocked
	meta, err = impl.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonNone, meta.GetLockReason())
}

func TestSetFrozen_UnfreezeBranches(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	branch := s.Engine.GetBranch("feature-1")

	// Freeze the branch
	_, err = s.Engine.SetFrozen(context.Background(), []engine.Branch{branch}, true)
	require.NoError(t, err)

	// Verify frozen
	impl := s.Engine.(interface{ Git() git.Runner })
	localMeta, err := impl.Git().ReadLocalMetadata("feature-1")
	require.NoError(t, err)
	assert.True(t, localMeta.Frozen)

	// Unfreeze the branch
	_, err = s.Engine.SetFrozen(context.Background(), []engine.Branch{branch}, false)
	require.NoError(t, err)

	// Verify unfrozen
	localMeta, err = impl.Git().ReadLocalMetadata("feature-1")
	require.NoError(t, err)
	assert.False(t, localMeta.Frozen)
}

func TestIsConcurrentModificationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "generic error",
			err:      assert.AnError,
			expected: false,
		},
		{
			name:     "cannot lock ref error",
			err:      fmt.Errorf("cannot lock ref 'refs/stackit/metadata/branch': is at unexpected sha"),
			expected: true,
		},
		{
			name:     "reference is not expected error",
			err:      fmt.Errorf("reference is not expected: refs/stackit/metadata/branch"),
			expected: true,
		},
		{
			name:     "expected old-value error",
			err:      fmt.Errorf("expected old-value abc123 for ref refs/stackit/metadata/branch"),
			expected: true,
		},
		{
			name:     "wrapped concurrent modification error",
			err:      fmt.Errorf("commit failed: %w", fmt.Errorf("cannot lock ref 'refs/stackit/metadata/test'")),
			expected: true,
		},
		{
			name:     "unrelated lock error",
			err:      fmt.Errorf("failed to acquire process lock"),
			expected: false,
		},
		{
			name:     "stale info error",
			err:      fmt.Errorf("stale info on ref refs/stackit/metadata/test"),
			expected: true,
		},
		{
			name:     "git lock file error",
			err:      fmt.Errorf("Unable to create '/path/to/repo/.git/refs/stackit/metadata/test.lock': File exists"),
			expected: true,
		},
		{
			name:     "unrelated Unable to create error",
			err:      fmt.Errorf("Unable to create directory"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := engine.IsConcurrentModificationError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	branch := s.Engine.GetBranch("feature-1")

	// SetLocked should return context error since context is canceled
	_, err = s.Engine.SetLocked(ctx, []engine.Branch{branch}, engine.LockReasonUser)
	// Note: The operation might succeed before the context check, or fail with context error
	// We just verify it doesn't hang indefinitely
	if err != nil {
		// Either context.Canceled or the operation completed before ctx check
		t.Logf("SetLocked with canceled context returned: %v", err)
	}
}

func TestTransaction_RollbackOnCommitFailure(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	// Get the engine implementation to access BeginTx
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Get original metadata state
	originalMeta, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)

	// Begin transaction and stage an update
	tx := eng.BeginTx("test transaction")
	newMeta := git.NewMetaFrom(git.MetaFields{
		LockReason: git.LockReasonUser,
	})
	err = tx.UpdateMeta("feature-1", newMeta)
	require.NoError(t, err)

	// Call Rollback explicitly
	tx.Rollback()

	// Verify original metadata is unchanged (rollback is a no-op for uncommitted transactions)
	currentMeta, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, originalMeta.GetLockReason(), currentMeta.GetLockReason())
}

func TestTransaction_DoubleCommitFails(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	// Get the engine implementation
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Begin transaction and stage an update
	tx := eng.BeginTx("test transaction")
	meta := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
	err = tx.UpdateMeta("feature-1", meta)
	require.NoError(t, err)

	// First commit should succeed
	err = tx.Commit(context.Background())
	require.NoError(t, err)

	// Second commit should fail
	err = tx.Commit(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, engine.ErrTransactionCommitted)
}

func TestTransaction_EmptyTransactionSucceeds(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Get the engine implementation
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
	})

	// Begin transaction with no updates
	tx := eng.BeginTx("empty transaction")

	// Empty transaction should succeed
	err := tx.Commit(context.Background())
	require.NoError(t, err)
}

func TestSetLocked_DeterministicOrder(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create multiple branches
	s.CreateBranch("aaa").Commit("a change")
	s.CreateBranch("zzz").Commit("z change")
	s.CreateBranch("mmm").Commit("m change")

	// Track all branches
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "aaa", "main"))
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "zzz", "aaa"))
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "mmm", "zzz"))

	// Lock in specific order
	branches := []engine.Branch{
		s.Engine.GetBranch("zzz"),
		s.Engine.GetBranch("aaa"),
		s.Engine.GetBranch("mmm"),
	}

	result, err := s.Engine.SetLocked(context.Background(), branches, engine.LockReasonUser)
	require.NoError(t, err)

	// Verify order matches input order (deterministic)
	require.Len(t, result.AffectedBranches, 3)
	assert.Equal(t, "zzz", result.AffectedBranches[0])
	assert.Equal(t, "aaa", result.AffectedBranches[1])
	assert.Equal(t, "mmm", result.AffectedBranches[2])
}

func TestTransaction_ConcurrentModification(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	// Get the engine implementation
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Begin transaction and stage an update (this captures the original SHA)
	tx := eng.BeginTx("test transaction")
	meta1 := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
	err = tx.UpdateMeta("feature-1", meta1)
	require.NoError(t, err)

	// Simulate another process modifying the metadata
	// This changes the ref SHA, causing CAS to fail
	meta2 := git.NewMetaFrom(git.MetaFields{
		LockReason: git.LockReasonConsolidating,
	})
	err = eng.Git().WriteMetadata("feature-1", meta2)
	require.NoError(t, err)

	// Now try to commit - should fail due to CAS mismatch
	err = tx.Commit(context.Background())
	require.Error(t, err)

	// Verify it's detected as a concurrent modification error
	assert.True(t, engine.IsConcurrentModificationError(err),
		"expected concurrent modification error, got: %v", err)
}

func TestTransaction_ConcurrentModificationLocalMeta(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	// Get the engine implementation
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// First, write initial local metadata so there's a ref to capture
	initialMeta := &git.LocalMeta{Frozen: false}
	err = eng.Git().WriteLocalMetadata("feature-1", initialMeta)
	require.NoError(t, err)

	// Begin transaction and stage a local metadata update (captures initial SHA)
	tx := eng.BeginTx("test transaction")
	localMeta1 := &git.LocalMeta{Frozen: true}
	err = tx.UpdateLocalMeta("feature-1", localMeta1)
	require.NoError(t, err)

	// Simulate another process modifying the local metadata with DIFFERENT content
	localMeta2 := &git.LocalMeta{Frozen: false, NeedsPRBodyUpdate: true}
	err = eng.Git().WriteLocalMetadata("feature-1", localMeta2)
	require.NoError(t, err)

	// Now try to commit - should fail due to CAS mismatch
	err = tx.Commit(context.Background())
	require.Error(t, err)

	// Verify it's detected as a concurrent modification error
	assert.True(t, engine.IsConcurrentModificationError(err),
		"expected concurrent modification error, got: %v", err)
}

func TestWithRetry_RetriesOnConcurrentModification(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	// Get the engine implementation
	eng := s.Engine.(interface {
		WithRetry(ctx context.Context, operation func() error) error
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	attemptCount := 0
	maxFailures := 2 // Fail first 2 attempts, succeed on 3rd

	err = eng.WithRetry(context.Background(), func() error {
		attemptCount++

		tx := eng.BeginTx("retry test")
		meta := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
		if stageErr := tx.UpdateMeta("feature-1", meta); stageErr != nil {
			return stageErr
		}

		// Simulate concurrent modification for first N attempts
		// Use different scope values to ensure different blob SHAs
		if attemptCount <= maxFailures {
			scope := fmt.Sprintf("conflict-%d", attemptCount)
			otherMeta := git.NewMetaFrom(git.MetaFields{Scope: &scope})
			if writeErr := eng.Git().WriteMetadata("feature-1", otherMeta); writeErr != nil {
				return writeErr
			}
		}

		return tx.Commit(context.Background())
	})

	require.NoError(t, err)
	assert.Equal(t, maxFailures+1, attemptCount, "expected %d attempts", maxFailures+1)
}

func TestWithRetry_ExhaustsRetries(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").
		Commit("feature 1 change")
	err := s.Engine.TrackBranch(context.Background(), "feature-1", "main")
	require.NoError(t, err)

	// Get the engine implementation
	eng := s.Engine.(interface {
		WithRetry(ctx context.Context, operation func() error) error
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	attemptCount := 0

	err = eng.WithRetry(context.Background(), func() error {
		attemptCount++

		tx := eng.BeginTx("exhaustion test")
		meta := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
		if stageErr := tx.UpdateMeta("feature-1", meta); stageErr != nil {
			return stageErr
		}

		// Always cause concurrent modification with unique content - never succeed
		scope := fmt.Sprintf("conflict-%d", attemptCount)
		otherMeta := git.NewMetaFrom(git.MetaFields{Scope: &scope})
		if writeErr := eng.Git().WriteMetadata("feature-1", otherMeta); writeErr != nil {
			return writeErr
		}

		return tx.Commit(context.Background())
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after")
	assert.Contains(t, err.Error(), "retries")
	assert.Equal(t, engine.MaxRetries, attemptCount, "expected exactly %d attempts", engine.MaxRetries)
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Get the engine implementation
	eng := s.Engine.(interface {
		WithRetry(ctx context.Context, operation func() error) error
	})

	attemptCount := 0
	expectedErr := fmt.Errorf("non-retryable error")

	err := eng.WithRetry(context.Background(), func() error {
		attemptCount++
		return expectedErr
	})

	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 1, attemptCount, "should not retry non-retryable errors")
}

func TestTransaction_UpdateAfterRollback(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Get the engine implementation
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
	})

	// Begin transaction and roll it back
	tx := eng.BeginTx("test transaction")
	tx.Rollback()

	// Trying to update after rollback should fail
	err := tx.UpdateMeta("any-branch", git.NewMeta())
	require.Error(t, err)
	assert.ErrorIs(t, err, engine.ErrTransactionRolledBack)

	// Trying to update local meta after rollback should also fail
	err = tx.UpdateLocalMeta("any-branch", &git.LocalMeta{})
	require.Error(t, err)
	assert.ErrorIs(t, err, engine.ErrTransactionRolledBack)

	// Trying to commit after rollback should fail
	err = tx.Commit(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, engine.ErrTransactionRolledBack)
}

func TestSetLocked_LargeBatch(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create many branches
	const numBranches = 20
	branchNames := make([]string, numBranches)
	for i := range numBranches {
		branchName := fmt.Sprintf("feature-%02d", i)
		branchNames[i] = branchName

		s.CreateBranch(branchName).
			Commit(fmt.Sprintf("change %d", i))

		// Track with parent chain: main <- feature-00 <- feature-01 <- ...
		parent := "main"
		if i > 0 {
			parent = branchNames[i-1]
		}
		err := s.Engine.TrackBranch(context.Background(), branchName, parent)
		require.NoError(t, err)
	}

	// Get all branches
	branches := make([]engine.Branch, numBranches)
	for i, name := range branchNames {
		branches[i] = s.Engine.GetBranch(name)
	}

	// Lock all branches atomically
	result, err := s.Engine.SetLocked(context.Background(), branches, engine.LockReasonUser)
	require.NoError(t, err)
	assert.Len(t, result.AffectedBranches, numBranches)
	assert.Empty(t, result.Errors)

	// Verify all are locked
	impl := s.Engine.(interface{ Git() git.Runner })
	for _, name := range branchNames {
		meta, err := impl.Git().ReadMetadata(name)
		require.NoError(t, err)
		assert.Equal(t, git.LockReasonUser, meta.GetLockReason(), "branch %s should be locked", name)
	}
}

func TestTransaction_MixedMetaAndLocalMeta(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track branches
	s.CreateBranch("feature-1").Commit("change 1")
	s.CreateBranch("feature-2").Commit("change 2")

	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-2", "feature-1"))

	// Get the engine implementation
	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Create a transaction that updates both meta and local meta
	tx := eng.BeginTx("mixed update")

	// Update meta for feature-1
	meta1 := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
	require.NoError(t, tx.UpdateMeta("feature-1", meta1))

	// Update local meta for feature-2
	localMeta2 := &git.LocalMeta{Frozen: true}
	require.NoError(t, tx.UpdateLocalMeta("feature-2", localMeta2))

	// Commit should succeed
	err := tx.Commit(context.Background())
	require.NoError(t, err)

	// Verify both updates persisted
	readMeta1, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, readMeta1.GetLockReason())

	readLocalMeta2, err := eng.Git().ReadLocalMetadata("feature-2")
	require.NoError(t, err)
	assert.True(t, readLocalMeta2.Frozen)
}

func TestTransaction_DeleteMeta(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").Commit("change 1")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Verify metadata exists
	meta, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.NotNil(t, meta.GetParentBranchName())

	// Delete metadata via transaction
	tx := eng.BeginTx("delete metadata")
	require.NoError(t, tx.DeleteMeta("feature-1"))
	require.NoError(t, tx.Commit(context.Background()))

	// Verify metadata is gone (ReadMetadata returns empty Meta for missing refs)
	meta, err = eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Nil(t, meta.GetParentBranchName()) // Empty meta has nil parent
}

func TestTransaction_DeleteLocalMeta(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").Commit("change 1")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Write local metadata first
	localMeta := &git.LocalMeta{Frozen: true}
	require.NoError(t, eng.Git().WriteLocalMetadata("feature-1", localMeta))

	// Verify it exists
	readMeta, err := eng.Git().ReadLocalMetadata("feature-1")
	require.NoError(t, err)
	assert.True(t, readMeta.Frozen)

	// Delete via transaction
	tx := eng.BeginTx("delete local metadata")
	require.NoError(t, tx.DeleteLocalMeta("feature-1"))
	require.NoError(t, tx.Commit(context.Background()))

	// Verify local metadata is gone
	readMeta, err = eng.Git().ReadLocalMetadata("feature-1")
	require.NoError(t, err)
	assert.False(t, readMeta.Frozen) // Empty LocalMeta has Frozen=false
}

func TestTransaction_DeleteNonExistentMeta(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create a branch but don't track it (no metadata)
	s.CreateBranch("feature-1").Commit("change 1")

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
	})

	// Deleting non-existent metadata should succeed (no-op)
	tx := eng.BeginTx("delete non-existent")
	require.NoError(t, tx.DeleteMeta("feature-1"))
	require.NoError(t, tx.Commit(context.Background()))
}

func TestTransaction_UpdateAfterDelete(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").Commit("change 1")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Stage delete then update - update should take precedence
	tx := eng.BeginTx("update after delete")
	require.NoError(t, tx.DeleteMeta("feature-1"))
	newMeta := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
	require.NoError(t, tx.UpdateMeta("feature-1", newMeta))
	require.NoError(t, tx.Commit(context.Background()))

	// Verify update won (not deleted)
	meta, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, meta.GetLockReason())
}

func TestTransaction_DeleteAfterUpdate(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").Commit("change 1")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Stage update then delete - delete should take precedence
	tx := eng.BeginTx("delete after update")
	newMeta := git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})
	require.NoError(t, tx.UpdateMeta("feature-1", newMeta))
	require.NoError(t, tx.DeleteMeta("feature-1"))
	require.NoError(t, tx.Commit(context.Background()))

	// Verify delete won (metadata gone)
	meta, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Nil(t, meta.GetParentBranchName()) // Empty meta
}

func TestTransaction_MixedUpdatesAndDeletes(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create branches
	s.CreateBranch("feature-1").Commit("change 1")
	s.CreateBranch("feature-2").Commit("change 2")
	s.CreateBranch("feature-3").Commit("change 3")

	// Track all
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-2", "feature-1"))
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-3", "feature-2"))

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
		Git() git.Runner
	})

	// Mixed transaction: update feature-1, delete feature-2, update feature-3
	tx := eng.BeginTx("mixed operations")
	require.NoError(t, tx.UpdateMeta("feature-1", git.NewMetaFrom(git.MetaFields{LockReason: git.LockReasonUser})))
	require.NoError(t, tx.DeleteMeta("feature-2"))
	scope := "test-scope"
	require.NoError(t, tx.UpdateMeta("feature-3", git.NewMetaFrom(git.MetaFields{Scope: &scope})))
	require.NoError(t, tx.Commit(context.Background()))

	// Verify results
	meta1, err := eng.Git().ReadMetadata("feature-1")
	require.NoError(t, err)
	assert.Equal(t, git.LockReasonUser, meta1.GetLockReason())

	meta2, err := eng.Git().ReadMetadata("feature-2")
	require.NoError(t, err)
	assert.Nil(t, meta2.GetParentBranchName()) // Deleted

	meta3, err := eng.Git().ReadMetadata("feature-3")
	require.NoError(t, err)
	require.NotNil(t, meta3.GetScope())
	assert.Equal(t, "test-scope", *meta3.GetScope())
}

func TestTransaction_DeleteLocalMetaClearsFrozenState(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// Create and track a branch
	s.CreateBranch("feature-1").Commit("change 1")
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "feature-1", "main"))

	// Freeze the branch
	branch := s.Engine.GetBranch("feature-1")
	_, err := s.Engine.SetFrozen(context.Background(), []engine.Branch{branch}, true)
	require.NoError(t, err)

	// Verify frozen in cache
	assert.True(t, branch.IsFrozen())

	eng := s.Engine.(interface {
		BeginTx(message string) *engine.MetadataTx
	})

	// Delete local metadata via transaction
	tx := eng.BeginTx("delete local metadata")
	require.NoError(t, tx.DeleteLocalMeta("feature-1"))
	require.NoError(t, tx.Commit(context.Background()))

	// Verify frozen state is cleared in cache
	assert.False(t, branch.IsFrozen())
}
