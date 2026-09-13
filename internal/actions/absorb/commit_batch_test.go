package absorb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type failingCommitBatchEngine struct {
	engine.Engine
	failure error
	reads   int
}

func (e *failingCommitBatchEngine) BatchCommits(branches engine.Branches, format engine.CommitFormat) engine.CommitBatch {
	e.reads++
	batch := e.Engine.BatchCommits(branches, format)
	delete(batch.Commits, "a")
	batch.Errors["a"] = e.failure
	return batch
}

func TestAbsorbStopsOnPartialCommitBatch(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("c")
	require.NoError(t, s.Scene.Repo.CreateChange("staged change", "new-file", false))
	head, err := s.Engine.GetBranch("c").GetRevision()
	require.NoError(t, err)
	staged, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached")
	require.NoError(t, err)
	require.NotEmpty(t, staged)
	failure := errors.New("cannot read parent history")
	eng := &failingCommitBatchEngine{Engine: s.Engine, failure: failure}
	s.Context.Engine = eng
	err = Action(s.Context, Options{Force: true}, nil)
	require.ErrorIs(t, err, failure)
	require.ErrorContains(t, err, "failed to get commits for branch a")
	require.Equal(t, 1, eng.reads, "failed histories must not trigger individual retries")
	afterHead, err := s.Engine.GetBranch("c").GetRevision()
	require.NoError(t, err)
	require.Equal(t, head, afterHead)
	afterStaged, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached")
	require.NoError(t, err)
	require.Equal(t, staged, afterStaged, "absorb must stop before modifying staged hunks")
}
