package engine_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type commitBatchRunner struct {
	git.Runner
	fault         string
	failure       error
	metadataReads int
	revisionReads int
	revisionNames []string
	historyReads  atomic.Int64
}

func (r *commitBatchRunner) BatchReadMetadata(names []string) (map[string]*git.Meta, map[string]error) {
	r.metadataReads++
	metas, errs := r.Runner.BatchReadMetadata(names)
	if r.fault == "" {
		return metas, errs
	}
	if errs == nil {
		errs = make(map[string]error)
	}
	switch r.fault {
	case "metadata":
		errs["b"] = r.failure
	case "missing metadata":
		delete(metas, "b")
	case "parent":
		metas["b"] = metas["b"].WithParentBranchRevision(nil)
	case "history":
		base := "missing-base"
		metas["b"] = metas["b"].WithParentBranchRevision(&base)
	}
	return metas, errs
}

func (r *commitBatchRunner) BatchGetRevisions(names []string) (map[string]string, []error) {
	r.revisionReads++
	r.revisionNames = names
	revs, errs := r.Runner.BatchGetRevisions(names)
	switch r.fault {
	case "head":
		delete(revs, "b")
		errs = append(errs, r.failure)
	case "parent":
		delete(revs, "a")
		errs = append(errs, r.failure)
	}
	return revs, errs
}

func (r *commitBatchRunner) GetCommitRange(ctx context.Context, base, head, format string) ([]string, error) {
	r.historyReads.Add(1)
	return r.Runner.GetCommitRange(ctx, base, head, format)
}

func TestCommitBatchPartialFailures(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"metadata", "missing metadata", "head", "parent", "history"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
			r := &commitBatchRunner{Runner: s.Engine.Git(), failure: errors.New("injected read failure")}
			eng, err := engine.NewEngine(engine.Options{RepoRoot: s.Scene.Dir, Trunk: "main", Git: r})
			require.NoError(t, err)
			r.fault, r.metadataReads, r.revisionReads = fault, 0, 0
			batch := eng.BatchCommits(engine.BranchesFromNames(eng, []string{"main", "b", "c"}), engine.CommitFormatSHA)
			commits, err := batch.ForBranch(eng.GetBranch("b"))
			require.Error(t, err)
			require.Nil(t, commits)
			require.NotContains(t, batch.Commits, "b")
			if fault == "metadata" || fault == "head" || fault == "parent" {
				require.ErrorIs(t, err, r.failure)
			}
			commits, err = batch.ForBranch(eng.GetBranch("c"))
			require.NoError(t, err, "a sibling failure must not poison successful histories")
			require.Len(t, commits, 1)
			commits, err = batch.ForBranch(eng.Trunk())
			require.NoError(t, err)
			require.Empty(t, commits)
			_, err = batch.ForBranch(eng.GetBranch("not-requested"))
			require.ErrorContains(t, err, "were not read")
			require.Len(t, batch.Errors, 1)
			require.Equal(t, 1, r.metadataReads)
			require.Equal(t, 1, r.revisionReads)
			if fault == "history" {
				require.EqualValues(t, 2, r.historyReads.Load())
			} else {
				require.EqualValues(t, 1, r.historyReads.Load(), "invalid ranges must never read full repository history")
			}
			if fault != "parent" {
				require.NotContains(t, r.revisionNames, "a", "stored divergence needs no parent lookup")
			}
		})
	}
}

func TestCommitBatchEmptyAndOrderedHistories(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
	s.Checkout("c").CommitChange("second.txt", "second commit")
	// Make b legitimately empty relative to its recorded divergence point.
	head, err := s.Engine.GetBranch("b").GetRevision()
	require.NoError(t, err)
	meta, err := s.Engine.Git().ReadMetadata("b")
	require.NoError(t, err)
	require.NoError(t, s.Engine.Git().WriteMetadata("b", meta.WithParentBranchRevision(&head)))
	r := &commitBatchRunner{Runner: s.Engine.Git()}
	eng, err := engine.NewEngine(engine.Options{RepoRoot: s.Scene.Dir, Trunk: "main", Git: r})
	require.NoError(t, err)
	r.metadataReads, r.revisionReads = 0, 0
	batch := eng.BatchCommits(engine.BranchesFromNames(eng, []string{"main", "b", "c"}), engine.CommitFormatSHA)
	require.Empty(t, batch.Errors)
	commits, err := batch.ForBranch(eng.GetBranch("b"))
	require.NoError(t, err)
	require.Empty(t, commits)
	commits, err = batch.ForBranch(eng.GetBranch("c"))
	require.NoError(t, err)
	want, err := r.RunGitCommandWithContext(context.Background(), "log", "--format=%H", "b..c")
	require.NoError(t, err)
	require.Equal(t, strings.Split(want, "\n"), commits)
	// Repeated access, including empty histories, must never retry Git reads.
	_, err = batch.ForBranch(eng.GetBranch("b"))
	require.NoError(t, err)
	require.EqualValues(t, 2, r.historyReads.Load())
	require.Equal(t, 1, r.metadataReads)
	require.Equal(t, 1, r.revisionReads)
	eng.BatchCommits(nil, engine.CommitFormatSHA)
	eng.BatchCommits(engine.BranchesOf(eng.Trunk()), engine.CommitFormatSHA)
	require.Equal(t, 1, r.metadataReads, "empty and trunk-only batches need no Git reads")
	require.Equal(t, 1, r.revisionReads)
}
