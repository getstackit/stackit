package engine_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

type countingDiffRunner struct {
	git.Runner
	batches atomic.Int64
	singles atomic.Int64
	fail    bool
}

func (r *countingDiffRunner) BatchDiffNumstat(ctx context.Context, ranges []git.RevRange) (map[git.RevRange]string, error) {
	r.batches.Add(1)
	if r.fail {
		return nil, fmt.Errorf("batch failed")
	}
	return r.Runner.BatchDiffNumstat(ctx, ranges)
}

func (r *countingDiffRunner) RunGitCommandWithContext(ctx context.Context, args ...string) (string, error) {
	if len(args) > 1 && args[0] == "diff" && args[1] == "--numstat" {
		r.singles.Add(1)
	}
	return r.Runner.RunGitCommandWithContext(ctx, args...)
}

func TestDiffReadersShareBatchAndCache(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			t.Parallel()
			s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()
			r := &countingDiffRunner{Runner: s.Engine.Git(), fail: fail}
			eng, err := engine.NewEngine(engine.Options{RepoRoot: s.Scene.Dir, Trunk: "main", Git: r})
			require.NoError(t, err)
			branches := engine.BranchesFromNames(eng, []string{"main", "a", "b", "c"})
			stats := eng.BatchBranchStats(branches)
			files := eng.BatchChangedFileCounts(context.Background(), branches)
			diffs := eng.BatchDiffStats(branches)
			require.EqualValues(t, 1, r.batches.Load())
			if fail {
				require.EqualValues(t, 3, r.singles.Load(), "failed batch falls back per distinct range")
			} else {
				require.Zero(t, r.singles.Load(), "cold stats should not spawn per-branch diffs")
			}
			for _, b := range branches {
				name := b.GetName()
				require.Equal(t, stats[name].LinesAdded, diffs[name].Added)
				require.Equal(t, stats[name].LinesDeleted, diffs[name].Deleted)
				if b.IsTrunk() {
					require.Zero(t, files[name])
					continue
				}
				base, err := eng.GetDivergencePoint(name)
				require.NoError(t, err)
				want, err := s.Engine.GetChangedFiles(context.Background(), git.RevRange{Base: base, Head: name})
				require.NoError(t, err)
				require.Equal(t, len(want), files[name])
			}
			// A new tip must not reuse the previous range's stats. A lone miss
			// takes the cheap single-range path, without another batch setup.
			s.Checkout("c").CommitChange("fresh.txt", "new file")
			before := r.singles.Load()
			updated := eng.BatchChangedFileCounts(context.Background(), branches)
			require.Equal(t, files["c"]+1, updated["c"])
			require.EqualValues(t, 1, r.batches.Load())
			require.Equal(t, before+1, r.singles.Load())
		})
	}
}
