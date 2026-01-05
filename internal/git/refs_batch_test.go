package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestUpdateRefsBatch(t *testing.T) {
	t.Run("atomically updates multiple refs", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Create initial blobs for refs (using non-branch refs since they can be any object)
		sha1, err := runner.CreateBlob("content1")
		require.NoError(t, err)
		sha2, err := runner.CreateBlob("content2")
		require.NoError(t, err)

		// Create initial refs
		err = runner.UpdateRef("refs/test/ref1", sha1)
		require.NoError(t, err)
		err = runner.UpdateRef("refs/test/ref2", sha2)
		require.NoError(t, err)

		// Create new blobs
		newSha1, err := runner.CreateBlob("new-content1")
		require.NoError(t, err)
		newSha2, err := runner.CreateBlob("new-content2")
		require.NoError(t, err)

		// Update both refs atomically
		updates := []git.RefUpdate{
			{RefName: "refs/test/ref1", NewSHA: newSha1, OldSHA: sha1},
			{RefName: "refs/test/ref2", NewSHA: newSha2, OldSHA: sha2},
		}
		err = runner.UpdateRefsBatch(ctx, updates)
		require.NoError(t, err)

		// Verify both refs are updated
		ref1, err := runner.GetRef("refs/test/ref1")
		require.NoError(t, err)
		require.Equal(t, newSha1, ref1)

		ref2, err := runner.GetRef("refs/test/ref2")
		require.NoError(t, err)
		require.Equal(t, newSha2, ref2)
	})

	t.Run("fails atomically when OldSHA does not match", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Create initial blobs and refs
		sha1, err := runner.CreateBlob("content1")
		require.NoError(t, err)
		sha2, err := runner.CreateBlob("content2")
		require.NoError(t, err)

		err = runner.UpdateRef("refs/test/ref1", sha1)
		require.NoError(t, err)
		err = runner.UpdateRef("refs/test/ref2", sha2)
		require.NoError(t, err)

		// Create new blobs
		newSha1, err := runner.CreateBlob("new-content1")
		require.NoError(t, err)
		newSha2, err := runner.CreateBlob("new-content2")
		require.NoError(t, err)

		// Try to update with wrong OldSHA - should fail atomically
		wrongOldSha := "0000000000000000000000000000000000000000"
		updates := []git.RefUpdate{
			{RefName: "refs/test/ref1", NewSHA: newSha1, OldSHA: sha1},        // correct
			{RefName: "refs/test/ref2", NewSHA: newSha2, OldSHA: wrongOldSha}, // wrong
		}
		err = runner.UpdateRefsBatch(ctx, updates)
		require.Error(t, err)

		// Verify neither ref was updated (atomic rollback)
		ref1, err := runner.GetRef("refs/test/ref1")
		require.NoError(t, err)
		require.Equal(t, sha1, ref1, "ref1 should not have been updated")

		ref2, err := runner.GetRef("refs/test/ref2")
		require.NoError(t, err)
		require.Equal(t, sha2, ref2, "ref2 should not have been updated")
	})

	t.Run("handles empty updates", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		err := runner.UpdateRefsBatch(ctx, []git.RefUpdate{})
		require.NoError(t, err)
	})

	t.Run("updates without OldSHA verification", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		sha, err := runner.CreateBlob("content")
		require.NoError(t, err)

		// Update without OldSHA - creates new ref
		updates := []git.RefUpdate{
			{RefName: "refs/test/newref", NewSHA: sha},
		}
		err = runner.UpdateRefsBatch(ctx, updates)
		require.NoError(t, err)

		ref, err := runner.GetRef("refs/test/newref")
		require.NoError(t, err)
		require.Equal(t, sha, ref)
	})
}

func TestUpdateRefsBatchWithLog(t *testing.T) {
	t.Run("updates metadata refs with reflog message", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		sha, err := runner.CreateBlob(`{"parent":"main"}`)
		require.NoError(t, err)

		// Use metadata ref (not branch ref) since blobs can be stored there
		updates := []git.RefUpdate{
			{RefName: "refs/stackit/metadata/testbranch", NewSHA: sha},
		}
		err = runner.UpdateRefsBatchWithLog(ctx, updates, "test reflog message")
		require.NoError(t, err)

		// Verify ref was updated
		ref, err := runner.GetRef("refs/stackit/metadata/testbranch")
		require.NoError(t, err)
		require.Equal(t, sha, ref)
	})

	t.Run("updates branch refs with commits", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Get current commit SHA
		commitSha, err := runner.GetCurrentRevision(ctx)
		require.NoError(t, err)

		// Create a new branch ref using commit
		updates := []git.RefUpdate{
			{RefName: "refs/heads/testbranch", NewSHA: commitSha},
		}
		err = runner.UpdateRefsBatchWithLog(ctx, updates, "create branch")
		require.NoError(t, err)

		// Verify ref was updated
		ref, err := runner.GetRef("refs/heads/testbranch")
		require.NoError(t, err)
		require.Equal(t, commitSha, ref)

		// Verify reflog entry was created
		output, err := scene.Repo.RunGitCommandAndGetOutput("reflog", "show", "refs/heads/testbranch", "--format=%gs")
		require.NoError(t, err)
		require.Contains(t, output, "create branch")
	})
}

func TestDeleteRefsBatch(t *testing.T) {
	t.Run("atomically deletes multiple refs", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Create refs to delete
		sha1, err := runner.CreateBlob("content1")
		require.NoError(t, err)
		sha2, err := runner.CreateBlob("content2")
		require.NoError(t, err)

		err = runner.UpdateRef("refs/stackit/metadata/branch1", sha1)
		require.NoError(t, err)
		err = runner.UpdateRef("refs/stackit/metadata/branch2", sha2)
		require.NoError(t, err)

		// Delete both refs atomically
		err = runner.DeleteRefsBatch(ctx, []string{
			"refs/stackit/metadata/branch1",
			"refs/stackit/metadata/branch2",
		})
		require.NoError(t, err)

		// Verify both refs are deleted
		_, err = runner.GetRef("refs/stackit/metadata/branch1")
		require.Error(t, err)

		_, err = runner.GetRef("refs/stackit/metadata/branch2")
		require.Error(t, err)
	})

	t.Run("handles empty deletion list", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		err := runner.DeleteRefsBatch(ctx, []string{})
		require.NoError(t, err)
	})

	t.Run("handles deleting non-existent ref gracefully", func(t *testing.T) {
		// Note: git update-ref --stdin with delete does NOT fail on non-existent refs
		// It silently succeeds, which is different from individual delete operations
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Create one ref
		sha, err := runner.CreateBlob("content")
		require.NoError(t, err)
		err = runner.UpdateRef("refs/test/exists", sha)
		require.NoError(t, err)

		// Try to delete one that exists and one that doesn't
		// This should succeed (git update-ref --stdin is lenient with deletes)
		err = runner.DeleteRefsBatch(ctx, []string{
			"refs/test/exists",
			"refs/test/does-not-exist",
		})
		require.NoError(t, err)

		// The existing ref should have been deleted
		_, err = runner.GetRef("refs/test/exists")
		require.Error(t, err)
	})
}

func TestRefUpdateIntegration(t *testing.T) {
	t.Run("simulates restack atomic update pattern", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Get initial commit for branch ref
		initialCommit, err := runner.GetCurrentRevision(ctx)
		require.NoError(t, err)

		// Create a second commit for the "rebased" state
		err = scene.Repo.CreateChangeAndCommit("second", "second commit")
		require.NoError(t, err)
		rebasedCommit, err := runner.GetCurrentRevision(ctx)
		require.NoError(t, err)

		// Create branch at initial commit (simulating pre-restack state)
		err = scene.Repo.RunGitCommand("branch", "feature", initialCommit)
		require.NoError(t, err)

		// Create metadata ref
		metaSha, err := runner.CreateBlob(`{"parent":"main"}`)
		require.NoError(t, err)
		err = runner.UpdateRef("refs/stackit/metadata/feature", metaSha)
		require.NoError(t, err)

		// Prepare new metadata
		newMetaSha, err := runner.CreateBlob(`{"parent":"main","parentRev":"abc123"}`)
		require.NoError(t, err)

		// Simulate restack: atomically update branch ref (to rebased commit) and metadata ref
		updates := []git.RefUpdate{
			{RefName: "refs/heads/feature", NewSHA: rebasedCommit, OldSHA: initialCommit},
			{RefName: "refs/stackit/metadata/feature", NewSHA: newMetaSha, OldSHA: metaSha},
		}
		err = runner.UpdateRefsBatch(ctx, updates)
		require.NoError(t, err)

		// Verify both are updated
		branchRef, err := runner.GetRef("refs/heads/feature")
		require.NoError(t, err)
		require.Equal(t, rebasedCommit, branchRef)

		metaRef, err := runner.GetRef("refs/stackit/metadata/feature")
		require.NoError(t, err)
		require.Equal(t, newMetaSha, metaRef)
	})

	t.Run("simulates undo restore pattern", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		runner := git.NewRunnerWithPath(scene.Dir)
		ctx := context.Background()

		// Get initial commit
		commit1, err := runner.GetCurrentRevision(ctx)
		require.NoError(t, err)

		// Create second commit
		err = scene.Repo.CreateChangeAndCommit("second", "second commit")
		require.NoError(t, err)
		commit2, err := runner.GetCurrentRevision(ctx)
		require.NoError(t, err)

		// Create branches at current commit (simulating "current state")
		err = scene.Repo.RunGitCommand("branch", "branch1", commit2)
		require.NoError(t, err)
		err = scene.Repo.RunGitCommand("branch", "branch2", commit2)
		require.NoError(t, err)

		// Create current metadata refs
		currentMeta, _ := runner.CreateBlob(`{"parent":"current"}`)
		_ = runner.UpdateRef("refs/stackit/metadata/branch1", currentMeta)
		_ = runner.UpdateRef("refs/stackit/metadata/branch2", currentMeta)

		// Snapshot state (what we want to restore to) - using commit1 for branches
		snapshotMeta1, _ := runner.CreateBlob(`{"parent":"main"}`)
		snapshotMeta2, _ := runner.CreateBlob(`{"parent":"branch1"}`)

		// Restore all refs atomically
		updates := []git.RefUpdate{
			{RefName: "refs/heads/branch1", NewSHA: commit1},
			{RefName: "refs/heads/branch2", NewSHA: commit1},
			{RefName: "refs/stackit/metadata/branch1", NewSHA: snapshotMeta1},
			{RefName: "refs/stackit/metadata/branch2", NewSHA: snapshotMeta2},
		}
		err = runner.UpdateRefsBatchWithLog(ctx, updates, "stackit undo: restored to before sync")
		require.NoError(t, err)

		// Verify all refs are restored
		ref1, _ := runner.GetRef("refs/heads/branch1")
		require.Equal(t, commit1, ref1)

		ref2, _ := runner.GetRef("refs/heads/branch2")
		require.Equal(t, commit1, ref2)

		meta1, _ := runner.GetRef("refs/stackit/metadata/branch1")
		require.Equal(t, snapshotMeta1, meta1)

		meta2, _ := runner.GetRef("refs/stackit/metadata/branch2")
		require.Equal(t, snapshotMeta2, meta2)
	})
}
