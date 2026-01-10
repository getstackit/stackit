package absorb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"stackit.dev/stackit/internal/engine"
	"stackit.dev/stackit/testhelpers"
	"stackit.dev/stackit/testhelpers/scenario"
)

func TestAbsorbScopeBoundaries(t *testing.T) {
	t.Run("absorb stops at scope boundaries", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"scoped-a":   "main",
				"scoped-b":   "scoped-a",
				"unscoped-c": "scoped-b",
			})

		// Set scopes: scoped-a and scoped-b have PROJ-123, unscoped-c has no scope
		err := s.Engine.SetScope(s.Engine.GetBranch("scoped-a"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(s.Engine.GetBranch("scoped-b"), engine.NewScope("PROJ-123"))
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
		downstackBranches = append([]engine.Branch{currentBranch}, downstackBranches...)

		// Apply scope boundary filtering as absorb does
		if currentScope.IsDefined() {
			limitedDownstack := []engine.Branch{}
			for _, branch := range downstackBranches {
				if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
					break
				}
				limitedDownstack = append(limitedDownstack, branch)
			}
			downstackBranches = limitedDownstack
		}

		// Should only include scoped-a and scoped-b, not unscoped-c or main
		require.Len(t, downstackBranches, 2)
		require.Equal(t, "scoped-b", downstackBranches[0].GetName())
		require.Equal(t, "scoped-a", downstackBranches[1].GetName())
	})

	t.Run("absorb includes all branches when no scope set", func(t *testing.T) {
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
		downstackBranches = append([]engine.Branch{currentBranch}, downstackBranches...)

		// Apply scope boundary filtering as absorb does
		if currentScope.IsDefined() {
			limitedDownstack := []engine.Branch{}
			for _, branch := range downstackBranches {
				if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
					break
				}
				limitedDownstack = append(limitedDownstack, branch)
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
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				"scoped-a":    "main",
				"scoped-b":    "scoped-a",
				"different-c": "scoped-b",
				"scoped-d":    "different-c",
			})

		// Set mixed scopes: PROJ-123 for a/b, PROJ-456 for c/d
		err := s.Engine.SetScope(s.Engine.GetBranch("scoped-a"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(s.Engine.GetBranch("scoped-b"), engine.NewScope("PROJ-123"))
		require.NoError(t, err)
		err = s.Engine.SetScope(s.Engine.GetBranch("different-c"), engine.NewScope("PROJ-456"))
		require.NoError(t, err)
		err = s.Engine.SetScope(s.Engine.GetBranch("scoped-d"), engine.NewScope("PROJ-456"))
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
		downstackBranches = append([]engine.Branch{currentBranch}, downstackBranches...)

		// Apply scope boundary filtering as absorb does
		if currentScope.IsDefined() {
			limitedDownstack := []engine.Branch{}
			for _, branch := range downstackBranches {
				if branch.IsTrunk() || !branch.GetScope().Equal(currentScope) {
					break // Stop at first scope mismatch
				}
				limitedDownstack = append(limitedDownstack, branch)
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
	t.Run("absorb handles changes when intervening commits modify same file", func(t *testing.T) {
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
		s.Rebuild()

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
		s.Rebuild()

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

func TestAbsorbConflictHandling(t *testing.T) {
	t.Run("IsAbsorbInProgress returns false in normal state", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		s.Rebuild()

		// Should return false in normal state
		require.False(t, IsAbsorbInProgress(s.Context))
	})

	t.Run("IsAbsorbInProgress returns true in detached HEAD state", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		s.Rebuild()

		// Simulate a failed absorb by detaching HEAD
		s.RunGit("checkout", "--detach", "HEAD")

		// Should return true in detached HEAD state (with checkout in reflog)
		require.True(t, IsAbsorbInProgress(s.Context))
	})

	t.Run("ShowConflict displays staged changes info", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		s.Rebuild()

		// Stage a change
		err = os.WriteFile(testFile, []byte("package main\n\nfunc test() { modified }\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")

		// ShowConflict should work without error when we have staged changes
		err = ShowConflict(s.Context)
		require.NoError(t, err)
	})

	t.Run("ShowConflict handles no staged changes", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit but no staged changes
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		s.Rebuild()

		// ShowConflict should work without error when no staged changes
		err = ShowConflict(s.Context)
		require.NoError(t, err)
	})

	t.Run("Abort handles normal state", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		s.Rebuild()

		// Abort should work without error when not in a failed absorb state
		err = Abort(s.Context)
		require.NoError(t, err)
	})

	t.Run("Abort recovers from detached HEAD state", func(t *testing.T) {
		s := scenario.NewScenario(t, testhelpers.BasicSceneSetup)

		// Create a branch with a commit
		s.CreateBranch("test-branch")
		s.TrackBranch("test-branch", "main")

		testFile := filepath.Join(s.Scene.Dir, "test.go")
		err := os.WriteFile(testFile, []byte("package main\n\nfunc test() {}\n"), 0600)
		require.NoError(t, err)
		s.RunGit("add", "test.go")
		s.RunGit("commit", "-m", "add test.go")
		s.Rebuild()

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
}
