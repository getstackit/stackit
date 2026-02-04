package git_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
)

func TestStackMetaOperations(t *testing.T) {
	t.Parallel()

	t.Run("read returns nil for non-existent stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		meta, err := runner.ReadStackMeta("nonexistent-stack-id")
		require.NoError(t, err)
		require.Nil(t, meta)
	})

	t.Run("write and read stack metadata", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		stackID := "1234567890-test-stack"
		meta := &git.StackMeta{
			ID:          stackID,
			Title:       "Test Stack",
			Description: "This is a test stack",
			CreatedAt:   time.Now().Truncate(time.Second),
			CreatedBy:   "test-user",
		}

		// Write
		err := runner.WriteStackMeta(stackID, meta)
		require.NoError(t, err)

		// Read back
		readMeta, err := runner.ReadStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, readMeta)
		require.Equal(t, stackID, readMeta.ID)
		require.Equal(t, "Test Stack", readMeta.Title)
		require.Equal(t, "This is a test stack", readMeta.Description)
		require.Equal(t, "test-user", readMeta.CreatedBy)
	})

	t.Run("delete stack metadata", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		stackID := "delete-test-stack"
		meta := &git.StackMeta{
			ID:        stackID,
			Title:     "To Be Deleted",
			CreatedAt: time.Now(),
		}

		// Write
		err := runner.WriteStackMeta(stackID, meta)
		require.NoError(t, err)

		// Delete
		err = runner.DeleteStackMeta(stackID)
		require.NoError(t, err)

		// Verify deleted
		readMeta, err := runner.ReadStackMeta(stackID)
		require.NoError(t, err)
		require.Nil(t, readMeta)
	})

	t.Run("list stack metadata", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		// Write multiple stacks
		stacks := []string{"stack-1", "stack-2", "stack-3"}
		for _, stackID := range stacks {
			meta := &git.StackMeta{
				ID:        stackID,
				CreatedAt: time.Now(),
			}
			err := runner.WriteStackMeta(stackID, meta)
			require.NoError(t, err)
		}

		// List
		list, err := runner.ListStackMetas()
		require.NoError(t, err)
		require.Len(t, list, 3)

		for _, stackID := range stacks {
			_, exists := list[stackID]
			require.True(t, exists, "stack %s should exist in list", stackID)
		}
	})
}

func TestStackMeta_StackDescription(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty stack meta", func(t *testing.T) {
		t.Parallel()
		meta := &git.StackMeta{}
		desc := meta.StackDescription()
		require.Nil(t, desc)
	})

	t.Run("returns nil for nil stack meta", func(t *testing.T) {
		t.Parallel()
		var meta *git.StackMeta
		desc := meta.StackDescription()
		require.Nil(t, desc)
	})

	t.Run("returns description with title", func(t *testing.T) {
		t.Parallel()
		meta := &git.StackMeta{
			Title: "Test Title",
		}
		desc := meta.StackDescription()
		require.NotNil(t, desc)
		require.Equal(t, "Test Title", desc.Title)
		require.Empty(t, desc.Description)
	})

	t.Run("returns description with both fields", func(t *testing.T) {
		t.Parallel()
		meta := &git.StackMeta{
			Title:       "Test Title",
			Description: "Test Description",
		}
		desc := meta.StackDescription()
		require.NotNil(t, desc)
		require.Equal(t, "Test Title", desc.Title)
		require.Equal(t, "Test Description", desc.Description)
	})
}

func TestStackMeta_IsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta *git.StackMeta
		want bool
	}{
		{
			name: "nil is empty",
			meta: nil,
			want: true,
		},
		{
			name: "zero value is empty",
			meta: &git.StackMeta{},
			want: true,
		},
		{
			name: "only ID is empty",
			meta: &git.StackMeta{ID: "test"},
			want: true,
		},
		{
			name: "title makes it not empty",
			meta: &git.StackMeta{Title: "Test"},
			want: false,
		},
		{
			name: "description makes it not empty",
			meta: &git.StackMeta{Description: "Test"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.meta.IsEmpty()
			require.Equal(t, tt.want, got)
		})
	}
}
