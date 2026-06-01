package merge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	mergeAction "github.com/getstackit/stackit/internal/actions/merge"
	"github.com/getstackit/stackit/internal/app"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/internal/github"
	"github.com/getstackit/stackit/internal/tui"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestNewMergeCmd(t *testing.T) {
	t.Run("creates command with subcommands", func(t *testing.T) {
		cmd := NewMergeCmd(nil)

		require.Equal(t, "merge", cmd.Use)
		require.NotEmpty(t, cmd.Short)
		require.NotEmpty(t, cmd.Long)

		// Check subcommands exist
		nextCmd, _, err := cmd.Find([]string{"next"})
		require.NoError(t, err)
		require.NotNil(t, nextCmd)
		require.Equal(t, "next", nextCmd.Use)

		shipCmd, _, err := cmd.Find([]string{"ship"})
		require.NoError(t, err)
		require.NotNil(t, shipCmd)
		require.Equal(t, "ship", shipCmd.Use)

		// Squash alias resolves to ship
		squashCmd, _, err := cmd.Find([]string{"squash"})
		require.NoError(t, err)
		require.NotNil(t, squashCmd)
		require.Equal(t, "ship", squashCmd.Use)
	})

	t.Run("has expected flags", func(t *testing.T) {
		cmd := NewMergeCmd(nil)

		// Check root command flags
		dryRunFlag := cmd.Flags().Lookup("dry-run")
		require.NotNil(t, dryRunFlag)

		forceFlag := cmd.Flags().Lookup("force")
		require.NotNil(t, forceFlag)

		waitFlag := cmd.Flags().Lookup("wait")
		require.NotNil(t, waitFlag)

		yesFlag := cmd.Flags().Lookup("yes")
		require.NotNil(t, yesFlag)
		require.Equal(t, "y", yesFlag.Shorthand)

		scopeFlag := cmd.Flags().Lookup("scope")
		require.NotNil(t, scopeFlag)

		branchFlag := cmd.Flags().Lookup("branch")
		require.NotNil(t, branchFlag)
	})
}

func TestNewNextCmd(t *testing.T) {
	t.Run("has expected flags", func(t *testing.T) {
		cmd := NewNextCmd(nil)

		require.Equal(t, "next", cmd.Use)

		dryRunFlag := cmd.Flags().Lookup("dry-run")
		require.NotNil(t, dryRunFlag)

		yesFlag := cmd.Flags().Lookup("yes")
		require.NotNil(t, yesFlag)

		forceFlag := cmd.Flags().Lookup("force")
		require.NotNil(t, forceFlag)

		waitFlag := cmd.Flags().Lookup("wait")
		require.NotNil(t, waitFlag)

		branchFlag := cmd.Flags().Lookup("branch")
		require.NotNil(t, branchFlag)

		scopeFlag := cmd.Flags().Lookup("scope")
		require.NotNil(t, scopeFlag)
	})
}

func TestNewShipCmd(t *testing.T) {
	t.Run("has expected flags", func(t *testing.T) {
		cmd := NewShipCmd(nil)

		require.Equal(t, "ship", cmd.Use)

		dryRunFlag := cmd.Flags().Lookup("dry-run")
		require.NotNil(t, dryRunFlag)

		yesFlag := cmd.Flags().Lookup("yes")
		require.NotNil(t, yesFlag)

		forceFlag := cmd.Flags().Lookup("force")
		require.NotNil(t, forceFlag)

		waitFlag := cmd.Flags().Lookup("wait")
		require.NotNil(t, waitFlag)

		scopeFlag := cmd.Flags().Lookup("scope")
		require.NotNil(t, scopeFlag)

		branchFlag := cmd.Flags().Lookup("branch")
		require.NotNil(t, branchFlag)

		stacksFlag := cmd.Flags().Lookup("stacks")
		require.NotNil(t, stacksFlag)

		skipLocalCIFlag := cmd.Flags().Lookup("skip-local-ci")
		require.NotNil(t, skipLocalCIFlag)
	})
}

func TestNewDrainCmd(t *testing.T) {
	t.Run("has expected flags", func(t *testing.T) {
		cmd := NewDrainCmd()

		require.Equal(t, "drain", cmd.Use)

		dryRunFlag := cmd.Flags().Lookup("dry-run")
		require.NotNil(t, dryRunFlag)

		yesFlag := cmd.Flags().Lookup("yes")
		require.NotNil(t, yesFlag)

		forceFlag := cmd.Flags().Lookup("force")
		require.NotNil(t, forceFlag)

		methodFlag := cmd.Flags().Lookup("method")
		require.NotNil(t, methodFlag)

		branchFlag := cmd.Flags().Lookup("branch")
		require.NotNil(t, branchFlag)

		scopeFlag := cmd.Flags().Lookup("scope")
		require.NotNil(t, scopeFlag)

		// drain should NOT have a --wait flag (it always waits)
		waitFlag := cmd.Flags().Lookup("wait")
		require.Nil(t, waitFlag)
	})
}

func TestRequireYesInNonInteractive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		interactive bool
		yes         bool
		dryRun      bool
		wantErr     bool
	}{
		{name: "interactive prompts later", interactive: true},
		{name: "non-interactive with yes", yes: true},
		{name: "non-interactive dry run", dryRun: true},
		{name: "non-interactive without yes", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := &app.Context{Interactive: tt.interactive}
			err := requireYesInNonInteractive(ctx, "merge next", tt.yes, tt.dryRun)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "--yes")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMergeNextUsesCreateMergePlan(t *testing.T) {
	t.Parallel()

	t.Run("CreateMergePlan returns error when not on a branch", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Detach HEAD
		s.RunGit("checkout", "HEAD~0").Rebuild()

		_, _, err := mergeAction.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, nil, mergeAction.CreatePlanOptions{
			Strategy: mergeAction.StrategyBottomUp,
		})
		require.Error(t, err)
	})

	t.Run("CreateMergePlan returns error when on trunk", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.Checkout("main")

		_, _, err := mergeAction.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, nil, mergeAction.CreatePlanOptions{
			Strategy: mergeAction.StrategyBottomUp,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot merge from trunk")
	})

	t.Run("CreateMergePlan returns error when branch is not tracked", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			CreateBranch("untracked")

		_, _, err := mergeAction.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, nil, mergeAction.CreatePlanOptions{
			Strategy: mergeAction.StrategyBottomUp,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not tracked")
	})

	t.Run("CreateMergePlan returns error when no PRs found", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
			})

		s.Checkout("branch-a")

		_, _, err := mergeAction.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, nil, mergeAction.CreatePlanOptions{
			Strategy: mergeAction.StrategyBottomUp,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "no open PRs found")
	})

	t.Run("finds bottom PR in stack", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
				"branch-b": "branch-a",
				"branch-c": "branch-b",
			})

		// Add PR info to branches
		branchA := s.Engine.GetBranch("branch-a")
		branchB := s.Engine.GetBranch("branch-b")
		branchC := s.Engine.GetBranch("branch-c")

		err := s.Engine.UpsertPrInfo(context.Background(), branchA, testhelpers.NewTestPrInfo(101).
			WithURL("https://github.com/owner/repo/pull/101"))
		require.NoError(t, err)

		err = s.Engine.UpsertPrInfo(context.Background(), branchB, testhelpers.NewTestPrInfo(102).
			WithURL("https://github.com/owner/repo/pull/102"))
		require.NoError(t, err)

		err = s.Engine.UpsertPrInfo(context.Background(), branchC, testhelpers.NewTestPrInfo(103).
			WithURL("https://github.com/owner/repo/pull/103"))
		require.NoError(t, err)

		// Switch to branch-c (top of stack)
		s.Checkout("branch-c")

		plan, _, err := mergeAction.CreateMergePlan(s.Context.Context, s.Engine, s.Context.Output, nil, mergeAction.CreatePlanOptions{
			Strategy: mergeAction.StrategyBottomUp,
			Force:    true,
		})
		require.NoError(t, err)
		require.NotNil(t, plan)

		// Should find branch-a as the bottom PR
		require.NotEmpty(t, plan.BranchesToMerge)
		require.Equal(t, "branch-a", plan.BranchesToMerge[0].BranchName)
		require.Equal(t, 101, plan.BranchesToMerge[0].PRNumber)
	})
}

// stubConfirm replaces tui.PromptConfirm for the duration of a test and
// restores it on cleanup.
func stubConfirm(t *testing.T, answer bool) {
	t.Helper()
	orig := tui.PromptConfirm
	tui.PromptConfirm = func(string, bool) (bool, error) { return answer, nil }
	t.Cleanup(func() { tui.PromptConfirm = orig })
}

// stubTextInput replaces tui.PromptTextInput for the duration of a test and
// restores it on cleanup.
func stubTextInput(t *testing.T, value string) {
	t.Helper()
	orig := tui.PromptTextInput
	tui.PromptTextInput = func(string, string) (string, error) { return value, nil }
	t.Cleanup(func() { tui.PromptTextInput = orig })
}

// TestPromptForShipDescription is not parallel: its subtests stub the
// package-level tui.PromptConfirm/PromptTextInput vars, so they must run
// sequentially to avoid racing on that shared state.
func TestPromptForShipDescription(t *testing.T) {
	newStack := func(t *testing.T) *scenario.Scenario {
		t.Helper()
		return scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
				"branch-b": "branch-a",
			})
	}

	branches := []mergeAction.BranchMergeInfo{
		{BranchName: "branch-a", PRNumber: 101},
		{BranchName: "branch-b", PRNumber: 102},
	}

	t.Run("no-op for empty branch list", func(t *testing.T) {
		s := newStack(t)

		promptForShipDescription(s.Context, nil, false)
		require.Empty(t, s.Output.String())
	})

	t.Run("skips silently when a description is already set", func(t *testing.T) {
		s := newStack(t)
		require.NoError(t, s.Engine.SetStackDescription(context.Background(),
			s.Engine.GetBranch("branch-a"),
			&git.StackDescription{Title: "Existing title"}))

		// PromptConfirm should never be reached when a description exists.
		orig := tui.PromptConfirm
		tui.PromptConfirm = func(string, bool) (bool, error) {
			t.Fatal("should not prompt when description exists")
			return false, nil
		}
		t.Cleanup(func() { tui.PromptConfirm = orig })

		promptForShipDescription(s.Context, branches, false)
		require.Empty(t, s.Output.String())
	})

	t.Run("warns and continues in non-interactive mode", func(t *testing.T) {
		s := newStack(t)
		s.Context.Interactive = false

		promptForShipDescription(s.Context, branches, false)
		require.Contains(t, s.Output.String(), "2 PRs")
		require.Contains(t, s.Output.String(), "stackit describe")

		// No description should have been set.
		require.Nil(t, s.Engine.GetStackDescription(s.Engine.GetBranch("branch-a")))
	})

	t.Run("warns and continues when --yes skips the prompt", func(t *testing.T) {
		s := newStack(t)
		s.Context.Interactive = true

		promptForShipDescription(s.Context, branches, true)
		require.Contains(t, s.Output.String(), "stackit describe")
		require.Nil(t, s.Engine.GetStackDescription(s.Engine.GetBranch("branch-a")))
	})

	t.Run("does not set a description when the user declines", func(t *testing.T) {
		s := newStack(t)
		s.Context.Interactive = true
		stubConfirm(t, false)

		promptForShipDescription(s.Context, branches, false)
		require.Nil(t, s.Engine.GetStackDescription(s.Engine.GetBranch("branch-a")))
	})

	t.Run("sets the description from the entered title", func(t *testing.T) {
		s := newStack(t)
		s.Context.Interactive = true
		stubConfirm(t, true)
		stubTextInput(t, "  My stack title  ")

		promptForShipDescription(s.Context, branches, false)

		desc := s.Engine.GetStackDescription(s.Engine.GetBranch("branch-a"))
		require.NotNil(t, desc)
		require.Equal(t, "My stack title", desc.Title)
		require.Contains(t, s.Output.String(), "My stack title")
	})

	t.Run("does not set a description for a blank title", func(t *testing.T) {
		s := newStack(t)
		s.Context.Interactive = true
		stubConfirm(t, true)
		stubTextInput(t, "   ")

		promptForShipDescription(s.Context, branches, false)
		require.Nil(t, s.Engine.GetStackDescription(s.Engine.GetBranch("branch-a")))
	})
}

func TestResolveMergeMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		methodFlag string
		expected   github.MergeMethod
		wantErr    string
	}{
		{
			name:       "squash method",
			methodFlag: "squash",
			expected:   github.MergeMethodSquash,
		},
		{
			name:       "merge method",
			methodFlag: "merge",
			expected:   github.MergeMethodMerge,
		},
		{
			name:       "rebase method",
			methodFlag: "rebase",
			expected:   github.MergeMethodRebase,
		},
		{
			name:       "invalid method returns error",
			methodFlag: "invalid",
			wantErr:    "invalid merge method: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// resolveMergeMethod with a non-empty flag doesn't need a full app.Context
			result, err := resolveMergeMethod(nil, tt.methodFlag)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatMergeDrainPlan(t *testing.T) {
	t.Parallel()

	t.Run("nil plan shows unknown branch", func(t *testing.T) {
		t.Parallel()
		result := formatMergeDrainPlan(nil, nil)
		require.Contains(t, result, "(unknown)")
		require.Contains(t, result, "(no branches to merge)")
	})

	t.Run("empty branches shows no branches", func(t *testing.T) {
		t.Parallel()
		plan := &mergeAction.Plan{
			CurrentBranch:   "feature",
			BranchesToMerge: nil,
		}
		result := formatMergeDrainPlan(plan, &mergeAction.PlanValidation{Valid: true})
		require.Contains(t, result, "feature")
		require.Contains(t, result, "(no branches to merge)")
	})

	t.Run("shows all branches in order", func(t *testing.T) {
		t.Parallel()
		plan := &mergeAction.Plan{
			CurrentBranch: "branch-c",
			BranchesToMerge: []mergeAction.BranchMergeInfo{
				{BranchName: "branch-a", PRNumber: 101},
				{BranchName: "branch-b", PRNumber: 102},
				{BranchName: "branch-c", PRNumber: 103},
			},
		}
		result := formatMergeDrainPlan(plan, &mergeAction.PlanValidation{Valid: true})
		require.Contains(t, result, "1. Merge PR #101 (branch-a)")
		require.Contains(t, result, "2. Merge PR #102 (branch-b)")
		require.Contains(t, result, "3. Merge PR #103 (branch-c)")
		require.Contains(t, result, "Total: 3 PRs to drain")
	})
}

func TestResolveDrainTargetBranch(t *testing.T) {
	t.Parallel()

	t.Run("uses explicit branch option", func(t *testing.T) {
		t.Parallel()
		branch := resolveDrainTargetBranch(mergeDrainOptions{branch: "explicit-branch"}, &mergeAction.Plan{
			CurrentBranch: "plan-branch",
		})
		require.Equal(t, "explicit-branch", branch)
	})

	t.Run("falls back to plan current branch", func(t *testing.T) {
		t.Parallel()
		branch := resolveDrainTargetBranch(mergeDrainOptions{}, &mergeAction.Plan{
			CurrentBranch: "plan-branch",
		})
		require.Equal(t, "plan-branch", branch)
	})

	t.Run("returns empty when neither option nor plan branch exist", func(t *testing.T) {
		t.Parallel()
		branch := resolveDrainTargetBranch(mergeDrainOptions{}, nil)
		require.Empty(t, branch)
	})
}

func TestNewShipCmdSquashAlias(t *testing.T) {
	t.Parallel()

	t.Run("ship command has squash alias", func(t *testing.T) {
		t.Parallel()
		cmd := NewShipCmd(nil)
		require.Contains(t, cmd.Aliases, "squash")
	})
}

func TestShipDefaultWait(t *testing.T) {
	t.Parallel()

	t.Run("ship defaults to wait=true", func(t *testing.T) {
		t.Parallel()
		cmd := NewShipCmd(nil)
		waitFlag := cmd.Flags().Lookup("wait")
		require.NotNil(t, waitFlag)
		require.Equal(t, "true", waitFlag.DefValue)
	})

	t.Run("next defaults to wait=false", func(t *testing.T) {
		t.Parallel()
		cmd := NewNextCmd(nil)
		waitFlag := cmd.Flags().Lookup("wait")
		require.NotNil(t, waitFlag)
		require.Equal(t, "false", waitFlag.DefValue)
	})
}
