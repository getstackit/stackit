package git_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestNamespacedNotesPreserveOtherNamespaces(t *testing.T) {
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})

	runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
	require.NoError(t, runner.InitDefaultRepo())

	ctx := context.Background()
	sha, err := scene.Repo.GetCurrentSHA()
	require.NoError(t, err)

	require.NoError(t, runner.AddPromptNote(ctx, sha, "story", &git.PromptNote{
		Prompt:  "implemented feature x",
		Summary: "added handlers and tests",
	}))
	require.NoError(t, runner.AddPromptNote(ctx, sha, "learnings", &git.PromptNote{
		Prompt:  "be careful with force-push",
		Summary: "metadata refs can race",
	}))

	storyNote, err := runner.ShowPromptNote(ctx, sha, "story")
	require.NoError(t, err)
	require.NotNil(t, storyNote)
	require.Equal(t, "implemented feature x", storyNote.Prompt)
	require.NotEmpty(t, storyNote.Timestamp)

	learningNote, err := runner.ShowPromptNote(ctx, sha, "learnings")
	require.NoError(t, err)
	require.NotNil(t, learningNote)
	require.Equal(t, "be careful with force-push", learningNote.Prompt)
	require.NotEmpty(t, learningNote.Timestamp)

	missing, err := runner.ShowPromptNote(ctx, sha, "missing")
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestNamespacedNotesReadsLegacyPayload(t *testing.T) {
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})

	runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
	require.NoError(t, runner.InitDefaultRepo())

	sha, err := scene.Repo.GetCurrentSHA()
	require.NoError(t, err)

	require.NoError(t, scene.Repo.RunGitCommand(
		"notes", "--ref=refs/notes/prompts", "add", "-f", "-m",
		`{"prompt":"legacy","summary":"legacy summary"}`, sha,
	))

	ctx := context.Background()
	story, err := runner.ShowPromptNote(ctx, sha, git.DefaultNotesNamespace)
	require.NoError(t, err)
	require.NotNil(t, story)
	require.Equal(t, "legacy", story.Prompt)

	require.NoError(t, runner.AddPromptNote(ctx, sha, "learnings", &git.PromptNote{
		Prompt:  "new format",
		Summary: "migrated on write",
	}))

	learning, err := runner.ShowPromptNote(ctx, sha, "learnings")
	require.NoError(t, err)
	require.NotNil(t, learning)
	require.Equal(t, "new format", learning.Prompt)

	rawPayload, err := scene.Repo.RunGitCommandAndGetOutput("notes", "--ref=refs/notes/prompts", "show", sha)
	require.NoError(t, err)
	require.Contains(t, rawPayload, `"namespaces"`)
	require.Contains(t, rawPayload, `"story"`)
	require.Contains(t, rawPayload, `"learnings"`)
}

func TestShowPromptNoteReturnsErrorForInvalidCommit(t *testing.T) {
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})

	runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
	require.NoError(t, runner.InitDefaultRepo())

	_, err := runner.ShowPromptNote(context.Background(), "not-a-real-commit", git.DefaultNotesNamespace)
	require.Error(t, err)
}

func TestLogWithNotesUsesSupportedCommitFormat(t *testing.T) {
	scene := testhelpers.NewScene(t, func(s *testhelpers.Scene) error {
		return s.Repo.CreateChangeAndCommit("initial", "init")
	})

	require.NoError(t, scene.Repo.CreateAndCheckoutBranch("feature"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("feature 1", "f1"))
	require.NoError(t, scene.Repo.CreateChangeAndCommit("feature 2", "f2"))

	recent, err := scene.Repo.RunGitCommandAndGetOutput("rev-list", "--max-count=2", "feature")
	require.NoError(t, err)
	commits := strings.Split(strings.TrimSpace(recent), "\n")
	require.Len(t, commits, 2)
	firstFeatureCommit := commits[1]

	runner := git.NewRunnerWithPath(scene.Repo.Dir, nil)
	require.NoError(t, runner.InitDefaultRepo())
	require.NoError(t, runner.AddPromptNote(context.Background(), firstFeatureCommit, "story", &git.PromptNote{
		Prompt:  "feature narrative",
		Summary: "first feature commit rationale",
	}))

	entries, err := runner.LogWithNotes(context.Background(), "main", "feature", "story")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	hasStoryNote := false
	for _, entry := range entries {
		if entry.Note != nil && entry.Note.Prompt == "feature narrative" {
			hasStoryNote = true
			break
		}
	}
	require.True(t, hasStoryNote, "expected at least one story note in log entries")
}
