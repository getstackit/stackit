package integration

import (
	"testing"
)

func TestTrackIntegration(t *testing.T) {
	t.Parallel()

	t.Run("recover from metadata corruption after git operations", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Set up a working stack
		shell.Write("feature1.go", "package main").
			Run("create feature1 -m 'Add feature1'").
			Write("feature2.go", "package main").
			Run("create feature2 -m 'Add feature2'")

		// Verify stack is working
		shell.Run("log --stack").
			OutputContains("feature1").
			OutputContains("feature2")

		// Corrupt metadata by manually deleting a metadata ref
		shell.Git("update-ref -d refs/stackit/metadata/feature2")

		// Verify feature2 is no longer tracked
		shell.Checkout("feature2").
			Run("parent").
			OutputContains("no parent")

		// Recover by tracking feature2 again
		shell.Run("track feature2 --parent feature1")

		// Verify stack is restored
		shell.Run("log --stack").
			OutputContains("feature1").
			OutputContains("feature2").
			Checkout("feature2").
			Run("parent").
			OutputContains("feature1")
	})

	t.Run("track existing branches created outside stackit", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Create branches using raw git (simulating branches created outside stackit)
		shell.Git("checkout -b feature-a").
			Write("a.go", "package main").
			Commit("a.go", "Add feature A").
			Git("checkout main").
			Git("checkout -b feature-b").
			Write("b.go", "package main").
			Commit("b.go", "Add feature B").
			Git("checkout feature-a").
			Git("checkout -b feature-c").
			Write("c.go", "package main").
			Commit("c.go", "Add feature C")

		// Verify branches exist but aren't tracked
		shell.HasBranches("main", "feature-a", "feature-b", "feature-c").
			Checkout("feature-a").
			Run("parent").
			OutputContains("no parent")

		// Track the branches to establish relationships
		shell.Run("track feature-a --parent main").
			Run("track feature-b --parent main").
			Run("track feature-c --parent feature-a")

		// Verify tracking relationships
		shell.Checkout("feature-a").
			Run("parent").
			OutputContains("main").
			Checkout("feature-b").
			Run("parent").
			OutputContains("main").
			Checkout("feature-c").
			Run("parent").
			OutputContains("feature-a")

		// Verify stack operations work
		shell.Run("log").
			OutputContains("feature-a").
			OutputContains("feature-b").
			OutputContains("feature-c")
	})

	t.Run("track entire stack of untracked branches", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Create a stack of branches manually (simulating import from another tool)
		shell.Git("checkout -b layer1").
			Write("layer1.go", "package main").
			Commit("layer1.go", "Add layer1").
			Git("checkout -b layer2").
			Write("layer2.go", "package main").
			Commit("layer2.go", "Add layer2").
			Git("checkout -b layer3").
			Write("layer3.go", "package main").
			Commit("layer3.go", "Add layer3")

		// Track the entire stack bottom-up
		shell.Run("track layer1 --parent main").
			Run("track layer2 --parent layer1").
			Run("track layer3 --parent layer2")

		// Verify the complete stack
		shell.Run("log --stack").
			OutputContains("layer1").
			OutputContains("layer2").
			OutputContains("layer3").
			Checkout("layer3").
			Run("bottom").
			OnBranch("layer1").
			Run("top").
			OnBranch("layer3")
	})

	t.Run("track branches to fix wrong parent after manual operations", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Set up a stack
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Write("b.go", "package main").
			Run("create feature-b -m 'Add feature B'").
			Write("c.go", "package main").
			Run("create feature-c -m 'Add feature C'")

		// Verify initial relationships: main -> a -> b -> c
		shell.Checkout("feature-c").
			Run("parent").
			OutputContains("feature-b")

		// Manually change feature-c's parent to feature-a (wrong relationship)
		// This simulates a mistake or manual git operation
		shell.Run("track feature-c --parent feature-a")

		// Verify the relationship was updated
		shell.Run("parent").
			OutputContains("feature-a")

		// Fix it back to the correct parent
		shell.Run("track feature-c --parent feature-b")

		// Verify correct relationship restored
		shell.Run("parent").
			OutputContains("feature-b")
	})

	t.Run("track with force finds most recent ancestor in complex stack", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Create a complex stack
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Write("b.go", "package main").
			Run("create feature-b -m 'Add feature B'").
			Write("c.go", "package main").
			Run("create feature-c -m 'Add feature C'")

		// Create an untracked branch from feature-c
		shell.Git("checkout -b feature-d").
			Write("d.go", "package main").
			Commit("d.go", "Add feature D")

		// Track with --force should find feature-c as most recent ancestor
		shell.Run("track feature-d --force")

		// Verify it found the correct parent
		shell.Run("parent").
			OutputContains("feature-c")
	})

	t.Run("track branches after split operation", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Set up initial branch with multiple commits
		shell.Write("file1.go", "package main").
			Run("create feature -m 'Add feature'").
			Write("file2.go", "package main").
			Commit("file2.go", "Add file2").
			Write("file3.go", "package main").
			Commit("file3.go", "Add file3")

		// Split the branch into multiple branches
		// We use --by-file to avoid interactivity in tests
		shell.Run("split --by-file file2.go_test.txt").
			HasBranches("main", "feature", "feature_split")

		// The split branches might not be properly tracked
		// Track them to establish relationships
		// Use --force because split --by-file doesn't maintain direct ancestry until restack
		shell.Checkout("feature").
			Run("track --parent feature_split --force")

		// Verify the stack
		shell.Run("log").
			OutputContains("feature").
			OutputContains("feature_split")
	})

	t.Run("track branches after force push recovery", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellWithRemoteInProcess(t)

		// Set up a stack and push it
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Write("b.go", "package main").
			Run("create feature-b -m 'Add feature B'").
			Run("sync")

		// Simulate force push on remote (someone else rewrote history)
		// This would break local tracking. Delete metadata to simulate
		shell.Git("update-ref -d refs/stackit/metadata/feature-a").
			Git("update-ref -d refs/stackit/metadata/feature-b")

		// Re-track the branches
		shell.Run("track feature-a --parent main").
			Run("track feature-b --parent feature-a")

		// Verify relationships restored
		shell.Checkout("feature-b").
			Run("parent").
			OutputContains("feature-a")
	})

	t.Run("track parallel branches and connect them", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Create two parallel stacks
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Checkout("main").
			Write("x.go", "package main").
			Run("create feature-x -m 'Add feature X'")

		// Create a branch that should connect the two stacks
		shell.Checkout("feature-a").
			Git("checkout -b feature-ax").
			Write("ax.go", "package main").
			Commit("ax.go", "Combine A and X")

		// Track the connecting branch with --force to find correct parent
		shell.Run("track feature-ax --force")

		// Verify it found feature-a as parent (most recent ancestor)
		shell.Run("parent").
			OutputContains("feature-a")

		// Now manually set it to have both as dependencies (by tracking with feature-x)
		// Actually, stackit doesn't support multiple parents, so this tests single parent
		// But we can verify the relationship is correct
		shell.Run("track feature-ax --parent feature-a")
	})

	t.Run("track branches after branch recreation", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Set up a stack
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Write("b.go", "package main").
			Run("create feature-b -m 'Add feature B'")

		// Delete and recreate feature-b (simulating branch recreation)
		shell.Checkout("main").
			Git("branch -D feature-b").
			Git("update-ref -d refs/stackit/metadata/feature-b").
			Git("checkout -b feature-b").
			Write("b.go", "package main").
			Commit("b.go", "Add feature B recreated")

		// Verify it's no longer tracked
		shell.Run("parent").
			OutputContains("no parent")

		// Re-track it with --force because it was recreated from main (ancestry broken)
		shell.Run("track feature-b --parent feature-a --force")

		// Verify relationship restored
		shell.Run("parent").
			OutputContains("feature-a")
	})

	t.Run("track with force handles multiple potential ancestors correctly", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Create a complex branching structure
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Checkout("main").
			Write("b.go", "package main").
			Run("create feature-b -m 'Add feature B'").
			Checkout("feature-a").
			Write("c.go", "package main").
			Run("create feature-c -m 'Add feature C'")

		// Create a branch that could be based on either feature-a or feature-b
		// But it's actually based on feature-c (most recent)
		shell.Checkout("feature-c").
			Git("checkout -b feature-d").
			Write("d.go", "package main").
			Commit("d.go", "Add feature D")

		// Track with --force should find feature-c (most recent tracked ancestor)
		shell.Run("track feature-d --force")

		// Verify it found the correct parent
		shell.Run("parent").
			OutputContains("feature-c")
	})

	t.Run("track can recover entire corrupted stack", func(t *testing.T) {
		t.Parallel()
		shell := NewTestShellInProcess(t)

		// Set up a large stack
		shell.Write("a.go", "package main").
			Run("create feature-a -m 'Add feature A'").
			Write("b.go", "package main").
			Run("create feature-b -m 'Add feature B'").
			Write("c.go", "package main").
			Run("create feature-c -m 'Add feature C'").
			Write("d.go", "package main").
			Run("create feature-d -m 'Add feature D'")

		// Corrupt all metadata
		shell.Git("update-ref -d refs/stackit/metadata/feature-a").
			Git("update-ref -d refs/stackit/metadata/feature-b").
			Git("update-ref -d refs/stackit/metadata/feature-c").
			Git("update-ref -d refs/stackit/metadata/feature-d")

		// Recover the entire stack using --force
		shell.Run("track feature-a --force").
			Run("track feature-b --force").
			Run("track feature-c --force").
			Run("track feature-d --force")

		// Verify the entire stack is restored (single log call checks all)
		shell.Run("log --stack").
			OutputContains("feature-a").
			OutputContains("feature-b").
			OutputContains("feature-c").
			OutputContains("feature-d")

		// Verify relationships (batch verification by checking all in sequence)
		shell.Checkout("feature-b").
			Run("parent").
			OutputContains("feature-a")
		shell.Checkout("feature-c").
			Run("parent").
			OutputContains("feature-b")
		shell.Checkout("feature-d").
			Run("parent").
			OutputContains("feature-c")
	})
}
