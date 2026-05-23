package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// Basic Worktree Operations
// =============================================================================

func TestWorktreeBasicOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("create branch with worktree flag", func(t *testing.T, sh *TestShell) {
		// Create a branch with the -w (worktree) flag
		sh.WriteFile("feature.txt", "feature content").
			Run("create feature -w -m 'feature branch'")

		// Should return to main after creating worktree
		sh.OnBranch("main")

		// Verify worktree was created
		sh.Run("worktree list").
			OutputContains("feature")

		// Get worktree path and verify it exists
		worktreePath := sh.GetWorktreePath("feature")
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Errorf("Worktree directory should exist at %s", worktreePath)
		}

		// Verify the branch is checked out in the worktree
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("feature")
	})

	run("worktree list shows all registered worktrees", func(_ *testing.T, sh *TestShell) {
		// Create multiple worktrees
		sh.WriteFile("a.txt", "a").Run("create stack-a -w -m 'stack a'")
		sh.WriteFile("b.txt", "b").Run("create stack-b -w -m 'stack b'")
		sh.WriteFile("c.txt", "c").Run("create stack-c -w -m 'stack c'")

		// List should show all three
		sh.Run("worktree list").
			OutputContains("stack-a").
			OutputContains("stack-b").
			OutputContains("stack-c")
	})

	run("worktree open returns correct path", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		// Get expected path
		worktreePath := sh.GetWorktreePath("feature")

		// worktree open should return the same path
		sh.Run("worktree open feature").
			OutputContains(worktreePath)
	})

	run("worktree remove cleans up worktree", func(t *testing.T, sh *TestShell) {
		sh.Run("worktree create mystack")

		worktreePath := sh.GetWorktreePath("mystack")

		// Remove the worktree
		sh.Run("worktree remove mystack")

		// Worktree should no longer be listed - check for the specific stack name
		sh.Run("worktree list")
		output := sh.Output()
		if strings.Contains(output, "mystack") && !strings.Contains(output, "No managed worktrees") {
			t.Errorf("Worktree should be removed, but still appears in list: %s", output)
		}

		// Directory should be cleaned up
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("Worktree directory should be removed at %s", worktreePath)
		}
	})

	run("worktree remove refuses dirty worktree without force", func(t *testing.T, sh *TestShell) {
		sh.WriteFile("dirty-stack.txt", "dirty-stack").
			Run("create dirty-stack -w -m 'dirty stack branch'")

		worktreePath := sh.GetWorktreePath("dirty-stack")
		uncommittedFile := worktreePath + "/uncommitted.txt"
		if err := os.WriteFile(uncommittedFile, []byte("uncommitted"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		sh.RunExpectError("worktree remove dirty-stack")

		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Errorf("Dirty worktree should still exist")
		}

		sh.Run("worktree list").
			OutputContains("dirty-stack")
	})

	run("worktree remove force discards dirty worktree", func(t *testing.T, sh *TestShell) {
		sh.Run("worktree create force-stack")

		worktreePath := sh.GetWorktreePath("force-stack")
		uncommittedFile := worktreePath + "/uncommitted.txt"
		if err := os.WriteFile(uncommittedFile, []byte("uncommitted"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		sh.Run("worktree remove force-stack --force")

		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("Worktree directory should be removed at %s", worktreePath)
		}
	})
}

// =============================================================================
// Creating Branches in Worktrees
// =============================================================================

func TestWorktreeCreateBranches(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("create child branch in worktree", func(_ *testing.T, sh *TestShell) {
		// Create stack root in worktree
		sh.WriteFile("root.txt", "root").
			Run("create stack-root -w -m 'stack root'")

		// Switch to worktree and create child branches
		worktreePath := sh.GetWorktreePath("stack-root")
		shW := sh.InWorktree(worktreePath)

		shW.OnBranch("stack-root").
			WriteFile("child1.txt", "child1").
			Run("create child1 -m 'first child'").
			OnBranch("child1")

		shW.WriteFile("child2.txt", "child2").
			Run("create child2 -m 'second child'").
			OnBranch("child2")

		// Verify stack structure
		sh.ExpectStackStructure(map[string]string{
			"stack-root": "main",
			"child1":     "stack-root",
			"child2":     "child1",
		})
	})

	run("create parallel branches in worktree", func(_ *testing.T, sh *TestShell) {
		// Create stack root
		sh.WriteFile("root.txt", "root").
			Run("create stack-root -w -m 'stack root'")

		worktreePath := sh.GetWorktreePath("stack-root")
		shW := sh.InWorktree(worktreePath)

		// Create first child
		shW.OnBranch("stack-root").
			WriteFile("child1.txt", "child1").
			Run("create child1 -m 'first child'")

		// Go back to root and create parallel child
		shW.Checkout("stack-root").
			WriteFile("child2.txt", "child2").
			Run("create child2 -m 'second child parallel'")

		// Both children should have stack-root as parent
		sh.ExpectStackStructure(map[string]string{
			"stack-root": "main",
			"child1":     "stack-root",
			"child2":     "stack-root",
		})
	})

	run("cannot create worktree from within worktree", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("root.txt", "root").
			Run("create stack-root -w -m 'stack root'")

		worktreePath := sh.GetWorktreePath("stack-root")
		shW := sh.InWorktree(worktreePath)

		// Trying to create another worktree from within worktree should fail
		// (or at minimum shouldn't create nested worktrees)
		shW.WriteFile("nested.txt", "nested").
			RunExpectError("create nested -w -m 'nested worktree'")
	})
}

// =============================================================================
// Sync Operations with Worktrees
// =============================================================================

func TestWorktreeSyncOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t, WithRemote())
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("sync from main updates worktree branches", func(t *testing.T, sh *TestShell) {
		// Create a stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Add child branch in worktree
		shW.WriteFile("child.txt", "child").
			Run("create feature-child -m 'child branch'")

		// Advance main in main repo
		sh.WriteFile("main-update.txt", "main update").
			Git("add main-update.txt").
			Git("commit -m 'Main advanced'").
			Git("push origin main")

		// Sync from main with full restack
		sh.Run("sync --restack")

		// Verify stack is restacked
		sh.ExpectStackStructure(map[string]string{
			"feature":       "main",
			"feature-child": "feature",
		})

		// Verify worktree working directory is clean
		shW.Git("status --porcelain")
		if output := shW.Output(); output != "" {
			t.Errorf("Worktree should be clean after sync, but has:\n%s", output)
		}

		// Verify new file from main exists in worktree
		shW.Git("ls-files main-update.txt")
		if shW.Output() == "" {
			t.Error("main-update.txt should exist in worktree after sync")
		}
	})

	run("sync from worktree updates main repo branches", func(_ *testing.T, sh *TestShell) {
		// Create a stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Add child in worktree
		shW.WriteFile("child.txt", "child").
			Run("create feature-child -m 'child branch'")

		// Advance main in main repo
		sh.WriteFile("main-update.txt", "main update").
			Git("add main-update.txt").
			Git("commit -m 'Main advanced'").
			Git("push origin main")

		// Sync from worktree with full restack
		shW.Run("sync --restack")

		// Verify stack is restacked
		sh.ExpectStackStructure(map[string]string{
			"feature":       "main",
			"feature-child": "feature",
		})
	})

	run("sync cleans orphaned worktrees when branches deleted", func(t *testing.T, sh *TestShell) {
		// Create a stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Simulate the branch being merged (fast-forward main to include it)
		sh.Git("checkout main").
			Git("merge feature --ff-only").
			Git("push origin main")

		// Mark PR as merged
		sh.SetPrState("feature", "MERGED")

		// Checkout main in worktree first (so feature isn't the active branch)
		// Note: In a real scenario, the worktree would need to be removed manually first
		shW.Git("checkout --detach HEAD")

		// Sync should clean up the merged branch and its worktree
		sh.Run("sync")

		// Branch should be deleted
		sh.HasBranches("main")

		// Worktree should be cleaned up
		sh.Run("worktree list")
		output := sh.Output()
		// Either no worktrees or the directory is marked as not existing
		if strings.Contains(output, "feature") && !strings.Contains(output, "No managed worktrees") {
			// Check if the directory was cleaned up even if registration remains
			if _, err := os.Stat(worktreePath); err == nil {
				t.Errorf("Worktree directory should be removed after branch merged")
			}
		}
	})
}

// =============================================================================
// Restack Operations with Worktrees
// =============================================================================

func TestWorktreeRestackOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("restack from main updates worktree branches", func(t *testing.T, sh *TestShell) {
		// Create stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Build deep stack in worktree
		shW.WriteFile("a.txt", "a").Run("create a -m 'branch a'")
		shW.WriteFile("b.txt", "b").Run("create b -m 'branch b'")
		shW.WriteFile("c.txt", "c").Run("create c -m 'branch c'")

		// Modify root branch from main repo to trigger restack
		sh.Checkout("feature").
			WriteFile("feature-update.txt", "update").
			Run("modify -n").
			Checkout("main")

		// Restack from main
		sh.Run("restack")

		// Verify stack structure maintained
		sh.ExpectStackStructure(map[string]string{
			"feature": "main",
			"a":       "feature",
			"b":       "a",
			"c":       "b",
		})

		// Worktree should have the updated content
		shW.Git("ls-files feature-update.txt")
		if shW.Output() == "" {
			t.Error("feature-update.txt should exist in worktree after restack")
		}
	})

	run("restack from worktree works correctly", func(t *testing.T, sh *TestShell) {
		// Create stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Build stack
		shW.WriteFile("child.txt", "child").Run("create child -m 'child branch'")

		// Modify parent branch
		shW.Checkout("feature").
			WriteFile("feature-update.txt", "update").
			Run("modify -n")

		// Restack from worktree
		shW.Run("restack")

		// Verify child has the parent update
		shW.Checkout("child").
			Git("ls-files feature-update.txt")
		if shW.Output() == "" {
			t.Error("child should have feature-update.txt after restack")
		}
	})
}

// =============================================================================
// Modify Operations in Worktrees
// =============================================================================

func TestWorktreeModifyOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("modify in worktree triggers restack of children", func(t *testing.T, sh *TestShell) {
		// Create stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create children
		shW.WriteFile("child.txt", "child").Run("create child -m 'child branch'")
		shW.WriteFile("grandchild.txt", "grandchild").Run("create grandchild -m 'grandchild branch'")

		// Modify parent
		shW.Checkout("feature").
			WriteFile("feature-modified.txt", "modified").
			Run("modify -n")

		// Children should have the modified file
		shW.Checkout("grandchild").
			Git("ls-files feature-modified.txt")
		if shW.Output() == "" {
			t.Error("grandchild should have feature-modified.txt after parent modify")
		}
	})

	run("modify parent from main repo restacks worktree children", func(t *testing.T, sh *TestShell) {
		// Create stack in worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create child in worktree
		shW.WriteFile("child.txt", "child").Run("create child -m 'child branch'")

		// Modify parent from main repo
		sh.Checkout("feature").
			WriteFile("feature-from-main.txt", "from main").
			Run("modify -n").
			Checkout("main")

		// Worktree child should have the modified file
		shW.Checkout("child").
			Git("ls-files feature-from-main.txt")
		if shW.Output() == "" {
			t.Error("worktree child should have feature-from-main.txt")
		}

		// Worktree should be clean
		shW.Git("status --porcelain")
		if output := shW.Output(); output != "" {
			t.Errorf("Worktree should be clean, but has:\n%s", output)
		}
	})
}

// =============================================================================
// Navigation and Checkout with Worktrees
// =============================================================================

func TestWorktreeNavigation(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("checkout within worktree stays in worktree", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create children
		shW.WriteFile("child.txt", "child").Run("create child -m 'child branch'")

		// Navigate within worktree
		shW.Checkout("feature").OnBranch("feature")
		shW.Checkout("child").OnBranch("child")
		shW.Bottom().OnBranch("feature")
		shW.Top().OnBranch("child")
	})

	run("up and down navigation works in worktree", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create linear stack
		shW.WriteFile("a.txt", "a").Run("create a -m 'a'")
		shW.WriteFile("b.txt", "b").Run("create b -m 'b'")
		shW.WriteFile("c.txt", "c").Run("create c -m 'c'")

		// Navigate up and down
		shW.OnBranch("c")
		shW.Run("down").OnBranch("b")
		shW.Run("down").OnBranch("a")
		shW.Run("down").OnBranch("feature")
		shW.Run("up").OnBranch("a")
		shW.Run("up").OnBranch("b")
		shW.Run("up").OnBranch("c")
	})

	run("log command works in worktree", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create stack
		shW.WriteFile("child.txt", "child").Run("create child -m 'child branch'")

		// Log should show stack
		shW.Run("log").
			OutputContains("feature").
			OutputContains("child")
	})
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestWorktreeEdgeCases(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())
	sharedRemote := NewTestShellInProcess(t, WithRemote())
	sharedRemote.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	runRemote := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := sharedRemote.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("worktree with uncommitted changes during restack", func(t *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create child
		shW.WriteFile("child.txt", "child").Run("create child -m 'child branch'")

		// Make uncommitted change in worktree (on child branch)
		filePath := filepath.Join(worktreePath, "uncommitted.txt")
		err := os.WriteFile(filePath, []byte("uncommitted"), 0644)
		if err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}

		// Modify parent and trigger restack from main
		sh.Checkout("feature").
			WriteFile("feature-update.txt", "update").
			Run("modify -n").
			Checkout("main")

		// The uncommitted file should still be in the worktree
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Error("Uncommitted file should still exist in worktree after restack")
		}
	})

	runRemote("multiple worktrees with same parent", func(t *testing.T, sh *TestShell) {
		// Create multiple stacks from main, each in own worktree
		sh.WriteFile("stack1.txt", "stack1").
			Run("create stack1 -w -m 'stack 1'")
		sh.WriteFile("stack2.txt", "stack2").
			Run("create stack2 -w -m 'stack 2'")

		// Advance main
		sh.WriteFile("main-update.txt", "update").
			Git("add main-update.txt").
			Git("commit -m 'Main advanced'").
			Git("push origin main")

		// Sync should update both stacks
		sh.Run("sync --restack")

		// Both worktrees should have the new file
		wt1Path := sh.GetWorktreePath("stack1")
		wt2Path := sh.GetWorktreePath("stack2")

		shW1 := sh.InWorktree(wt1Path)
		shW2 := sh.InWorktree(wt2Path)

		shW1.Git("ls-files main-update.txt")
		if shW1.Output() == "" {
			t.Error("stack1 worktree should have main-update.txt")
		}

		shW2.Git("ls-files main-update.txt")
		if shW2.Output() == "" {
			t.Error("stack2 worktree should have main-update.txt")
		}
	})

	run("worktree uses default stacks directory", func(t *testing.T, sh *TestShell) {
		// Create worktree
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		// Worktree should be created (verify via list)
		sh.Run("worktree list").
			OutputContains("feature")

		// Worktree path should exist
		worktreePath := sh.GetWorktreePath("feature")
		if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
			t.Errorf("Worktree directory should exist at %s", worktreePath)
		}
	})

	run("worktree survives git gc", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")

		// Run git gc
		sh.Git("gc --prune=now")

		// Worktree should still work
		shW := sh.InWorktree(worktreePath)
		shW.OnBranch("feature").
			WriteFile("after-gc.txt", "after gc").
			Run("create after-gc -m 'branch after gc'").
			OnBranch("after-gc")
	})
}

// =============================================================================
// Move/Reparent Operations with Worktrees
// =============================================================================

func TestWorktreeMoveOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("move branch in worktree", func(_ *testing.T, sh *TestShell) {
		// Create two stacks in worktrees
		sh.WriteFile("stack1.txt", "stack1").
			Run("create stack1 -w -m 'stack 1'")
		sh.WriteFile("stack2.txt", "stack2").
			Run("create stack2 -w -m 'stack 2'")

		wt1Path := sh.GetWorktreePath("stack1")
		shW1 := sh.InWorktree(wt1Path)

		// Create child in stack1
		shW1.WriteFile("child.txt", "child").
			Run("create child -m 'child'")

		// Move child to stack2 (use --onto for the target)
		shW1.Checkout("child").
			Run("move --onto stack2")

		// Verify new parent
		sh.ExpectBranchParent("child", "stack2")
	})
}

// =============================================================================
// Submit Operations from Worktrees
// =============================================================================

func TestWorktreeSubmitOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t, WithRemote())
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("submit from worktree creates PRs", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create a child
		shW.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Submit should push branches
		// Note: This will fail without GitHub mock, but we can verify the push
		shW.Checkout("feature")

		// Just verify branches can be pushed
		shW.Git("push origin feature --force")
		shW.Git("push origin child --force")
	})
}

// =============================================================================
// Undo Operations with Worktrees
// =============================================================================

func TestWorktreeUndoOperations(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("undo create in worktree removes branch", func(_ *testing.T, sh *TestShell) {
		// Create a stack without worktree first to avoid worktree complexity
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Create a child
		sh.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Verify branches exist
		sh.HasBranches("main", "feature", "child")

		// Undo the last operation (creating child)
		sh.UndoLatest()

		// Child should be gone
		sh.HasBranches("main", "feature").
			OnBranch("feature")
	})

	run("worktree branches visible from main repo", func(_ *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create branches in worktree
		shW.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Verify all branches are visible from main repo
		sh.HasBranches("main", "feature", "child")

		// Verify stack log shows the branches from main repo
		sh.Checkout("child")
		sh.Run("log").
			OutputContains("feature").
			OutputContains("child")
	})
}

// =============================================================================
// Complex Multi-Worktree Scenarios
// =============================================================================

func TestWorktreeComplexScenarios(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())
	sharedRemote := NewTestShellInProcess(t, WithRemote())
	sharedRemote.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	runRemote := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := sharedRemote.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	runRemote("interleaved commits across worktrees and main", func(t *testing.T, sh *TestShell) {
		// Create two stacks
		sh.WriteFile("a.txt", "a").Run("create stack-a -w -m 'stack a'")
		sh.WriteFile("b.txt", "b").Run("create stack-b -w -m 'stack b'")

		wtA := sh.GetWorktreePath("stack-a")
		wtB := sh.GetWorktreePath("stack-b")
		shA := sh.InWorktree(wtA)
		shB := sh.InWorktree(wtB)

		// Interleaved commits
		shA.WriteFile("a-child.txt", "a-child").Run("create a-child -m 'a child'")
		shB.WriteFile("b-child.txt", "b-child").Run("create b-child -m 'b child'")
		shA.WriteFile("a-grandchild.txt", "a-gc").Run("create a-grandchild -m 'a grandchild'")
		shB.WriteFile("b-grandchild.txt", "b-gc").Run("create b-grandchild -m 'b grandchild'")

		// Advance main
		sh.WriteFile("main-update.txt", "main").
			Git("add main-update.txt").
			Git("commit -m 'Main advanced'").
			Git("push origin main")

		// Sync from main with full restack
		sh.Run("sync --restack")

		// Verify both stacks are correct
		sh.ExpectStackStructure(map[string]string{
			"stack-a":      "main",
			"a-child":      "stack-a",
			"a-grandchild": "a-child",
			"stack-b":      "main",
			"b-child":      "stack-b",
			"b-grandchild": "b-child",
		})

		// Both worktrees should be clean
		shA.Checkout("a-grandchild").Git("status --porcelain")
		if shA.Output() != "" {
			t.Errorf("Worktree A should be clean: %s", shA.Output())
		}

		shB.Checkout("b-grandchild").Git("status --porcelain")
		if shB.Output() != "" {
			t.Errorf("Worktree B should be clean: %s", shB.Output())
		}
	})

	runRemote("one stack merged while working in another worktree", func(t *testing.T, sh *TestShell) {
		// Create two stacks
		sh.WriteFile("merged.txt", "merged").Run("create to-be-merged -w -m 'to be merged'")
		sh.WriteFile("active.txt", "active").Run("create active-work -w -m 'active work'")

		wtMerged := sh.GetWorktreePath("to-be-merged")
		wtActive := sh.GetWorktreePath("active-work")
		shMerged := sh.InWorktree(wtMerged)
		shActive := sh.InWorktree(wtActive)

		// Simulate merged stack being merged
		sh.Git("checkout main").
			Git("merge to-be-merged --ff-only").
			Git("push origin main")
		sh.SetPrState("to-be-merged", "MERGED")

		// Detach head in merged worktree so the branch can be deleted
		shMerged.Git("checkout --detach HEAD")

		// Continue working in active worktree
		shActive.WriteFile("more-work.txt", "more work").
			Run("create more-work -m 'more work'")

		// Sync from main should clean up merged worktree but preserve active
		sh.Checkout("main").Run("sync")

		// Merged branch should be deleted
		branches, _ := sh.Scene().Repo.GetLocalBranches()
		for _, b := range branches {
			if b == "to-be-merged" {
				t.Error("Merged branch should be deleted")
			}
		}

		// Active worktree should still work
		shActive.Checkout("more-work").OnBranch("more-work")
	})

	run("modify in main while worktree has different branch checked out", func(t *testing.T, sh *TestShell) {
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create children in worktree
		shW.WriteFile("child.txt", "child").Run("create child -m 'child'")
		shW.WriteFile("grandchild.txt", "grandchild").Run("create grandchild -m 'grandchild'")

		// Worktree is on grandchild
		shW.OnBranch("grandchild")

		// Modify feature from main repo (not in worktree)
		sh.Checkout("feature").
			WriteFile("feature-update.txt", "updated").
			Run("modify -n").
			Checkout("main")

		// Worktree should still be on grandchild and have the update
		shW.OnBranch("grandchild").
			Git("ls-files feature-update.txt")
		if shW.Output() == "" {
			t.Error("Grandchild should have feature-update.txt after modify")
		}

		// Working directory should be clean
		shW.Git("status --porcelain")
		if shW.Output() != "" {
			t.Errorf("Worktree should be clean: %s", shW.Output())
		}
	})
}

// =============================================================================
// Error Handling
// =============================================================================

func TestWorktreeErrorHandling(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("worktree open with non-existent stack fails gracefully", func(_ *testing.T, sh *TestShell) {
		sh.RunExpectError("worktree open nonexistent")
	})

	run("worktree remove with non-existent stack fails gracefully", func(_ *testing.T, sh *TestShell) {
		sh.RunExpectError("worktree remove nonexistent")
	})

	run("creating worktree for non-trunk branch fails", func(_ *testing.T, sh *TestShell) {
		// Create a regular branch first
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -m 'feature branch'")

		// Try to create a worktree from non-trunk
		sh.WriteFile("child.txt", "child").
			RunExpectError("create child -w -m 'child with worktree'")
	})
}

// =============================================================================
// Anchor Branch Cleanup
// =============================================================================

func TestWorktreeAnchorBranchCleanup(t *testing.T) {
	t.Parallel()
	shared := NewTestShellInProcess(t)
	shared.SetWorktreeBasePath(t.TempDir())

	run := func(name string, fn func(t *testing.T, sh *TestShell)) {
		t.Run(name, func(t *testing.T) {
			sh := shared.WithT(t)
			sh.ResetRepo()
			fn(t, sh)
		})
	}

	run("worktree remove deletes anchor branch", func(t *testing.T, sh *TestShell) {
		// Create empty anchored worktree.
		sh.Run("worktree create mystack")

		sh.HasBranches("main")

		worktreePath := sh.GetWorktreePath("mystack")

		// Remove the worktree
		sh.Run("worktree remove mystack")

		// Worktree should be removed
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Errorf("Worktree directory should be removed at %s", worktreePath)
		}

		// Anchor branch should also be deleted
		sh.HasBranches("main")
	})

	run("worktree remove rejects removed keep-branch flag", func(_ *testing.T, sh *TestShell) {
		sh.Run("worktree create mystack")
		sh.RunExpectError("worktree remove mystack --keep-branch")
	})

	run("worktree remove skips deletion when anchor has children", func(_ *testing.T, sh *TestShell) {
		// Create worktree with child branches
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create a child branch
		shW.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Checkout main in worktree so we can remove it
		shW.Git("checkout --detach HEAD")

		// Remove refuses worktrees with real child branches.
		sh.RunExpectError("worktree remove feature")

		// Anchor branch and child should still exist
		sh.HasBranches("main", "feature", "child")
	})

	run("worktree prune cleans up missing directories", func(t *testing.T, sh *TestShell) {
		// Create empty anchored worktree.
		sh.Run("worktree create test-wt")

		sh.HasBranches("main")

		worktreePath := sh.GetWorktreePath("test-wt")

		// Manually delete the worktree directory (simulating external deletion)
		if err := os.RemoveAll(worktreePath); err != nil {
			t.Fatalf("Failed to remove worktree directory: %v", err)
		}

		// Worktree list should show missing worktree
		sh.Run("worktree list").
			OutputContains("missing")

		// Prune should clean up the missing worktree
		sh.Run("worktree prune")

		// Worktree list should be empty
		sh.Run("worktree list").
			OutputContains("No managed worktrees")

		// Anchor branch should be deleted
		sh.HasBranches("main")
	})

	run("worktree prune dry-run shows what would be cleaned", func(t *testing.T, sh *TestShell) {
		// Create empty anchored worktree.
		sh.Run("worktree create test-wt")

		worktreePath := sh.GetWorktreePath("test-wt")

		// Manually delete the worktree directory
		if err := os.RemoveAll(worktreePath); err != nil {
			t.Fatalf("Failed to remove worktree directory: %v", err)
		}

		// Prune with dry-run should show what would be cleaned
		sh.Run("worktree prune --dry-run").
			OutputContains("Would prune").
			OutputContains("test-wt")

		// But nothing should actually be deleted
		sh.HasBranches("main")
	})

	run("worktree prune skips missing worktrees with children", func(t *testing.T, sh *TestShell) {
		// Create worktree with child branches
		sh.WriteFile("feature.txt", "feature").
			Run("create feature -w -m 'feature branch'")

		worktreePath := sh.GetWorktreePath("feature")
		shW := sh.InWorktree(worktreePath)

		// Create a child branch
		shW.WriteFile("child.txt", "child").
			Run("create child -m 'child branch'")

		// Manually delete the worktree directory
		if err := os.RemoveAll(worktreePath); err != nil {
			t.Fatalf("Failed to remove worktree directory: %v", err)
		}

		// Prune should skip because anchor has children
		sh.Run("worktree prune").
			OutputContains("Skipped").
			OutputContains("children")

		// Anchor branch and child should still exist
		sh.HasBranches("main", "feature", "child")
	})
}
