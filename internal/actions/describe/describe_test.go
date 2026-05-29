package describe_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/describe"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// testDescribeHandler is a test handler for describe operations
type testDescribeHandler struct {
	interactive bool
}

func (h *testDescribeHandler) Cleanup() {}

func (h *testDescribeHandler) IsInteractive() bool { return h.interactive }

func TestDescribeAction(t *testing.T) {
	t.Parallel()

	t.Run("set description with title flag", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		err := describe.Action(s.Context, describe.Options{
			Title: "Auth Feature",
		}, nil)
		require.NoError(t, err)

		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("feature"))
		require.NotNil(t, desc)
		require.Equal(t, "Auth Feature", desc.Title)
		require.Empty(t, desc.Description)
	})

	t.Run("set description with title and description flags", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		err := describe.Action(s.Context, describe.Options{
			Title:       "Auth Feature",
			Description: "OAuth2 implementation with JWT tokens",
		}, nil)
		require.NoError(t, err)

		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("feature"))
		require.NotNil(t, desc)
		require.Equal(t, "Auth Feature", desc.Title)
		require.Equal(t, "OAuth2 implementation with JWT tokens", desc.Description)
	})

	t.Run("clear description", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		// First set a description
		err := describe.Action(s.Context, describe.Options{
			Title: "Auth Feature",
		}, nil)
		require.NoError(t, err)

		// Then clear it
		err = describe.Action(s.Context, describe.Options{
			Clear: true,
		}, nil)
		require.NoError(t, err)

		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("feature"))
		require.True(t, desc == nil || desc.IsEmpty())
	})

	t.Run("show description when set", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		// Set description first
		err := describe.Action(s.Context, describe.Options{
			Title: "Auth Feature",
		}, nil)
		require.NoError(t, err)

		// Show description (should not error)
		err = describe.Action(s.Context, describe.Options{
			Show: true,
		}, nil)
		require.NoError(t, err)
	})

	t.Run("show description when not set", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		// Show description when none is set (should not error)
		err := describe.Action(s.Context, describe.Options{
			Show: true,
		}, nil)
		require.NoError(t, err)
	})

	t.Run("cannot set description on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit()

		err := describe.Action(s.Context, describe.Options{
			Title: "Some Title",
		}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot set stack description on trunk")
	})

	t.Run("description stored on root branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("root").
			Commit("root commit").
			TrackBranch("root", "main").
			CreateBranch("child").
			Commit("child commit").
			TrackBranch("child", "root")

		// Set description while on child
		err := describe.Action(s.Context, describe.Options{
			Title: "Stack Title",
		}, nil)
		require.NoError(t, err)

		// Description should be retrievable from both child and root
		childDesc := s.Engine.GetStackDescription(s.Engine.GetBranch("child"))
		require.NotNil(t, childDesc)
		require.Equal(t, "Stack Title", childDesc.Title)

		rootDesc := s.Engine.GetStackDescription(s.Engine.GetBranch("root"))
		require.NotNil(t, rootDesc)
		require.Equal(t, "Stack Title", rootDesc.Title)
	})

	t.Run("requires message in non-interactive mode", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.WithInitialCommit().
			CreateBranch("feature").
			Commit("feature commit").
			TrackBranch("feature", "main")

		handler := &testDescribeHandler{interactive: false}
		err := describe.Action(s.Context, describe.Options{}, handler)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nothing to set")
	})
}

func TestTruncateDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated",
			input:  "hello world this is long",
			maxLen: 10,
			want:   "hello w...",
		},
		{
			name:   "multiline adds ellipsis",
			input:  "line1\nline2\nline3",
			maxLen: 100,
			want:   "line1...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "maxLen 3 returns just ellipsis",
			input:  "hello",
			maxLen: 3,
			want:   "...",
		},
		{
			name:   "maxLen 2 returns just ellipsis",
			input:  "hello",
			maxLen: 2,
			want:   "...",
		},
		{
			name:   "maxLen 0 returns just ellipsis",
			input:  "hello",
			maxLen: 0,
			want:   "...",
		},
		{
			name:   "maxLen 4 truncates to 1 char",
			input:  "hello",
			maxLen: 4,
			want:   "h...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := describe.TruncateDescription(tt.input, tt.maxLen)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseEditorContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    *git.StackDescription
	}{
		{
			name:    "empty content returns nil",
			content: "",
			want:    nil,
		},
		{
			name:    "only comments returns nil",
			content: "# This is a comment\n# Another comment\n",
			want:    nil,
		},
		{
			name:    "title only",
			content: "Auth Feature\n",
			want:    &git.StackDescription{Title: "Auth Feature", Description: ""},
		},
		{
			name:    "title with comments",
			content: "Auth Feature\n# This is a comment\n",
			want:    &git.StackDescription{Title: "Auth Feature", Description: ""},
		},
		{
			name:    "title and description",
			content: "Auth Feature\n\nThis implements OAuth2.\n",
			want:    &git.StackDescription{Title: "Auth Feature", Description: "This implements OAuth2."},
		},
		{
			name:    "title and multi-line description",
			content: "Auth Feature\n\nFirst paragraph.\n\nSecond paragraph.\n",
			want:    &git.StackDescription{Title: "Auth Feature", Description: "First paragraph.\n\nSecond paragraph."},
		},
		{
			name:    "title and description with comments",
			content: "Auth Feature\n\nThis implements OAuth2.\n# Ignore this\n",
			want:    &git.StackDescription{Title: "Auth Feature", Description: "This implements OAuth2."},
		},
		{
			name:    "whitespace before title",
			content: "\n\nAuth Feature\n",
			want:    &git.StackDescription{Title: "Auth Feature", Description: ""},
		},
		{
			name:    "template with content",
			content: "My Stack Title\n\nDescription here.\n\n# Stack Description\n#\n# First line: Title\n",
			want:    &git.StackDescription{Title: "My Stack Title", Description: "Description here."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := describe.ParseEditorContent(tt.content)
			if tt.want == nil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, tt.want.Title, got.Title)
				require.Equal(t, tt.want.Description, got.Description)
			}
		})
	}
}
