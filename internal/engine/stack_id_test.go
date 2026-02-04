package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
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

func TestCreateStackRef(t *testing.T) {
	t.Parallel()

	t.Run("creates stack ref with provided metadata", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		stackID := "test-stack-id"
		createdAt := time.Now().Truncate(time.Second)
		meta := &git.StackMeta{
			ID:          stackID,
			Title:       "Test Stack",
			Description: "A test stack description",
			CreatedAt:   createdAt,
			CreatedBy:   "test-user",
		}

		err := s.Engine.CreateStackRef(stackID, meta)
		require.NoError(t, err)

		// Verify GetStackMeta returns the correct metadata
		retrieved, err := s.Engine.GetStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, stackID, retrieved.ID)
		require.Equal(t, "Test Stack", retrieved.Title)
		require.Equal(t, "A test stack description", retrieved.Description)
		require.Equal(t, createdAt, retrieved.CreatedAt)
		require.Equal(t, "test-user", retrieved.CreatedBy)
	})

	t.Run("creates stack ref with nil metadata using defaults", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		stackID := "default-meta-stack"

		err := s.Engine.CreateStackRef(stackID, nil)
		require.NoError(t, err)

		// Verify GetStackMeta returns metadata with defaults
		retrieved, err := s.Engine.GetStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, stackID, retrieved.ID)
		require.False(t, retrieved.CreatedAt.IsZero())
	})
}

func TestGetStackMeta(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for non-existent stack ID", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		meta, err := s.Engine.GetStackMeta("non-existent-stack-id")
		require.NoError(t, err)
		require.Nil(t, meta)
	})

	t.Run("returns correct metadata for existing stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		stackID := "existing-stack"
		createdAt := time.Now().Truncate(time.Second)
		meta := &git.StackMeta{
			ID:          stackID,
			Title:       "Existing Stack",
			Description: "This stack exists",
			CreatedAt:   createdAt,
			CreatedBy:   "author",
		}

		err := s.Engine.CreateStackRef(stackID, meta)
		require.NoError(t, err)

		// Retrieve and verify
		retrieved, err := s.Engine.GetStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, stackID, retrieved.ID)
		require.Equal(t, "Existing Stack", retrieved.Title)
		require.Equal(t, "This stack exists", retrieved.Description)
		require.Equal(t, createdAt, retrieved.CreatedAt)
		require.Equal(t, "author", retrieved.CreatedBy)
	})

	t.Run("returns different metadata for different stacks", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		stack1ID := "stack-1"
		stack2ID := "stack-2"

		meta1 := &git.StackMeta{
			ID:        stack1ID,
			Title:     "Stack One",
			CreatedAt: time.Now().Truncate(time.Second),
		}
		meta2 := &git.StackMeta{
			ID:        stack2ID,
			Title:     "Stack Two",
			CreatedAt: time.Now().Truncate(time.Second),
		}

		err := s.Engine.CreateStackRef(stack1ID, meta1)
		require.NoError(t, err)
		err = s.Engine.CreateStackRef(stack2ID, meta2)
		require.NoError(t, err)

		retrieved1, err := s.Engine.GetStackMeta(stack1ID)
		require.NoError(t, err)
		require.Equal(t, "Stack One", retrieved1.Title)

		retrieved2, err := s.Engine.GetStackMeta(stack2ID)
		require.NoError(t, err)
		require.Equal(t, "Stack Two", retrieved2.Title)
	})
}
