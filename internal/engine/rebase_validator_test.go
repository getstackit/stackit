package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestValidateRebases(t *testing.T) {
	t.Parallel()

	t.Run("returns success for empty specs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		result, err := s.Engine.ValidateRebases(context.Background(), []engine.RebaseSpec{})
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Empty(t, result.FailedBranch)
		require.Empty(t, result.ErrorMessage)
	})

	t.Run("validates single successful rebase", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Add a commit to main so rebase actually does something
		s.Checkout("main").
			Commit("main update").
			Checkout("branch1")

		// Get revisions
		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)
		branch1OldBase, err := s.Engine.Git().GetMergeBase("main", "branch1")
		require.NoError(t, err)

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Empty(t, result.FailedBranch)
		require.NotEmpty(t, result.NewSHAs["branch1"])
	})

	t.Run("detects conflict during rebase", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a file on main first
		s.Checkout("main").
			CommitChange("conflicting-file.txt", "original version")

		// Save this as the old base
		oldBase, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		// Create branch1 from main and modify the file
		s.CreateBranch("branch1").
			CommitChange("conflicting-file.txt", "branch1 version").
			TrackBranch("branch1", "main")

		// Now update main with a DIFFERENT conflicting change
		s.Checkout("main").
			CommitChange("conflicting-file.txt", "main conflicting version")

		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: oldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		require.Contains(t, result.ErrorMessage, "conflict")
	})

	t.Run("validates chained rebases in sequence", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "branch2",
			})

		// Add a commit to main
		s.Checkout("main").
			Commit("main update")

		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		branch1OldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")
		branch1Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		branch2Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch2"))

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
			{
				Branch:      "branch2",
				NewParent:   branch1Rev, // Will use rebased SHA from first spec
				OldUpstream: branch1Rev,
			},
			{
				Branch:      "branch3",
				NewParent:   branch2Rev, // Will use rebased SHA from second spec
				OldUpstream: branch2Rev,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)
		require.NotEmpty(t, result.NewSHAs["branch1"])
		require.NotEmpty(t, result.NewSHAs["branch2"])
		require.NotEmpty(t, result.NewSHAs["branch3"])
	})

	t.Run("stops at first conflict in chain", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Setup: main has a file, branch1 modifies it, branch2 is clean
		s.Checkout("main").
			CommitChange("file.txt", "original")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())

		// branch1 modifies the same file - will conflict
		s.CreateBranch("branch1").
			CommitChange("file.txt", "branch1 change").
			TrackBranch("branch1", "main")

		branch1Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))

		// branch2 is clean, no conflict
		s.CreateBranch("branch2").
			CommitChange("other-file.txt", "branch2 change").
			TrackBranch("branch2", "branch1")

		// Now update main with conflicting change
		s.Checkout("main").
			CommitChange("file.txt", "main conflicting change")

		newMainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   newMainRev,
				OldUpstream: mainRev, // Old main before conflict
			},
			{
				Branch:      "branch2",
				NewParent:   branch1Rev,
				OldUpstream: branch1Rev,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		// branch2 should not have been processed
		require.Empty(t, result.NewSHAs["branch2"])
	})

	t.Run("uses rebased SHA for chained descendants when caller passes SHAs", func(t *testing.T) {
		// This test verifies that when specs use SHAs (not branch names) for NewParent,
		// the validator correctly substitutes the rebased SHA for subsequent rebases.
		// This is critical for move operations where descendants depend on parent's new position.
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
			})

		// Add a commit to main that will cause branch1 to have a different SHA after rebase
		s.Checkout("main").
			Commit("main update")

		// Get SHAs (this is what move.go does - resolves to SHAs before validation)
		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		branch1OldSHA, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		branch1OldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		// Build specs using SHAs (not branch names) - mimicking what move.go does
		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
			{
				Branch:      "branch2",
				NewParent:   branch1OldSHA, // Using branch1's OLD SHA
				OldUpstream: branch1OldSHA,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)

		// Both branches should have new SHAs
		require.NotEmpty(t, result.NewSHAs["branch1"])
		require.NotEmpty(t, result.NewSHAs["branch2"])

		// branch1's new SHA should be different from old (it was rebased)
		require.NotEqual(t, branch1OldSHA, result.NewSHAs["branch1"])
	})

	t.Run("does not modify actual branch refs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Get original SHA
		originalSHA, err := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		require.NoError(t, err)

		// Add commit to main
		s.Checkout("main").
			Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		branch1OldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)

		// Verify branch1 SHA is unchanged
		currentSHA, err := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		require.NoError(t, err)
		require.Equal(t, originalSHA, currentSHA, "branch ref should not be modified by validation")
	})

	t.Run("sets ValidationErrorConflict for merge conflicts", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a file on main first
		s.Checkout("main").
			CommitChange("conflicting-file.txt", "original version")

		// Save this as the old base
		oldBase, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		// Create branch1 from main and modify the file
		s.CreateBranch("branch1").
			CommitChange("conflicting-file.txt", "branch1 version").
			TrackBranch("branch1", "main")

		// Now update main with a DIFFERENT conflicting change
		s.Checkout("main").
			CommitChange("conflicting-file.txt", "main conflicting version")

		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: oldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		require.Equal(t, engine.ValidationErrorConflict, result.ErrorType, "should set ValidationErrorConflict for merge conflicts")
		require.Contains(t, result.ErrorMessage, "conflict")
	})

	t.Run("sets ValidationErrorNone for successful validation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		// Add a commit to main so rebase actually does something
		s.Checkout("main").
			Commit("main update").
			Checkout("branch1")

		// Get revisions
		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)
		branch1OldBase, err := s.Engine.Git().GetMergeBase("main", "branch1")
		require.NoError(t, err)

		specs := []engine.RebaseSpec{
			{
				Branch:      "branch1",
				NewParent:   mainRev,
				OldUpstream: branch1OldBase,
			},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Equal(t, engine.ValidationErrorNone, result.ErrorType, "should set ValidationErrorNone for successful validation")
	})
}

func TestValidateRebasesUsesRerereResolvedConflicts(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	g := s.Engine.Git()
	require.NoError(t, g.SetConfig("rerere.enabled", "true"))
	require.NoError(t, g.SetConfig("rerere.autoupdate", "true"))

	s.Checkout("main").
		CommitChange("conflicting-file.txt", "original version")

	oldBase, err := s.Engine.GetRevision(s.Engine.Trunk())
	require.NoError(t, err)

	s.CreateBranch("branch1").
		CommitChange("conflicting-file.txt", "branch version")

	s.Checkout("main").
		CommitChange("conflicting-file.txt", "main version")

	result, err := g.Rebase(context.Background(), "branch1", "main", oldBase)
	require.NoError(t, err)
	require.Equal(t, git.RebaseConflict, result.Result)
	require.True(t, g.IsRebaseInProgress(context.Background()))

	require.NoError(t, s.Scene.Repo.ResolveMergeConflicts())
	require.NoError(t, s.Scene.Repo.MarkMergeConflictsAsResolved())
	result, err = g.RebaseContinue(context.Background())
	require.NoError(t, err)
	require.Equal(t, git.RebaseDone, result.Result)

	require.NoError(t, s.Scene.Repo.CheckoutBranch("main"))
	require.NoError(t, s.Scene.Repo.RunGitCommand("checkout", "-b", "branch2", oldBase))
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("branch version", "conflicting-file.txt"))

	validation, err := s.Engine.ValidateRebases(context.Background(), []engine.RebaseSpec{
		{
			Branch:      "branch2",
			NewParent:   "main",
			OldUpstream: oldBase,
		},
	})
	require.NoError(t, err)
	require.True(t, validation.Success)
	require.NotEmpty(t, validation.NewSHAs["branch2"])
}

func TestRestackBranchesPropagatesRerereResolvedCount(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)
	g := s.Engine.Git()
	require.NoError(t, g.SetConfig("rerere.enabled", "true"))
	require.NoError(t, g.SetConfig("rerere.autoupdate", "true"))

	s.Checkout("main").
		CommitChange("conflicting-file.txt", "original version")

	oldBase, err := s.Engine.GetRevision(s.Engine.Trunk())
	require.NoError(t, err)

	s.CreateBranch("branch2").
		CommitChange("conflicting-file.txt", "branch version").
		TrackBranch("branch2", "main")

	s.Checkout("main").
		CommitChange("conflicting-file.txt", "main version")

	// Record a rerere resolution for this exact conflict using a throwaway
	// branch created off oldBase with the same content as branch2.
	require.NoError(t, s.Scene.Repo.RunGitCommand("checkout", "-b", "branch1", oldBase))
	require.NoError(t, s.Scene.Repo.CreateChangeAndCommit("branch version", "conflicting-file.txt"))
	rebaseResult, err := g.Rebase(context.Background(), "branch1", "main", oldBase)
	require.NoError(t, err)
	require.Equal(t, git.RebaseConflict, rebaseResult.Result)
	require.NoError(t, s.Scene.Repo.ResolveMergeConflicts())
	require.NoError(t, s.Scene.Repo.MarkMergeConflictsAsResolved())
	rebaseResult, err = g.RebaseContinue(context.Background())
	require.NoError(t, err)
	require.Equal(t, git.RebaseDone, rebaseResult.Result)

	// Now restack branch2 through the engine. The same conflict should be
	// auto-resolved by rerere and the count should propagate through
	// RestackBranchResult.
	branch2 := s.Engine.GetBranch("branch2")
	require.NotNil(t, branch2)
	batchResult, err := s.Engine.RestackBranches(context.Background(), engine.BranchesOf(branch2))
	require.NoError(t, err)
	require.Equal(t, engine.RestackDone, batchResult.Results["branch2"].Result)
	require.Greater(t, batchResult.Results["branch2"].RerereResolvedCount, 0)
}

func TestRestackBranchesWithValidatedRebasesUsesValidationSHA(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"branch1": "main",
		})

	s.Checkout("main").
		Commit("main update").
		Checkout("branch1")

	mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
	require.NoError(t, err)
	oldBase, err := s.Engine.Git().GetMergeBase("main", "branch1")
	require.NoError(t, err)

	validation, err := s.Engine.ValidateRebases(context.Background(), []engine.RebaseSpec{{
		Branch:      "branch1",
		NewParent:   "main",
		OldUpstream: oldBase,
	}})
	require.NoError(t, err)
	require.True(t, validation.Success)
	require.NotEmpty(t, validation.NewSHAs["branch1"])

	branch1 := s.Engine.GetBranch("branch1")
	oldBranchRev, err := branch1.GetRevision()
	require.NoError(t, err)
	require.NotEqual(t, validation.NewSHAs["branch1"], oldBranchRev)

	result, err := s.Engine.RestackBranchesWithValidatedRebases(context.Background(), engine.BranchesOf(branch1), validation, nil)
	require.NoError(t, err)
	require.Equal(t, engine.RestackDone, result.Results["branch1"].Result)

	newBranchRev, err := s.Engine.GetBranch("branch1").GetRevision()
	require.NoError(t, err)
	require.Equal(t, validation.NewSHAs["branch1"], newBranchRev)

	meta, err := s.Engine.Git().ReadMetadata("branch1")
	require.NoError(t, err)
	require.NotNil(t, meta.GetParentBranchRevision())
	require.Equal(t, mainRev, *meta.GetParentBranchRevision())
}

func TestRestackBranchesWithValidatedPlanAppliesFrozenBranch(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("parent").
		Commit("parent change").
		CreateBranch("child").
		Commit("child change")

	require.NoError(t, s.Engine.TrackBranch(context.Background(), "parent", "main"))
	require.NoError(t, s.Engine.TrackBranch(context.Background(), "child", "parent"))

	child := s.Engine.GetBranch("child")
	_, err := s.Engine.SetFrozen(context.Background(), engine.BranchesOf(child), true)
	require.NoError(t, err)

	s.CreateBranch("remote-child").
		Commit("remote child")
	remoteSHA, err := s.Engine.GetBranch("remote-child").GetRevision()
	require.NoError(t, err)
	s.RunGit("update-ref", "refs/heads/origin/child", remoteSHA)

	plan, err := s.Engine.PlanRestack(context.Background(), engine.BranchesOf(child))
	require.NoError(t, err)
	require.Empty(t, plan.Specs)
	require.True(t, plan.ApplyMap["child"])

	validation := &engine.RebaseValidation{Success: true, NewSHAs: map[string]string{}, RerereResolved: map[string]int{}}
	result, err := s.Engine.RestackBranchesWithValidatedPlan(context.Background(), engine.BranchesOf(child), validation, plan, nil)
	require.NoError(t, err)
	require.Equal(t, engine.RestackDone, result.Results["child"].Result)

	childSHA, err := s.Scene.Repo.GetBranchSHA("child")
	require.NoError(t, err)
	require.Equal(t, remoteSHA, childSHA)
}

func TestRestackBranchesWithValidatedPlanAppliesAnchorBranch(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"anchor": "main",
		})

	anchor := s.Engine.GetBranch("anchor")
	require.NoError(t, s.Engine.SetBranchType(anchor, git.BranchTypeWorktreeAnchor))
	oldSHA, err := anchor.GetRevision()
	require.NoError(t, err)

	s.Checkout("main").
		Commit("main update")
	trunkSHA, err := s.Engine.Trunk().GetRevision()
	require.NoError(t, err)
	require.NotEqual(t, oldSHA, trunkSHA)

	plan, err := s.Engine.PlanRestack(context.Background(), engine.BranchesOf(anchor))
	require.NoError(t, err)
	require.Empty(t, plan.Specs)
	require.True(t, plan.ApplyMap["anchor"])

	validation := &engine.RebaseValidation{Success: true, NewSHAs: map[string]string{}, RerereResolved: map[string]int{}}
	result, err := s.Engine.RestackBranchesWithValidatedPlan(context.Background(), engine.BranchesOf(anchor), validation, plan, nil)
	require.NoError(t, err)
	require.Equal(t, engine.RestackDone, result.Results["anchor"].Result)

	anchorSHA, err := s.Scene.Repo.GetBranchSHA("anchor")
	require.NoError(t, err)
	require.Equal(t, trunkSHA, anchorSHA)
}

func TestRestackBranchesWithValidatedPlanReparentsMergedParent(t *testing.T) {
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
		WithStack(map[string]string{
			"branch1": "main",
			"branch2": "branch1",
		})

	s.Checkout("main").
		RunGit("merge", "branch1", "--no-ff", "-m", "Merge branch1")
	s.RunGit("branch", "-D", "branch1")
	require.NoError(t, s.Engine.Rebuild("main"))

	branch2 := s.Engine.GetBranch("branch2")
	plan, err := s.Engine.PlanRestack(context.Background(), engine.BranchesOf(branch2))
	require.NoError(t, err)
	require.Len(t, plan.Specs, 1)
	require.True(t, plan.ApplyMap["branch2"])
	require.True(t, plan.Items["branch2"].Reparented)
	require.Equal(t, "branch1", plan.Items["branch2"].OldParent)
	require.Equal(t, "main", plan.Items["branch2"].NewParent)

	validation, err := s.Engine.ValidateRebases(context.Background(), plan.Specs)
	require.NoError(t, err)
	require.True(t, validation.Success)

	result, err := s.Engine.RestackBranchesWithValidatedPlan(context.Background(), engine.BranchesOf(branch2), validation, plan, nil)
	require.NoError(t, err)
	require.Equal(t, engine.RestackDone, result.Results["branch2"].Result)
	require.True(t, result.Results["branch2"].Reparented)
	require.Equal(t, "branch1", result.Results["branch2"].OldParent)
	require.Equal(t, "main", result.Results["branch2"].NewParent)

	require.NoError(t, s.Engine.Rebuild("main"))
	branch2 = s.Engine.GetBranch("branch2")
	require.Equal(t, "main", branch2.GetParentOrTrunk())
	history := branch2.GetMergedDownstack()
	require.Len(t, history, 1)
	require.Equal(t, "branch1", history[0].BranchName)
}

func TestValidateRebasesParallel(t *testing.T) {
	t.Parallel()

	t.Run("validates wide stack with multiple siblings in parallel", func(t *testing.T) {
		t.Parallel()
		// Create a wide stack: main has 5 child branches (all at depth 1)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature-a": "main",
				"feature-b": "main",
				"feature-c": "main",
				"feature-d": "main",
				"feature-e": "main",
			})

		// Add a commit to main so all branches need rebasing
		s.Checkout("main").
			Commit("main update")

		mainRev, err := s.Engine.GetRevision(s.Engine.Trunk())
		require.NoError(t, err)

		// Get old bases for all branches (they all share the same old main)
		oldBase, _ := s.Engine.Git().GetMergeBase("main", "feature-a")

		// Build specs for all branches
		specs := []engine.RebaseSpec{
			{Branch: "feature-a", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "feature-b", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "feature-c", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "feature-d", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "feature-e", NewParent: mainRev, OldUpstream: oldBase},
		}

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success, "validation failed for branch %q: %s (error type: %d, conflicting files: %v)",
			result.FailedBranch, result.ErrorMessage, result.ErrorType, result.ConflictingFiles)

		// All branches should have new SHAs
		require.NotEmpty(t, result.NewSHAs["feature-a"], "feature-a should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-b"], "feature-b should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-c"], "feature-c should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-d"], "feature-d should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-e"], "feature-e should have new SHA")
	})

	t.Run("validates mixed depth stack correctly", func(t *testing.T) {
		t.Parallel()
		// Create a mixed stack:
		// main
		//   ├── feature-a (depth 1)
		//   │   ├── feature-a1 (depth 2)
		//   │   └── feature-a2 (depth 2)
		//   └── feature-b (depth 1)
		//       └── feature-b1 (depth 2)
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"feature-a":  "main",
				"feature-a1": "feature-a",
				"feature-a2": "feature-a",
				"feature-b":  "main",
				"feature-b1": "feature-b",
			})

		// Add commit to main
		s.Checkout("main").
			Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		oldMainBase, _ := s.Engine.Git().GetMergeBase("main", "feature-a")

		// Get SHAs for depth-1 branches (for depth-2 rebases)
		featureARev, _ := s.Engine.GetRevision(s.Engine.GetBranch("feature-a"))
		featureBRev, _ := s.Engine.GetRevision(s.Engine.GetBranch("feature-b"))

		specs := []engine.RebaseSpec{
			// Depth 1: feature-a and feature-b (can run in parallel)
			{Branch: "feature-a", NewParent: mainRev, OldUpstream: oldMainBase},
			{Branch: "feature-b", NewParent: mainRev, OldUpstream: oldMainBase},
			// Depth 2: children (can run in parallel after depth 1 completes)
			{Branch: "feature-a1", NewParent: featureARev, OldUpstream: featureARev},
			{Branch: "feature-a2", NewParent: featureARev, OldUpstream: featureARev},
			{Branch: "feature-b1", NewParent: featureBRev, OldUpstream: featureBRev},
		}

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success, "validation failed for branch %q: %s (error type: %d, conflicting files: %v)",
			result.FailedBranch, result.ErrorMessage, result.ErrorType, result.ConflictingFiles)

		// All branches should have new SHAs
		require.NotEmpty(t, result.NewSHAs["feature-a"], "feature-a should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-a1"], "feature-a1 should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-a2"], "feature-a2 should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-b"], "feature-b should have new SHA")
		require.NotEmpty(t, result.NewSHAs["feature-b1"], "feature-b1 should have new SHA")
	})

	t.Run("detects conflict in parallel execution", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create file on main
		s.Checkout("main").
			CommitChange("file.txt", "original")

		oldBase, _ := s.Engine.GetRevision(s.Engine.Trunk())

		// Create two branches that both modify the same file
		s.CreateBranch("branch1").
			CommitChange("file.txt", "branch1 version").
			TrackBranch("branch1", "main")

		s.Checkout("main").
			CreateBranch("branch2").
			CommitChange("other-file.txt", "branch2 change").
			TrackBranch("branch2", "main")

		// Update main with conflicting change
		s.Checkout("main").
			CommitChange("file.txt", "main conflicting version")

		newMainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: newMainRev, OldUpstream: oldBase},
			{Branch: "branch2", NewParent: newMainRev, OldUpstream: oldBase},
		}

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		require.Contains(t, result.ErrorMessage, "conflict")
	})

	t.Run("validates complex stack correctly", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "branch1",
				"branch3": "main",
				"branch4": "branch3",
			})

		// Add commit to main
		s.Checkout("main").
			Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		oldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")
		branch1Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch1"))
		branch3Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("branch3"))

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch2", NewParent: branch1Rev, OldUpstream: branch1Rev},
			{Branch: "branch3", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch4", NewParent: branch3Rev, OldUpstream: branch3Rev},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success, "validation failed for branch %q: %s (error type: %d, conflicting files: %v)",
			result.FailedBranch, result.ErrorMessage, result.ErrorType, result.ConflictingFiles)
		require.Len(t, result.NewSHAs, 4)

		// All branches should have new SHAs
		for _, spec := range specs {
			require.NotEmpty(t, result.NewSHAs[spec.Branch], "should have SHA for %s", spec.Branch)
		}
	})

	t.Run("validates multiple sibling branches", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "main",
				"branch3": "main",
			})

		s.Checkout("main").Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		oldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch2", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch3", NewParent: mainRev, OldUpstream: oldBase},
		}

		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success, "validation failed for branch %q: %s (error type: %d, conflicting files: %v)",
			result.FailedBranch, result.ErrorMessage, result.ErrorType, result.ConflictingFiles)
		require.Len(t, result.NewSHAs, 3)
	})

	t.Run("handles empty specs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), []engine.RebaseSpec{})
		require.NoError(t, err)
		require.True(t, result.Success)
		require.Empty(t, result.NewSHAs)
	})

	t.Run("handles single spec efficiently", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		s.Checkout("main").Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		oldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: mainRev, OldUpstream: oldBase},
		}

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), specs)
		require.NoError(t, err)
		require.True(t, result.Success, "validation failed for branch %q: %s (error type: %d, conflicting files: %v)",
			result.FailedBranch, result.ErrorMessage, result.ErrorType, result.ConflictingFiles)
		require.NotEmpty(t, result.NewSHAs["branch1"], "branch1 should have new SHA")
	})

	t.Run("stops on first conflict in level", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create file on main
		s.Checkout("main").
			CommitChange("file.txt", "original")

		oldBase, _ := s.Engine.GetRevision(s.Engine.Trunk())

		// Create three siblings, one will conflict
		s.CreateBranch("branch1").
			CommitChange("file.txt", "branch1 conflict").
			TrackBranch("branch1", "main")

		s.Checkout("main").
			CreateBranch("branch2").
			CommitChange("other.txt", "branch2 safe").
			TrackBranch("branch2", "main")

		s.Checkout("main").
			CreateBranch("branch3").
			CommitChange("third.txt", "branch3 safe").
			TrackBranch("branch3", "main")

		// Update main with conflicting change
		s.Checkout("main").
			CommitChange("file.txt", "main conflicting")

		newMainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: newMainRev, OldUpstream: oldBase},
			{Branch: "branch2", NewParent: newMainRev, OldUpstream: oldBase},
			{Branch: "branch3", NewParent: newMainRev, OldUpstream: oldBase},
		}

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Equal(t, "branch1", result.FailedBranch)
		require.Contains(t, result.ErrorMessage, "conflict")
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
				"branch2": "main",
				"branch3": "main",
			})

		s.Checkout("main").Commit("main update")

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		oldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch2", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch3", NewParent: mainRev, OldUpstream: oldBase},
		}

		// Create a canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := s.Engine.ValidateRebasesParallel(ctx, specs)
		require.NoError(t, err)
		require.False(t, result.Success)
		require.Contains(t, result.ErrorMessage, "canceled")
	})

	t.Run("rejects duplicate branches in specs", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch1": "main",
			})

		mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
		oldBase, _ := s.Engine.Git().GetMergeBase("main", "branch1")

		specs := []engine.RebaseSpec{
			{Branch: "branch1", NewParent: mainRev, OldUpstream: oldBase},
			{Branch: "branch1", NewParent: mainRev, OldUpstream: oldBase}, // Duplicate!
		}

		result, err := s.Engine.ValidateRebasesParallel(context.Background(), specs)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "duplicate branch")
	})
}
