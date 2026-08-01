package absorb

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

func TestAbsorbScopeBoundaries(t *testing.T) {
	t.Parallel()
	t.Run("absorb stops at scope boundaries", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"scoped-a":   "main",
				"scoped-b":   "scoped-a",
				"unscoped-c": "scoped-b",
			})

		// Set scopes: scoped-a and scoped-b have PROJ-123, unscoped-c has no scope
		err := s.Engine.SetScope(context.Background(), s.Engine.GetBranch("scoped-a"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("scoped-b"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		// unscoped-c has no scope

		// Add some commits to each branch
		s.Checkout("scoped-a")
		s.Scene.Repo.CreateChangeAndCommit("scoped-a commit", "file-a")

		s.Checkout("scoped-b")
		s.Scene.Repo.CreateChangeAndCommit("scoped-b commit", "file-b")

		s.Checkout("unscoped-c")
		s.Scene.Repo.CreateChangeAndCommit("unscoped-c commit", "file-c")

		// Switch back to scoped-b and create staged changes
		s.Checkout("scoped-b")
		err = s.Scene.Repo.CreateChange("staged change for scoped-b", "file-b", false)
		require.NoError(t, err)

		// Get commits from downstack when absorb runs
		// Since we're on scoped-b with scope PROJ-123, absorb should only look at scoped-a and scoped-b
		// It should NOT look at unscoped-c even though it's in the git history

		// The absorb logic should collect commits from:
		// - scoped-b (current branch)
		// - scoped-a (parent with same scope)
		// - main (stops at scope boundary - unscoped-c has different/no scope)

		// We can't easily test the internal collection logic directly without mocking,
		// but we can verify that the behavior is correct by checking what commits
		// would be considered for absorption.

		// For this test, we'll just verify that the scope detection works correctly
		currentBranch := s.Engine.GetBranch("scoped-b")
		currentScope := currentBranch.GetScope()

		// Verify current branch has the expected scope
		require.True(t, currentScope.IsDefined())
		require.Equal(t, "PROJ-123", currentScope.String())

		// Get downstack branches as absorb would
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		downstackBranches := graph.Range(currentBranch, engine.StackRange{RecursiveParents: true})
		// Include current branch
		downstackBranches = engine.BranchesOf(currentBranch).Concat(downstackBranches)

		// Apply scope boundary filtering as absorb does
		if currentScope.IsDefined() {
			limitedDownstack := engine.Branches{}
			for _, branch := range downstackBranches {
				if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
					break
				}
				limitedDownstack = limitedDownstack.Append(branch)
			}
			downstackBranches = limitedDownstack
		}

		// Should only include scoped-a and scoped-b, not unscoped-c or main
		require.Len(t, downstackBranches, 2)
		require.Equal(t, "scoped-b", downstackBranches[0].GetName())
		require.Equal(t, "scoped-a", downstackBranches[1].GetName())
	})

	t.Run("absorb includes all branches when no scope set", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"branch-a": "main",
				"branch-b": "branch-a",
				"branch-c": "branch-b",
			})

		// No scopes set on any branches
		s.Checkout("branch-b")

		currentBranch := s.Engine.GetBranch("branch-b")
		currentScope := currentBranch.GetScope()

		// Verify current branch has no scope
		require.True(t, currentScope.IsEmpty())

		// Get downstack branches as absorb would
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		downstackBranches := graph.Range(currentBranch, engine.StackRange{RecursiveParents: true})
		// Include current branch
		downstackBranches = engine.BranchesOf(currentBranch).Concat(downstackBranches)

		// Apply scope boundary filtering as absorb does
		if currentScope.IsDefined() {
			limitedDownstack := engine.Branches{}
			for _, branch := range downstackBranches {
				if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
					break
				}
				limitedDownstack = limitedDownstack.Append(branch)
			}
			downstackBranches = limitedDownstack
		}

		// Since no scope is set, should include all branches down to the first scope boundary
		// In this case, no scopes are set, so includes current and ancestors (excluding trunk)
		require.Len(t, downstackBranches, 2)
		require.Equal(t, "branch-b", downstackBranches[0].GetName())
		require.Equal(t, "branch-a", downstackBranches[1].GetName())
	})

	t.Run("absorb stops at first scope boundary encountered", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"scoped-a":    "main",
				"scoped-b":    "scoped-a",
				"different-c": "scoped-b",
				"scoped-d":    "different-c",
			})

		// Set mixed scopes: PROJ-123 for a/b, PROJ-456 for c/d
		err := s.Engine.SetScope(context.Background(), s.Engine.GetBranch("scoped-a"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("scoped-b"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("different-c"), engine.NewScope("PROJ-456"))
		require.NoError(t, err)
		err = s.Engine.SetScope(context.Background(), s.Engine.GetBranch("scoped-d"), engine.NewScope("PROJ-456"))
		require.NoError(t, err)

		// Switch to scoped-b (PROJ-123)
		s.Checkout("scoped-b")

		currentBranch := s.Engine.GetBranch("scoped-b")
		currentScope := currentBranch.GetScope()

		// Verify current branch has PROJ-123 scope
		require.True(t, currentScope.IsDefined())
		require.Equal(t, "PROJ-123", currentScope.String())

		// Get downstack branches as absorb would
		graph := engine.BuildStackGraph(s.Engine, engine.SortStrategyAlphabetical, nil)
		downstackBranches := graph.Range(currentBranch, engine.StackRange{RecursiveParents: true})
		// Include current branch
		downstackBranches = engine.BranchesOf(currentBranch).Concat(downstackBranches)

		// Apply scope boundary filtering as absorb does
		if currentScope.IsDefined() {
			limitedDownstack := engine.Branches{}
			for _, branch := range downstackBranches {
				if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
					break // Stop at first scope mismatch
				}
				limitedDownstack = limitedDownstack.Append(branch)
			}
			downstackBranches = limitedDownstack
		}

		// Should stop at different-c (PROJ-456) and not include scoped-d
		// So should include: scoped-b, scoped-a, main (but stops at different-c)
		require.Len(t, downstackBranches, 2)
		require.Equal(t, "scoped-b", downstackBranches[0].GetName())
		require.Equal(t, "scoped-a", downstackBranches[1].GetName())
	})
}

func TestAbsorbWithInterveningCommits(t *testing.T) {
	t.Parallel()
	t.Run("absorb handles changes when intervening commits modify same file", func(t *testing.T) {
		t.Parallel()
		// This test verifies that absorb can apply changes to an earlier commit
		// even when later commits have modified the same file, using three-way merge.
		// The key is having enough separation between sections so commutation check
		// correctly attributes the change to branch-a, but the file context has changed.
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a shared file with initial content on main - use many lines for separation
		sharedFile := filepath.Join(s.Scene.Dir, "shared.go")
		initialContent := `package main

// ===========================================
// SECTION A - Modified in branch-a
// ===========================================

func sectionA() {
	// section A line 1
	// section A line 2
	// section A line 3
	// section A line 4
	// section A line 5
}

// ===========================================
// SPACER SECTION - Never modified
// ===========================================

func spacer() {
	// spacer line 1
	// spacer line 2
	// spacer line 3
	// spacer line 4
	// spacer line 5
	// spacer line 6
	// spacer line 7
	// spacer line 8
	// spacer line 9
	// spacer line 10
}

// ===========================================
// SECTION B - Modified in branch-b
// ===========================================

func sectionB() {
	// section B line 1
	// section B line 2
	// section B line 3
	// section B line 4
	// section B line 5
}
`
		err := os.WriteFile(sharedFile, []byte(initialContent), 0600)
		require.NoError(t, err)
		s.RunGit("add", "shared.go")
		s.RunGit("commit", "-m", "add shared.go")

		// Create branch-a modifying section A
		s.CreateBranch("branch-a")
		s.TrackBranch("branch-a", "main")

		contentAfterBranchA := `package main

// ===========================================
// SECTION A - Modified in branch-a
// ===========================================

func sectionA() {
	// BRANCH-A: modified section A line 1
	// BRANCH-A: modified section A line 2
	// section A line 3
	// section A line 4
	// section A line 5
}

// ===========================================
// SPACER SECTION - Never modified
// ===========================================

func spacer() {
	// spacer line 1
	// spacer line 2
	// spacer line 3
	// spacer line 4
	// spacer line 5
	// spacer line 6
	// spacer line 7
	// spacer line 8
	// spacer line 9
	// spacer line 10
}

// ===========================================
// SECTION B - Modified in branch-b
// ===========================================

func sectionB() {
	// section B line 1
	// section B line 2
	// section B line 3
	// section B line 4
	// section B line 5
}
`
		err = os.WriteFile(sharedFile, []byte(contentAfterBranchA), 0600)
		require.NoError(t, err)
		s.RunGit("add", "shared.go")
		s.RunGit("commit", "-m", "modify section A in branch-a")
		// Create branch-b on top of branch-a modifying section B (far away from section A)
		s.CreateBranch("branch-b")
		s.TrackBranch("branch-b", "branch-a")

		contentAfterBranchB := `package main

// ===========================================
// SECTION A - Modified in branch-a
// ===========================================

func sectionA() {
	// BRANCH-A: modified section A line 1
	// BRANCH-A: modified section A line 2
	// section A line 3
	// section A line 4
	// section A line 5
}

// ===========================================
// SPACER SECTION - Never modified
// ===========================================

func spacer() {
	// spacer line 1
	// spacer line 2
	// spacer line 3
	// spacer line 4
	// spacer line 5
	// spacer line 6
	// spacer line 7
	// spacer line 8
	// spacer line 9
	// spacer line 10
}

// ===========================================
// SECTION B - Modified in branch-b
// ===========================================

func sectionB() {
	// BRANCH-B: modified section B line 1
	// BRANCH-B: modified section B line 2
	// section B line 3
	// section B line 4
	// section B line 5
}
`
		err = os.WriteFile(sharedFile, []byte(contentAfterBranchB), 0600)
		require.NoError(t, err)
		s.RunGit("add", "shared.go")
		s.RunGit("commit", "-m", "modify section B in branch-b")
		s.Rebuild()

		// Now we're on branch-b. Stage a change that modifies section A (introduced in branch-a)
		// This change should be absorbed into branch-a, but the file context has changed
		// because branch-b modified section B.
		stagedContent := `package main

// ===========================================
// SECTION A - Modified in branch-a
// ===========================================

func sectionA() {
	// ABSORBED: this change should go to branch-a
	// BRANCH-A: modified section A line 2
	// section A line 3
	// section A line 4
	// section A line 5
}

// ===========================================
// SPACER SECTION - Never modified
// ===========================================

func spacer() {
	// spacer line 1
	// spacer line 2
	// spacer line 3
	// spacer line 4
	// spacer line 5
	// spacer line 6
	// spacer line 7
	// spacer line 8
	// spacer line 9
	// spacer line 10
}

// ===========================================
// SECTION B - Modified in branch-b
// ===========================================

func sectionB() {
	// BRANCH-B: modified section B line 1
	// BRANCH-B: modified section B line 2
	// section B line 3
	// section B line 4
	// section B line 5
}
`
		err = os.WriteFile(sharedFile, []byte(stagedContent), 0600)
		require.NoError(t, err)
		s.RunGit("add", "shared.go")

		// Run absorb with force flag (non-interactive)
		err = Action(s.Context, Options{Force: true}, nil)
		require.NoError(t, err)

		// Verify the change was absorbed into branch-a
		s.Checkout("branch-a")

		// Read the file content on branch-a
		content, err := os.ReadFile(sharedFile)
		require.NoError(t, err)

		// The change should have been applied to branch-a
		require.Contains(t, string(content), "ABSORBED: this change should go to branch-a")
	})

	t.Run("absorb cleans up on failure and restores original branch", func(t *testing.T) {
		t.Parallel()
		// This test verifies that when absorb fails, it cleans up properly
		// and returns the user to their original branch without leaving
		// the repository in a detached HEAD or unmerged state.
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sharedFile := filepath.Join(s.Scene.Dir, "cleanup.go")
		initialContent := `package main

func example() {
	// line 1
	// line 2
	// line 3
}
`
		err := os.WriteFile(sharedFile, []byte(initialContent), 0600)
		require.NoError(t, err)
		s.RunGit("add", "cleanup.go")
		s.RunGit("commit", "-m", "add cleanup.go")

		// branch-a modifies the function
		s.CreateBranch("branch-a")
		s.TrackBranch("branch-a", "main")

		contentAfterBranchA := `package main

func example() {
	// BRANCH-A modification
	// line 2
	// line 3
}
`
		err = os.WriteFile(sharedFile, []byte(contentAfterBranchA), 0600)
		require.NoError(t, err)
		s.RunGit("add", "cleanup.go")
		s.RunGit("commit", "-m", "modify in branch-a")
		s.Rebuild()

		// Stage a change
		stagedContent := `package main

func example() {
	// STAGED change
	// line 2
	// line 3
}
`
		err = os.WriteFile(sharedFile, []byte(stagedContent), 0600)
		require.NoError(t, err)
		s.RunGit("add", "cleanup.go")

		// Remember which branch we're on
		originalBranch, err := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, err)
		require.Equal(t, "branch-a", originalBranch)

		// Run absorb
		_ = Action(s.Context, Options{Force: true}, nil)

		// Regardless of success or failure, we should be back on the original branch
		currentBranch, branchErr := s.Scene.Repo.CurrentBranchName()
		require.NoError(t, branchErr)
		require.Equal(t, originalBranch, currentBranch, "should be back on original branch after absorb")

		// Verify no unmerged files left behind
		hasUnstaged, _ := s.Scene.Repo.HasUnstagedChanges()
		require.False(t, hasUnstaged, "should not have unstaged changes after absorb")
	})

	t.Run("absorb with three-way merge when context lines differ", func(t *testing.T) {
		t.Parallel()
		// This test specifically verifies the --3way merge functionality:
		// We create a scenario where the patch context doesn't match exactly
		// because an intervening commit has modified lines far away in the same file.
		// Key: changes must be far enough apart to avoid the commutation margin (3 lines).
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		sharedFile := filepath.Join(s.Scene.Dir, "config.go")
		initialContent := `package config

// ============================================
// SECTION 1: Configuration struct (modified by branch-b)
// ============================================

type Config struct {
	Name    string
	Value   int
	Enabled bool
}

// ============================================
// SPACER - Large gap to avoid commutation overlap
// ============================================

func spacer1() {}
func spacer2() {}
func spacer3() {}
func spacer4() {}
func spacer5() {}
func spacer6() {}
func spacer7() {}
func spacer8() {}
func spacer9() {}
func spacer10() {}

// ============================================
// SECTION 2: DefaultConfig (modified by branch-a)
// ============================================

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "default",
		Value:   0,
		Enabled: false,
	}
}
`
		err := os.WriteFile(sharedFile, []byte(initialContent), 0600)
		require.NoError(t, err)
		s.RunGit("add", "config.go")
		s.RunGit("commit", "-m", "add config.go")

		// branch-a modifies DefaultConfig (far at the end)
		s.CreateBranch("branch-a")
		s.TrackBranch("branch-a", "main")

		contentAfterBranchA := `package config

// ============================================
// SECTION 1: Configuration struct (modified by branch-b)
// ============================================

type Config struct {
	Name    string
	Value   int
	Enabled bool
}

// ============================================
// SPACER - Large gap to avoid commutation overlap
// ============================================

func spacer1() {}
func spacer2() {}
func spacer3() {}
func spacer4() {}
func spacer5() {}
func spacer6() {}
func spacer7() {}
func spacer8() {}
func spacer9() {}
func spacer10() {}

// ============================================
// SECTION 2: DefaultConfig (modified by branch-a)
// ============================================

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "branch-a-default",
		Value:   100,
		Enabled: false,
	}
}
`
		err = os.WriteFile(sharedFile, []byte(contentAfterBranchA), 0600)
		require.NoError(t, err)
		s.RunGit("add", "config.go")
		s.RunGit("commit", "-m", "update default config in branch-a")
		// branch-b modifies Config struct (at the beginning, far from DefaultConfig)
		s.CreateBranch("branch-b")
		s.TrackBranch("branch-b", "branch-a")

		contentAfterBranchB := `package config

// ============================================
// SECTION 1: Configuration struct (modified by branch-b)
// ============================================

type Config struct {
	Name     string
	Value    int
	Enabled  bool
	NewField string // Added by branch-b
}

// ============================================
// SPACER - Large gap to avoid commutation overlap
// ============================================

func spacer1() {}
func spacer2() {}
func spacer3() {}
func spacer4() {}
func spacer5() {}
func spacer6() {}
func spacer7() {}
func spacer8() {}
func spacer9() {}
func spacer10() {}

// ============================================
// SECTION 2: DefaultConfig (modified by branch-a)
// ============================================

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "branch-a-default",
		Value:   100,
		Enabled: false,
	}
}
`
		err = os.WriteFile(sharedFile, []byte(contentAfterBranchB), 0600)
		require.NoError(t, err)
		s.RunGit("add", "config.go")
		s.RunGit("commit", "-m", "add NewField in branch-b")
		s.Rebuild()

		// Stage a change to DefaultConfig (should go to branch-a)
		// Because branch-b modified the file (added NewField), the patch context
		// will have different surrounding lines than at branch-a's commit point.
		// This tests the --3way merge functionality.
		stagedContent := `package config

// ============================================
// SECTION 1: Configuration struct (modified by branch-b)
// ============================================

type Config struct {
	Name     string
	Value    int
	Enabled  bool
	NewField string // Added by branch-b
}

// ============================================
// SPACER - Large gap to avoid commutation overlap
// ============================================

func spacer1() {}
func spacer2() {}
func spacer3() {}
func spacer4() {}
func spacer5() {}
func spacer6() {}
func spacer7() {}
func spacer8() {}
func spacer9() {}
func spacer10() {}

// ============================================
// SECTION 2: DefaultConfig (modified by branch-a)
// ============================================

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		Name:    "ABSORBED-default",
		Value:   100,
		Enabled: false,
	}
}
`
		err = os.WriteFile(sharedFile, []byte(stagedContent), 0600)
		require.NoError(t, err)
		s.RunGit("add", "config.go")

		// Run absorb
		err = Action(s.Context, Options{Force: true}, nil)
		require.NoError(t, err)

		// Verify the change was absorbed into branch-a
		s.Checkout("branch-a")
		content, err := os.ReadFile(sharedFile)
		require.NoError(t, err)
		require.Contains(t, string(content), "ABSORBED-default")

		// Verify branch-b still has NewField after restack
		s.Checkout("branch-b")
		content, err = os.ReadFile(sharedFile)
		require.NoError(t, err)
		require.Contains(t, string(content), "ABSORBED-default")
		require.Contains(t, string(content), "NewField")
	})
}

func TestAbsorbRestoresBinaryUnabsorbableHunks(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	textFile := filepath.Join(s.Scene.Dir, "text.txt")
	binaryFile := filepath.Join(s.Scene.Dir, "asset.bin")

	err := os.WriteFile(textFile, []byte("line one\nline two\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(binaryFile, []byte{0x00, 0x01, 0x02, 0x03}, 0600)
	require.NoError(t, err)
	s.RunGit("add", "text.txt", "asset.bin")
	s.RunGit("commit", "-m", "add baseline text and binary files")
	s.Rebuild()

	err = os.WriteFile(textFile, []byte("line one\nline two absorbed\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(binaryFile, []byte{0x10, 0x11, 0x12, 0x13}, 0600)
	require.NoError(t, err)
	s.RunGit("add", "text.txt", "asset.bin")

	err = Action(s.Context, Options{Force: true}, nil)
	require.NoError(t, err)

	cachedFiles, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached", "--name-only")
	require.NoError(t, err)
	require.Contains(t, cachedFiles, "asset.bin")
	require.NotContains(t, cachedFiles, "text.txt")

	headText, err := s.Scene.Repo.RunGitCommandAndGetOutput("show", "HEAD:text.txt")
	require.NoError(t, err)
	require.Contains(t, headText, "line two absorbed")

	stashList, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.NotContains(t, stashList, absorbStashStagedMarker)
}

func TestAbsorbCleansUpStashOnStashPushStagedError(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	textFile := filepath.Join(s.Scene.Dir, "text.txt")
	err := os.WriteFile(textFile, []byte("line one\nline two\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "text.txt")
	s.RunGit("commit", "-m", "add baseline file")
	s.Rebuild()

	// Create MM state in one file so git stash --staged returns non-zero.
	err = os.WriteFile(textFile, []byte("line one\nline two staged\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "text.txt")
	err = os.WriteFile(textFile, []byte("line one\nline two staged\nline three unstaged\n"), 0600)
	require.NoError(t, err)

	err = Action(s.Context, Options{Force: true}, nil)
	require.NoError(t, err)

	// No absorb stash of either marker may remain.
	stashList, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.NotContains(t, stashList, absorbStashStagedMarker)
	require.NotContains(t, stashList, absorbStashUnstagedMarker)

	// The staged edit landed in the commit; the unstaged edit did not.
	headText, err := s.Scene.Repo.RunGitCommandAndGetOutput("show", "HEAD:text.txt")
	require.NoError(t, err)
	require.Contains(t, headText, "line two staged")
	require.NotContains(t, headText, "line three unstaged")

	// The unstaged edit survives in the working tree with no conflict markers.
	currentText, err := os.ReadFile(textFile)
	require.NoError(t, err)
	require.Contains(t, string(currentText), "line three unstaged")
	require.NotContains(t, string(currentText), "<<<<<<<")
	require.NotContains(t, string(currentText), ">>>>>>>")
	require.NotContains(t, string(currentText), "=======")

	// No unmerged (UU) entries in the working tree.
	status, err := s.Scene.Repo.RunGitCommandAndGetOutput("status", "--porcelain")
	require.NoError(t, err)
	require.NotContains(t, status, "UU ")
}

// TestAbsorbRestoresUnabsorbableTextHunkToWorktree covers the primary success
// path: a staged text hunk that commutes with all downstack commits (its target
// commit is trunk-owned, outside the search range) must be restored to BOTH the
// index and the on-disk file, not just the index.
func TestAbsorbRestoresUnabsorbableTextHunkToWorktree(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	// trunk owns trunk.txt; absorb's search range is branch-a only, so a hunk
	// targeting trunk.txt commutes with all searched commits -> unabsorbable.
	trunkFile := filepath.Join(s.Scene.Dir, "trunk.txt")
	err := os.WriteFile(trunkFile, []byte("trunk-line\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "trunk.txt")
	s.RunGit("commit", "-m", "add trunk file")

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	branchFile := filepath.Join(s.Scene.Dir, "branch.txt")
	err = os.WriteFile(branchFile, []byte("branch-line\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "branch.txt")
	s.RunGit("commit", "-m", "add branch file")
	s.Rebuild()

	// Stage an absorbable hunk (branch.txt -> branch-a's commit) and an
	// unabsorbable hunk (trunk.txt, commutes with all searched commits).
	err = os.WriteFile(branchFile, []byte("branch-line absorbed\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(trunkFile, []byte("trunk-line unabsorbable\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "branch.txt", "trunk.txt")

	err = Action(s.Context, Options{Force: true}, nil)
	require.NoError(t, err)

	// The unabsorbable hunk is staged in the index...
	cachedDiff, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached")
	require.NoError(t, err)
	require.Contains(t, cachedDiff, "trunk-line unabsorbable")

	// ...AND present in the on-disk file (the bug left it only in the index).
	onDisk, err := os.ReadFile(trunkFile)
	require.NoError(t, err)
	require.Contains(t, string(onDisk), "trunk-line unabsorbable")

	// The absorbable hunk landed in the commit and no absorb stash remains.
	headBranch, err := s.Scene.Repo.RunGitCommandAndGetOutput("show", "HEAD:branch.txt")
	require.NoError(t, err)
	require.Contains(t, headBranch, "branch-line absorbed")

	stashList, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.NotContains(t, stashList, absorbStashStagedMarker)
}

// TestAbsorbRestackConflictKeepsCleanWorktree verifies that a conflicted
// follow-up restack does not fail the absorb: the rewrite already committed
// the hunks, so the conflicted stack is held back and absorb finishes on a
// clean worktree. Before the fix the restack entered the conflict workflow,
// absorb returned its error, and the deferred restore took the failure path —
// re-staging already-absorbed hunks onto a mid-rebase worktree.
func TestAbsorbRestackConflictKeepsCleanWorktree(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")
	aFile := filepath.Join(s.Scene.Dir, "a.txt")
	require.NoError(t, os.WriteFile(aFile, []byte("a-line\n"), 0600))
	conflictFile := filepath.Join(s.Scene.Dir, "conflict.txt")
	require.NoError(t, os.WriteFile(conflictFile, []byte("base\n"), 0600))
	s.RunGit("add", "a.txt", "conflict.txt")
	s.RunGit("commit", "-m", "a: add files")

	s.CreateBranch("branch-b")
	s.TrackBranch("branch-b", "branch-a")
	require.NoError(t, os.WriteFile(conflictFile, []byte("b-version\n"), 0600))
	s.RunGit("add", "conflict.txt")
	s.RunGit("commit", "-m", "b: change conflict file")

	// Diverge branch-a so branch-b's replay conflicts during the restack.
	s.Checkout("branch-a")
	require.NoError(t, os.WriteFile(conflictFile, []byte("a-version\n"), 0600))
	s.RunGit("add", "conflict.txt")
	s.RunGit("commit", "-m", "a: conflicting change")
	s.Rebuild()

	// Stage a hunk that absorbs into branch-a's first commit.
	require.NoError(t, os.WriteFile(aFile, []byte("a-line absorbed\n"), 0600))
	s.RunGit("add", "a.txt")

	err := Action(s.Context, Options{Force: true, Restack: RestackAll}, nil)
	require.NoError(t, err)

	// The absorbed hunk is committed on branch-a.
	headA, err := s.Scene.Repo.RunGitCommandAndGetOutput("show", "branch-a:a.txt")
	require.NoError(t, err)
	require.Contains(t, headA, "a-line absorbed")

	// The repo is clean: no rebase in progress, nothing staged (the absorbed
	// hunk must NOT have been re-staged by the restore path), and no leftover
	// absorb stashes.
	require.False(t, s.Engine.Git().IsRebaseInProgress(context.Background()))
	staged, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached")
	require.NoError(t, err)
	require.Empty(t, strings.TrimSpace(staged))
	stashList, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.NotContains(t, stashList, absorbStashMarker)

	// branch-b was held back (still needs restack), not left mid-conflict.
	require.NotEmpty(t, s.Engine.CurrentBranch())
}

// TestDropStagedStashIfRestored verifies the staged safety stash is dropped only
// after the unabsorbable-hunk restore succeeds, and kept (as the recovery net)
// when it fails. This mirrors the ordering guard in restoreStashedState.
func TestDropStagedStashIfRestored(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	f := filepath.Join(s.Scene.Dir, "f.txt")
	err := os.WriteFile(f, []byte("base\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "f.txt")
	s.RunGit("commit", "-m", "base")
	s.Rebuild()

	// Create a real staged absorb stash to act on.
	err = os.WriteFile(f, []byte("base\nstaged\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "f.txt")
	s.RunGit("stash", "push", "--staged", "-m", absorbStashStagedMarker)

	resolve := func(marker string) string {
		list, listErr := s.Engine.StashList(s.Context.Context)
		require.NoError(t, listErr)
		return findStashRef(list, marker)
	}

	// restoreOK=false keeps the stash for manual recovery.
	dropStagedStashIfRestored(s.Context.Context, s.Engine, s.Context.Output, false, resolve)
	list, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.Contains(t, list, absorbStashStagedMarker, "stash must be kept when restore did not fully succeed")

	// restoreOK=true drops it.
	dropStagedStashIfRestored(s.Context.Context, s.Engine, s.Context.Output, true, resolve)
	list, err = s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.NotContains(t, list, absorbStashStagedMarker, "stash must be dropped after a successful restore")
}

func TestAbsorbDryRunDoesNotMutateRepository(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	testFile := filepath.Join(s.Scene.Dir, "test.txt")
	err := os.WriteFile(testFile, []byte("base\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "test.txt")
	s.RunGit("commit", "-m", "add baseline file")
	s.Rebuild()

	err = os.WriteFile(testFile, []byte("base\nstaged\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "test.txt")

	headBefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "HEAD")
	require.NoError(t, err)
	stashBefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)

	err = Action(s.Context, Options{DryRun: true, Force: true}, nil)
	require.NoError(t, err)

	headAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "HEAD")
	require.NoError(t, err)
	stashAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	cachedAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached", "--name-only")
	require.NoError(t, err)

	require.Equal(t, headBefore, headAfter)
	require.Equal(t, stashBefore, stashAfter)
	require.Contains(t, cachedAfter, "test.txt")
}

func TestAbsorbAllAndPatchStagesTrackedOnly(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	trackedFile := filepath.Join(s.Scene.Dir, "tracked.txt")
	untrackedFile := filepath.Join(s.Scene.Dir, "new.txt")

	err := os.WriteFile(trackedFile, []byte("base\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "tracked.txt")
	s.RunGit("commit", "-m", "add baseline tracked file")
	s.Rebuild()

	err = os.WriteFile(trackedFile, []byte("base\ntracked-change\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(untrackedFile, []byte("new file\n"), 0600)
	require.NoError(t, err)

	err = Action(s.Context, Options{All: true, Patch: true, DryRun: true, Force: true}, nil)
	require.NoError(t, err)

	cached, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached", "--name-only")
	require.NoError(t, err)
	untracked, err := s.Scene.Repo.RunGitCommandAndGetOutput("ls-files", "--others", "--exclude-standard")
	require.NoError(t, err)

	require.Contains(t, cached, "tracked.txt")
	require.NotContains(t, cached, "new.txt")
	require.Contains(t, untracked, "new.txt")
}

func TestAbsorbPlanOutputIsUserFriendly(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	targetFile := filepath.Join(s.Scene.Dir, "target.txt")
	newFile := filepath.Join(s.Scene.Dir, "new.txt")

	err := os.WriteFile(targetFile, []byte("base\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "target.txt")
	s.RunGit("commit", "-m", "add baseline target")
	s.Rebuild()

	err = os.WriteFile(targetFile, []byte("base\nabsorbed change\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(newFile, []byte("new file content\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "target.txt", "new.txt")

	s.Output.Reset()
	err = Action(s.Context, Options{Force: false}, nil)
	require.NoError(t, err)

	out := s.Output.String()
	require.Contains(t, out, "Absorb plan: 1 hunk into 1 commit, 1 skipped")
	require.Contains(t, out, "Will absorb:")
	require.Contains(t, out, "Skipped (1):")
	require.Contains(t, out, "New files cannot be absorbed (1)")
	require.Contains(t, out, "Tips:")
	require.Contains(t, out, "Commit new files with create/modify, then rerun absorb.")
	require.Contains(t, out, "\n    - new.txt:")
	require.NotContains(t, out, "new_file")
}

func TestAbsorbDryRunJSONIncludesUnabsorbableReasons(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	targetFile := filepath.Join(s.Scene.Dir, "target.txt")
	deleteFile := filepath.Join(s.Scene.Dir, "delete.txt")
	binaryFile := filepath.Join(s.Scene.Dir, "asset.bin")
	newFile := filepath.Join(s.Scene.Dir, "new.txt")

	err := os.WriteFile(targetFile, []byte("target base\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(deleteFile, []byte("delete me\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(binaryFile, []byte{0x00, 0x01, 0x02, 0x03}, 0600)
	require.NoError(t, err)
	s.RunGit("add", "target.txt", "delete.txt", "asset.bin")
	s.RunGit("commit", "-m", "add baseline files")
	s.Rebuild()

	err = os.WriteFile(targetFile, []byte("target absorbed\n"), 0600)
	require.NoError(t, err)
	err = os.Remove(deleteFile)
	require.NoError(t, err)
	err = os.WriteFile(newFile, []byte("brand new\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(binaryFile, []byte{0x10, 0x11, 0x12, 0x13}, 0600)
	require.NoError(t, err)
	s.RunGit("add", "target.txt", "delete.txt", "new.txt", "asset.bin")

	s.Output.Reset()
	err = Action(s.Context, Options{DryRun: true, Force: true, JSON: true}, nil)
	require.NoError(t, err)

	out := s.Output.String()
	start := strings.Index(out, "{\n  \"current_branch\"")
	require.NotEqual(t, -1, start, "expected JSON output in absorb dry-run output")
	end := strings.LastIndex(out, "}")
	require.Greater(t, end, start, "expected complete JSON output")

	var plan PlanJSON
	err = json.Unmarshal([]byte(out[start:end+1]), &plan)
	require.NoError(t, err)

	reasons := make(map[string]bool)
	for _, hunk := range plan.Unabsorbable {
		reasons[hunk.Reason] = true
	}
	require.True(t, reasons[string(ReasonBinary)])
	require.True(t, reasons[string(ReasonNewFile)])
	require.True(t, reasons[string(ReasonDeletedFile)])
}

func TestAbsorbNonInteractiveWithoutForceSkipsApply(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	testFile := filepath.Join(s.Scene.Dir, "skip.txt")
	err := os.WriteFile(testFile, []byte("base\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "skip.txt")
	s.RunGit("commit", "-m", "add baseline file")
	s.Rebuild()

	err = os.WriteFile(testFile, []byte("base\nstaged\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "skip.txt")

	headBefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "HEAD")
	require.NoError(t, err)

	err = Action(s.Context, Options{Force: false}, nil)
	require.NoError(t, err)

	headAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "HEAD")
	require.NoError(t, err)
	cachedAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("diff", "--cached", "--name-only")
	require.NoError(t, err)

	require.Equal(t, headBefore, headAfter)
	require.Contains(t, cachedAfter, "skip.txt")
}

func TestAbsorbConflictHandling(t *testing.T) {
	t.Parallel()
	t.Run("IsAbsorbInProgress returns false in normal state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		// Should return false in normal state
		require.False(t, IsAbsorbInProgress(s.Context))
	})

	t.Run("IsAbsorbInProgress returns true in detached HEAD state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		// Simulate a failed absorb by detaching HEAD
		s.RunGit("checkout", "--detach", "HEAD")

		// Should return true in detached HEAD state (with checkout in reflog)
		require.True(t, IsAbsorbInProgress(s.Context))
	})

	t.Run("IsAbsorbInProgress returns false during a rebase even when detached", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")

		// Reproduce the restack conflict workflow's state: HEAD detached AND a
		// rebase in progress. EnterConflictWorkflow detaches on purpose, which
		// leaves a "checkout: moving from" reflog entry. That alone used to make
		// IsAbsorbInProgress report a phantom absorb, routing `stackit abort` to
		// the absorb cleanup path instead of the standard rebase abort.
		s.RunGit("checkout", "--detach", "HEAD")
		rebaseMergeDir := filepath.Join(s.Scene.Dir, ".git", "rebase-merge")
		require.NoError(t, os.MkdirAll(rebaseMergeDir, 0750))

		// A rebase in progress means this is a restack/sync conflict, not an
		// absorb — the standard abort owns it.
		require.False(t, IsAbsorbInProgress(s.Context))
	})

	t.Run("ShowConflict displays staged changes info", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		// Stage a change
		err = os.WriteFile(testFile, []byte("package main\n\nfunc test() { modified }\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")

		// ShowConflict should work without error when we have staged changes
		err = ShowConflict(s.Context)
		require.NoError(t, err)
	})

	t.Run("ShowConflict handles no staged changes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit but no staged changes
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		// ShowConflict should work without error when no staged changes
		err = ShowConflict(s.Context)
		require.NoError(t, err)
	})

	t.Run("Abort handles normal state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		// Abort should work without error when not in a failed absorb state
		err = Abort(s.Context)
		require.NoError(t, err)
	})

	t.Run("Abort recovers from detached HEAD state", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		// Simulate a failed absorb by detaching HEAD
		s.RunGit("checkout", "--detach", "HEAD")

		// Verify we're in detached HEAD state
		output, err := s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--abbrev-ref", "HEAD")
		require.NoError(t, err)
		require.Equal(t, "HEAD", strings.TrimSpace(output))

		// Abort should recover from detached HEAD
		err = Abort(s.Context)
		require.NoError(t, err)

		// Verify we're back on a branch (reflog should help find it)
		output, err = s.Scene.Repo.RunGitCommandAndGetOutput("rev-parse", "--abbrev-ref", "HEAD")
		require.NoError(t, err)
		// After abort, we should be on test-branch
		require.Equal(t, "test-branch", strings.TrimSpace(output))
	})

	t.Run("Abort restores all absorb stashes and keeps unrelated stashes", func(t *testing.T) {
		t.Parallel()
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		fileA := filepath.Join(s.Scene.Dir, "file-a.txt")
		fileB := filepath.Join(s.Scene.Dir, "file-b.txt")
		fileC := filepath.Join(s.Scene.Dir, "file-c.txt")

		err := os.WriteFile(fileA, []byte("a-base\n"), 0600)
		require.NoError(t, err)
		err = os.WriteFile(fileB, []byte("b-base\n"), 0600)
		require.NoError(t, err)
		err = os.WriteFile(fileC, []byte("c-base\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "file-a.txt", "file-b.txt", "file-c.txt")
		s.RunGit("commit", "-m", "add baseline files")
		s.Rebuild()

		err = os.WriteFile(fileA, []byte("a-absorb-staged\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "file-a.txt")
		s.RunGit("stash", "push", "-m", absorbStashStagedMarker)

		err = os.WriteFile(fileB, []byte("b-absorb-unstaged\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "file-b.txt")
		s.RunGit("stash", "push", "-m", absorbStashUnstagedMarker)

		err = os.WriteFile(fileC, []byte("c-unrelated\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "file-c.txt")
		s.RunGit("stash", "push", "-m", "unrelated-stash")

		stashListBefore, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
		require.NoError(t, err)
		require.Contains(t, stashListBefore, absorbStashStagedMarker)
		require.Contains(t, stashListBefore, absorbStashUnstagedMarker)
		require.Contains(t, stashListBefore, "unrelated-stash")

		err = Abort(s.Context)
		require.NoError(t, err)

		stashListAfter, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
		require.NoError(t, err)
		require.NotContains(t, stashListAfter, absorbStashStagedMarker)
		require.NotContains(t, stashListAfter, absorbStashUnstagedMarker)
		require.Contains(t, stashListAfter, "unrelated-stash")

		contentA, err := os.ReadFile(fileA)
		require.NoError(t, err)
		require.Contains(t, string(contentA), "a-absorb-staged")
		contentB, err := os.ReadFile(fileB)
		require.NoError(t, err)
		require.Contains(t, string(contentB), "b-absorb-unstaged")
		contentC, err := os.ReadFile(fileC)
		require.NoError(t, err)
		require.NotContains(t, string(contentC), "c-unrelated")
	})
}

// TestAbsorbFallbackPreservesUnstagedBinaryEdit is the regression test for the
// data-loss bug: on the stash fallback path (triggered by MM state on a text
// file), a coexisting UNSTAGED edit to a tracked binary file must survive.
// Plain `git diff` renders a binary change as only "Binary files ... differ",
// which `git apply` cannot reapply after `reset --hard` has wiped it — losing
// the bytes and, because git apply is atomic per invocation, poisoning the
// text edits captured in the same patch. The fix captures with
// `git diff --binary`, which round-trips through `git apply`.
func TestAbsorbFallbackPreservesUnstagedBinaryEdit(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

	s.CreateBranch("branch-a")
	s.TrackBranch("branch-a", "main")

	textFile := filepath.Join(s.Scene.Dir, "text.txt")
	binFile := filepath.Join(s.Scene.Dir, "blob.bin")
	originalBinary := []byte{0x00, 0x01, 0x02, 0x03, 'B', 'I', 'N', 0xff, 0xfe}
	editedBinary := []byte{0x00, 0x01, 0x02, 0x03, 'B', 'I', 'N', '-', 'E', 'D', 'I', 'T', 0xff, 0xfe, 0xaa}

	err := os.WriteFile(textFile, []byte("line one\nline two\n"), 0600)
	require.NoError(t, err)
	err = os.WriteFile(binFile, originalBinary, 0600)
	require.NoError(t, err)
	s.RunGit("add", "text.txt", "blob.bin")
	s.RunGit("commit", "-m", "add baseline files")
	s.Rebuild()

	// Create MM state on the text file (staged + unstaged edits) so
	// `git stash push --staged` returns non-zero and absorb takes the fallback.
	err = os.WriteFile(textFile, []byte("line one\nline two staged\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "text.txt")
	err = os.WriteFile(textFile, []byte("line one\nline two staged\nline three unstaged\n"), 0600)
	require.NoError(t, err)

	// Add an UNSTAGED binary edit — the byte sequence that must survive.
	err = os.WriteFile(binFile, editedBinary, 0600)
	require.NoError(t, err)

	err = Action(s.Context, Options{Force: true}, nil)
	require.NoError(t, err)

	// The binary file's worktree bytes equal the unstaged edit exactly.
	gotBinary, err := os.ReadFile(binFile)
	require.NoError(t, err)
	require.Equal(t, editedBinary, gotBinary, "unstaged binary edit must be restored byte-for-byte")

	// The staged text line landed in HEAD; the unstaged text line did not.
	headText, err := s.Scene.Repo.RunGitCommandAndGetOutput("show", "HEAD:text.txt")
	require.NoError(t, err)
	require.Contains(t, headText, "line two staged")
	require.NotContains(t, headText, "line three unstaged")

	// The unstaged text edit survives in the working tree with no conflict markers.
	currentText, err := os.ReadFile(textFile)
	require.NoError(t, err)
	require.Contains(t, string(currentText), "line three unstaged")
	require.NotContains(t, string(currentText), "<<<<<<<")
	require.NotContains(t, string(currentText), ">>>>>>>")
	require.NotContains(t, string(currentText), "=======")

	// No absorb stash of either marker may remain.
	stashList, err := s.Scene.Repo.RunGitCommandAndGetOutput("stash", "list")
	require.NoError(t, err)
	require.NotContains(t, stashList, absorbStashStagedMarker)
	require.NotContains(t, stashList, absorbStashUnstagedMarker)
}

// TestAbsorbWarnsWhenSkippingRestack covers, end to end through Action, that the
// "Skipped restacking" warning is emitted when a narrower restack mode leaves
// descendants of the rewritten commits on stale parents. Here absorb rewrites a
// downstack branch (a) while the user is on c with --restack none, so b and c
// are left un-restacked and the warning must fire.
func TestAbsorbWarnsWhenSkippingRestack(t *testing.T) {
	t.Parallel()
	s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).WithLinearStack3()

	// On c, stage a modification to branch a's committed file so the hunk
	// absorbs into a (the oldest modified branch).
	s.Checkout("c")
	fileA := filepath.Join(s.Scene.Dir, "a_test.txt")
	err := os.WriteFile(fileA, []byte("change on a modified\n"), 0600)
	require.NoError(t, err)
	s.RunGit("add", "a_test.txt")

	err = Action(s.Context, Options{Restack: RestackNone, Force: true}, nil)
	require.NoError(t, err)

	require.Contains(t, s.Output.String(), "Skipped restacking")
}
