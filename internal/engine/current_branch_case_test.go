package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// Issue #1532: on case-insensitive filesystems `refs/heads/Claude/x` and
// `refs/heads/claude/x` are the same file, so HEAD can record a casing that
// for-each-ref never reports. Every current-branch lookup then misses and
// stackit reports the branch you are standing on as nonexistent.
//
// The divergence itself — HEAD naming a ref with different casing than the one
// on disk — is reproducible on a case-sensitive filesystem by pointing HEAD at
// the other spelling directly, so this covers the resolution on Linux CI.
func TestCurrentBranchResolvesRefCasing(t *testing.T) {
	t.Parallel()

	t.Run("HEAD casing is realigned with the on-disk ref", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.CreateBranch("claude/feature").Commit("feature change")

		s.RunGit("symbolic-ref", "HEAD", "refs/heads/Claude/feature")
		s.Rebuild()

		current := s.Engine.CurrentBranch()
		require.NotNil(t, current)
		require.Equal(t, "claude/feature", current.GetName())
	})

	t.Run("matching casing is left untouched", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.CreateBranch("claude/feature").Commit("feature change")

		current := s.Engine.CurrentBranch()
		require.NotNil(t, current)
		require.Equal(t, "claude/feature", current.GetName())
	})

	t.Run("track succeeds on a branch HEAD spells differently", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
		s.CreateBranch("claude/feature").Commit("feature change")

		s.RunGit("symbolic-ref", "HEAD", "refs/heads/Claude/feature")
		s.Rebuild()

		name, err := s.Engine.ValidateOnBranch()
		require.NoError(t, err)
		require.Equal(t, "claude/feature", name)

		require.NoError(t, s.Engine.TrackBranch(context.Background(), name, "main"))
		parent := s.Engine.GetBranch(name).GetParent()
		require.NotNil(t, parent)
		require.Equal(t, "main", parent.GetName())
	})
}

func TestTrackBranchReportsCaseMismatch(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.CreateBranch("claude/feature").Commit("feature change")
	s.Checkout("main")

	err := s.Engine.TrackBranch(context.Background(), "Claude/feature", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "claude/feature")
	require.Contains(t, err.Error(), "differ only in case")
}
