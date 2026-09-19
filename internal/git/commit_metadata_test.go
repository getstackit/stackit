package git_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestGetCommitRangeMetadata(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	ctx := context.Background()
	base, err := runner.ReadRevisions(context.Background(), "HEAD").One()
	require.NoError(t, err)

	// Distinct authors, time zones, multiline and empty messages must survive
	// batching, including the final empty field in the oldest commit's record.
	for i, message := range []string{"", "subject\n\nbody\twith separators \x1e\x1f\n\nfooter\n"} {
		dates := []string{"2026-06-01T12:00:00-04:00", "2026-06-02T09:30:00+05:30"}
		_, err := runner.RunGitCommandWithEnv(ctx, []string{
			"GIT_AUTHOR_NAME=Author " + dates[i],
			"GIT_AUTHOR_EMAIL=author@example.com",
			"GIT_AUTHOR_DATE=" + dates[i],
		}, "commit", "--allow-empty", "--allow-empty-message", "-m", message)
		require.NoError(t, err)
	}
	logger.calls = 0
	commits, err := runner.ReadCommitRanges(ctx, git.RevRange{Base: base, Head: "HEAD"}).One()
	require.NoError(t, err)
	require.Len(t, commits, 2)
	require.Equal(t, 1, logger.calls, "metadata for the whole range uses one subprocess")
	for _, commit := range commits {
		for format, got := range map[string]string{
			"%P": strings.Join(commit.Parents, " "), "%an": commit.AuthorName,
			"%ae": commit.AuthorEmail, "%aI": commit.AuthorDate, "%B": commit.Message,
		} {
			want, err := runner.RunGitCommandWithContext(ctx, "log", "-1", "--format="+format, commit.SHA)
			require.NoError(t, err)
			require.Equal(t, want, strings.TrimSpace(got), format)
		}
	}
	require.Equal(t, []string{commits[1].SHA}, commits[0].Parents)
	require.Equal(t, []string{base}, commits[1].Parents)
	require.Empty(t, commits[1].Message)

	empty, err := runner.ReadCommitRanges(ctx, git.RevRange{Base: "HEAD", Head: "HEAD"}).One()
	require.NoError(t, err)
	require.Empty(t, empty)
	_, err = runner.ReadCommitRanges(ctx, git.RevRange{Head: "missing-branch"}).One()
	require.Error(t, err)
}
