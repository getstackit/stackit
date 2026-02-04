package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestGenerateStackID(t *testing.T) {
	t.Parallel()

	t.Run("generates sortable ID with timestamp prefix", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		id1 := s.Engine.GenerateStackID("feature")
		id2 := s.Engine.GenerateStackID("another")

		// IDs should be different
		require.NotEqual(t, id1, id2)

		// Both should contain a timestamp (large number prefix)
		require.True(t, strings.Contains(id1, "-feature"))
		require.True(t, strings.Contains(id2, "-another"))
	})

	t.Run("sanitizes branch name in ID", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		id := s.Engine.GenerateStackID("feature/with/slashes")

		// Slashes should be replaced with hyphens
		require.NotContains(t, id, "/")
		require.Contains(t, id, "feature-with-slashes")
	})
}

func TestGetStackID(t *testing.T) {
	t.Parallel()

	t.Run("returns empty for trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		trunk := s.Engine.Trunk()
		stackID := s.Engine.GetStackID(trunk)
		require.Empty(t, stackID)
	})

	t.Run("returns empty for untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("untracked").
			Commit("untracked commit")

		branch := s.Engine.GetBranch("untracked")
		stackID := s.Engine.GetStackID(branch)
		require.Empty(t, stackID)
	})

	t.Run("returns stack ID for tracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		branch := s.Engine.GetBranch("feature")
		stackID := s.Engine.GetStackID(branch)
		require.NotEmpty(t, stackID)
		require.Contains(t, stackID, "feature")
	})

	t.Run("child inherits parent stack ID", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "root")

		rootBranch := s.Engine.GetBranch("root")
		childBranch := s.Engine.GetBranch("child")

		rootStackID := s.Engine.GetStackID(rootBranch)
		childStackID := s.Engine.GetStackID(childBranch)

		require.NotEmpty(t, rootStackID)
		require.Equal(t, rootStackID, childStackID)
	})
}

func TestSetStackID(t *testing.T) {
	t.Parallel()

	t.Run("sets stack ID on branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		branch := s.Engine.GetBranch("feature")
		newStackID := "new-stack-id"

		err := s.Engine.SetStackID(context.Background(), branch, newStackID)
		require.NoError(t, err)

		// Verify it was set
		readStackID := s.Engine.GetStackID(branch)
		require.Equal(t, newStackID, readStackID)
	})
}

func TestStackIDInheritance(t *testing.T) {
	t.Parallel()

	t.Run("multiple children inherit same stack ID", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main").
			Checkout("root").
			CreateBranch("child1").
			Commit("child1 commit").
			TrackBranch("child1", "root").
			Checkout("root").
			CreateBranch("child2").
			Commit("child2 commit").
			TrackBranch("child2", "root")

		root := s.Engine.GetBranch("root")
		child1 := s.Engine.GetBranch("child1")
		child2 := s.Engine.GetBranch("child2")

		rootID := s.Engine.GetStackID(root)
		child1ID := s.Engine.GetStackID(child1)
		child2ID := s.Engine.GetStackID(child2)

		require.NotEmpty(t, rootID)
		require.Equal(t, rootID, child1ID)
		require.Equal(t, rootID, child2ID)
	})

	t.Run("grandchildren inherit same stack ID", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "root").
			CreateBranch("grandchild").
			Commit("grandchild commit").
			TrackBranch("grandchild", "child")

		root := s.Engine.GetBranch("root")
		grandchild := s.Engine.GetBranch("grandchild")

		rootID := s.Engine.GetStackID(root)
		grandchildID := s.Engine.GetStackID(grandchild)

		require.NotEmpty(t, rootID)
		require.Equal(t, rootID, grandchildID)
	})

	t.Run("separate stacks have different IDs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("stack1").
			Commit("stack1 commit").
			TrackBranch("stack1", "main").
			Checkout("main").
			CreateBranch("stack2").
			Commit("stack2 commit").
			TrackBranch("stack2", "main")

		stack1 := s.Engine.GetBranch("stack1")
		stack2 := s.Engine.GetBranch("stack2")

		id1 := s.Engine.GetStackID(stack1)
		id2 := s.Engine.GetStackID(stack2)

		require.NotEmpty(t, id1)
		require.NotEmpty(t, id2)
		require.NotEqual(t, id1, id2)
	})
}
