package git_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestUpdateRefs(t *testing.T) {
	t.Parallel()

	t.Run("atomically updates multiple refs", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Create initial blobs for refs (using non-branch refs since they can be any object)
		sha1, err := git.One(runner.CreateBlobs(context.Background(), "content1"))
		require.NoError(t, err)
		sha2, err := git.One(runner.CreateBlobs(context.Background(), "content2"))
		require.NoError(t, err)

		// Create initial refs
		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/test/ref1", NewSHA: sha1}}, "")
		require.NoError(t, err)
		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/test/ref2", NewSHA: sha2}}, "")
		require.NoError(t, err)

		// Create new blobs
		newSha1, err := git.One(runner.CreateBlobs(context.Background(), "new-content1"))
		require.NoError(t, err)
		newSha2, err := git.One(runner.CreateBlobs(context.Background(), "new-content2"))
		require.NoError(t, err)

		// Update both refs atomically
		updates := []git.RefUpdate{
			{RefName: "refs/test/ref1", NewSHA: newSha1, OldSHA: sha1},
			{RefName: "refs/test/ref2", NewSHA: newSha2, OldSHA: sha2},
		}
		err = runner.UpdateRefs(ctx, updates, "")
		require.NoError(t, err)

		// Verify both refs are updated
		ref1, err := runner.ReadRevisions(context.Background(), "refs/test/ref1").One()
		require.NoError(t, err)
		require.Equal(t, newSha1, ref1)

		ref2, err := runner.ReadRevisions(context.Background(), "refs/test/ref2").One()
		require.NoError(t, err)
		require.Equal(t, newSha2, ref2)
	})

	t.Run("fails atomically when OldSHA does not match", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Create initial blobs and refs
		sha1, err := git.One(runner.CreateBlobs(context.Background(), "content1"))
		require.NoError(t, err)
		sha2, err := git.One(runner.CreateBlobs(context.Background(), "content2"))
		require.NoError(t, err)

		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/test/ref1", NewSHA: sha1}}, "")
		require.NoError(t, err)
		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/test/ref2", NewSHA: sha2}}, "")
		require.NoError(t, err)

		// Create new blobs
		newSha1, err := git.One(runner.CreateBlobs(context.Background(), "new-content1"))
		require.NoError(t, err)
		newSha2, err := git.One(runner.CreateBlobs(context.Background(), "new-content2"))
		require.NoError(t, err)

		// Try to update with wrong OldSHA - should fail atomically
		wrongOldSha := "0000000000000000000000000000000000000000"
		updates := []git.RefUpdate{
			{RefName: "refs/test/ref1", NewSHA: newSha1, OldSHA: sha1},        // correct
			{RefName: "refs/test/ref2", NewSHA: newSha2, OldSHA: wrongOldSha}, // wrong
		}
		err = runner.UpdateRefs(ctx, updates, "")
		require.Error(t, err)

		// Verify neither ref was updated (atomic rollback)
		ref1, err := runner.ReadRevisions(context.Background(), "refs/test/ref1").One()
		require.NoError(t, err)
		require.Equal(t, sha1, ref1, "ref1 should not have been updated")

		ref2, err := runner.ReadRevisions(context.Background(), "refs/test/ref2").One()
		require.NoError(t, err)
		require.Equal(t, sha2, ref2, "ref2 should not have been updated")
	})

	t.Run("handles empty updates", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		err := runner.UpdateRefs(ctx, []git.RefUpdate{}, "")
		require.NoError(t, err)
	})

	t.Run("updates without OldSHA verification", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		sha, err := git.One(runner.CreateBlobs(context.Background(), "content"))
		require.NoError(t, err)

		// Update without OldSHA - creates new ref
		updates := []git.RefUpdate{
			{RefName: "refs/test/newref", NewSHA: sha},
		}
		err = runner.UpdateRefs(ctx, updates, "")
		require.NoError(t, err)

		ref, err := runner.ReadRevisions(context.Background(), "refs/test/newref").One()
		require.NoError(t, err)
		require.Equal(t, sha, ref)
	})
}

func TestUpdateRefsWithLog(t *testing.T) {
	t.Parallel()

	t.Run("updates metadata refs with reflog message", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		sha, err := git.One(runner.CreateBlobs(context.Background(), `{"parent":"main"}`))
		require.NoError(t, err)

		// Use metadata ref (not branch ref) since blobs can be stored there
		updates := []git.RefUpdate{
			{RefName: "refs/stackit/metadata/testbranch", NewSHA: sha},
		}
		err = runner.UpdateRefs(ctx, updates, "test reflog message")
		require.NoError(t, err)

		// Verify ref was updated
		ref, err := runner.ReadRevisions(context.Background(), "refs/stackit/metadata/testbranch").One()
		require.NoError(t, err)
		require.Equal(t, sha, ref)
	})

	t.Run("updates branch refs with commits", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Get current commit SHA
		commitSha, err := runner.ReadRevisions(ctx, "HEAD").One()
		require.NoError(t, err)

		// Create a new branch ref using commit
		updates := []git.RefUpdate{
			{RefName: "refs/heads/testbranch", NewSHA: commitSha},
		}
		err = runner.UpdateRefs(ctx, updates, "create branch")
		require.NoError(t, err)

		// Verify ref was updated
		ref, err := runner.ReadRevisions(context.Background(), "refs/heads/testbranch").One()
		require.NoError(t, err)
		require.Equal(t, commitSha, ref)

		// Verify reflog entry was created
		output, err := scene.Repo.RunGitCommandAndGetOutput("reflog", "show", "refs/heads/testbranch", "--format=%gs")
		require.NoError(t, err)
		require.Contains(t, output, "create branch")
	})
}

func TestDeleteRefs(t *testing.T) {
	t.Parallel()

	t.Run("atomically deletes multiple refs", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Create refs to delete
		sha1, err := git.One(runner.CreateBlobs(context.Background(), "content1"))
		require.NoError(t, err)
		sha2, err := git.One(runner.CreateBlobs(context.Background(), "content2"))
		require.NoError(t, err)

		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/stackit/metadata/branch1", NewSHA: sha1}}, "")
		require.NoError(t, err)
		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/stackit/metadata/branch2", NewSHA: sha2}}, "")
		require.NoError(t, err)

		// Delete both refs atomically
		err = runner.DeleteRefs(ctx, []string{
			"refs/stackit/metadata/branch1",
			"refs/stackit/metadata/branch2",
		}...)
		require.NoError(t, err)

		// Verify both refs are deleted
		_, err = runner.ReadRevisions(context.Background(), "refs/stackit/metadata/branch1").One()
		require.Error(t, err)

		_, err = runner.ReadRevisions(context.Background(), "refs/stackit/metadata/branch2").One()
		require.Error(t, err)
	})

	t.Run("handles empty deletion list", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		err := runner.DeleteRefs(ctx, []string{}...)
		require.NoError(t, err)
	})

	t.Run("handles deleting non-existent ref gracefully", func(t *testing.T) {
		t.Parallel()
		// Note: git update-ref --stdin with delete does NOT fail on non-existent refs
		// It silently succeeds, which is different from individual delete operations
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Create one ref
		sha, err := git.One(runner.CreateBlobs(context.Background(), "content"))
		require.NoError(t, err)
		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/test/exists", NewSHA: sha}}, "")
		require.NoError(t, err)

		// Try to delete one that exists and one that doesn't
		// This should succeed (git update-ref --stdin is lenient with deletes)
		err = runner.DeleteRefs(ctx, []string{
			"refs/test/exists",
			"refs/test/does-not-exist",
		}...)
		require.NoError(t, err)

		// The existing ref should have been deleted
		_, err = runner.ReadRevisions(context.Background(), "refs/test/exists").One()
		require.Error(t, err)
	})
}

func TestRefUpdateIntegration(t *testing.T) {
	t.Parallel()

	t.Run("simulates restack atomic update pattern", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Get initial commit for branch ref
		initialCommit, err := runner.ReadRevisions(ctx, "HEAD").One()
		require.NoError(t, err)

		// Create a second commit for the "rebased" state
		err = scene.Repo.CreateChangeAndCommit("second", "second commit")
		require.NoError(t, err)
		rebasedCommit, err := runner.ReadRevisions(ctx, "HEAD").One()
		require.NoError(t, err)

		// Create branch at initial commit (simulating pre-restack state)
		err = scene.Repo.RunGitCommand("branch", "feature", initialCommit)
		require.NoError(t, err)

		// Create metadata ref
		metaSha, err := git.One(runner.CreateBlobs(context.Background(), `{"parent":"main"}`))
		require.NoError(t, err)
		err = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/stackit/metadata/feature", NewSHA: metaSha}}, "")
		require.NoError(t, err)

		// Prepare new metadata
		newMetaSha, err := git.One(runner.CreateBlobs(context.Background(), `{"parent":"main","parentRev":"abc123"}`))
		require.NoError(t, err)

		// Simulate restack: atomically update branch ref (to rebased commit) and metadata ref
		updates := []git.RefUpdate{
			{RefName: "refs/heads/feature", NewSHA: rebasedCommit, OldSHA: initialCommit},
			{RefName: "refs/stackit/metadata/feature", NewSHA: newMetaSha, OldSHA: metaSha},
		}
		err = runner.UpdateRefs(ctx, updates, "")
		require.NoError(t, err)

		// Verify both are updated
		branchRef, err := runner.ReadRevisions(context.Background(), "refs/heads/feature").One()
		require.NoError(t, err)
		require.Equal(t, rebasedCommit, branchRef)

		metaRef, err := runner.ReadRevisions(context.Background(), "refs/stackit/metadata/feature").One()
		require.NoError(t, err)
		require.Equal(t, newMetaSha, metaRef)
	})

	t.Run("simulates undo restore pattern", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)

		runner := git.NewRunnerWithPath(scene.Dir, nil)
		ctx := context.Background()

		// Get initial commit
		commit1, err := runner.ReadRevisions(ctx, "HEAD").One()
		require.NoError(t, err)

		// Create second commit
		err = scene.Repo.CreateChangeAndCommit("second", "second commit")
		require.NoError(t, err)
		commit2, err := runner.ReadRevisions(ctx, "HEAD").One()
		require.NoError(t, err)

		// Create branches at current commit (simulating "current state")
		err = scene.Repo.RunGitCommand("branch", "branch1", commit2)
		require.NoError(t, err)
		err = scene.Repo.RunGitCommand("branch", "branch2", commit2)
		require.NoError(t, err)

		// Create current metadata refs
		currentMeta, _ := git.One(runner.CreateBlobs(context.Background(), `{"parent":"current"}`))
		_ = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/stackit/metadata/branch1", NewSHA: currentMeta}}, "")
		_ = runner.UpdateRefs(context.Background(), []git.RefUpdate{{RefName: "refs/stackit/metadata/branch2", NewSHA: currentMeta}}, "")

		// Snapshot state (what we want to restore to) - using commit1 for branches
		snapshotMeta1, _ := git.One(runner.CreateBlobs(context.Background(), `{"parent":"main"}`))
		snapshotMeta2, _ := git.One(runner.CreateBlobs(context.Background(), `{"parent":"branch1"}`))

		// Restore all refs atomically
		updates := []git.RefUpdate{
			{RefName: "refs/heads/branch1", NewSHA: commit1},
			{RefName: "refs/heads/branch2", NewSHA: commit1},
			{RefName: "refs/stackit/metadata/branch1", NewSHA: snapshotMeta1},
			{RefName: "refs/stackit/metadata/branch2", NewSHA: snapshotMeta2},
		}
		err = runner.UpdateRefs(ctx, updates, "stackit undo: restored to before sync")
		require.NoError(t, err)

		// Verify all refs are restored
		ref1, _ := runner.ReadRevisions(context.Background(), "refs/heads/branch1").One()
		require.Equal(t, commit1, ref1)

		ref2, _ := runner.ReadRevisions(context.Background(), "refs/heads/branch2").One()
		require.Equal(t, commit1, ref2)

		meta1, _ := runner.ReadRevisions(context.Background(), "refs/stackit/metadata/branch1").One()
		require.Equal(t, snapshotMeta1, meta1)

		meta2, _ := runner.ReadRevisions(context.Background(), "refs/stackit/metadata/branch2").One()
		require.Equal(t, snapshotMeta2, meta2)
	})
}
