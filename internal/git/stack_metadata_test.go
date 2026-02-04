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
			name: "only ID set (no title/description) is empty",
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

func TestWriteStackMetaBlob(t *testing.T) {
	t.Parallel()

	t.Run("write blob and use SHA to create valid stack meta ref", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		stackID := "blob-test-stack"
		meta := &git.StackMeta{
			ID:          stackID,
			Title:       "Blob Test Stack",
			Description: "Testing WriteStackMetaBlob",
			CreatedAt:   time.Now().Truncate(time.Second),
			CreatedBy:   "test-user",
		}

		// Write blob (does not create ref)
		sha, err := runner.WriteStackMetaBlob(meta)
		require.NoError(t, err)
		require.NotEmpty(t, sha)

		// Verify no ref exists yet
		existingSHA := runner.GetStackMetaRefSHA(stackID)
		require.Empty(t, existingSHA)

		// Use UpdateRef to create the stack meta ref
		refName := git.StackMetaRefName(stackID)
		err = runner.UpdateRef(refName, sha)
		require.NoError(t, err)

		// Verify we can read the metadata back
		readMeta, err := runner.ReadStackMeta(stackID)
		require.NoError(t, err)
		require.NotNil(t, readMeta)
		require.Equal(t, stackID, readMeta.ID)
		require.Equal(t, "Blob Test Stack", readMeta.Title)
		require.Equal(t, "Testing WriteStackMetaBlob", readMeta.Description)
		require.Equal(t, "test-user", readMeta.CreatedBy)
	})
}

func TestGetStackMetaRefSHA(t *testing.T) {
	t.Parallel()

	t.Run("returns SHA for existing stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		stackID := "sha-test-stack"
		meta := &git.StackMeta{
			ID:        stackID,
			Title:     "SHA Test Stack",
			CreatedAt: time.Now(),
		}

		// Write stack meta
		err := runner.WriteStackMeta(stackID, meta)
		require.NoError(t, err)

		// Get SHA
		sha := runner.GetStackMetaRefSHA(stackID)
		require.NotEmpty(t, sha)
		require.Len(t, sha, 40) // Git SHAs are 40 hex characters
	})

	t.Run("returns empty string for non-existent stack", func(t *testing.T) {
		t.Parallel()
		scene := testhelpers.NewSceneParallel(t, nil)
		runner := git.NewRunnerWithPath(scene.Dir, nil)

		sha := runner.GetStackMetaRefSHA("nonexistent-stack-id")
		require.Empty(t, sha)
	})
}

func TestStackMetaRefName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stackID  string
		expected string
	}{
		{
			name:     "simple stack ID",
			stackID:  "test-id",
			expected: "refs/stackit/stacks/test-id",
		},
		{
			name:     "stack ID with timestamp prefix",
			stackID:  "1234567890-my-feature",
			expected: "refs/stackit/stacks/1234567890-my-feature",
		},
		{
			name:     "stack ID with special characters",
			stackID:  "feature/add-login",
			expected: "refs/stackit/stacks/feature/add-login",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := git.StackMetaRefName(tt.stackID)
			require.Equal(t, tt.expected, result)
		})
	}
}
