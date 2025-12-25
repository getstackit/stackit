package git_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestGetCommitSubject(t *testing.T) {
	t.Run("returns the oldest commit subject for a branch with multiple commits", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Initialize git repo
		err := git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		branchName := "feature"
		err = scene.Repo.CreateAndCheckoutBranch(branchName)
		require.NoError(t, err)

		// Create two commits
		oldestSubject := "feat: first commit"
		err = scene.Repo.CreateChangeAndCommit(oldestSubject, "change1")
		require.NoError(t, err)

		newestSubject := "feat: second commit"
		err = scene.Repo.CreateChangeAndCommit(newestSubject, "change2")
		require.NoError(t, err)

		// Set up metadata so GetCommitSubject knows the range
		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		parentName := "main"
		meta := &git.Meta{
			ParentBranchName:     &parentName,
			ParentBranchRevision: &mainRev,
		}
		err = git.WriteMetadataRef(branchName, meta)
		require.NoError(t, err)

		// Test GetCommitSubject
		subject, err := git.GetCommitSubject(branchName)
		require.NoError(t, err)
		require.Equal(t, oldestSubject, subject, "Should return the oldest commit subject on the branch")
	})

	t.Run("returns the subject for a branch with a single commit", func(t *testing.T) {
		scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
			return s.Repo.CreateChangeAndCommit("initial", "init")
		})

		// Initialize git repo
		err := git.InitDefaultRepoInDir(scene.Dir)
		require.NoError(t, err)

		branchName := "feature"
		err = scene.Repo.CreateAndCheckoutBranch(branchName)
		require.NoError(t, err)

		subjectText := "feat: single commit"
		err = scene.Repo.CreateChangeAndCommit(subjectText, "change1")
		require.NoError(t, err)

		mainRev, err := scene.Repo.GetRef("main")
		require.NoError(t, err)

		parentName := "main"
		meta := &git.Meta{
			ParentBranchName:     &parentName,
			ParentBranchRevision: &mainRev,
		}
		err = git.WriteMetadataRef(branchName, meta)
		require.NoError(t, err)

		subject, err := git.GetCommitSubject(branchName)
		require.NoError(t, err)
		require.Equal(t, subjectText, subject)
	})
}
