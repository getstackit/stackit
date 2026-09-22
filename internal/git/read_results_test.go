package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
)

func TestReadResults(t *testing.T) {
	t.Parallel()
	failure := errors.New("read failed")
	var results git.ReadResults[string]
	results.Set("ok", "sha")
	results.Fail("bad", failure)
	value, err := results.Get("ok")
	require.NoError(t, err)
	require.Equal(t, "sha", value)
	_, err = results.Get("bad")
	require.ErrorIs(t, err, failure)
	_, err = results.Get("not-requested")
	require.Error(t, err)
	_, err = results.One()
	require.Error(t, err)
	_, err = (git.ReadResults[string]{}).One()
	require.Error(t, err)
}

func TestReadRevisionsConsistentForOneAndMany(t *testing.T) {
	t.Parallel()
	scene := testhelpers.NewSceneParallel(t, testhelpers.InitialCommitSceneSetup)
	require.NoError(t, scene.Repo.RunGitCommand("tag", "-a", "tag", "-m", "tag"))
	logger := &traceCaptureLogger{}
	runner := git.NewRunnerWithPath(scene.Dir, logger)
	one, err := runner.ReadRevisions(t.Context(), "tag").One()
	require.NoError(t, err)
	logger.calls = 0
	many := runner.ReadRevisions(t.Context(), "tag", "main", "missing", "tag")
	require.Equal(t, one, many.Values()["tag"])
	require.Equal(t, one, many.Values()["main"])
	require.ErrorContains(t, many.Failures()["missing"], "reference not found")
	require.Equal(t, 1, logger.calls)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runner.ReadRevisions(ctx, "HEAD").One()
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, runner.ReadRevisions(t.Context()).Values())
}

func TestReadResultsReplaceAndIsolateOutcomes(t *testing.T) {
	t.Parallel()
	var results git.ReadResults[[]string]
	results.Set("empty", nil)
	value, err := results.One()
	require.NoError(t, err)
	require.Nil(t, value)
	failure := errors.New("failed")
	results.Record("empty", []string{"stale"}, failure)
	value, err = results.One()
	require.ErrorIs(t, err, failure)
	require.Nil(t, value)
	require.Empty(t, results.Values())
	results.Set("empty", []string{"restored"})
	require.Empty(t, results.Failures())
	values, failures := results.Split()
	delete(values, "empty")
	failures["empty"] = failure
	value, err = results.One()
	require.NoError(t, err)
	require.Equal(t, []string{"restored"}, value)
	results.Fail("z", errors.New("last"))
	results.Fail("a", errors.New("first"))
	require.EqualError(t, results.Errs()[0], "first")
	require.EqualError(t, results.Errs()[1], "last")
	seen := 0
	for range results.All() {
		seen++
		break
	}
	require.Equal(t, 1, seen)
}
