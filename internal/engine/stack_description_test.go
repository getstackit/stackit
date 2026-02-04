package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/git"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestGetStackDescription(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit")

		branch := s.Engine.GetBranch("feature")
		desc := s.Engine.GetStackDescription(branch)
		require.Nil(t, desc)
	})

	t.Run("returns nil when no description set", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		branch := s.Engine.GetBranch("feature")
		desc := s.Engine.GetStackDescription(branch)
		require.Nil(t, desc)
	})

	t.Run("returns description from stack root", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main")

		// Set description on root
		err := s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("root"), &git.StackDescription{
			Title:       "Feature Stack",
			Description: "Details here",
		})
		require.NoError(t, err)

		// Get description
		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("root"))
		require.NotNil(t, desc)
		require.Equal(t, "Feature Stack", desc.Title)
		require.Equal(t, "Details here", desc.Description)
	})

	t.Run("returns description from root when queried from child", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "root")

		// Set description on root
		err := s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("root"), &git.StackDescription{
			Title: "Feature Stack",
		})
		require.NoError(t, err)

		// Get description from child
		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("child"))
		require.NotNil(t, desc)
		require.Equal(t, "Feature Stack", desc.Title)
	})
}

func TestSetStackDescription(t *testing.T) {
	t.Parallel()

	t.Run("sets description on stack root", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		err := s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("feature"), &git.StackDescription{
			Title:       "Test Title",
			Description: "Test Description",
		})
		require.NoError(t, err)

		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("feature"))
		require.NotNil(t, desc)
		require.Equal(t, "Test Title", desc.Title)
		require.Equal(t, "Test Description", desc.Description)
	})

	t.Run("setting from child stores on root", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "root")

		// Set description from child
		err := s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("child"), &git.StackDescription{
			Title: "Stack Title",
		})
		require.NoError(t, err)

		// Verify it's retrievable from root
		rootDesc := s.Engine.GetStackDescription(s.Engine.GetBranch("root"))
		require.NotNil(t, rootDesc)
		require.Equal(t, "Stack Title", rootDesc.Title)

		// Verify it's retrievable from child
		childDesc := s.Engine.GetStackDescription(s.Engine.GetBranch("child"))
		require.NotNil(t, childDesc)
		require.Equal(t, "Stack Title", childDesc.Title)
	})

	t.Run("error for untracked branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit")

		err := s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("feature"), &git.StackDescription{
			Title: "Test",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not tracked")
	})
}

func TestClearStackDescription(t *testing.T) {
	t.Parallel()

	t.Run("clears description", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		// Set description
		err := s.Engine.SetStackDescription(context.Background(), s.Engine.GetBranch("feature"), &git.StackDescription{
			Title: "Test",
		})
		require.NoError(t, err)

		// Clear it
		err = s.Engine.ClearStackDescription(context.Background(), s.Engine.GetBranch("feature"))
		require.NoError(t, err)

		// Verify it's cleared
		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("feature"))
		require.True(t, desc == nil || desc.IsEmpty())
	})
}

func TestStackDescriptionIsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		desc *git.StackDescription
		want bool
	}{
		{
			name: "nil is empty",
			desc: nil,
			want: true,
		},
		{
			name: "zero value is empty",
			desc: &git.StackDescription{},
			want: true,
		},
		{
			name: "empty strings are empty",
			desc: &git.StackDescription{Title: "", Description: ""},
			want: true,
		},
		{
			name: "title only is not empty",
			desc: &git.StackDescription{Title: "Test"},
			want: false,
		},
		{
			name: "description only is not empty",
			desc: &git.StackDescription{Description: "Test"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.desc.IsEmpty()
			require.Equal(t, tt.want, got)
		})
	}
}
