package engine_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestGenerateStackID(t *testing.T) {
	t.Parallel()

	t.Run("generates sortable ID with timestamp prefix", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		id1 := engine.Impl(s.Engine).GenerateStackID("feature")
		id2 := engine.Impl(s.Engine).GenerateStackID("another")

		// IDs should be different
		require.NotEqual(t, id1, id2)

		// Both should contain a timestamp (large number prefix)
		require.Contains(t, id1, "-feature")
		require.Contains(t, id2, "-another")
	})

	t.Run("sanitizes branch name in ID", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		id := engine.Impl(s.Engine).GenerateStackID("feature/with/slashes")

		// Slashes should be replaced with hyphens
		require.NotContains(t, id, "/")
		require.Contains(t, id, "feature-with-slashes")
	})
}

func TestGetStackID(t *testing.T) {
	t.Parallel()

	// The read-only lookups and the EnsureStackID write share one scene. They
	// run sequentially, with EnsureStackID last, because it mutates "feature".
	t.Run("shared scene", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("untracked").
			Commit("untracked commit").
			Checkout("main").
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		t.Run("returns empty for trunk", func(t *testing.T) {
			trunk := s.Engine.Trunk()
			stackID := s.Engine.GetStackID(trunk)
			require.Empty(t, stackID)
		})

		t.Run("returns empty for untracked branch", func(t *testing.T) {
			branch := s.Engine.GetBranch("untracked")
			stackID := s.Engine.GetStackID(branch)
			require.Empty(t, stackID)
		})

		t.Run("returns empty for tracked branch without explicit stack ID", func(t *testing.T) {
			branch := s.Engine.GetBranch("feature")
			// Stack IDs are not auto-created during TrackBranch
			stackID := s.Engine.GetStackID(branch)
			require.Empty(t, stackID)
		})

		t.Run("returns stack ID after EnsureStackID", func(t *testing.T) {
			branch := s.Engine.GetBranch("feature")
			stackID, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), branch)
			require.NoError(t, err)
			require.NotEmpty(t, stackID)
			require.Contains(t, stackID, "feature")

			// GetStackID should now return the same ID
			readID := s.Engine.GetStackID(branch)
			require.Equal(t, stackID, readID)
		})
	})

	t.Run("child inherits parent stack ID after EnsureStackID", func(t *testing.T) {
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

		// Ensure stack ID from child - should propagate to all branches in stack
		childStackID, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), childBranch)
		require.NoError(t, err)

		rootStackID := s.Engine.GetStackID(rootBranch)

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

		err := s.Engine.SetStackID(t.Context(), engine.BranchesOf(branch), newStackID)
		require.NoError(t, err)

		// Verify it was set
		readStackID := s.Engine.GetStackID(branch)
		require.Equal(t, newStackID, readStackID)
	})
}

func TestStackIDInheritance(t *testing.T) {
	t.Parallel()

	t.Run("multiple children inherit same stack ID after EnsureStackID", func(t *testing.T) {
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

		// EnsureStackID from any branch propagates to whole stack
		rootID, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), root)
		require.NoError(t, err)

		child1ID := s.Engine.GetStackID(child1)
		child2ID := s.Engine.GetStackID(child2)

		require.NotEmpty(t, rootID)
		require.Equal(t, rootID, child1ID)
		require.Equal(t, rootID, child2ID)
	})

	t.Run("grandchildren inherit same stack ID after EnsureStackID", func(t *testing.T) {
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

		// EnsureStackID from grandchild propagates to all ancestors and descendants
		grandchildID, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), grandchild)
		require.NoError(t, err)

		rootID := s.Engine.GetStackID(root)

		require.NotEmpty(t, rootID)
		require.Equal(t, rootID, grandchildID)
	})

	t.Run("separate stacks have different IDs after EnsureStackID", func(t *testing.T) {
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

		id1, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), stack1)
		require.NoError(t, err)
		id2, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), stack2)
		require.NoError(t, err)

		require.NotEmpty(t, id1)
		require.NotEmpty(t, id2)
		require.NotEqual(t, id1, id2)
	})

	t.Run("assigning branches to a new stack creates a new stack ID", func(t *testing.T) {
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
		child := s.Engine.GetBranch("child")
		rootID, err := engine.Impl(s.Engine).EnsureStackID(t.Context(), root)
		require.NoError(t, err)
		require.Equal(t, rootID, s.Engine.GetStackID(child))

		_, err = s.Engine.AssignBranchesToNewStack(t.Context(), child, engine.Branches{child, s.Engine.GetBranch("grandchild")})
		require.NoError(t, err)

		child = s.Engine.GetBranch("child")
		grandchild := s.Engine.GetBranch("grandchild")
		childID := s.Engine.GetStackID(child)
		require.NotEmpty(t, childID)
		require.NotEqual(t, rootID, childID)
		require.Equal(t, childID, s.Engine.GetStackID(grandchild))
		require.Equal(t, rootID, s.Engine.GetStackID(root))
	})
}

// TestStackRefMeta covers CreateStackRef and GetStackMeta on one shared scene.
// Every case uses a distinct stack ID, so they run sequentially without
// interfering; the missing-ID read runs first.
func TestStackRefMeta(t *testing.T) { //nolint:tparallel // subtests share one fixture and run in order
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()

	t.Run("GetStackMeta returns nil for non-existent stack ID", func(t *testing.T) {
		meta, err := engine.Impl(s.Engine).GetStackMeta("non-existent-stack-id")
		require.NoError(t, err)
		require.Nil(t, meta)
	})

	t.Run("CreateStackRef creates stack ref with provided metadata", func(t *testing.T) {
		stackID := "test-stack-id"
		createdAt := time.Now().Truncate(time.Second)
		meta := &git.StackMeta{
			ID:          stackID,
			Title:       "Test Stack",
			Description: "A test stack description",
			CreatedAt:   createdAt,
			CreatedBy:   "test-user",
		}

		err := engine.Impl(s.Engine).CreateStackRef(stackID, meta)
		require.NoError(t, err)

		// Verify GetStackMeta returns the correct metadata
		retrieved, err := engine.Impl(s.Engine).GetStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, stackID, retrieved.ID)
		require.Equal(t, "Test Stack", retrieved.Title)
		require.Equal(t, "A test stack description", retrieved.Description)
		require.WithinDuration(t, createdAt, retrieved.CreatedAt, time.Second)
		require.Equal(t, "test-user", retrieved.CreatedBy)
	})

	t.Run("CreateStackRef creates stack ref with nil metadata using defaults", func(t *testing.T) {
		stackID := "default-meta-stack"

		err := engine.Impl(s.Engine).CreateStackRef(stackID, nil)
		require.NoError(t, err)

		// Verify GetStackMeta returns metadata with defaults
		retrieved, err := engine.Impl(s.Engine).GetStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, stackID, retrieved.ID)
		require.False(t, retrieved.CreatedAt.IsZero())
	})

	t.Run("GetStackMeta returns different metadata for different stacks", func(t *testing.T) {
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

		err := engine.Impl(s.Engine).CreateStackRef(stack1ID, meta1)
		require.NoError(t, err)
		err = engine.Impl(s.Engine).CreateStackRef(stack2ID, meta2)
		require.NoError(t, err)

		retrieved1, err := engine.Impl(s.Engine).GetStackMeta(stack1ID)
		require.NoError(t, err)
		require.Equal(t, "Stack One", retrieved1.Title)

		retrieved2, err := engine.Impl(s.Engine).GetStackMeta(stack2ID)
		require.NoError(t, err)
		require.Equal(t, "Stack Two", retrieved2.Title)
	})
}
