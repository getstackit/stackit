package restack

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
)

// fakeApplyEngine implements ApplyEngine with no repository behind it, so the
// executor's decisions can be checked without a git repo.
type fakeApplyEngine struct {
	restacked []engine.Branches
	results   map[string]engine.RestackBranchResult
}

func (f *fakeApplyEngine) GetBranch(name string) engine.Branch { return engine.NewBranch(name, nil) }
func (f *fakeApplyEngine) Trunk() engine.Branch                { return engine.NewBranch("main", nil) }
func (f *fakeApplyEngine) Graph(context.Context, engine.SortStrategy) *engine.StackGraph {
	return nil
}
func (f *fakeApplyEngine) CurrentBranch(context.Context) *engine.Branch { return nil }
func (f *fakeApplyEngine) GetCurrentRevision(context.Context) (string, error) {
	return "head", nil
}

func (f *fakeApplyEngine) RestackBranches(_ context.Context, branches engine.Branches, opts engine.RestackOpts) (engine.RestackBatchResult, error) {
	f.restacked = append(f.restacked, branches)
	for _, b := range branches {
		if r, ok := f.results[b.Name()]; ok && opts.Progress != nil {
			opts.Progress(b, r)
		}
	}
	return engine.RestackBatchResult{Results: f.results}, nil
}
func (f *fakeApplyEngine) IsRebaseInProgress(context.Context) bool                 { return false }
func (f *fakeApplyEngine) RebaseAbort(context.Context) error                       { return nil }
func (f *fakeApplyEngine) CheckoutBranch(context.Context, engine.Branch) error     { return nil }
func (f *fakeApplyEngine) Detach(context.Context, string) error                    { return nil }
func (f *fakeApplyEngine) LastSnapshotID() string                                  { return "" }
func (f *fakeApplyEngine) ListWorktrees(context.Context) (git.WorktreeList, error) { return nil, nil }
func (f *fakeApplyEngine) WorktreeRebaseInProgress(context.Context, engine.WorktreePath) bool {
	return false
}
func (f *fakeApplyEngine) WorktreeHasUncommittedChanges(context.Context, engine.WorktreePath) (bool, error) {
	return false, nil
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}

func TestExecutorExecute(t *testing.T) {
	t.Parallel()

	a, b := engine.NewBranch("a", nil), engine.NewBranch("b", nil)

	t.Run("a plan with no work only reports what planning decided", func(t *testing.T) {
		t.Parallel()
		eng := &fakeApplyEngine{}
		var got []Progress
		exec := Executor{Engine: eng, Log: nopLogger{}, Report: func(p Progress) { got = append(got, p) }}

		plan := &Plan{
			Branches: engine.BranchesOf(a, b),
			Engine: &engine.RestackPlan{PlannedResults: map[string]engine.RestackBranchResult{
				"b": {Result: engine.RestackUnneeded, Frozen: true},
			}},
		}
		require.NoError(t, exec.Execute(t.Context(), plan, ConflictEnterWorkflow, nil))

		require.Empty(t, eng.restacked, "an empty plan must not touch the repository")
		require.Len(t, got, 1)
		require.Equal(t, "b", got[0].Branch)
		require.True(t, got[0].Frozen)
	})

	t.Run("a clean plan applies exactly the validated branches and reports each", func(t *testing.T) {
		t.Parallel()
		eng := &fakeApplyEngine{results: map[string]engine.RestackBranchResult{
			"a": {Result: engine.RestackDone, NewRev: "1234567890"},
		}}
		var got []Progress
		exec := Executor{Engine: eng, Log: nopLogger{}, Report: func(p Progress) { got = append(got, p) }}

		plan := &Plan{
			Branches: engine.BranchesOf(a, b),
			Engine:   &engine.RestackPlan{ApplyMap: map[string]bool{"a": true}},
			Apply:    engine.BranchesOf(a),
		}
		require.NoError(t, exec.Execute(t.Context(), plan, ConflictContinue, nil))

		require.Len(t, eng.restacked, 1)
		require.Equal(t, []string{"a"}, eng.restacked[0].Names())
		require.Len(t, got, 1)
		require.Equal(t, Progress{Branch: "a", Result: engine.RestackDone, NewRev: "1234567", branch: a}, got[0])
	})
}
