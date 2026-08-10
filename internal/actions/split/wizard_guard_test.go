package split_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/actions/split"
	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

// guardOnlyHandler is interactive enough to reach the wizard and nothing more.
// InteractiveHandler is embedded as a nil interface, so any prompt call panics —
// which is the assertion: the ownership guard must refuse before the wizard
// touches the user or the branch.
type guardOnlyHandler struct {
	split.InteractiveHandler
}

func (guardOnlyHandler) IsInteractive() bool { return true }
func (guardOnlyHandler) Cleanup()            {}

// TestRunWizardRefusesBranchOwnedByAnotherWorktree covers the default
// `stackit split` entry point. Action returns into RunWizard before reaching its
// own guard, so the wizard has to carry one: splitting rewrites the branch's
// commits, and doing that outside the owning worktree leaves that worktree's
// files diverged from the ref, which the user's next `modify -a` commits as a
// deletion.
//
// See "Branch-Content Mutations Run Only In The Owning Worktree" in
// .claude/rules/worktree-safety.md.
func TestRunWizardRefusesBranchOwnedByAnotherWorktree(t *testing.T) {
	t.Parallel()

	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	s.WithInitialCommit()
	s.CreateBranch("feature").Commit("feature change")
	s.TrackBranch("feature", "main")
	s.Checkout("feature")

	worktreePath := t.TempDir()
	require.NoError(t, s.Engine.RegisterWorktree(context.Background(), engine.WorktreeRegistration{AnchorBranch: "feature", Path: engine.WorktreePath(worktreePath), Name: "feature-wt"}))
	t.Cleanup(func() { _ = s.Engine.UnregisterWorktree(s.Context, "feature") })

	err := split.RunWizard(s.Context, guardOnlyHandler{}, split.WizardOptions{})

	require.Error(t, err)
	require.ErrorContains(t, err, "belongs to worktree feature-wt")
}
