package engine_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/getstackit/stackit/internal/engine"
	"github.com/getstackit/stackit/internal/git"
	"github.com/getstackit/stackit/testhelpers"
	"github.com/getstackit/stackit/testhelpers/scenario"
)

const mainBranch = "main"

// branchName generates a branch name from an index
func branchName(i int) string {
	return fmt.Sprintf("branch-%d", i)
}

// BenchmarkWorktreeCreation specifically benchmarks worktree creation
func BenchmarkWorktreeCreation(b *testing.B) {
	// Setup: Create a test repo
	tmpDir, err := os.MkdirTemp("", "stackit-bench-worktree-*")
	if err != nil {
		b.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoPath := filepath.Join(tmpDir, "repo")
	if err := os.Mkdir(repoPath, 0o755); err != nil {
		b.Fatalf("failed to create repo dir: %v", err)
	}

	// Initialize git repo
	g := git.NewRunnerWithPath(repoPath, nil)
	ctx := context.Background()

	if _, err := g.RunGitCommandWithContext(ctx, "init"); err != nil {
		b.Fatalf("failed to init repo: %v", err)
	}

	// Configure git
	if _, err := g.RunGitCommandWithContext(ctx, "config", "user.name", "Test User"); err != nil {
		b.Fatalf("failed to configure git: %v", err)
	}
	if _, err := g.RunGitCommandWithContext(ctx, "config", "user.email", "test@example.com"); err != nil {
		b.Fatalf("failed to configure git: %v", err)
	}

	// Create initial commit
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial content\n"), 0o644); err != nil {
		b.Fatalf("failed to write test file: %v", err)
	}
	if _, err := g.RunGitCommandWithContext(ctx, "add", "test.txt"); err != nil {
		b.Fatalf("failed to add file: %v", err)
	}
	if err := g.CommitWithOptions(git.CommitOptions{Message: "Initial commit"}); err != nil {
		b.Fatalf("failed to create initial commit: %v", err)
	}

	// Create engine
	eng, err := engine.NewEngine(engine.Options{
		RepoRoot: repoPath,
		Trunk:    mainBranch,
	})
	if err != nil {
		b.Fatalf("failed to create engine: %v", err)
	}

	b.ResetTimer()

	// Benchmark worktree creation
	for i := 0; i < b.N; i++ {
		_, cleanup, err := eng.CreateTemporaryWorktree(ctx, "HEAD", "stackit-bench-*")
		if err != nil {
			b.Fatalf("CreateTemporaryWorktree failed: %v", err)
		}
		cleanup()
	}
}

// BenchmarkValidateRebases benchmarks the parallel validation approach
func BenchmarkValidateRebases(b *testing.B) {
	b.Run("wide_stack_5_siblings", func(b *testing.B) {
		benchmarkWideStack(b, 5)
	})

	b.Run("wide_stack_10_siblings", func(b *testing.B) {
		benchmarkWideStack(b, 10)
	})

	b.Run("linear_stack_5_deep", func(b *testing.B) {
		benchmarkLinearStack(b, 5)
	})

	b.Run("linear_stack_10_deep", func(b *testing.B) {
		benchmarkLinearStack(b, 10)
	})

	b.Run("mixed_stack_wide_and_deep", func(b *testing.B) {
		benchmarkMixedStack(b)
	})
}

// benchmarkWideStack creates N sibling branches and validates them
func benchmarkWideStack(b *testing.B, numBranches int) {
	// Setup once before benchmark
	s := &scenario.Scenario{}
	setupBenchmark := func() {
		// Create a fresh scenario for setup
		setupScenario := scenario.NewScenario(&testing.T{}, testhelpers.BasicSceneSetup)

		// Create N sibling branches
		branches := make(map[string]string)
		for i := range numBranches {
			branches[branchName(i)] = mainBranch
		}
		s = setupScenario.WithStack(branches)

		// Add commit to main
		s.Checkout(mainBranch).Commit("main update")
	}

	// Setup before timing
	setupBenchmark()

	mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
	oldBase, _ := s.Engine.Git().GetMergeBase(context.Background(), mainBranch, branchName(0))

	// Build specs
	specs := make([]engine.RebaseSpec, numBranches)
	for i := range numBranches {
		specs[i] = engine.RebaseSpec{
			Branch:      branchName(i),
			NewParent:   mainRev,
			OldUpstream: oldBase,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		if err != nil || !result.Success {
			b.Fatalf("validation failed: %v, result: %+v", err, result)
		}
	}
}

// benchmarkLinearStack creates a linear chain of N branches
func benchmarkLinearStack(b *testing.B, depth int) {
	s := &scenario.Scenario{}
	setupBenchmark := func() {
		setupScenario := scenario.NewScenario(&testing.T{}, testhelpers.BasicSceneSetup)

		// Create linear stack
		branches := make(map[string]string)
		for i := range depth {
			if i == 0 {
				branches[branchName(i)] = mainBranch
			} else {
				branches[branchName(i)] = branchName(i - 1)
			}
		}
		s = setupScenario.WithStack(branches)

		// Add commit to main
		s.Checkout(mainBranch).Commit("main update")
	}

	setupBenchmark()

	mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
	oldBase, _ := s.Engine.Git().GetMergeBase(context.Background(), mainBranch, branchName(0))

	// Build specs for chained rebases
	specs := make([]engine.RebaseSpec, depth)
	specs[0] = engine.RebaseSpec{
		Branch:      branchName(0),
		NewParent:   mainRev,
		OldUpstream: oldBase,
	}

	for i := 1; i < depth; i++ {
		parentRev, _ := s.Engine.GetRevision(s.Engine.GetBranch(branchName(i - 1)))
		specs[i] = engine.RebaseSpec{
			Branch:      branchName(i),
			NewParent:   parentRev,
			OldUpstream: parentRev,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		if err != nil || !result.Success {
			b.Fatalf("validation failed: %v, result: %+v", err, result)
		}
	}
}

// benchmarkMixedStack creates a realistic mixed topology
func benchmarkMixedStack(b *testing.B) {
	s := &scenario.Scenario{}
	setupBenchmark := func() {
		s = scenario.NewScenario(&testing.T{}, testhelpers.BasicSceneSetup).
			WithStack(map[string]string{
				// Depth 1: 3 siblings
				"feature-a": "main",
				"feature-b": "main",
				"feature-c": "main",
				// Depth 2: 2 children under feature-a, 1 under feature-b
				"feature-a1": "feature-a",
				"feature-a2": "feature-a",
				"feature-b1": "feature-b",
				// Depth 3: 1 child under feature-a1
				"feature-a1-1": "feature-a1",
			})

		// Add commit to main
		s.Checkout(mainBranch).Commit("main update")
	}

	setupBenchmark()

	mainRev, _ := s.Engine.GetRevision(s.Engine.Trunk())
	oldBase, _ := s.Engine.Git().GetMergeBase(context.Background(), mainBranch, "feature-a")

	// Get revisions for chaining
	featureARev, _ := s.Engine.GetRevision(s.Engine.GetBranch("feature-a"))
	featureBRev, _ := s.Engine.GetRevision(s.Engine.GetBranch("feature-b"))
	featureA1Rev, _ := s.Engine.GetRevision(s.Engine.GetBranch("feature-a1"))

	specs := []engine.RebaseSpec{
		// Depth 1
		{Branch: "feature-a", NewParent: mainRev, OldUpstream: oldBase},
		{Branch: "feature-b", NewParent: mainRev, OldUpstream: oldBase},
		{Branch: "feature-c", NewParent: mainRev, OldUpstream: oldBase},
		// Depth 2
		{Branch: "feature-a1", NewParent: featureARev, OldUpstream: featureARev},
		{Branch: "feature-a2", NewParent: featureARev, OldUpstream: featureARev},
		{Branch: "feature-b1", NewParent: featureBRev, OldUpstream: featureBRev},
		// Depth 3
		{Branch: "feature-a1-1", NewParent: featureA1Rev, OldUpstream: featureA1Rev},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := s.Engine.ValidateRebases(context.Background(), specs)
		if err != nil || !result.Success {
			b.Fatalf("validation failed: %v, result: %+v", err, result)
		}
	}
}
